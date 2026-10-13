package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/mapseekai/ogc-api/adaptertest"
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
