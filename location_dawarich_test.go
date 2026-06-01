package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMQTTMsg() MQTTMsg {
	return MQTTMsg{
		Type:                 "location",
		TrackerID:            "AB",
		Accuracy:             10.5,
		VerticalAccuracy:     3.2,
		Battery:              85,
		Connection:           "w",
		Latitude:             51.5074,
		Longitude:            -0.1278,
		Speed:                48.0,
		Altitude:             42.0,
		Course:               270,
		DeviceTimestampAsInt: 1704067200,
		DeviceTimestamp:      time.Unix(1704067200, 0),
		User:                 "alice",
		Device:               "iphone",
	}
}

func TestSendLocationToDawarich_Success(t *testing.T) {
	var received dawarichPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/owntracks/points", r.URL.Path)
		assert.Equal(t, "testkey", r.URL.Query().Get("api_key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		err := json.NewDecoder(r.Body).Decode(&received)
		require.NoError(t, err)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := &Env{
		configuration: &Configuration{
			DawarichURL:    server.URL,
			DawarichAPIKey: "testkey",
		},
	}

	msg := testMQTTMsg()
	err := env.sendLocationToDawarich(context.Background(), msg)
	require.NoError(t, err)

	assert.Equal(t, "location", received.Type)
	assert.InDelta(t, 51.5074, received.Latitude, 0.0001)
	assert.InDelta(t, -0.1278, received.Longitude, 0.0001)
	assert.Equal(t, int64(1704067200), received.Timestamp)
	assert.Equal(t, "2024-01-01T00:00:00Z", received.ISOTimestamp)
	assert.Equal(t, "owntracks/alice/iphone", received.Topic)
	assert.Equal(t, "AB", received.TrackerID)
	assert.Equal(t, 85, received.Battery)
	assert.Equal(t, "w", received.Conn)
}

func TestSendLocationToDawarich_Non200Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	env := &Env{
		configuration: &Configuration{
			DawarichURL:    server.URL,
			DawarichAPIKey: "badkey",
		},
	}

	err := env.sendLocationToDawarich(context.Background(), testMQTTMsg())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestSendLocationToDawarich_NetworkError(t *testing.T) {
	env := &Env{
		configuration: &Configuration{
			DawarichURL:    "http://127.0.0.1:1", // nothing listening
			DawarichAPIKey: "key",
		},
	}

	err := env.sendLocationToDawarich(context.Background(), testMQTTMsg())
	require.Error(t, err)
}

func TestForwardToDawarich_DrainAndShutdown(t *testing.T) {
	received := make([]string, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p dawarichPayload

		_ = json.NewDecoder(r.Body).Decode(&p)
		received = append(received, p.Topic)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	env := &Env{
		configuration: &Configuration{
			DawarichURL:    server.URL,
			DawarichAPIKey: "key",
		},
	}

	queue := make(chan MQTTMsg, 5)

	msg1 := testMQTTMsg()
	msg2 := testMQTTMsg()
	msg2.User = "bob"
	msg2.Device = "android"

	queue <- msg1

	queue <- msg2

	close(queue)

	env.ForwardToDawarich(context.Background(), queue)

	assert.Equal(t, []string{"owntracks/alice/iphone", "owntracks/bob/android"}, received)
}
