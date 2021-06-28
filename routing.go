package main

import (
	"github.com/gin-gonic/gin"
	_ "time"
)

func (env *Env) BuildRoutes(router *gin.Engine) {
	router.SetHTMLTemplate(BuildTemplates())

	router.GET("place/", func(c *gin.Context) {
		c.HTML(200, "place", nil)
	})
	router.POST("place/", env.PlaceHandler)

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
