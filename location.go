package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/martinlindhe/unit"
	log "github.com/sirupsen/logrus"
	"net/http"
	"time"
)

/*
This should be some sort of thing that's sent from the phone
*/
type Location struct {
	Latitude             float64 `json:"lat" binding:"required"`
	Longitude            float64 `json:"long" binding:"required"`
	Timestamp            time.Time
	DeviceTimestamp      time.Time
	DeviceTimestampAsInt int64   `json:"time" binding:"required"`
	Accuracy             float32 `json:"acc" binding:"required"`
	Distance             float64
	DeviceID             string `json:"deviceid" binding:"required"`
	Altitude             float32
	VerticalAccuracy     float32
	Speed                float32
	Geocoding            string
}

func (env *Env) GetLastLocation(user string) (*Location, error) {
	if env.db == nil {
		return nil, errors.New("No database connection available")
	}
	var location Location
	defer timeTrack(time.Now())
	query := "select " +
		"geocoding, " +
		"ST_Y(ST_AsText(point)), " +
		"ST_X(ST_AsText(point)) " +
		"from locations " +
		"where geocoding is not null " +
		"and \"user\"=$1 " +
		"order by devicetimestamp desc " +
		"limit 1"
	err := env.db.QueryRow(query, user).Scan(&location.Geocoding, &location.Latitude, &location.Longitude)
	return &location, err
}

func (env *Env) GetTotalDistanceInMiles() (float64, error) {
	if env.db == nil {
		return 0, errors.New("No database connection available")
	}
	var distance float64
	defer timeTrack(time.Now())
	err := env.db.QueryRow("select distance from locations_distance_this_year").Scan(&distance)
	if err != nil {
		return 0, err
	}
	distanceInMeters := unit.Length(distance) * unit.Meter
	return distanceInMeters.Miles(), nil
}

