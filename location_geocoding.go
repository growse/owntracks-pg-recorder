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

// NominatimReverseGeocodeResult represents the response from Nominatim reverse geocoding API.
//
//nolint:tagliatelle
type NominatimReverseGeocodeResult struct {
	PlaceID     int    `json:"place_id"`
	Licence     string `json:"licence"`
	OsmType     string `json:"osm_type"`
	OsmID       int64  `json:"osm_id"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
	Address     struct {
		HouseNumber  string `json:"house_number"`
		Road         string `json:"road"`
		Suburb       string `json:"suburb"`
		Village      string `json:"village"`
		Town         string `json:"town"`
		City         string `json:"city"`
		Municipality string `json:"municipality"`
		County       string `json:"county"`
		State        string `json:"state"`
		Postcode     string `json:"postcode"`
		Country      string `json:"country"`
		CountryCode  string `json:"country_code"`
	} `json:"address"`
	BoundingBox []string `json:"boundingbox"`
}

// GeocodedName extracts a sane name from the geocoded name component.
func (location *Location) GeocodedName(ctx context.Context) string {
	unknownLocation := "Unknown"

	var geoLocation NominatimReverseGeocodeResult

	err := json.Unmarshal([]byte(location.Geocoding), &geoLocation)
	if err != nil {
		slog.With("err", err).
			With("geocoding", location.Geocoding).
			ErrorContext(ctx, "Error decoding location object")

		return unknownLocation
	}

	// Try to extract a meaningful location name
	if geoLocation.Address.City != "" {
		return geoLocation.Address.City
	}

	if geoLocation.Address.Town != "" {
		return geoLocation.Address.Town
	}

	if geoLocation.Address.Village != "" {
		return geoLocation.Address.Village
	}

	if geoLocation.Address.Municipality != "" {
		return geoLocation.Address.Municipality
	}

	if geoLocation.Address.County != "" {
		return geoLocation.Address.County
	}

	return unknownLocation
}

func (env *Env) GetGeocoding(
	ctx context.Context,
	place string,
) (*geojson.FeatureCollection, error) {
	if env.configuration.GeocodeAPIURL == "" {
		err := errors.New("geocoding API should not be blank")
		InternalError(ctx, err)

		return nil, err
	}

	if place == "" {
		err := errors.New("place should not be blank")
		InternalError(ctx, err)

		return nil, err
	}

	// Build Nominatim search URL
	// Format: https://nominatim.example.com/search?q=PLACE&format=geojson
	geocodingURL := fmt.Sprintf(
		"%s/reverse?q=%s&format=geojson&addressdetails=1",
		env.configuration.GeocodeAPIURL,
		url.QueryEscape(place),
	)

	geocodingResponse, err := fetchGeocodingResponse(ctx, geocodingURL)
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
	rounded, err := strconv.ParseFloat(fmt.Sprintf("%.5f", input), 64)
	if err != nil {
		slog.With("input", input).
			Error("Unable to truncate float to precision")
		panic(err)
	}

	return rounded
}

var reverseGeocodeCache = cache.New(cache.NoExpiration, 0)

func (location *Location) GetReverseGeocoding(ctx context.Context, env *Env) (string, error) {
	if env.configuration.ReverseGeocodeAPIURL == "" {
		err := errors.New("reverse Geocoding API should not be blank")
		InternalError(ctx, err)

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
				DebugContext(ctx, "Found cached reverse geocode")

			return stringValue, nil
		}

		slog.With("value", value).
			WarnContext(ctx, "Cache value wasn't a string")
		reverseGeocodeCache.Delete(cacheKey)
	}

	// Build Nominatim reverse geocode URL
	// Format: https://nominatim.example.com/reverse?lat=LAT&lon=LON&format=json
	geocodingURL := fmt.Sprintf(
		"%s/reverse?lat=%f&lon=%f&format=json&addressdetails=1",
		env.configuration.ReverseGeocodeAPIURL,
		location.Latitude,
		location.Longitude,
	)

	geocodingResponse, err := fetchGeocodingResponse(ctx, geocodingURL)
	slog.With("url", geocodingURL).
		With("response", geocodingResponse).
		DebugContext(ctx, "Reverse Geocoding Response")

	if err != nil {
		return "", err
	}

	var response NominatimReverseGeocodeResult

	err = json.Unmarshal([]byte(geocodingResponse), &response)
	if err != nil {
		return "", err
	}

	// Store the entire response as JSON
	geocodingJSON, err := json.Marshal(response)
	if err != nil {
		return "", err
	}

	reverseGeocodeCache.Set(cacheKey, string(geocodingJSON), 0)

	return string(geocodingJSON), nil
}

func fetchGeocodingResponse(ctx context.Context, geocodingURL string) (string, error) {
	defer timeTrack(ctx, time.Now())

	//nolint: gosec
	response, err := http.Get(geocodingURL)

	defer func(r *http.Response) {
		if r == nil {
			return
		}

		_ = r.Body.Close()
	}(response)

	if err != nil || response == nil {
		slog.With("err", err).
			ErrorContext(ctx, "Error getting geolocation from API")

		return "", err
	}

	body, err := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		if err == nil {
			body = []byte("")
		}

		err := fmt.Errorf("invalid response from Geolocation API: %v %v", response.StatusCode, body)
		InternalError(ctx, err)

		return "", err
	}

	if err != nil {
		InternalError(ctx, err)

		return "", err
	}

	return string(body), nil
}

func (env *Env) UpdateLocationWithGeocoding(ctx context.Context, queue <-chan int) {
	slog.InfoContext(ctx, "Starting geocoding goroutine")

	for {
		locationID, more := <-queue
		if more {
			slog.With("locationID", locationID).
				InfoContext(ctx, "Updating geocoding for entry")

			location := Location{Type: "location"}

			err := env.database.QueryRow(`select ST_Y(ST_AsText(point)), ST_X(ST_AsText(point))
from locations
where id = $1`, locationID).
				Scan(&location.Latitude, &location.Longitude)
			if err != nil {
				slog.With("err", err).
					With("locationID", locationID).
					ErrorContext(ctx, "Error fetching location from database")
			}

			env.geocodeAndUpdateDatabase(ctx, location, locationID)
		} else {
			slog.InfoContext(
				ctx,
				"Got signal, quitting geocoding goroutine.",
			)

			return
		}
	}
}

func (env *Env) geocodeAndUpdateDatabase(ctx context.Context, location Location, id int) {
	geocodingJSON, err := location.GetReverseGeocoding(ctx, env)
	if err != nil {
		slog.With("err", err).
			ErrorContext(ctx, "Could not reverse geocode")

		return
	}

	_, err = env.database.Exec("update locations set geocoding=$1 where id=$2", geocodingJSON, id)
	if err != nil {
		slog.With("err", err).
			With("geocoding", geocodingJSON).
			ErrorContext(ctx, "could not update database with geocode")
	} else {
		slog.With("id", id).
			InfoContext(ctx, "Geocoded location id")
	}
}

const geocodingCrawlerInterval = 10 * time.Second

func (env *Env) GeocodingCrawler(ctx context.Context) {
	slog.InfoContext(ctx, "Starting geocoding backlog crawler")

	ticker := time.NewTicker(geocodingCrawlerInterval)

	for {
		select {
		case <-ticker.C:
			location := Location{Type: "location"}

			var locationID int

			err := env.database.QueryRow(`select id, ST_Y(ST_AsText(point)), ST_X(ST_AsText(point))
from locations
where geocoding is null
  and devicetimestamp < CURRENT_DATE - 1
order by devicetimestamp desc
limit 1`).
				Scan(&locationID, &location.Latitude, &location.Longitude)
			if err != nil {
				slog.With("err", err).
					ErrorContext(ctx, "Error fetching latest location without geocode")

				break
			}

			env.geocodeAndUpdateDatabase(ctx, location, locationID)
		case <-ctx.Done():
			slog.InfoContext(ctx, "Closing geocoding crawler")

			return
		}
	}
}
