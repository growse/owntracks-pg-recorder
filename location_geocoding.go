package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/patrickmn/go-cache"
	geojson "github.com/paulmach/go.geojson"
)

type OpencageReverseGeocodeResult struct {
	Bounds struct {
		Northeast struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"northeast"`
		Southwest struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"southwest"`
	} `binding:"required" json:"bounds"`
	Components struct {
		ISO31661Alpha2 string `json:"ISO_3166-1_alpha-2"`
		ISO31661Alpha3 string `json:"ISO_3166-1_alpha-3"`
		Category       string `json:"_category"`
		Type           string `json:"_type"`
		City           string `json:"city"`
		CityDistrict   string `json:"city_district"`
		Continent      string `json:"continent"`
		Country        string `json:"country"`
		CountryCode    string `json:"country_code"`
		HouseNumber    string `json:"house_number"`
		Neighbourhood  string `json:"neighbourhood"`
		PoliticalUnion string `json:"political_union"`
		Postcode       string `json:"postcode"`
		Road           string `json:"road"`
		State          string `json:"state"`
		StateCode      string `json:"state_code"`
		Suburb         string `json:"suburb"`
	} `binding:"required" json:"components"`
	Confidence int    `                   json:"confidence"`
	Formatted  string `                   json:"formatted"`
	Geometry   struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `                   json:"geometry"`
}
type OpencageReverseGeocodeResponse struct {
	PlusCode struct {
		CompoundCode string `json:"compound_code"`
		GlobalCode   string `json:"global_code"`
	} `json:"plus_code"`
	Results []OpencageReverseGeocodeResult `json:"results"   binding:"required"`
}

// GeocodedName extracts a sane name from the geocoded name component.
func (location *Location) GeocodedName() string {
	ctx := context.Background()
	unknownLocation := "Unknown"

	var geoLocation []OpencageReverseGeocodeResult

	err := json.Unmarshal([]byte(location.Geocoding), &geoLocation)
	if err != nil {
		slog.With("err", err).
			With("geocoding", location.Geocoding).
			ErrorContext(ctx, "Error decoding location object")

		return unknownLocation
	}

	if len(geoLocation) == 0 {
		return unknownLocation
	}

	if geoLocation[0].Components.City != "" {
		return geoLocation[0].Components.City
	}

	return unknownLocation
}

func (env *Env) GetGeocoding(place string) (*geojson.FeatureCollection, error) {
	if env.configuration.GeocodeApiURL == "" {
		err := errors.New("geocoding API should not be blank")
		InternalError(err)

		return nil, err
	}

	if place == "" {
		err := errors.New("place should not be blank")
		InternalError(err)

		return nil, err
	}

	geocodingUrl := fmt.Sprintf(env.configuration.GeocodeApiURL, url.QueryEscape(place))

	geocodingResponse, err := fetchGeocodingResponse(geocodingUrl)
	if err != nil {
		return nil, err
	}

	featureCollection, err := geojson.UnmarshalFeatureCollection([]byte(geocodingResponse))
	if err != nil {
		return nil, err
	}

	return featureCollection, nil
}

func RoundCoordinate(input float64) float64 {
	ctx := context.Background()

	rounded, err := strconv.ParseFloat(fmt.Sprintf("%.5f", input), 64)
	if err != nil {
		slog.With("input", input).ErrorContext(ctx, "Unable to truncate float to precision")
		panic(err)
	}

	return rounded
}

var reverseGeocodeCache = cache.New(cache.NoExpiration, 0)

