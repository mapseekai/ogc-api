package nethttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/examples/internal/nethttp"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wmts"
)

func start(t *testing.T, engine *server.Engine) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/wms", nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMS}))
	mux.Handle("/wmts", nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMTS}))
	mux.Handle("/ows", nethttp.Handler(engine, server.Endpoint{}))
	mux.Handle("/", nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s.URL
}
func TestContracts(t *testing.T) { adaptertest.Run(t, start) }

func TestPanickingReaderStillCloses(t *testing.T) {
	config, err := demo.Config("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	body := &panicBody{}
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		return response.Stream("image/png", body, -1), nil
	}
	engine, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Error("host did not receive the Reader panic")
		}
		if body.closed != 1 {
			t.Errorf("body closed %d times", body.closed)
		}
	}()
	nethttp.Handler(engine, server.Endpoint{Protocol: owscore.WMTS})(
		httptest.NewRecorder(), httptest.NewRequest("GET", "/wmts?"+adaptertest.TileQuery, nil))
}

type panicBody struct{ closed int }

func (*panicBody) Read([]byte) (int, error) { panic("reader failed") }
func (b *panicBody) Close() error           { b.closed++; return nil }

func TestAPIContracts(t *testing.T) {
	adaptertest.RunAPI(t, func(t *testing.T, engine *api.Engine) string {
		host := httptest.NewServer(nethttp.APIHandler(engine, "public"))
		t.Cleanup(host.Close)
		return host.URL
	})
}
