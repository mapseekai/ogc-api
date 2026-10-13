package wmts_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestMatrixLimitsAndDimensionKeywords(t *testing.T) {
	_, catalog := demo.Catalogs()
	layer := catalog.Layers["world"]
	layer.Dimensions = map[string]owscore.Dimension{"Time": {Default: "2026-01-01", Values: []string{"2026-01-01"}, Current: true}}
	catalog.Layers["world"] = layer
	if err := catalog.Check(); err != nil {
		t.Fatal(err)
	}
	q := "SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=world&STYLE=default&FORMAT=image/png&TILEMATRIXSET=local&TILEMATRIX=10m&TILEROW=1&TILECOL=1"
	for _, value := range []string{"", "&TIME=default", "&TIME=current"} {
		r, err := wmts.Parse(q+value, owscore.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		matrix, err := catalog.ValidateTile(context.Background(), r.Tile)
		if err != nil {
			t.Fatal(err)
		}
		if matrix.MatrixWidth != 5 || matrix.TileHeight != 128 {
			t.Fatal("real matrix metadata was ignored")
		}
		if value != "&TIME=current" && r.Tile.Dimensions["Time"] != "2026-01-01" {
			t.Fatal(r.Tile.Dimensions)
		}
	}
	for _, bad := range []string{strings.ReplaceAll(q, "TILEROW=1", "TILEROW=0"), strings.ReplaceAll(q, "TILECOL=1", "TILECOL=4"), q + "&TIME=invalid"} {
		r, err := wmts.Parse(bad, owscore.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := catalog.ValidateTile(context.Background(), r.Tile); err == nil {
			t.Fatal("invalid matrix/dimension accepted")
		}
	}
}
