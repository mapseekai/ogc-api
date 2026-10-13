package server_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestErrorResponseHonorsParentCancellation(t *testing.T) {
	config := makeConfig(t)
	config.Timeout = time.Millisecond
	body := &reviewBody{Reader: strings.NewReader("discarded")}
	config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
		<-ctx.Done()
		return response.Stream("image/png", body, -1), ctx.Err()
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan, err := engine(t, config).Handle(parent, server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	data, readErr := io.ReadAll(plan.Body)
	finishErr := plan.Finish(int64(len(data)), readErr)
	if len(data) != 0 || !errors.Is(readErr, context.Canceled) || !errors.Is(finishErr, context.Canceled) || body.closes != 1 {
		t.Fatalf("parent cancellation ignored: bytes=%d read=%v finish=%v closes=%d", len(data), readErr, finishErr, body.closes)
	}
}

func TestFinishObserverPanicStillCancelsAndCloses(t *testing.T) {
	config := makeConfig(t)
	body := &reviewBody{Reader: strings.NewReader("tile")}
	var callbackCtx context.Context
	panicValue := errors.New("finish observer panic")
	config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
		callbackCtx = ctx
		return response.Stream("image/png", body, 4), nil
	}
	config.Observer = func(e server.Event) {
		if e.Stage == "finish" {
			panic(panicValue)
		}
	}
	plan, err := engine(t, config).Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if got := recover(); got != panicValue {
				t.Errorf("panic changed: %v", got)
			}
		}()
		_ = plan.Finish(0, nil)
	}()
	if body.closes != 1 || callbackCtx.Err() != context.Canceled {
		t.Fatalf("closes=%d context=%v", body.closes, callbackCtx.Err())
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if body.closes != 1 {
		t.Fatal("duplicate close")
	}
}

func TestStartObserverPanicDoesNotInvokeProvider(t *testing.T) {
	config := makeConfig(t)
	called := false
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		called = true
		return response.Payload{}, nil
	}
	panicValue := errors.New("start observer panic")
	config.Observer = func(e server.Event) {
		if e.Stage == "start" {
			panic(panicValue)
		}
	}
	e := engine(t, config)
	defer func() {
		if got := recover(); got != panicValue {
			t.Errorf("panic changed: %v", got)
		}
		if called {
			t.Error("provider invoked after start panic")
		}
	}()
	_, _ = e.Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
}
