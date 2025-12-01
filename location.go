package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/martinlindhe/unit"
	geojson "github.com/paulmach/go.geojson"
)

/*
This should be some sort of thing that's sent from the phone
*/

const NumberOfInaccuratePoints = 20

//nolint:tagliatelle
type Location struct {
	Timestamp        int64   `binding:"required" json:"tst"`
	Accuracy         float32 `binding:"required" json:"acc"`
	Type             string  `binding:"required" json:"_type"`
	Latitude         float64 `binding:"required" json:"lat"`
	Longitude        float64 `binding:"required" json:"lon"`
	Altitude         float32 `binding:"required" json:"alt"`
	VerticalAccuracy float32 `binding:"required" json:"vac"`
	Course           float32 `binding:"optional" json:"cog"`
	Speed            float32 `binding:"required" json:"vel"`
	Geocoding        string  `binding:"optional" json:"addr"`
	Username         string  `binding:"optional" json:"username"`
	Device           string  `binding:"optional" json:"device"`
}

//nolint:funlen
func (env *Env) GetLastLocations(ctx context.Context) (*[]Location, error) {
	if env.database == nil {
		return nil, errors.New("no database connection available")
	}

	defer timeTrack(ctx, time.Now())

	query := `select distinct on ("user") "user",
                            device,
                            geocoding,
                            ST_Y(ST_AsText(point)),
                            ST_X(ST_AsText(point)),
                            devicetimestamp,
                            accuracy,
                            altitude,
                            verticalAccuracy,
                            speed
from locations
order by "user", devicetimestamp desc`

	rows, err := env.database.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	if rows.Err() != nil {
		return nil, err
	}

	var locations []Location

	for rows.Next() {
		location := Location{Type: "location"}

		var (
			geocodingMaybe sql.NullString
			timestamp      time.Time
		)

		err = rows.Scan(
			&location.Username,
			&location.Device,
			&geocodingMaybe,
			&location.Latitude,
			&location.Longitude,
			&timestamp,
			&location.Accuracy,
			&location.Altitude,
			&location.VerticalAccuracy,
			&location.Speed,
		)
		if geocodingMaybe.Valid {
			location.Geocoding = geocodingMaybe.String
		}

		location.Timestamp = timestamp.Unix()

		if err != nil {
			slog.With("err", err).
				ErrorContext(ctx, "Unable to pull latest location row out of database")
		} else {
			locations = append(locations, location)
		}
	}

	return &locations, nil
}

func (env *Env) GetLastLocationForUser(ctx context.Context, user string) (*Location, error) {
	if env.database == nil {
		return nil, errors.New("no database connection available")
	}

	defer timeTrack(ctx, time.Now())

	query := `select "user",
       device,
       geocoding,
       ST_Y(ST_AsText(point)),
       ST_X(ST_AsText(point)),
       devicetimestamp,
       accuracy,
       altitude,
       verticalAccuracy,
       speed
from locations
where "user" = $1
order by devicetimestamp desc limit 1`
	location := Location{Type: "location"}

	var (
		geocodingMaybe sql.NullString
		timestamp      time.Time
	)

	err := env.database.QueryRowContext(ctx, query, user).
		Scan(
			&location.Username, &location.Device, &geocodingMaybe, &location.Latitude,
			&location.Longitude, &timestamp, &location.Accuracy, &location.Altitude,
			&location.VerticalAccuracy, &location.Speed,
		)
	if geocodingMaybe.Valid {
		location.Geocoding = geocodingMaybe.String
	}

	location.Timestamp = timestamp.Unix()

	if err != nil {
		return nil, err
	}

	return &location, nil
}

func (env *Env) GetTotalDistanceInMiles(ctx context.Context) (float64, error) {
	if env.database == nil {
		return 0, errors.New("no database connection available")
	}

	var distance float64

	defer timeTrack(ctx, time.Now())

	err := env.database.QueryRowContext(ctx, "select distance from locations_distance_this_year").
		Scan(&distance)
	if err != nil {
		return 0, err
	}

	distanceInMeters := unit.Length(distance) * unit.Meter

	return distanceInMeters.Miles(), nil
}

