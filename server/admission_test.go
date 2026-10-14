package server_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wmts"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/server"
)

func TestAdmissionHeldUntilFinish(t *testing.T) {
	config := makeConfig(t)
	config.MaxConcurrent = 1
	e := engine(t, config)
	input := server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery}
	first, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != 503 || second.Headers["Retry-After"] != "1" {
		t.Fatal(second.Status, second.Headers)
	}
	readPlan(t, second)
	if err := first.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if err := first.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	status, _ := call(t, e, input)
	if status != 200 {
		t.Fatal("admission slot not released")
	}
}

func TestAdmissionValidation(t *testing.T) {
	config := makeConfig(t)
	config.MaxConcurrent = -1
	if _, err := server.New(config); err == nil {
		t.Fatal("accepted negative concurrency")
	}
}

type panicCloseBody struct{ io.Reader }

func (b *panicCloseBody) Close() error { panic("foreign close panic") }

func TestClosePanicReleasesAdmission(t *testing.T) {
	config := makeConfig(t)
	config.MaxConcurrent = 1
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		return response.Stream("image/png", &panicCloseBody{Reader: strings.NewReader("tile")}, 4), nil
	}
	e := engine(t, config)
	plan, err := e.Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("close panic was swallowed")
			}
		}()
		_ = plan.Finish(0, nil)
	}()
	next, err := e.Handle(context.Background(), capabilities(owscore.WMTS, "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != 200 {
		t.Fatal("close panic leaked admission slot")
	}
	readPlan(t, next)
}
