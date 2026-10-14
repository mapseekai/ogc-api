package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/examples/internal/nethttp"
	"github.com/mapseekai/ogc-api/server"
)

func TestSharedContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptertest.Run(t, func(t *testing.T, engine *server.Engine) string {
		s := httptest.NewServer(router(engine))
		t.Cleanup(s.Close)
		return s.URL
	})
}

func TestAPIContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptertest.RunAPI(t, func(t *testing.T, engine *api.Engine) string {
		app := gin.New()
		app.NoRoute(gin.WrapH(nethttp.APIHandler(engine, "public")))
		host := httptest.NewServer(app)
		t.Cleanup(host.Close)
		return host.URL
	})
}
