package wmts_test

import (
	"strings"
	"testing"

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
		t.Run(q, func(t *testing.T) {
			if _, e := wmts.Parse(q, owscore.Limits{}); e == nil {
				t.Fatal("invalid request accepted")
			}

		})
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

func FuzzParse(f *testing.F) {
	f.Add(query)
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) { _, _ = wmts.Parse(s, owscore.Limits{MaxQueryBytes: 4096}) })
}

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		if _, err := wmts.Parse(query, owscore.Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}
