package main

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGinRoute(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.Default()
	env := Env{}
	env.BuildRoutes(&Configuration{}, router)
}
