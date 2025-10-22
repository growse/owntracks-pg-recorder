# OwnTracks Pg Recorder
An implementation of an [OwnTracks](https://owntracks.org/) [Recorder](https://github.com/owntracks/recorder) that persists data to a PostgreSQL database.

![Build, package and upload](https://github.com/growse/owntracks-pg-recorder/workflows/Build,%20package%20and%20upload/badge.svg)

[![codecov](https://codecov.io/gh/growse/owntracks-pg-recorder/branch/master/graph/badge.svg)](https://codecov.io/gh/growse/owntracks-pg-recorder)

## Configuration

The application is configured using environment variables with the prefix `OT_PG_RECORDER_`. Key configuration options include:

### Geocoding

The application supports reverse geocoding using a local [Nominatim](https://nominatim.org/) instance:

- `OT_PG_RECORDER_GEOCODEAPIURL`: URL of your Nominatim instance for forward geocoding (e.g., `http://nominatim:8080`)
- `OT_PG_RECORDER_REVERSGEOCODEAPIURL`: URL of your Nominatim instance for reverse geocoding (e.g., `http://nominatim:8080`)
- `OT_PG_RECORDER_GEOCODEONINSERT`: Enable geocoding on location insert (default: `false`)
- `OT_PG_RECORDER_ENABLEGEOCODINGCRAWLER`: Enable background geocoding crawler for historical data (default: `false`)

**Note**: The URLs should point to the base URL of your Nominatim instance. The application will automatically append the appropriate API endpoints:
- Forward geocoding: `/search?q=PLACE&format=geojson&addressdetails=1`
- Reverse geocoding: `/reverse?lat=LAT&lon=LON&format=json&addressdetails=1`

### Database

- `OT_PG_RECORDER_DBHOST`: PostgreSQL host
- `OT_PG_RECORDER_DBUSER`: PostgreSQL username
- `OT_PG_RECORDER_DBPASSWORD`: PostgreSQL password
- `OT_PG_RECORDER_DBNAME`: Database name (default: `locations`)
- `OT_PG_RECORDER_DBSSLMODE`: SSL mode (default: `require`)
- `OT_PG_RECORDER_MAXDBOPENCONNECTIONS`: Maximum database connections (default: `10`)

### MQTT

- `OT_PG_RECORDER_MQTTURL`: MQTT broker URL
- `OT_PG_RECORDER_MQTTUSERNAME`: MQTT username (optional)
- `OT_PG_RECORDER_MQTTPASSWORD`: MQTT password (optional)
- `OT_PG_RECORDER_MQTTCLIENTID`: MQTT client ID (default: `owntracks-pg-recorder`)
- `OT_PG_RECORDER_MQTTTOPIC`: MQTT topic to subscribe to (default: `owntracks/#`)

### Other

- `OT_PG_RECORDER_PORT`: HTTP server port (default: `8080`)
- `OT_PG_RECORDER_DEBUG`: Enable debug logging (default: `false`)
- `OT_PG_RECORDER_FILTERUSERS`: Comma-separated list of users to filter
- `OT_PG_RECORDER_DEFAULTUSER`: Default user for API queries
- `OT_PG_RECORDER_ENABLEPROMETHEUS`: Enable Prometheus metrics (default: `false`)

## Running with Nominatim

To use this application with a local Nominatim instance, you can use Docker Compose. Here's an example:

```yaml
version: '3'
services:
  nominatim:
    image: mediagis/nominatim:latest
    ports:
      - "8080:8080"
    environment:
      PBF_URL: https://download.geofabrik.de/europe/germany-latest.osm.pbf
      REPLICATION_URL: https://download.geofabrik.de/europe/germany-updates/
    volumes:
      - nominatim-data:/var/lib/postgresql/14/main

  owntracks-recorder:
    image: ghcr.io/growse/owntracks-pg-recorder:latest
    ports:
      - "8081:8080"
    environment:
      OT_PG_RECORDER_GEOCODEAPIURL: http://nominatim:8080
      OT_PG_RECORDER_REVERSGEOCODEAPIURL: http://nominatim:8080
      OT_PG_RECORDER_GEOCODEONINSERT: "true"
      # ... other configuration

volumes:
  nominatim-data:
```
