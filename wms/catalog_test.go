package wms_test

import (
	"context"
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
