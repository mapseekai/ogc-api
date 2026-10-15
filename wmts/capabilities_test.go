package wmts_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestUnknownFixedMatrixIsNotAdvertised(t *testing.T) {
	_, tiles := demo.Catalogs()
	router, err := wmts.NewRouter([]wmts.Template{{
		Pattern:   "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileRow}/{TileCol}.png",
		Operation: owscore.GetTile,
		Fixed:     map[string]string{"FORMAT": "image/png", "TILEMATRIX": "does-not-exist"},
	}}, owscore.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := wmts.EncodeCapabilities(tiles, wmts.CapabilitiesOptions{
		Endpoint: "https://example.test/wmts", PublicBaseURL: "https://example.test", Router: router,
	})
	if err != nil {
		t.Fatal(err)
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

func TestSelectedSectionsDoNotIncludeMetadataURL(t *testing.T) {
	_, catalog := demo.Catalogs()
	router, err := wmts.NewRouter([]wmts.Template{{Pattern: "/wmts/capabilities.xml", Operation: owscore.GetCapabilities}}, owscore.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sections := range [][]string{nil, {"All"}, {"Contents"}, {}} {
		data, err := wmts.EncodeCapabilities(catalog, wmts.CapabilitiesOptions{Endpoint: "https://example.invalid/wmts?", PublicBaseURL: "https://example.invalid", Router: router, Sections: sections})
		if err != nil {
			t.Fatal(err)
		}
		want := sections == nil || len(sections) == 1 && sections[0] == "All"
		if strings.Contains(string(data), "ServiceMetadataURL") != want {
			t.Fatalf("sections %v: %s", sections, data)
		}
	}
}

func TestAllAliasEncodesCompleteCapabilities(t *testing.T) {
	_, catalog := demo.Catalogs()
	var previous []byte
	for _, section := range []string{"All", "all"} {
		request, err := wmts.Parse("SERVICE=WMTS&REQUEST=GetCapabilities&SECTIONS="+section, owscore.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		data, err := wmts.EncodeCapabilities(catalog, wmts.CapabilitiesOptions{Endpoint: "https://example.invalid/wmts?", Sections: request.Capabilities.Sections})
		if err != nil || !bytes.Contains(data, []byte("<Contents>")) {
			t.Fatal(string(data), err)
		}
		if previous != nil && !bytes.Equal(previous, data) {
			t.Fatal("all alias changed the capabilities document")
		}
		previous = data
	}
}
