package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeocodingShouldDecodeLocalityToTheRightName(t *testing.T) {
	// Nominatim format response
	//nolint:lll
	testlocation := `{
  "place_id": 123456,
  "licence": "Data © OpenStreetMap contributors, ODbL 1.0. http://osm.org/copyright",
  "osm_type": "way",
  "osm_id": 12345678,
  "lat": "51.9526599",
  "lon": "7.632473",
  "display_name": "Friedrich-Ebert-Straße 7, Innenstadtring, Münster-Mitte, Münster, North Rhine-Westphalia, 48153, Germany",
  "address": {
    "house_number": "7",
    "road": "Friedrich-Ebert-Straße",
    "suburb": "Innenstadtring",
    "city": "Münster",
    "county": "Münster",
    "state": "North Rhine-Westphalia",
    "postcode": "48153",
    "country": "Germany",
    "country_code": "de"
  },
  "boundingbox": [
    "51.9525445",
    "51.9528202",
    "7.6323594",
    "7.6325938"
  ]
}`
	location := Location{Geocoding: testlocation}
	name := location.GeocodedName(t.Context())
	require.Equal(t, "Münster", name)
}

func TestRoundCoordinate(t *testing.T) {
	inputs := map[float64]float64{
		1.234567:       1.23457,
		75.0:           75.0,
		23.123:         23.123,
		784.1234567789: 784.12346,
	}
	for input, expected := range inputs {
		require.InEpsilon(t, expected, RoundCoordinate(input), 0.0001)
	}
}
