package main

import (
	"log"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mapseekai/ogc-api/examples/demo"
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
	app := fiber.New(fiber.Config{ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, ReadBufferSize: 128 << 10})
	app.All("/wms", ogcHandler(engine, server.Endpoint{Protocol: owscore.WMS}))
	app.All("/wmts", ogcHandler(engine, server.Endpoint{Protocol: owscore.WMTS}))
	app.All("/ows", ogcHandler(engine, server.Endpoint{}))
	app.Use(ogcHandler(engine, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	log.Fatal(app.Listen(address))
}
