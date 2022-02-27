package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeocodingShouldDecodeLocalityToTheRightName(t *testing.T) {
	testlocation := `
[
      {
         "bounds" : {
            "northeast" : {
               "lat" : 51.9528202,
               "lng" : 7.6325938
            },
            "southwest" : {
               "lat" : 51.9525445,
               "lng" : 7.6323594
            }
         },
         "components" : {
            "ISO_3166-1_alpha-2" : "DE",
            "ISO_3166-1_alpha-3" : "DEU",
            "_category" : "building",
            "_type" : "building",
            "city" : "M\u00fcnster",
            "city_district" : "M\u00fcnster-Mitte",
            "continent" : "Europe",
            "country" : "Germany",
            "country_code" : "de",
            "house_number" : "7",
            "neighbourhood" : "Josef",
            "political_union" : "European Union",
            "postcode" : "48153",
            "road" : "Friedrich-Ebert-Stra\u00dfe",
            "state" : "North Rhine-Westphalia",
            "state_code" : "NW",
            "suburb" : "Innenstadtring"
         },
         "confidence" : 10,
         "formatted" : "Friedrich-Ebert-Str 7, 48153 M\u00fcnster, Germany",
         "geometry" : {
            "lat" : 51.9526599,
            "lng" : 7.632473
         }
      }
   ]`
	location := Location{Geocoding: testlocation}
	name := location.GeocodedName()
	assert.Equal(t, "Münster", name)
}

func TestRoundCoordinate(t *testing.T) {
	inputs := map[float64]float64{
      1.234567: 1.23457,
      75.0:75.0,
      23.123:23.123,
      784.1234567789:784.12346,

   }
	for input, expected := range inputs {
		assert.Equal(t, expected, RoundCoordinate(input))
	}
}
