package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/examples/internal/nethttp"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/server"
)

func main() {
	address := demo.Address()
	config, err := demo.Config("http://" + address)
	if err != nil {
		log.Fatal(err)
	}
	engine, err := server.New(config)
	if err != nil {
		log.Fatal(err)
	}
	s := &http.Server{Addr: address, Handler: router(engine), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(s.ListenAndServe())
}

func router(engine *server.Engine) *gin.Engine {
	r := gin.New()
	r.Any("/wms", gin.WrapH(nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMS})))
	r.Any("/wmts", gin.WrapH(nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMTS})))
	r.Any("/ows", gin.WrapH(nethttp.Handler(engine, server.Endpoint{})))
	r.NoRoute(gin.WrapH(nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST})))
	return r
}
