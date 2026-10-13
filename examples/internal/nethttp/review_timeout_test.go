package nethttp_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestReviewBusinessTimeoutReturnsXMLOverTCP(t *testing.T) {
	config, err := demo.Config("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	config.Timeout = 5 * time.Millisecond
	config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
		<-ctx.Done()
		return response.Payload{}, ctx.Err()
	}
	e, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	base := start(t, e)
	client := http.Client{Timeout: time.Second}
	result, err := client.Get(base + "/wmts?" + adaptertest.TileQuery)
	if err != nil {
		t.Fatalf("business timeout aborts connection before a complete XML response: %v", err)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil || !strings.Contains(string(data), "ExceptionReport") {
		t.Fatalf("status=%d bytes=%d read-error=%v, expected complete XML error", result.StatusCode, len(data), err)
	}
}
