package main

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

func timeTrack(start time.Time) {
	ctx := context.Background()
	pc := make([]uintptr, 10) // at least 1 entry needed
	runtime.Callers(2, pc)
	f := runtime.FuncForPC(pc[0])
	elapsed := time.Since(start)
	slog.With("method", f.Name()).With("duration", elapsed).DebugContext(ctx, "timings")
}