//nolint:funlen
func (env *Env) GetLocationsBetweenDates(
	ctx context.Context,
	from time.Time,
	to time.Time,
	user string,
	device string,
) (*[]Location, error) {
	if env.database == nil {
		return nil, errors.New("no database connection available")
	}

	defer timeTrack(ctx, time.Now())

	query := `select coalesce(geocoding -> 'results' -> 0 ->> 'formatted_address', ''),
       ST_Y(ST_AsText(point)),
       ST_X(ST_AsText(point)),
       devicetimestamp,
       coalesce(speed, coalesce(3.6 * ST_Distance(point, lag(point, 1, point) over (order by devicetimestamp asc)) /
                                extract('epoch' from
                                        (devicetimestamp - lag(devicetimestamp) over (order by devicetimestamp asc))),
                                0))  as speed,
       coalesce(altitude, 0)         as altitude,
       accuracy,
       coalesce(verticalaccuracy, 0) as verticalaccuraccy
from locations
where devicetimestamp >= $1
  and devicetimestamp < $2
  and "user" = $3
  and device = $4
order by devicetimestamp desc`

	rows, err := env.database.QueryContext(ctx, query, from, to, user, device)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	if rows.Err() != nil {
		return nil, err
	}

	var (
		locations []Location
		timestamp time.Time
	)

	for rows.Next() {
		location := Location{Type: "location"}
		err := rows.Scan(
			&location.Geocoding,
			&location.Latitude,
			&location.Longitude,
			&timestamp,
			&location.Speed,
			&location.Altitude,
			&location.Accuracy,
			&location.VerticalAccuracy,
		)
		location.Timestamp = timestamp.Unix()

		if err != nil {
			return nil, err
		}

		location.Username = user
		location.Device = device
		locations = append(locations, location)
	}

	return &locations, nil
}

func (env *Env) LocationHandler(c *gin.Context) {
	ctx := c.Request.Context()
	slog.With("user", env.configuration.DefaultUser).
		DebugContext(c.Request.Context(), "Getting last location for default user")

	location, err := env.GetLastLocationForUser(ctx, env.configuration.DefaultUser)
	if err != nil {
		c.String(500, err.Error())

		return
	}

	distance, err := env.GetTotalDistanceInMiles(ctx)
	if err !=
		nil {
		c.String(500, err.Error())

		return
	}

	c.Header(
		"Last-modified",
		time.Unix(location.Timestamp, 0).Format("Mon, 02 Jal 2006 15:04:05 GMT"),
	)
	c.JSON(200, gin.H{
		"name":          location.GeocodedName(ctx),
		"latitude":      fmt.Sprintf("%.2f", location.Latitude),
		"longitude":     fmt.Sprintf("%.2f", location.Longitude),
		"totalDistance": humanize.FormatFloat("#,###.##", distance),
	})
}

func (env *Env) LocationHeadHandler(c *gin.Context) {
	ctx := c.Request.Context()

	location, err := env.GetLastLocationForUser(ctx, env.configuration.DefaultUser)
	if err != nil {
		c.String(500, err.Error())

		return
	}

	c.Header(
		"Last-modified",
		time.Unix(location.Timestamp, 0).Format("Mon, 02 Jal 2006 15:04:05 GMT"),
	)
	c.Status(200)
}

func (env *Env) OTListUserHandler(c *gin.Context) {
	var (
		rows *sql.Rows
		err  error
	)

	if c.Query("user") != "" {
		rows, err = env.database.Query(
			`select distinct "device" from locations where "user"=$1 order by "device";`,
			c.Query("user"),
		)
	} else {
		rows, err = env.database.Query(`select distinct "user" from locations order by "user";`)
	}

	if err != nil {
		_ = c.Error(err)

		return
	}

	if rows.Err() != nil {
		_ = c.Error(rows.Err())

		return
	}

	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var results []string

	for rows.Next() {
		var user string

		err := rows.Scan(&user)
		if err != nil {
			slog.With("err", err).
				ErrorContext(c.Request.Context(), "Error pulling user from database")
		}

		results = append(results, user)
	}

	c.JSON(200, gin.H{
		"results": results,
	})
}

