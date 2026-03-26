# OwnTracks PG Recorder

An [OwnTracks](https://owntracks.org/) recorder that subscribes to an MQTT broker and persists location data to a PostgreSQL database. It also exposes an HTTP API compatible with the [OwnTracks Recorder](https://github.com/owntracks/recorder) REST API.

## Requirements

- PostgreSQL with the [PostGIS](https://postgis.net/) extension
- An MQTT broker (e.g. [Mosquitto](https://mosquitto.org/)) receiving OwnTracks location messages
- Optionally, a [Nominatim](https://nominatim.org/) instance for reverse geocoding

## Running

### Docker

```sh
docker run \
  -e OT_PG_RECORDER_DBHOST=postgres \
  -e OT_PG_RECORDER_DBUSER=owntracks \
  -e OT_PG_RECORDER_DBPASSWORD=secret \
  -e OT_PG_RECORDER_MQTTURL=tcp://mosquitto:1883 \
  -p 8080:8080 \
  ghcr.io/growse/owntracks-pg-recorder:latest
```

### Docker Compose

A minimal setup with a Postgres/PostGIS database and MQTT broker:

```yaml
services:
  postgres:
    image: postgis/postgis:17-3.5-alpine
    environment:
      POSTGRES_USER: owntracks
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: locations
    volumes:
      - postgres-data:/var/lib/postgresql/data

  mosquitto:
    image: eclipse-mosquitto:latest
    ports:
      - "1883:1883"

  owntracks-pg-recorder:
    image: ghcr.io/growse/owntracks-pg-recorder:latest
    ports:
      - "8080:8080"
    environment:
      OT_PG_RECORDER_DBHOST: postgres
      OT_PG_RECORDER_DBUSER: owntracks
      OT_PG_RECORDER_DBPASSWORD: secret
      OT_PG_RECORDER_DBNAME: locations
      OT_PG_RECORDER_MQTTURL: tcp://mosquitto:1883
    depends_on:
      - postgres
      - mosquitto

volumes:
  postgres-data:
```

### Binary

Download a release binary and run it directly. All configuration is via environment variables:

```sh
OT_PG_RECORDER_DBHOST=localhost \
OT_PG_RECORDER_DBUSER=owntracks \
OT_PG_RECORDER_DBPASSWORD=secret \
OT_PG_RECORDER_MQTTURL=tcp://localhost:1883 \
owntracks-pg-recorder
```

Database schema migrations are applied automatically on startup.

## Configuration

All configuration is via environment variables prefixed with `OT_PG_RECORDER_`.

### Database

| Variable | Default | Description |
|---|---|---|
| `OT_PG_RECORDER_DBHOST` | *(required)* | PostgreSQL host |
| `OT_PG_RECORDER_DBUSER` | | PostgreSQL username |
| `OT_PG_RECORDER_DBPASSWORD` | | PostgreSQL password |
| `OT_PG_RECORDER_DBNAME` | `locations` | Database name |
| `OT_PG_RECORDER_DBSSLMODE` | `require` | SSL mode (`disable`, `require`, `verify-full`, etc.) |
| `OT_PG_RECORDER_MAXDBOPENCONNECTIONS` | `10` | Maximum open database connections |

### MQTT

| Variable | Default | Description |
|---|---|---|
| `OT_PG_RECORDER_MQTTURL` | `tcp://localhost:1883` | MQTT broker URL |
| `OT_PG_RECORDER_MQTTUSERNAME` | | MQTT username |
| `OT_PG_RECORDER_MQTTPASSWORD` | | MQTT password |
| `OT_PG_RECORDER_MQTTCLIENTID` | `owntracks-pg-recorder` | MQTT client ID |
| `OT_PG_RECORDER_MQTTTOPIC` | `owntracks/#` | MQTT topic to subscribe to |

### Geocoding

Optional reverse geocoding via a [Nominatim](https://nominatim.org/) instance. The base URL should point to the root of the Nominatim API; the application appends `/reverse?lat=LAT&lon=LON&format=json&addressdetails=1` automatically.

| Variable | Default | Description |
|---|---|---|
| `OT_PG_RECORDER_REVERSGEOCODEAPIURL` | | Nominatim base URL for reverse geocoding (e.g. `http://nominatim:8080`) |
| `OT_PG_RECORDER_GEOCODEAPIURL` | | Nominatim base URL for forward geocoding |
| `OT_PG_RECORDER_GEOCODEONINSERT` | `false` | Reverse-geocode each location immediately on insert |
| `OT_PG_RECORDER_ENABLEGEOCODINGCRAWLER` | `false` | Run a background crawler to geocode historical locations that are missing geocoding data |

### HTTP & General

| Variable | Default | Description |
|---|---|---|
| `OT_PG_RECORDER_PORT` | `8080` | HTTP server port |
| `OT_PG_RECORDER_DOMAIN` | | Public domain name |
| `OT_PG_RECORDER_DEFAULTUSER` | | Default OwnTracks user for single-user API endpoints |
| `OT_PG_RECORDER_FILTERUSERS` | | Comma-separated list of usernames to accept; all others are dropped |
| `OT_PG_RECORDER_ENABLEPROMETHEUS` | `false` | Expose a `/metrics` endpoint for Prometheus scraping |
| `OT_PG_RECORDER_DEBUG` | `false` | Enable debug-level logging |

## Dawarich Integration

Locations can be forwarded to a [Dawarich](https://dawarich.app) instance in real-time as they arrive from MQTT. Forwarding is asynchronous and never blocks MQTT processing.

| Variable | Default | Description |
|---|---|---|
| `OT_PG_RECORDER_DAWARICHURL` | | Base URL of your Dawarich instance (e.g. `http://dawarich:3000`). Forwarding is disabled when not set. |
| `OT_PG_RECORDER_DAWARICHAPIKEY` | | Dawarich API key (found in your Dawarich profile settings) |

## HTTP API

The service exposes an HTTP API compatible with the OwnTracks Recorder.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/0/list` | List users and devices |
| `GET` | `/api/0/last` | Last known position(s) |
| `GET` | `/api/0/locations` | Location history |
| `GET` | `/api/0/version` | Application version |
| `GET` | `/location/` | Last location for the default user (JSON) |
| `HEAD` | `/location/` | Last-Modified header for the default user |
| `GET` | `/points/:date` | All location points for a given date |
| `GET` | `/export/geojson/:from/:to` | Export locations as GeoJSON for a date range |
| `GET` | `/inaccurate/` | Location points with poor accuracy |
| `DELETE` | `/points/:id` | Delete a specific location point |
| `GET` | `/ws/last` | WebSocket stream of latest location |
| `GET` | `/metrics` | Prometheus metrics (if enabled) |
