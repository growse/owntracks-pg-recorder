package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// dawarichAPIPoint is a single point returned by GET /api/v1/points.
type dawarichAPIPoint struct {
	ID        int   `json:"id"`
	Timestamp int64 `json:"timestamp"`
}

// SyncToDawarich compares all local DB points in the half-open interval
// [start, end) against the points already stored in Dawarich and POSTs any
// that are missing.  Progress is logged at INFO; per-point detail at DEBUG.
func (env *Env) SyncToDawarich(ctx context.Context, start, end time.Time) error {
	slog.With("start", start, "end", end).InfoContext(ctx, "Starting Dawarich sync")

	existing, err := env.fetchDawarichTimestamps(ctx, start, end)
	if err != nil {
		return fmt.Errorf("fetching existing Dawarich points: %w", err)
	}

	slog.With("count", len(existing)).InfoContext(ctx, "Fetched existing Dawarich timestamps")

	rows, err := env.database.QueryContext(ctx, `
		SELECT
			EXTRACT(EPOCH FROM devicetimestamp)::bigint,
			devicetimestamp,
			ST_Y(point::geometry)  AS latitude,
			ST_X(point::geometry)  AS longitude,
			altitude,
			accuracy,
			verticalaccuracy,
			speed,
			batterylevel,
			connectiontype,
			cog,
			"user",
			device
		FROM locations
		WHERE devicetimestamp >= $1 AND devicetimestamp < $2
		ORDER BY devicetimestamp ASC
	`, start, end)
	if err != nil {
		return fmt.Errorf("querying database: %w", err)
	}
	defer rows.Close()

	var posted, skipped int

	for rows.Next() {
		var (
			tst     int64
			devTime time.Time
			lat     float64
			lon     float64
			alt     sql.NullFloat64
			acc     float64
			vac     sql.NullFloat64
			speed   sql.NullFloat64
			battery sql.NullInt64
			conn    sql.NullString
			cog     sql.NullInt64
			user    string
			device  string
		)

		if err := rows.Scan(
			&tst, &devTime,
			&lat, &lon,
			&alt, &acc, &vac, &speed,
			&battery, &conn, &cog,
			&user, &device,
		); err != nil {
			return fmt.Errorf("scanning row: %w", err)
		}

		if _, exists := existing[tst]; exists {
			skipped++
			continue
		}

		msg := MQTTMsg{
			Latitude:             lat,
			Longitude:            lon,
			Accuracy:             float32(acc),
			DeviceTimestampAsInt: tst,
			DeviceTimestamp:      devTime,
			User:                 user,
			Device:               device,
		}

		if alt.Valid {
			msg.Altitude = float32(alt.Float64)
		}

		if vac.Valid {
			msg.VerticalAccuracy = float32(vac.Float64)
		}

		if speed.Valid {
			msg.Speed = float32(speed.Float64)
		}

		if battery.Valid {
			msg.Battery = int(battery.Int64)
		}

		if conn.Valid {
			// connectiontype is CHAR(1) — trim padding
			msg.Connection = strings.TrimSpace(conn.String)
		}

		if cog.Valid {
			msg.Course = int(cog.Int64)
		}

		if err := env.sendLocationToDawarich(ctx, msg); err != nil {
			slog.With("err", err, "tst", devTime, "user", user, "device", device).
				WarnContext(ctx, "Failed to post point to Dawarich, skipping")
		} else {
			slog.With("tst", devTime, "user", user, "device", device).
				DebugContext(ctx, "Posted missing point to Dawarich")

			posted++
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating rows: %w", err)
	}

	slog.With("posted", posted, "skipped", skipped).InfoContext(ctx, "Dawarich sync complete")

	return nil
}

// fetchDawarichTimestamps pages through GET /api/v1/points and returns the set
// of Unix timestamps already stored in Dawarich for the given time window.
func (env *Env) fetchDawarichTimestamps(
	ctx context.Context,
	start, end time.Time,
) (map[int64]struct{}, error) {
	timestamps := make(map[int64]struct{})
	page := 1

	for {
		reqURL := fmt.Sprintf(
			"%s/api/v1/points?api_key=%s&start_at=%s&end_at=%s&page=%d&per_page=1000&order=asc",
			env.configuration.DawarichURL,
			env.configuration.DawarichAPIKey,
			start.UTC().Format(time.RFC3339),
			end.UTC().Format(time.RFC3339),
			page,
		)

		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, reqURL, nil)
		if err != nil {
			cancel()

			return nil, fmt.Errorf("creating request for page %d: %w", page, err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()

			return nil, fmt.Errorf("fetching page %d: %w", page, err)
		}

		totalPages, _ := strconv.Atoi(resp.Header.Get("X-Total-Pages"))

		var points []dawarichAPIPoint

		decodeErr := json.NewDecoder(resp.Body).Decode(&points)
		_ = resp.Body.Close()
		cancel()

		if decodeErr != nil {
			return nil, fmt.Errorf("decoding page %d: %w", page, decodeErr)
		}

		for _, p := range points {
			timestamps[p.Timestamp] = struct{}{}
		}

		slog.With("page", page, "totalPages", totalPages, "pointsOnPage", len(points)).
			DebugContext(ctx, "Fetched Dawarich page")

		if page >= totalPages || totalPages == 0 {
			break
		}

		page++
	}

	return timestamps, nil
}
