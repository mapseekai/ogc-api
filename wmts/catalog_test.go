package wmts_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestFeatureInfoTileFormatIsValidatedWhenPresent(t *testing.T) {
	_, catalog := demo.Catalogs()
	q := "SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetFeatureInfo&LAYER=world&STYLE=default&FORMAT=invalid&TILEMATRIXSET=local&TILEMATRIX=10m&TILEROW=1&TILECOL=1&I=0&J=0&INFOFORMAT=application/json"
	r, err := wmts.Parse(q, owscore.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	var protocolError *owscore.Error
	if err := catalog.ValidateInfo(context.Background(), r.FeatureInfo); !errors.As(err, &protocolError) || protocolError.Kind != owscore.InvalidFormat || protocolError.Locator != "FORMAT" {
		t.Fatalf("invalid queried tile format: %v", err)
	}
	for _, format := range []string{"image/png", ""} {
		r.FeatureInfo.Format = format
		if err := catalog.ValidateInfo(context.Background(), r.FeatureInfo); err != nil {
			t.Fatal("valid KVP format or REST information resource rejected", err)
		}
	}
}

func TestDimensionPolicyMatchesREST(t *testing.T) {
	_, tiles := demo.Catalogs()
	layer := tiles.Layers["world"]
	layer.Dimensions = map[string]owscore.Dimension{
		"SECTIONS": {Default: "normal", Values: []string{"normal"}},
	}
	tiles.Layers["world"] = layer
	catalogErr := tiles.Check()
	_, err := wmts.NewRouter([]wmts.Template{{
		Pattern:    "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}/{SECTIONS}.png",
		Operation:  owscore.GetTile,
		Dimensions: []string{"SECTIONS"},
		Fixed:      map[string]string{"FORMAT": "image/png"},
	}}, owscore.Limits{})
	if catalogErr == nil || err == nil {
		t.Fatalf("reserved dimension must be rejected by catalog and REST: catalog=%v router=%v", catalogErr, err)
	}
}

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
		t.Run("dimension_"+value, func(t *testing.T) {
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

		})
	}
	for _, bad := range []string{strings.ReplaceAll(q, "TILEROW=1", "TILEROW=0"), strings.ReplaceAll(q, "TILECOL=1", "TILECOL=4"), q + "&TIME=invalid"} {
		t.Run("invalid_"+bad, func(t *testing.T) {
			r, err := wmts.Parse(bad, owscore.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := catalog.ValidateTile(context.Background(), r.Tile); err == nil {
				t.Fatal("invalid matrix/dimension accepted")
			}

		})
	}
}

func TestDimensionReservedNamesAgreeAcrossCatalogAndREST(t *testing.T) {
	for _, name := range []string{"SERVICE", "sections", "AcceptVersions", "ACCEPTFORMATS", "UPDATESEQUENCE", "I", "time"} {
		t.Run(name, func(t *testing.T) {
			_, catalog := demo.Catalogs()
			layer := catalog.Layers["world"]
			layer.Dimensions = map[string]owscore.Dimension{name: {Default: "normal", Values: []string{"normal"}}}
			catalog.Layers["world"] = layer
			catalogErr := catalog.Check()
			_, routerErr := wmts.NewRouter([]wmts.Template{{Pattern: "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}.png", Operation: owscore.GetTile, Dimensions: []string{name}, Fixed: map[string]string{"FORMAT": "image/png"}}}, owscore.Limits{})
			wantValid := name == "time"
			if (catalogErr == nil) != wantValid || (routerErr == nil) != wantValid {
				t.Fatalf("catalog=%v router=%v valid=%t", catalogErr, routerErr, wantValid)
			}
		})
	}
}