//nolint:cyclop
func (env *Env) OTLastPosHandler(c *gin.Context) {
	ctx := c.Request.Context()
	user := c.Query("user")

	device := c.Query("device")
	if user != "" && device != "" {
		location, err := env.GetLastLocationForUser(ctx, user)
		if err != nil {
			c.String(500, err.Error())

			return
		}

		c.JSON(200, [1]*Location{location})
	} else {
		locations, err := env.GetLastLocations(ctx)
		if err != nil {
			c.String(500, err.Error())

			return
		}

		if locations == nil || len(*locations) == 0 {
			c.String(500, "No location found")

			return
		}

		var filteredLocations []Location

		for _, location := range *locations {
			if device == "" || location.Device == device {
				if user == "" || (location.Username == user) {
					filteredLocations = append(filteredLocations, location)
				}
			}
		}

		c.JSON(200, filteredLocations)
	}
}

const iso8061fmt = "2006-01-02T15:04:05"

func (env *Env) OTLocationsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	from := c.DefaultQuery("from", time.Now().AddDate(0, 0, -1).Format(iso8061fmt))
	to := c.DefaultQuery("to", time.Now().Format(iso8061fmt))

	fromTime, err := time.Parse(iso8061fmt, from)
	if err != nil {
		c.String(400, fmt.Sprintf("Invalid from time %v: %v", from, err))

		return
	}

	toTime, err := time.Parse(iso8061fmt, to)
	if err != nil {
		c.String(400, fmt.Sprintf("Invalid to time %v: %v", to, err))

		return
	}

	user := c.Query("user")
	device := c.Query("device")

	locations, err := env.GetLocationsBetweenDates(ctx, fromTime, toTime, user, device)
	if err != nil {
		c.String(500, err.Error())

		return
	}

	if locations == nil {
		c.String(500, "No locations found")

		return
	}

	response := struct {
		Data []Location `json:"data"`
	}{*locations}
	responseBytes, _ := json.Marshal(response) //nolint:errchkjson
	responseReader := bytes.NewReader(responseBytes)
	c.DataFromReader(200, int64(len(responseBytes)), "application/json", responseReader, nil)
}

func OTVersionHandler(c *gin.Context) {
	c.JSON(200, gin.H{"version": "1.0-owntracks-pg-recorder"})
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (env *Env) wshandler(w http.ResponseWriter, r *http.Request) {
	// At the moment, this is just an echo impl. At some point publish new updates down this.
	wsUpgrader.CheckOrigin = func(_ *http.Request) bool { return true }

	ctx := r.Context()

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.With("err", err).
			ErrorContext(ctx, "Failed to set websocket upgrade")

		return
	}

	for {
		t, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		switch string(msg) {
		case "LAST":
			locations, err := env.GetLastLocations(ctx)
			if err != nil {
				slog.With("err", err).
					ErrorContext(r.Context(), "Error fetching last locations")

				break
			}

			locationAsBytes, err := json.Marshal(*locations)
			if err != nil {
				slog.With("err", err).
					ErrorContext(r.Context(), "Error formatting location for websocket")

				break
			}

			err = conn.WriteMessage(t, locationAsBytes)
			if err != nil {
				slog.With("err", err).
					WarnContext(r.Context(), "error writing message to ws")
			}
		}
	}
}

