package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// dawarichPayload is the JSON body sent to POST /api/v1/owntracks/points.
//
//nolint:tagliatelle
type dawarichPayload struct {
	Type      string  `json:"_type"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	Altitude  float32 `json:"alt"`
	Accuracy  float32 `json:"acc"`
	Vac       float32 `json:"vac"`
	Speed     float32 `json:"vel"`
	Course    int     `json:"cog"`
	Battery   int     `json:"batt"`
	Conn      string  `json:"conn"`
	Timestamp int64   `json:"tst"`
	// ISOTimestamp is the ISO 8601 representation of tst. Dawarich uses this
	// field (rather than the raw Unix tst integer) to determine the stored
	// point timestamp; without it Dawarich falls back to its own receive time.
	ISOTimestamp string `json:"isotst"`
	Topic        string `json:"topic"`
	TrackerID    string `json:"tid"`
}

// ForwardToDawarich drains the queue channel and forwards each location to the
// configured Dawarich instance. It exits when the channel is closed.
func (env *Env) ForwardToDawarich(ctx context.Context, queue <-chan MQTTMsg) {
	slog.InfoContext(ctx, "Starting Dawarich forwarding goroutine")

	for {
		msg, more := <-queue
		if !more {
			slog.InfoContext(ctx, "Dawarich forwarding goroutine shutting down")

			return
		}

		err := env.sendLocationToDawarich(ctx, msg)
		if err != nil {
			slog.With("err", err).
				With("user", msg.User).
				With("device", msg.Device).
				With("timestamp", msg.DeviceTimestamp).
				ErrorContext(ctx, "Failed to forward location to Dawarich")
		}
	}
}

func (env *Env) sendLocationToDawarich(ctx context.Context, msg MQTTMsg) error {
	topic := fmt.Sprintf("owntracks/%s/%s", msg.User, msg.Device)

	deviceTime := time.Unix(msg.DeviceTimestampAsInt, 0).UTC()

	payload := dawarichPayload{
		Type:         locationType,
		Latitude:     msg.Latitude,
		Longitude:    msg.Longitude,
		Altitude:     msg.Altitude,
		Accuracy:     msg.Accuracy,
		Vac:          msg.VerticalAccuracy,
		Speed:        msg.Speed,
		Course:       msg.Course,
		Battery:      msg.Battery,
		Conn:         msg.Connection,
		Timestamp:    msg.DeviceTimestampAsInt,
		ISOTimestamp: deviceTime.Format(time.RFC3339),
		Topic:        topic,
		TrackerID:    msg.TrackerID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling Dawarich payload: %w", err)
	}

	url := fmt.Sprintf(
		"%s/api/v1/owntracks/points?api_key=%s",
		env.configuration.DawarichURL,
		env.configuration.DawarichAPIKey,
	)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating Dawarich request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending to Dawarich: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dawarich returned unexpected status %d", resp.StatusCode)
	}

	slog.With("user", msg.User).
		With("device", msg.Device).
		DebugContext(ctx, "Forwarded location to Dawarich")

	return nil
}
