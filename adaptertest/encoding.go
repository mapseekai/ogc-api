package adaptertest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wmts"
)

func testOwnedInput(t *testing.T, start Factory) {
	t.Helper()
	config, err := demo.Config("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	maps, tiles := demo.Catalogs()
	identifiers := []string{"Roads/中文+名称", "Roads%2F+literal", "world"}
	for _, identifier := range identifiers {
		tiles.Layers[identifier] = tiles.Layers["world"]
	}
	snapshot, err := server.NewSnapshot(&maps, &tiles)
	if err != nil {
		t.Fatal(err)
	}
	config.Catalog = server.Static(snapshot)
	var mu sync.Mutex
	var captured []*wmts.TileRequest
	config.Handlers.GetTile = func(_ context.Context, r *wmts.TileRequest) (response.Payload, error) {
		mu.Lock()
		captured = append(captured, r)
		mu.Unlock()
		return demo.PNG(), nil
	}
	engine, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	base := start(t, engine)
	client := &http.Client{Timeout: 5 * time.Second}
	const count = 18
	for i := 0; i < count; i++ {
		identifier := identifiers[i%len(identifiers)]
		target := fmt.Sprintf("%s/tiles/%s/default/local/L00/0/0.png?TRACE=entry-%d", base, url.PathEscape(identifier), i)
		result, _ := request(t, client, "GET", target)
		if result.StatusCode != 200 {
			t.Fatal("encoded resource identifier changed")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != count {
		t.Fatal(len(captured))
	}
	for i, r := range captured {
		trace, _, err := r.Parameters.One("TRACE")
		if err != nil || r.Layer != identifiers[i%len(identifiers)] || trace != fmt.Sprintf("entry-%d", i) {
			t.Fatalf("pooled request data changed after later requests: %+v", r)
		}
	}
}
