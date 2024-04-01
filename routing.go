package main

import (
	"embed"
	"fmt"
	"github.com/gin-gonic/gin"
	"html/template"
	"net/http"
	_ "time"
)

//go:embed templates/*
var embedFs embed.FS

func (env *Env) BuildRoutes(router *gin.Engine) {
	router.Use(ErrorHandler)
	router.SetHTMLTemplate(template.Must(template.New("").ParseFS(embedFs, "templates/*.gohtml")))

	router.GET("place/", func(c *gin.Context) {
		c.HTML(200, "place.gohtml", nil)
	})
	router.POST("place/", env.PlaceHandler)

	router.GET("inaccurate/", env.GetInaccurateLocationPoints)
	router.DELETE("points/:id", env.DeleteLocationPoint)
	router.GET("points/:date", env.GetPointsForDate)

	router.GET("export/:limit", env.Export)
	router.GET("export/", env.Export)

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
	for _, err := range c.Errors {
		errors += fmt.Sprintf("%v\n", err)
	}
	if errors != "" {
		c.String(http.StatusInternalServerError, "Many errors\n%s", errors)
	}
}