//nolint:cyclop,funlen
func (env *Env) PlaceHandler(c *gin.Context) {
	ctx := c.Request.Context()
	if env.database == nil {
		c.String(500, "No database connection available")
		c.Abort()

		return
	}

	place := c.PostForm("place")

	geocoding, err := env.GetGeocoding(ctx, place)
	if err != nil {
		InternalError(ctx, err)
		c.String(500, err.Error())
		c.Abort()

		return
	}

	if len(geocoding.Features) == 0 {
		c.HTML(200, "placeResults.gohtml", gin.H{"results": nil, "place": place})
		c.Abort()

		return
	}

	feature := geocoding.Features[0]

	var rows *sql.Rows

	//nolint:gocritic
	if feature.Properties["bounds"] != nil {
		bounds := feature.Properties["bounds"].(map[string]any)
		rows, err = env.database.Query(`select count(*) as c, date (devicetimestamp)
from locations
where point && ST_SetSRID(ST_MakeBox2D(ST_Point($1
    , $2)
    , ST_Point($3
    , $4))
    , 4326)
group by date (devicetimestamp)
order by c desc limit 20
`, bounds["northeast"].(map[string]any)["lng"],
			bounds["northeast"].(map[string]any)["lat"],
			bounds["southwest"].(map[string]any)["lng"],
			bounds["southwest"].(map[string]any)["lat"],
		)
	} else if feature.Geometry.IsPoint() &&
		feature.Geometry.Point != nil &&
		feature.Properties["confidence"] != nil &&
		feature.Properties["confidence"].(float64) >= 1 &&
		feature.Properties["confidence"].(float64) <= 10 {
		var radius int

		switch confidence := feature.Properties["confidence"].(float64); confidence {
		case 10:
			radius = 250
		case 9:
			radius = 500
		case 8:
			radius = 1000
		case 7:
			radius = 5000
		case 6:
			radius = 7500
		case 5:
			radius = 10000
		case 4:
			radius = 15000
		case 3:
			radius = 20000
		case 2:
			fallthrough
		case 1:
			fallthrough
		default:
			radius = 25000
		}

		rows, err = env.database.Query(`select count(*) as c, date (devicetimestamp)
from locations
where ST_DWithin(point
    , ST_SetSRID(ST_Point( $1
    , $2)
    , 4326)
    , $3)
group by date (devicetimestamp)
order by c desc limit 20
`, feature.Geometry.Point[0], feature.Geometry.Point[1], radius)
	} else {
		c.String(500, "No valid geometries found in geocoding response", geocoding)
		c.Abort()

		return
	}

	if err != nil {
		InternalError(ctx, err)
		c.String(500, err.Error())
		c.Abort()

		return
	}

	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	if rows.Err() != nil {
		InternalError(ctx, rows.Err())
		c.String(500, rows.Err().Error())
		c.Abort()

		return
	}

	var results []LocationCountPerDay

	for rows.Next() {
		var result LocationCountPerDay

		err := rows.Scan(&result.LocationCount, &result.Date)
		if err != nil {
			InternalError(ctx, err)
			c.String(500, "Error fetching values from database: %v", err)
			c.Abort()

			return
		}

		results = append(results, result)
	}

	c.HTML(
		200,
		"placeResults.gohtml",
		gin.H{"results": results, "place": place, "formatted": feature.Properties["formatted"]},
	)
}

type LocationWithMetadata struct {
	ID        int64
	Timestamp time.Time
	Accuracy  float64
	LatLng    string
	Geocoding string
	Distance  float64
	Speed     float64
}

func (env *Env) DeleteLocationPoint(c *gin.Context) {
	id := c.Param("id")
	slog.With("id", id).
		InfoContext(c.Request.Context(), "Deleting point")

	query := `DELETE
FROM locations
where id = $1
	`

	_, err := env.database.Exec(query, id)
	if err != nil {
		slog.With("err", err).
			With("id", id).
			ErrorContext(c.Request.Context(), "Error deleting point from database")
		c.String(http.StatusInternalServerError, "Error deleting point from database %v", err)
		c.Abort()
	} else {
		c.Status(http.StatusOK)
	}
}

func (env *Env) GetPointsForDate(c *gin.Context) {
	ctx := c.Request.Context()
	date := c.Param("date")
	//nolint:lll
	query := `SELECT id,
       devicetimestamp,
       accuracy,
       concat(st_y(st_astext(point)), ',', st_x(st_astext(point)))                                     as latlng,
       coalesce(geocoding - > 'results' - > 0 ->> 'formatted_address', '')                             as address,
       st_distance(locations.point,
                   lag(locations.point, 1, locations.point) OVER (ORDER BY locations.devicetimestamp)) AS distance,
       coalesce(3.6 * ST_Distance(point, lag(point, 1, point) OVER (ORDER BY devicetimestamp ASC)) /
                (extract('epoch' FROM (devicetimestamp - lag(devicetimestamp) OVER (ORDER BY devicetimestamp ASC))) + 1),
                0)                                                                                     AS speed
FROM locations
where devicetimestamp::date = $1
ORDER BY devicetimestamp `

	rows, err := env.database.QueryContext(ctx, query, date)
	if err != nil {
		slog.With("err", err).
			ErrorContext(ctx, "Error querying points from database")
		_ = c.Error(err)

		return
	}

	defer func() { _ = rows.Close() }()

	if rows.Err() != nil {
		slog.With("err", rows.Err()).
			ErrorContext(ctx, "Error querying points from database")

		_ = c.Error(rows.Err())

		return
	}

	var locations []LocationWithMetadata

	for rows.Next() {
		location := LocationWithMetadata{}

		err := rows.Scan(
			&location.ID,
			&location.Timestamp,
			&location.Accuracy,
			&location.LatLng,
			&location.Geocoding,
			&location.Distance,
			&location.Speed,
		)
		if err != nil {
			_ = c.Error(err)

			return
		}

		locations = append(locations, location)
	}

	c.HTML(200, "points.gohtml", gin.H{"date": date, "results": locations})
}