func (location *Location) GetReverseGeocoding(env *Env) (string, error) {
	if env.configuration.ReverseGeocodeApiURL == "" {
		err := errors.New("reverse Geocoding API should not be blank")
		InternalError(err)

		return "", err
	}

	cacheKey := fmt.Sprintf(
		"%v%v",
		RoundCoordinate(location.Latitude),
		RoundCoordinate(location.Longitude),
	)

	if value, present := reverseGeocodeCache.Get(cacheKey); present {
		if stringValue, ok := value.(string); ok {
			slog.With("cacheKey", cacheKey).
				DebugContext(context.Background(), "Found cached reverse geocode")

			return stringValue, nil
		} else {
			slog.With("value", value).WarnContext(context.Background(), "Cache value wasn't a string")
			reverseGeocodeCache.Delete(cacheKey)
		}
	}

	geocodingUrl := fmt.Sprintf(
		env.configuration.ReverseGeocodeApiURL,
		location.Latitude,
		location.Longitude,
	)

	geocodingResponse, err := fetchGeocodingResponse(geocodingUrl)
	slog.With("response", geocodingResponse).
		DebugContext(context.Background(), "Geocoding Response")

	if err != nil {
		return "", err
	}

	var response OpencageReverseGeocodeResponse

	err = json.Unmarshal([]byte(geocodingResponse), &response)
	if err != nil {
		return "", err
	}

	geocodingJson, err := json.Marshal(response.Results)
	if err != nil {
		return "", err
	}

	reverseGeocodeCache.Set(cacheKey, string(geocodingJson), 0)

	return string(geocodingJson), nil
}

func fetchGeocodingResponse(geocodingUrl string) (string, error) {
	defer timeTrack(time.Now())

	response, err := http.Get(geocodingUrl)

	if err != nil || response == nil {
		slog.With("err", err).
			ErrorContext(context.Background(), "Error getting geolocation from API")

		return "", err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	body, err := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		if err == nil {
			body = []byte("")
		}

		err := fmt.Errorf("invalid response from Geolocation API: %v %v", response.StatusCode, body)
		InternalError(err)

		return "", err
	}

	if err != nil {
		InternalError(err)

		return "", err
	}

	return string(body), nil
}

func (env *Env) UpdateLocationWithGeocoding(queue <-chan int) {
	ctx := context.Background()
	slog.InfoContext(ctx, "Starting geocoding goroutine")

	for {
		id, more := <-queue
		if more {
			slog.With("id", id).InfoContext(ctx, "Updating geocoding for entry")

			location := Location{Type: "location"}

			err := env.db.QueryRow("select ST_Y(ST_AsText(point)),ST_X(ST_AsText(point)) from locations where id=$1", id).
				Scan(&location.Latitude, &location.Longitude)
			if err != nil {
				slog.With("err", err).
					With("id", id).
					ErrorContext(ctx, "Error fetching location from database")
			}

			env.geocodeAndUpdateDatabase(location, id)
		} else {
			slog.InfoContext(ctx, "Got signal, quitting geocoding goroutine.")

			return
		}
	}
}

func (env *Env) geocodeAndUpdateDatabase(location Location, id int) {
	ctx := context.Background()

	geocodingJson, err := location.GetReverseGeocoding(env)
	if err != nil {
		slog.With("err", err).ErrorContext(ctx, "Could not reverse geocode")

		return
	} else {
		_, err = env.db.Exec("update locations set geocoding=$1 where id=$2", geocodingJson, id)
		if err != nil {
			slog.With("err", err).With("geocoding", geocodingJson).ErrorContext(ctx, "could not update database with geocode")
		} else {
			slog.With("id", id).InfoContext(ctx, "Geocoded location id")
		}
	}
}

func (env *Env) GeocodingCrawler(quitChan <-chan bool) {
	ctx := context.Background()
	slog.InfoContext(ctx, "Starting geocoding backlog crawler")

	ticker := time.NewTicker(10 * time.Second)

	for {
		select {
		case <-ticker.C:
			location := Location{Type: "location"}

			var id int

			err := env.db.QueryRow("select id,ST_Y(ST_AsText(point)),ST_X(ST_AsText(point)) from locations where geocoding is null and devicetimestamp<CURRENT_DATE - 1 order by devicetimestamp desc limit 1").
				Scan(&id, &location.Latitude, &location.Longitude)
			if err != nil {
				slog.With("err", err).
					ErrorContext(ctx, "Error fetching latest location without geocode")

				break
			}

			env.geocodeAndUpdateDatabase(location, id)
		case <-quitChan:
			slog.InfoContext(ctx, "Closing geocoding crawler")

			return
		}
	}
}
