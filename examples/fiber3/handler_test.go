package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wmts"
)

func start(t *testing.T, engine *server.Engine) string {
	t.Helper()
	app := fiber.New(fiber.Config{ReadBufferSize: 128 << 10, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	app.All("/wms", ogcHandler(engine, server.Endpoint{Protocol: owscore.WMS}))
	app.All("/wmts", ogcHandler(engine, server.Endpoint{Protocol: owscore.WMTS}))
	app.All("/ows", ogcHandler(engine, server.Endpoint{}))
	app.Use(ogcHandler(engine, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		if err := app.ShutdownWithTimeout(5 * time.Second); err != nil {
			t.Error(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Fiber listener did not shut down")
		}
	})
	return "http://" + listener.Addr().String()
}
func TestContracts(t *testing.T) { adaptertest.Run(t, start) }

func TestStreamReadStartsAfterHandlerReturns(t *testing.T) {
	config, err := demo.Config("https://public.example.test")
	if err != nil {
		t.Fatal(err)
	}
	var returned atomic.Bool
	config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
		return response.Stream("image/png", io.NopCloser(&lateReader{Reader: strings.NewReader("late tile"), returned: &returned, ctx: ctx}), -1), nil
	}
	engine, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	handler := ogcHandler(engine, server.Endpoint{Protocol: owscore.WMTS})
	app.Get("/wmts", func(c fiber.Ctx) error {
		defer returned.Store(true)
		return handler(c)
	})
	result, err := app.Test(httptest.NewRequest("GET", "http://example.test/wmts?"+adaptertest.TileQuery, nil))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if string(data) != "late tile" {
		t.Fatal(string(data))
	}
}

type lateReader struct {
	io.Reader
	returned *atomic.Bool
	ctx      context.Context
}

func (r *lateReader) Read(p []byte) (int, error) {
	if !r.returned.Load() {
		return 0, errors.New("stream was buffered inside the handler")
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}
