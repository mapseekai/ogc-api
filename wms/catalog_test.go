package wms_test

import (
	"context"
	"math"
	"testing"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wms"
)

func TestPerLayerDimensionDefaults(t *testing.T) {
	catalog := wms.Catalog{Formats: []string{"image/png"}, Layers: map[string]wms.Layer{}}
	for _, name := range []string{"a", "b"} {
		catalog.Layers[name] = wms.Layer{CRS: []string{"EPSG:4326"}, Dimensions: map[string]owscore.Dimension{"time": {Default: name, Values: []string{"a", "b"}}}}
	}
	r, err := wms.Parse(mapQuery("1.1.1"), wms.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.ValidateMap(context.Background(), r.Map); err != nil {
		t.Fatal(err)
	}
	if r.Map.LayerDimensions["a"]["time"] != "a" || r.Map.LayerDimensions["b"]["time"] != "b" {
		t.Fatal(r.Map.LayerDimensions)
	}
}

func TestInvalidHandBuiltMapDoesNotPanic(t *testing.T) {
	if err := (wms.Catalog{}).ValidateMap(context.Background(), &wms.MapRequest{Layers: []string{"world"}}); err == nil {
		t.Fatal("incomplete model accepted")
	}
}

func TestBoundingBoxesValidateAndDetach(t *testing.T) {
	c := wms.Catalog{Service: owscore.Service{Title: "test"}, Formats: []string{"image/png"}, Layers: map[string]wms.Layer{"a": {Title: "A", CRS: []string{"EPSG:3857"}, GeographicBounds: [4]float64{-1, -1, 1, 1}, BoundingBoxes: map[string][4]float64{"EPSG:3857": {0, 0, 1, 1}}}}}
	if err := c.Check(); err != nil {
		t.Fatal(err)
	}
	clone := c.Clone()
	layer := c.Layers["a"]
	layer.BoundingBoxes["EPSG:3857"] = [4]float64{0, 0, 2, 2}
	if clone.Layers["a"].BoundingBoxes["EPSG:3857"][2] != 1 {
		t.Fatal("clone aliases mutable metadata")
	}
	for _, b := range [][4]float64{{0, 0, 0, 1}, {0, 0, math.NaN(), 1}, {0, 0, math.Inf(1), 1}} {
		layer.BoundingBoxes["EPSG:3857"] = b
		if err := c.Check(); err == nil {
			t.Fatalf("invalid extent accepted: %v", b)
		}
	}
}
