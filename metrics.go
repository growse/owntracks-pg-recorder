package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	locationsReceived prometheus.Counter
}

func NewMetrics() *Metrics {
	return &Metrics{locationsReceived: promauto.NewCounter(prometheus.CounterOpts{
		Name: "location_messages_received_total",
		Help: "Number of location messages received by the recorder",
	}),
	}
}
