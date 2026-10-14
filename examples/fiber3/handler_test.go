package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wmts"
)

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

func TestAPIContracts(t *testing.T) {
	adaptertest.RunAPI(t, func(t *testing.T, engine *api.Engine) string {
		app := fiber.New(fiber.Config{ReadBufferSize: 128 << 10, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
		app.Use(apiHandler(engine, "public"))
		return startApp(t, app)
	})
}

func TestReaderPanicRetainsFailureAndReleasesQuota(t *testing.T) {
	config, err := demo.Config("https://public.example.test")
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConcurrent = 1
	var closes atomic.Int32
	finished := make(chan server.Event, 2)
	config.Observer = func(event server.Event) {
		if event.Stage == "finish" {
			finished <- event
		}
	}
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		return response.Stream("image/png", &panickingStream{closes: &closes}, -1), nil
	}
	engine, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	base := start(t, engine)
	client := &http.Client{Timeout: 5 * time.Second}
	reply, requestErr := client.Get(base + "/wmts?" + adaptertest.TileQuery)
	var readErr error
	if reply != nil {
		_, readErr = io.ReadAll(reply.Body)
		_ = reply.Body.Close()
	}
	if requestErr == nil && readErr == nil {
		t.Fatal("panicking stream appeared successful")
	}
	select {
	case event := <-finished:
		if event.Error == nil {
			t.Fatal("reader panic was lost at transport completion")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reader panic leaked completion")
	}
	if closes.Load() != 1 {
		t.Fatal("panicking stream did not close exactly once", closes.Load())
	}
	reply, err = client.Get(base + "/wmts?SERVICE=WMTS&REQUEST=GetCapabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer reply.Body.Close()
	if reply.StatusCode != 200 {
		t.Fatal("panic leaked quota", reply.StatusCode)
	}
}

func start(t *testing.T, engine *server.Engine) string {
	t.Helper()
	app := fiber.New(fiber.Config{ReadBufferSize: 128 << 10, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	app.All("/wms", ogcHandler(engine, server.Endpoint{Protocol: owscore.WMS}))
	app.All("/wmts", ogcHandler(engine, server.Endpoint{Protocol: owscore.WMTS}))
	app.All("/ows", ogcHandler(engine, server.Endpoint{}))
	app.Use(ogcHandler(engine, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	return startApp(t, app)
}

func startApp(t *testing.T, app *fiber.App) string {
	t.Helper()
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

type panickingStream struct{ closes *atomic.Int32 }

func (*panickingStream) Read([]byte) (int, error) { panic("fixture reader panic") }

func (p *panickingStream) Close() error { p.closes.Add(1); return nil }
