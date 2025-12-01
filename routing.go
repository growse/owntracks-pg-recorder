package main

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	_ "time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed templates/*
var embedFs embed.FS

func (env *Env) BuildRoutes(configuration *Configuration, router *gin.Engine) {
	router.Use(ErrorHandler)
	router.SetHTMLTemplate(template.Must(template.New("").ParseFS(embedFs, "templates/*.gohtml")))

	if configuration.EnablePrometheus {
		router.GET("/metrics", gin.WrapH(promhttp.Handler()))
	}

	router.GET("place/", func(c *gin.Context) {
		c.HTML(200, "place.gohtml", nil)
	})
	router.POST("place/", env.PlaceHandler)

	router.GET("inaccurate/", env.GetInaccurateLocationPoints)
	router.DELETE("points/:id", env.DeleteLocationPoint)
	router.GET("points/:date", env.GetPointsForDate)

	router.GET("export/geojson/:from/:to", env.ExportGeoJSON)

	otRecorderAPI := router.Group("api/")
	{
		restAPI := otRecorderAPI.Group("/0")
		{
			restAPI.GET("list", env.OTListUserHandler)
			restAPI.GET("last", env.OTLastPosHandler)
			restAPI.GET("locations", env.OTLocationsHandler)
			restAPI.GET("version", OTVersionHandler)
		}
	}

	wsAPI := router.Group("ws")
	{
		wsAPI.GET("last", func(c *gin.Context) {
			env.wshandler(c.Writer, c.Request)
		})
	}

	router.GET("/location/", env.LocationHandler)
	router.HEAD("/location/", env.LocationHeadHandler)
}

func ErrorHandler(c *gin.Context) {
	c.Next()

	errors := ""

	var errorsSb63 strings.Builder
	for _, err := range c.Errors {
		errorsSb63.WriteString(fmt.Sprintf("%v\n", err))
	}

	errors += errorsSb63.String()

	if errors != "" {
		c.String(http.StatusInternalServerError, "Many errors\n%s", errors)
	}
}