func (env *Env) GetInaccurateLocationPoints(c *gin.Context) {
	query := fmt.Sprintf(`SELECT
    id,
    devicetimestamp,
    accuracy,
    concat(st_y(st_astext(point)), ',', st_x(st_astext(point))) as latlng,
    coalesce(geocoding - > 'results' - > 0 ->> 'formatted_address', '') as address,
    st_distance(locations.point,
                lag(locations.point, 1, locations.point) OVER (ORDER BY locations.devicetimestamp)) AS distance,
    coalesce(3.6 * ST_Distance(point, lag(point, 1, point) OVER (ORDER BY devicetimestamp ASC)) /
             (extract('epoch' FROM (devicetimestamp - lag(devicetimestamp) OVER (ORDER BY devicetimestamp ASC))) + 1),
             0) AS speed
FROM locations
ORDER BY speed DESC LIMIT %d
`, NumberOfInaccuratePoints)

	rows, err := env.database.Query(query)
	if err != nil {
		_ = c.Error(err)

		return
	}

	defer func() { _ = rows.Close() }()

	if rows.Err() != nil {
		_ = c.Error(rows.Err())

		return
	}

	var locations []LocationWithMetadata

	for rows.Next() {
		location := LocationWithMetadata{}

		err := rows.Scan(
			&location.ID,
			&location.Timestamp,
			&location.Accuracy,
			&location.LatLng,
			&location.Geocoding,
			&location.Distance,
			&location.Speed,
		)
		if err != nil {
			_ = c.Error(err)

			return
		}

		locations = append(locations, location)
	}

	if locations == nil {
		c.String(404, "No locations found")

		return
	}

	c.HTML(200, "inaccurateLocations.gohtml", gin.H{"results": locations})
}

type (
	LocationCountPerDay struct {
		LocationCount int
		Date          time.Time
	}
)

//nolint:tagliatelle
type DeviceRecord struct {
	DeviceTimestamp  *time.Time `binding:"required" json:"device_timestamp"`
	Timestamp        *time.Time `binding:"required" json:"timestamp"`
	Accuracy         float32    `binding:"required" json:"accuracy"`
	Geocoding        *string    `binding:"optional" json:"geocoding"`
	BatteryLevel     *int       `binding:"required" json:"battery_level"`
	ConnectionType   *string    `binding:"required" json:"connection_type"`
	Doze             *bool      `binding:"required" json:"doze"`
	Latitude         float64    `binding:"required" json:"latitude"`
	Longitude        float64    `binding:"required" json:"longitude"`
	Speed            *float32   `binding:"required" json:"speed"`
	Altitude         *float32   `binding:"required" json:"altitude"`
	VerticalAccuracy *float32   `binding:"required" json:"vertical_accuracy"`
	User             string     `binding:"required" json:"user"`
	Device           string     `binding:"required" json:"device"`
}

func (env *Env) getPoints(from *time.Time, to *time.Time) (*sql.Rows, error) {
	query := `SELECT
    devicetimestamp, timestamp, accuracy, geocoding, batterylevel, connectiontype, doze, st_y(
    st_astext(
    point)) AS latitude, st_x(
    st_astext(
    point)) AS longitude, speed, altitude, verticalaccuracy, "user", device

FROM locations
WHERE deviceTimestamp>=$1 AND deviceTimestamp<=$2
ORDER by devicetimestamp ASC`

	return env.database.Query(query, from, to)
}

