package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/server"
)

func exampleServer(t *testing.T) *httptest.Server {
	t.Helper()
	host := httptest.NewUnstartedServer(nil)
	snapshot, config, err := publications("http://" + host.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	apiEngine, err := api.New(api.Config{Catalog: api.Static(snapshot)})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	host.Config.Handler = combinedRouter(apiEngine, legacy)
	host.Start()
	t.Cleanup(host.Close)
	return host
}

func fetch(t *testing.T, host *httptest.Server, method, path, body string) (int, []byte, http.Header) {
	t.Helper()
	request, err := http.NewRequest(method, host.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", common.JSON)
	}
	reply, err := host.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reply.Body)
	closeErr := reply.Body.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return reply.StatusCode, data, reply.Header
}

func TestCombinedExampleRealTCP(t *testing.T) {
	host := exampleServer(t)
	status, data, _ := fetch(t, host, "GET", "/ogc/", "")
	if status != 200 || !json.Valid(data) {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "GET", "/ogc/collections/world/items?bbox=-1,-1,1,1", "")
	if status != 200 || !strings.Contains(string(data), `"numberReturned":1`) {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "GET", "/ogc/collections/world/items?bbox=10,10,20,20", "")
	if status != 200 || !strings.Contains(string(data), `"numberReturned":0`) {
		t.Fatal(status, string(data))
	}
	status, tile, _ := fetch(t, host, "GET", "/ogc/collections/world/tiles/local/10m/1/1", "")
	if status != 200 {
		t.Fatal(status, string(tile))
	}
	dimensions, err := png.DecodeConfig(bytes.NewReader(tile))
	if err != nil {
		t.Fatal(err)
	}
	if dimensions.Width != 512 || dimensions.Height != 128 {
		t.Fatal(dimensions)
	}
	status, legacy, _ := fetch(t, host, "GET", "/wmts?SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=world&STYLE=default&FORMAT=image/png&TILEMATRIXSET=local&TILEMATRIX=10m&TILEROW=1&TILECOL=1", "")
	if status != 200 || !bytes.Equal(tile, legacy) {
		t.Fatal("WMTS and OGC API Tiles did not share the provider", status)
	}
	status, data, headers := fetch(t, host, "GET", "/ogc/collections/world/map?bbox=-10,-5,10,5&width=100&height=50", "")
	if status != 200 || headers.Get("Content-Bbox") != "-10,-5,10,5" {
		t.Fatal(status, headers, string(data))
	}
	dimensions, err = png.DecodeConfig(bytes.NewReader(data))
	if err != nil || dimensions.Width != 100 || dimensions.Height != 50 {
		t.Fatal(dimensions, err)
	}
	status, data, _ = fetch(t, host, "GET", "/ogc/collections/world/coverage?subset=x(0:1)", "")
	if status != 200 || !json.Valid(data) {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"ready"},"response":"document"}`)
	if status != 200 || string(data) != `{"result":"ready"}` {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "HEAD", "/ogc/collections/world/items", "")
	if status != 200 || len(data) != 0 {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "GET", "/ogc/api", "")
	if status != 200 {
		t.Fatal(status, string(data))
	}
	var description struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(data, &description); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/collections/{collectionId}/items", "/collections/{collectionId}/map", "/collections/{collectionId}/coverage", "/processes/{processId}/execution"} {
		if _, ok := description.Paths[path]; !ok {
			t.Fatal("missing OpenAPI path", path)
		}
	}
}
