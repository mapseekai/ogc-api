package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/response"
)

func TestMapAndCoverageEndToEnd(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	var rendered maps.Request
	c.Collections[0].Map = &maps.Service{Formats: []string{"image/png"}, Renderer: maps.RendererFunc(func(_ context.Context, request maps.Request) (maps.Result, error) {
		rendered = request
		return maps.Result{Content: response.Bytes("image/png", []byte("map")), Width: request.Width, Height: request.Height, BBox: request.BBox, CRS: request.CRS}, nil
	})}
	var covered coverages.Request
	c.Collections[0].Coverage = &coverages.Service{Formats: []string{"image/tiff"}, Axes: []string{"x"}, Properties: []string{"temperature"}, Schema: json.RawMessage(`{"type":"object"}`), Reader: coverages.ReaderFunc(func(_ context.Context, request coverages.Request) (response.Payload, error) {
		covered = request
		return response.Bytes("image/tiff", []byte("coverage")), nil
	})}
	e := engine(t, c, 8)
	status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/map", RawQuery: "bbox=0,0,10,10&width=100&height=100"})
	if status != 200 || string(data) != "map" || rendered.Layers[0].ID != "roads" || headers["Content-Bbox"] != "0,0,10,10" {
		t.Fatal(status, string(data), headers)
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/coverage", RawQuery: "subset=x(1:2)&properties=temperature"})
	if status != 200 || string(data) != "coverage" || covered.Subsets[0].Axis != "x" || covered.CollectionID != "roads" {
		t.Fatal(status, string(data), covered)
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/schema"})
	if status != 200 || !json.Valid(data) {
		t.Fatal(status, string(data))
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	if status != 200 || strings.Contains(string(data), "coverages") || strings.Contains(string(data), "maps-1") {
		t.Fatal("profile routes must not claim unimplemented conformance", string(data))
	}
}
