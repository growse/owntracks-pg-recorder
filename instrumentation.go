package main

import (
	log "github.com/sirupsen/logrus"
	"runtime"
	"time"
)

func timeTrack(start time.Time) {
	pc := make([]uintptr, 10) // at least 1 entry needed
	runtime.Callers(2, pc)
	f := runtime.FuncForPC(pc[0])
	elapsed := time.Since(start)
	log.WithField("method", f.Name()).WithField("duration", elapsed).Info("elapsed")
}
