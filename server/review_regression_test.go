package server_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestReviewTimeoutExceptionRemainsReadable(t *testing.T) {
	config := makeConfig(t)
	config.Timeout = time.Millisecond
	config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
		<-ctx.Done()
		return response.Payload{}, ctx.Err()
	}
	plan, err := engine(t, config).Handle(context.Background(), server.Input{
		Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(plan.Body)
	_ = plan.Finish(int64(len(data)), readErr)
	if readErr != nil || !bytes.Contains(data, []byte("ExceptionReport")) {
		t.Fatalf("generated error plan cannot be sent: status=%d declared-length=%d bytes=%d read-error=%v", plan.Status, plan.Length, len(data), readErr)
	}
}

type reviewBody struct {
	io.Reader
	closes int
}

func (b *reviewBody) Close() error { b.closes++; return nil }

func TestReviewObserverPanicReleasesTransferredBody(t *testing.T) {
	for _, stage := range []string{"business", "handoff"} {
		t.Run(stage, func(t *testing.T) {
			config := makeConfig(t)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := &reviewBody{Reader: strings.NewReader("tile")}
			var callbackCtx context.Context
			config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
				callbackCtx = ctx
				return response.Stream("image/png", body, 4), nil
			}
			config.Observer = func(event server.Event) {
				if event.Stage == stage {
					panic("observer fixture panic")
				}
			}
			e := engine(t, config)
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				plan, err := e.Handle(parent, server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
				if plan != nil {
					_ = plan.Finish(0, err)
				}
			}()
			if recovered == nil {
				t.Fatal("expected observer panic to reach the host")
			}
			closed := body.closes
			ctxErr := callbackCtx.Err()
			if body.closes == 0 {
				_ = body.Close()
			}
			if closed != 1 || ctxErr == nil {
				t.Fatalf("resource ownership lost after %s observer panic: closes=%d context-error=%v", stage, closed, ctxErr)
			}
		})
	}
}

func TestReviewUnknownFixedMatrixIsNotAdvertised(t *testing.T) {
	_, tiles := demo.Catalogs()
	router, err := wmts.NewRouter([]wmts.Template{{
		Pattern:   "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileRow}/{TileCol}.png",
		Operation: owscore.GetTile,
		Fixed:     map[string]string{"FORMAT": "image/png", "TILEMATRIX": "does-not-exist"},
	}}, owscore.Limits{})
	if err != nil {
		return
	} // Rejecting an impossible fixed route is also safe.
	data, err := wmts.EncodeCapabilities(tiles, wmts.CapabilitiesOptions{
		Endpoint: "https://example.test/wmts", PublicBaseURL: "https://example.test", Router: router,
	})
	if err != nil {
		return
	}
	request, err := router.Match("/tiles/world/default/local/0/0.png", "")
	if err != nil {
		t.Fatal(err)
	}
	_, tileErr := tiles.ValidateTile(context.Background(), request.Tile)
	if tileErr == nil {
		t.Fatal("fixture should identify an unknown matrix")
	}
	if bytes.Contains(data, []byte("ResourceURL")) {
		t.Fatalf("capabilities advertises an unusable route: fixed-matrix=%q request-error=%v", request.Tile.TileMatrix, tileErr)
	}
}

func TestReviewDimensionPolicyMatchesREST(t *testing.T) {
	_, tiles := demo.Catalogs()
	layer := tiles.Layers["world"]
	layer.Dimensions = map[string]owscore.Dimension{
		"SECTIONS": {Default: "normal", Values: []string{"normal"}},
	}
	tiles.Layers["world"] = layer
	if err := tiles.Check(); err != nil {
		return
	}
	_, err := wmts.NewRouter([]wmts.Template{{
		Pattern:    "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}/{SECTIONS}.png",
		Operation:  owscore.GetTile,
		Dimensions: []string{"SECTIONS"},
		Fixed:      map[string]string{"FORMAT": "image/png"},
	}}, owscore.Limits{})
	if err != nil {
		t.Fatalf("catalog accepts a dimension rejected by REST: %v", err)
	}
}

func BenchmarkReviewRESTRouteScale(b *testing.B) {
	for _, count := range []int{1, 16, 256} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			templates := make([]wmts.Template, count)
			for i := range templates {
				templates[i] = wmts.Template{
					Pattern:   fmt.Sprintf("/r%d/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}.png", i),
					Operation: owscore.GetTile, Fixed: map[string]string{"FORMAT": "image/png"},
				}
			}
			router, err := wmts.NewRouter(templates, owscore.Limits{})
			if err != nil {
				b.Fatal(err)
			}
			path := fmt.Sprintf("/r%d/world/default/local/L00/0/0.png", count-1)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := router.Match(path, ""); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReviewLayerLimitAllocations(b *testing.B) {
	for _, count := range []int{1, 65, 30000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			layers := strings.TrimSuffix(strings.Repeat("a,", count), ",")
			query := "SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=" + layers + "&STYLES=&CRS=CRS:84&BBOX=0,0,1,1&WIDTH=1&HEIGHT=1&FORMAT=image/png"
			b.ReportMetric(float64(len(query)), "query-bytes")
			b.ReportAllocs()
			for b.Loop() {
				_, err := wms.Parse(query, wms.Options{})
				if count > 64 && err == nil {
					b.Fatal("expected layer limit rejection")
				}
				if count <= 64 && err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReviewEngineGetTile(b *testing.B) {
	config, err := demo.Config("https://example.test")
	if err != nil {
		b.Fatal(err)
	}
	e, err := server.New(config)
	if err != nil {
		b.Fatal(err)
	}
	input := server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery}
	b.ReportAllocs()
	for b.Loop() {
		p, err := e.Handle(context.Background(), input)
		if err != nil {
			b.Fatal(err)
		}
		n, err := io.Copy(io.Discard, p.Body)
		if err := p.Finish(n, err); err != nil {
			b.Fatal(err)
		}
	}
}
