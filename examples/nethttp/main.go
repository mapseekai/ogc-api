package main

import (
	"log"
	"net/http"
	"time"

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
	mux := http.NewServeMux()
	mux.Handle("/wms", nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMS}))
	mux.Handle("/wmts", nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMTS}))
	mux.Handle("/ows", nethttp.Handler(engine, server.Endpoint{}))
	mux.Handle("/", nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	httpServer := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 128 << 10}
	log.Printf("Static OGC demo listening on http://%s", address)
	log.Fatal(httpServer.ListenAndServe())
}
