package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChiRoute(t *testing.T) {
	t.Helper()

	env := Env{}
	router := env.BuildRoutes(&Configuration{})

	req := httptest.NewRequest(http.MethodGet, "/location/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
}
