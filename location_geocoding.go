package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/patrickmn/go-cache"
	geojson "github.com/paulmach/go.geojson"
	log "github.com/sirupsen/logrus"
)

type GeoLocation struct {
	Status  string            `json:"status"`
	Results []GeocodingResult `json:"results"`
}

type GeocodingResult struct {
	AddressComponents []GeocodingAddressComponent `json:"address_components"`
}

type GeocodingAddressComponent struct {
	LongName  string   `json:"long_name"`
	ShortName string   `json:"short_name"`
	Types     []string `json:"types"`
}

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
	} `json:"bounds" binding:"required"`
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
	} `json:"components" binding:"required"`
	Confidence int    `json:"confidence"`
	Formatted  string `json:"formatted"`
	Geometry   struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"geometry"`
}
type OpencageReverseGeocodeResponse struct {
	PlusCode struct {
		CompoundCode string `json:"compound_code"`
		GlobalCode   string `json:"global_code"`
	} `json:"plus_code"`
	Results []OpencageReverseGeocodeResult `json:"results" binding:"required"`
}

/*
Extract a sane name from the geocoding object
*/
func (location *Location) GeocodedName() string {
	unknownLocation := "Unknown"
	var geoLocation []OpencageReverseGeocodeResult
	err := json.Unmarshal([]byte(location.Geocoding), &geoLocation)
	if err != nil {
		log.WithError(err).WithField("geocoding", location.Geocoding).Error("Error decoding location object")
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
		err := errors.New("Geocoding API should not be blank")
		InternalError(err)
		return nil, err
	}
	if place == "" {
		err := errors.New("Place should not be blank")
		InternalError(err)
		return nil, err
	}
	geocodingUrl := fmt.Sprintf(env.configuration.GeocodeApiURL, url.QueryEscape(place))

	geocodingResponse, err := fetchGeocodingResponse(geocodingUrl)
	if err != nil {
		return nil, err
	}
	featureColletion, err := geojson.UnmarshalFeatureCollection([]byte(geocodingResponse))
	if err != nil {
		return nil, err
	}
	return featureColletion, nil
}

func RoundCoordinate(input float64) float64 {
	rounded, err := strconv.ParseFloat(fmt.Sprintf("%.5f", input), 64)
	if err != nil {
		log.Fatalf("Unable to truncate float to precision: %v", input)
	}
	return rounded

}

var reverseGeocodeCache = cache.New(cache.NoExpiration, 0)

func (location *Location) GetReverseGeocoding(env *Env) (string, error) {
	if env.configuration.ReverseGeocodeApiURL == "" {
		err := errors.New("Reverse Geocoding API should not be blank")
		InternalError(err)
		return "", err
	}

	cacheKey := fmt.Sprintf("%v%v", RoundCoordinate(location.Latitude), RoundCoordinate(location.Longitude))

	if value, present := reverseGeocodeCache.Get(cacheKey); present {
		if stringValue, ok := value.(string); ok {
			log.Debugf("Found cached reverse geocode for %v", cacheKey)
			return stringValue, nil
		} else {
			log.Warnf("Cache value wasn't a string? %v", value)
			reverseGeocodeCache.Delete(cacheKey)
		}
	}

	geocodingUrl := fmt.Sprintf(env.configuration.ReverseGeocodeApiURL, location.Latitude, location.Longitude)

	geocodingResponse, err := fetchGeocodingResponse(geocodingUrl)
	log.Debugf("Geocoding Response %+v", geocodingResponse)
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

	if err != nil {
		log.WithError(err).Error("Error getting geolocation from API")
		return "", err
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if response.StatusCode != 200 {
		if err == nil {
			body = []byte("")
		}
		err := errors.New(fmt.Sprintf("invalid response from Geolocation API: %v %v", response.StatusCode, body))
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
	log.Info("Starting geocoding goroutine")
	for {
		id, more := <-queue
		if more {
			log.WithField("id", id).Info("Updating geocoding for entry")
			location := Location{Type: "location"}

			err := env.db.QueryRow("select ST_Y(ST_AsText(point)),ST_X(ST_AsText(point)) from locations where id=$1", id).Scan(&location.Latitude, &location.Longitude)
			if err != nil {
				log.WithError(err).WithField("id", id).Error("Error fetching location from database")
			}
			env.geocodeAndUpdateDatabase(location, id)
		} else {
			log.Info("Got signal, quitting geocoding goroutine.")
			return
		}

	}
}

func (env *Env) geocodeAndUpdateDatabase(location Location, id int) {
	geocodingJson, err := location.GetReverseGeocoding(env)
	if err != nil {
		log.WithError(err).Error("Could not reverse geocode")
		return
	} else {
		_, err = env.db.Exec("update locations set geocoding=$1 where id=$2", geocodingJson, id)
		if err != nil {
			log.WithError(err).WithField("geocoding", geocodingJson).Error("could not update database with geocode")
		} else {
			log.WithField("id", id).Info("Geocoded location id")
		}
	}
}

func (env *Env) GeocodingCrawler(quitChan <-chan bool) {
	log.Info("Starting geocoding backlog crawler")
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-ticker.C:
			location := Location{Type: "location"}
			var id int
			err := env.db.QueryRow("select id,ST_Y(ST_AsText(point)),ST_X(ST_AsText(point)) from locations where geocoding is null and devicetimestamp<CURRENT_DATE - 1 order by devicetimestamp desc limit 1").Scan(&id, &location.Latitude, &location.Longitude)
			if err != nil {
				log.WithError(err).Error("Error fetching latest location without geocode")
				break
			}
			env.geocodeAndUpdateDatabase(location, id)
		case <-quitChan:
			log.Info("Closing geocoding crawler")
			return
		}
	}
}
