package wmts_test

import (
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wmts"
)

const query = "SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=Roads&STYLE=default&FORMAT=image/png&TILEMATRIXSET=local&TILEMATRIX=10m&TILEROW=2&TILECOL=3"

func TestKVP(t *testing.T) {
	r, e := wmts.Parse(query, owscore.Limits{})
	if e != nil || r.Tile.TileMatrix != "10m" || r.Tile.TileRow != 2 {
		t.Fatal(r, e)
	}
	for _, q := range []string{query + "&tilerow=1", strings.ReplaceAll(query, "TILEROW=2", "TILEROW=-1"), strings.ReplaceAll(query, "VERSION=1.0.0", "VERSION=1.3.0")} {
		if _, e := wmts.Parse(q, owscore.Limits{}); e == nil {
			t.Fatal("invalid request accepted")
		}
	}
	info := strings.ReplaceAll(query, "REQUEST=GetTile", "REQUEST=GetFeatureInfo") + "&I=4&J=5&INFOFORMAT=application/json"
	info = strings.ReplaceAll(info, "FORMAT=image/png&", "")
	if r, e := wmts.Parse(info, owscore.Limits{}); e != nil || r.FeatureInfo.InfoFormat != "application/json" {
		t.Fatal(r, e)
	}
	if _, e := wmts.Parse("SERVICE=WMTS&REQUEST=GetCapabilities&ACCEPTVERSIONS=1.3.0,1.0.0", owscore.Limits{}); e != nil {
		t.Fatal(e)
	}
	if _, e := wmts.Parse("SERVICE=WMTS&REQUEST=GetCapabilities&ACCEPTVERSIONS=1.3.0", owscore.Limits{}); e == nil {
		t.Fatal("bad negotiation accepted")
	}
}
func TestREST(t *testing.T) {
	router, e := wmts.NewRouter([]wmts.Template{{Pattern: "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileCol}/{TileRow}.png", Operation: owscore.GetTile, Fixed: map[string]string{"FORMAT": "image/png"}}}, owscore.Limits{})
	if e != nil {
		t.Fatal(e)
	}
	r, e := router.Match("/tiles/A%2FB+C/default/local/10m/3/2.png", "")
	if e != nil || r.Tile.Layer != "A/B+C" || r.Tile.TileRow != 2 {
		t.Fatal(r, e)
	}
	if _, e := router.Match("/tiles/Roads/default/local/10m/3/2.png", "TILEROW=0"); e == nil {
		t.Fatal("query overrides route")
	}
	if _, e := router.Match("/tiles/Roads/default/local/10m/3/2.png", "TILEROW=2&token=ignored"); e != nil {
		t.Fatal(e)
	}
	if _, e := wmts.NewRouter([]wmts.Template{{Pattern: "/x/{Layer}", Operation: owscore.GetTile}}, owscore.Limits{}); e == nil {
		t.Fatal("missing slots accepted")
	}
}
func TestRESTAmbiguity(t *testing.T) {
	base := wmts.Template{Pattern: "/tiles/{Layer}/{TileMatrix}/{TileCol}/{TileRow}.png", Operation: owscore.GetTile, Fixed: map[string]string{"STYLE": "default", "FORMAT": "image/png", "TILEMATRIXSET": "local"}}
	if _, e := wmts.NewRouter([]wmts.Template{base, base}, owscore.Limits{}); e == nil {
		t.Fatal("ambiguous templates accepted")
	}
}
func FuzzParse(f *testing.F) {
	f.Add(query)
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) { _, _ = wmts.Parse(s, owscore.Limits{MaxQueryBytes: 4096}) })
}

func TestResourceMetadataDoesNotAdvertiseUnavailableFixedStyle(t *testing.T) {
	router, err := wmts.NewRouter([]wmts.Template{{Pattern: "/tiles/{Layer}/{TileMatrix}/{TileRow}/{TileCol}.png", Operation: owscore.GetTile, Fixed: map[string]string{"STYLE": "missing", "TILEMATRIXSET": "local", "FORMAT": "image/png"}}}, owscore.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	_, catalog := demo.Catalogs()
	if len(router.Resources("world", catalog, "https://example.test", false)) != 0 {
		t.Fatal("unavailable style was advertised")
	}
}

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		if _, err := wmts.Parse(query, owscore.Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}
