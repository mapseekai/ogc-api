package wmts_test

import (
	"bytes"
	"context"
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