// renderDeviceRecordAsGeoJSON converts a DeviceRecord to a GeoJSON Feature.
//

func renderDeviceRecordAsGeoJSON(deviceRecord DeviceRecord) *geojson.Feature {
	var geometry *geojson.Geometry
	if deviceRecord.Altitude == nil {
		geometry = geojson.NewPointGeometry(
			[]float64{deviceRecord.Longitude, deviceRecord.Latitude},
		)
	} else {
		geometry = geojson.NewPointGeometry([]float64{
			deviceRecord.Longitude, deviceRecord.Latitude, float64(*deviceRecord.Altitude),
		})
	}

	feature := geojson.NewFeature(geometry)
	feature.SetProperty("timestamp", deviceRecord.DeviceTimestamp.Unix())
	feature.SetProperty("accuracy", deviceRecord.Accuracy)
	feature.SetProperty("battery", deviceRecord.BatteryLevel)
	feature.SetProperty("connection", deviceRecord.ConnectionType)
	feature.SetProperty("velocity", deviceRecord.Speed)
	feature.SetProperty("vertical_accuracy", deviceRecord.VerticalAccuracy)

	return feature
}

func writeExportHTTPHeader(c *gin.Context, filename string) *gzip.Writer {
	writer := c.Writer
	header := writer.Header()
	header.Set("Content-Type", "application/json")
	header.Set("Content-Disposition", "attachment; filename="+filename)
	header.Set("Transfer-Encoding", "chunked")
	header.Set("Content-Encoding", "gzip")
	writer.WriteHeader(http.StatusOK)
	writer.(http.Flusher).Flush()
	gzipWriter := gzip.NewWriter(writer)

	return gzipWriter
}

const featureCollectionHeader = `{"type":"FeatureCollection", "features":[`
const featureCollectionFooter = `]}`

// ExportGeoJSON exports location data as a GeoJSON file.
//
//nolint:funlen
func (env *Env) ExportGeoJSON(c *gin.Context) {
	fromParam := c.Param("from")
	toParam := c.Param("to")

	from, err := time.Parse(time.RFC3339, fromParam)
	if err != nil {
		_ = c.Error(err)

		return
	}

	to, err := time.Parse(time.RFC3339, toParam)
	if err != nil {
		_ = c.Error(err)

		return
	}

	rows, err := env.getPoints(&from, &to)
	if err != nil {
		_ = c.Error(err)

		return
	}

	defer func() { _ = rows.Close() }()

	if rows.Err() != nil {
		_ = c.Error(rows.Err())

		return
	}

	writer := c.Writer
	gzipWriter := writeExportHTTPHeader(c, "owntracks-geojson.json")

	defer func() {
		_ = gzipWriter.Close()

		writer.Flush()
	}()

	counter := 0
	_, _ = gzipWriter.Write([]byte(featureCollectionHeader))

	for rows.Next() {
		var deviceRecord DeviceRecord

		err := rows.Scan(
			&deviceRecord.DeviceTimestamp,
			&deviceRecord.Timestamp,
			&deviceRecord.Accuracy,
			&deviceRecord.Geocoding,
			&deviceRecord.BatteryLevel,
			&deviceRecord.ConnectionType,
			&deviceRecord.Doze,
			&deviceRecord.Latitude,
			&deviceRecord.Longitude,
			&deviceRecord.Speed,
			&deviceRecord.Altitude,
			&deviceRecord.VerticalAccuracy,
			&deviceRecord.User,
			&deviceRecord.Device,
		)
		if err != nil {
			slog.With("err", err).
				ErrorContext(c.Request.Context(), "Error scanning row")
		} else {
			feature := renderDeviceRecordAsGeoJSON(deviceRecord)

			featureBytes, err := feature.MarshalJSON()
			if err != nil {
				slog.With("err", err).
					ErrorContext(c.Request.Context(), "Error marshalling feature to JSON")
			} else {
				if counter > 0 {
					_, _ = gzipWriter.Write([]byte(","))
				}

				_, _ = gzipWriter.Write(featureBytes)
			}
		}

		if counter%100 == 0 {
			_ = gzipWriter.Flush()
			writer.Flush()
		}

		counter++
	}

	_, _ = gzipWriter.Write([]byte(featureCollectionFooter))
}
