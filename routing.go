package main

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed templates/*
var embedFs embed.FS

func (env *Env) BuildRoutes(configuration *Configuration) http.Handler {
	env.tmpl = template.Must(template.New("").ParseFS(embedFs, "templates/*.gohtml"))

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	if configuration.EnablePrometheus {
		r.Handle("/metrics", promhttp.Handler())
	}

	r.Get("/place/", func(w http.ResponseWriter, r *http.Request) {
		env.respondHTML(w, "place.gohtml", nil)
	})
	r.Post("/place/", env.PlaceHandler)
	r.Get("/inaccurate/", env.GetInaccurateLocationPoints)
	r.Delete("/points/{id}", env.DeleteLocationPoint)
	r.Get("/points/{date}", env.GetPointsForDate)
	r.Get("/export/geojson/{from}/{to}", env.ExportGeoJSON)

	r.Route("/api/0", func(r chi.Router) {
		r.Get("/list", env.OTListUserHandler)
		r.Get("/last", env.OTLastPosHandler)
		r.Get("/locations", env.OTLocationsHandler)
		r.Get("/version", OTVersionHandler)
	})

	r.Get("/ws/last", func(w http.ResponseWriter, r *http.Request) {
		env.wshandler(w, r)
	})

	r.Get("/location/", env.LocationHandler)
	r.Head("/location/", env.LocationHeadHandler)

	return r
}