func (env *Env) GetLocationsBetweenDates(from time.Time, to time.Time, user string, device string) (*[]Location, error) {
	if env.db == nil {
		return nil, errors.New("No database connection available")
	}
	defer timeTrack(time.Now())
	query := "select " +
		"coalesce(geocoding -> 'results' -> 0 ->> 'formatted_address', ''), " +
		"ST_Y(ST_AsText(point)), " +
		"ST_X(ST_AsText(point)), " +
		"devicetimestamp, " +
		"coalesce(speed, coalesce(3.6*ST_Distance(point,lag(point,1,point) over (order by devicetimestamp asc))/extract('epoch' from (devicetimestamp-lag(devicetimestamp) over (order by devicetimestamp asc))),0)) as speed, " +
		"coalesce(altitude, 0), " +
		"accuracy, " +
		"coalesce(verticalaccuracy, 0) " +
		"from locations where " +
		"devicetimestamp>=$1 and devicetimestamp<$2 " +
		"and \"user\"=$3 and device=$4" +
		"order by devicetimestamp desc"
	rows, err := env.db.Query(query, from, to, user, device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var locations []Location
	for rows.Next() {
		var location Location
		err := rows.Scan(
			&location.Geocoding,
			&location.Latitude,
			&location.Longitude,
			&location.DeviceTimestamp,
			&location.Speed,
			&location.Altitude,
			&location.Accuracy,
			&location.VerticalAccuracy,
		)
		if err != nil {
			return nil, err
		}
		locations = append(locations, location)
	}
	return &locations, nil

}

/* HTTP handlers */
func (env *Env) LocationHandler(c *gin.Context) {
	location, err := env.GetLastLocation("growse")
	distance, err := env.GetTotalDistanceInMiles()
	if err != nil {
		c.String(500, err.Error())
		return
	}
	c.Header("Last-modified", location.Timestamp.Format("Mon, 02 Jal 2006 15:04:05 GMT"))
	c.JSON(200, gin.H{
		"name":          location.Name(),
		"latitude":      fmt.Sprintf("%.2f", location.Latitude),
		"longitude":     fmt.Sprintf("%.2f", location.Longitude),
		"totalDistance": humanize.FormatFloat("#,###.##", distance),
	})
}

func (env *Env) LocationHeadHandler(c *gin.Context) {
	location, err := env.GetLastLocation("growse")
	if err != nil {
		c.String(500, err.Error())
		return
	}
	c.Header("Last-modified", location.Timestamp.Format("Mon, 02 Jal 2006 15:04:05 GMT"))
	c.Status(200)
}

func (env *Env) OTListUserHandler(c *gin.Context) {
	var rows *sql.Rows
	var err error
	if c.Query("user") != "" {
		rows, err = env.db.Query("select distinct \"device\" from locations where \"user\"=$1 order by \"device\";", c.Query("user"))
	} else {
		rows, err = env.db.Query("select distinct \"user\" from locations order by \"user\";")
	}
	if err != nil {
		c.Error(err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var user string
		err := rows.Scan(&user)
		if err != nil {
			log.WithError(err).Error("Error pulling user from db")
		}
		results = append(results, user)
	}
	c.JSON(200, gin.H{
		"results": results,
	})
}

type OTPos struct {
	Tst      int64   `json:"tst" binding:"required"`
	Acc      float32 `json:"acc" binding:"required"`
	Type     string  `json:"_type" binding:"required"`
	Alt      float32 `json:"alt" binding:"required"`
	Lon      float64 `json:"lon" binding:"required"`
	Vac      float32 `json:"vac" binding:"required"`
	Vel      float32 `json:"vel" binding:"required"`
	Lat      float64 `json:"lat" binding:"required"`
	Addr     string  `json:"addr" binding:"required"`
	Username string  `json:"username" binding:"optional"`
	Device   string  `json:"device" binding:"optional"`
}

func (location Location) toOT(user string, device string) OTPos {
	return OTPos{
		Tst:      location.DeviceTimestamp.Unix(),
		Acc:      location.Accuracy,
		Type:     "location",
		Alt:      location.Altitude,
		Lat:      location.Latitude,
		Lon:      location.Longitude,
		Vel:      location.Speed,
		Vac:      location.VerticalAccuracy,
		Addr:     location.Geocoding,
		Username: user,
		Device:   device,
	}
}

func (env *Env) OTLastPosHandler(c *gin.Context) {
	location, err := env.GetLastLocation(c.Query("user"))
	if err != nil {
		c.String(500, err.Error())
		return
	}
	if location == nil {
		c.String(500, "No location found")
		return
	}
	last := location.toOT(c.Query("user"), c.Query("device"))
	c.JSON(200, []OTPos{
		last,
	})
}

func (env *Env) OTLocationsHandler(c *gin.Context) {
	const iso8061fmt = "2006-01-02T15:04:05"
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

	locations, err := env.GetLocationsBetweenDates(fromTime, toTime, user, device)
	if err != nil {
		c.String(500, err.Error())
		return
	}
	if locations == nil {
		c.String(500, "No locations found")
		return
	}
	var otpos []OTPos
	for _, location := range *locations {
		otpos = append(otpos, location.toOT(user, device))
	}
	response := struct {
		Data []OTPos `json:"data"`
	}{otpos}
	responseBytes, _ := json.Marshal(response)
	responseReader := bytes.NewReader(responseBytes)
	c.DataFromReader(200, int64(len(responseBytes)), "application/json", responseReader, nil)
}

func OTVersionHandler(c *gin.Context) {
	c.JSON(200, gin.H{"version": "1.0-owntracks-pg-recorder"})
}

var wsupgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (env *Env) wshandler(w http.ResponseWriter, r *http.Request) {
	// At the moment, this is just an echo impl. At some point publish new updates down this.
	wsupgrader.CheckOrigin = func(r *http.Request) bool { return true }
	conn, err := wsupgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Printf("Failed to set websocket upgrade: %+v\n", err)
		return
	}

	for {
		t, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		switch string(msg) {
		case "LAST":
			location, err := env.GetLastLocation("growse")
			if err != nil {
				log.WithError(err).Error("Error fetching last location")
				break
			}
			locationAsBytes, err := json.Marshal(location.toOT("growse", "nexus5"))
			if err != nil {
				log.WithError(err).Error("Error formatting location for websocket")
				break
			}
			conn.WriteMessage(t, locationAsBytes)
			break
		default:
			break
		}
	}
}

func (env *Env) PlaceHandler(c *gin.Context) {
	if env.db == nil {
		c.String(500, "No database connection available")
		c.Abort()
		return
	}
	place := c.PostForm("place")
	geocoding, err := env.GetGeocoding(place)
	if err != nil {
		InternalError(err)
		c.String(500, err.Error())
		c.Abort()
		return
	}

	if len(geocoding.Features) == 0 {
		c.HTML(200, "placeResults", gin.H{"results": nil, "place": place})
		c.Abort()
		return
	}
	feature := geocoding.Features[0]
	var rows *sql.Rows
	if feature.Properties["bounds"] != nil {
		bounds := feature.Properties["bounds"].(map[string]interface{})
		rows, err = env.db.Query(`
select 
count(*) as c,
date(devicetimestamp) 
from locations 
where point && ST_SetSRID(ST_MakeBox2D(ST_Point($1,$2),	ST_Point($3,$4)),4326)
group by date(devicetimestamp) order by c desc limit 20
`,
			bounds["northeast"].(map[string]interface{})["lng"],
			bounds["northeast"].(map[string]interface{})["lat"],
			bounds["southwest"].(map[string]interface{})["lng"],
			bounds["southwest"].(map[string]interface{})["lat"])
	} else if feature.Geometry.IsPoint() && feature.Properties["confidence"] != nil && feature.Properties["confidence"].(float64) >= 1 && feature.Properties["confidence"].(float64) <= 10 {
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
		rows, err = env.db.Query(`
select 
count(*) as c,
date(devicetimestamp) 
from locations 
where ST_DWithin(point,ST_SetSRID(ST_Point( $1, $2),4326),$3)
group by date(devicetimestamp) order by c desc limit 20
`, feature.Geometry.Point[0], feature.Geometry.Point[1], radius)
	} else {
		c.String(500, "No valid geometries found in geocoding response", geocoding)
		c.Abort()
		return
	}
	if err != nil {
		InternalError(err)
		c.String(500, err.Error())
		c.Abort()
		return
	}
	defer rows.Close()
	var results []LocationCountPerDay
	for rows.Next() {
		var result LocationCountPerDay
		err := rows.Scan(&result.LocationCount, &result.Date)
		if err != nil {
			InternalError(err)
			c.String(500, "Error fetching values from database: %v", err)
			c.Abort()
			return
		}
		results = append(results, result)
	}
	c.HTML(200, "placeResults", gin.H{"results": results, "place": place, "formatted": feature.Properties["formatted"]})
}

type (
	LocationCountPerDay struct {
		LocationCount int
		Date          time.Time
	}
)
