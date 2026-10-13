package wmts_test

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestFixedResourcesUseCatalogMatrixLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields map[string]string
		info   bool
		want   bool
	}{
		{"valid", map[string]string{"TILEMATRIX": "L00"}, false, true},
		{"missing matrix", map[string]string{"TILEMATRIX": "missing"}, false, false},
		{"missing set", map[string]string{"TILEMATRIXSET": "missing"}, false, false},
		{"valid limits", map[string]string{"TILEMATRIX": "10m", "TILEROW": "1", "TILECOL": "1"}, false, true},
		{"outside layer limits", map[string]string{"TILEMATRIX": "10m", "TILEROW": "0"}, false, false},
		{"outside all matrices", map[string]string{"TILEROW": "100"}, false, false},
		{"invalid row", map[string]string{"TILEROW": "-1"}, false, false},
		{"pixel boundary", map[string]string{"TILEMATRIX": "10m", "I": "511", "J": "127"}, true, true},
		{"pixel overflow", map[string]string{"TILEMATRIX": "10m", "I": "512"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, catalog := demo.Catalogs()
			fixed := maps.Clone(tc.fields)
			op := owscore.GetTile
			pattern := "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}.png"
			fixed["FORMAT"] = "image/png"
			if tc.info {
				op = owscore.GetFeatureInfo
				pattern += "/{I}/{J}.json"
				delete(fixed, "FORMAT")
				fixed["INFOFORMAT"] = "application/json"
			}
			for _, slot := range []string{"TileMatrixSet", "TileMatrix", "TileRow", "TileCol", "I", "J"} {
				if _, ok := fixed[strings.ToUpper(slot)]; ok {
					pattern = strings.ReplaceAll(pattern, "{"+slot+"}", "fixed"+slot)
				}
			}
			router, err := wmts.NewRouter([]wmts.Template{{Pattern: pattern, Operation: op, Fixed: fixed}}, owscore.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(router.Resources("world", catalog, "https://example.test", true)) > 0; got != tc.want {
				t.Fatalf("advertised=%t want=%t", got, tc.want)
			}
		})
	}
}

func TestFixedMatrixRemovedFromLayerLimitsIsNotAdvertised(t *testing.T) {
	_, catalog := demo.Catalogs()
	delete(catalog.Layers["world"].MatrixSets["local"].Limits, "L00")
	router, err := wmts.NewRouter([]wmts.Template{{Pattern: "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileRow}/{TileCol}.png", Operation: owscore.GetTile, Fixed: map[string]string{"FORMAT": "image/png", "TILEMATRIX": "L00"}}}, owscore.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(router.Resources("world", catalog, "https://example.test", false)) != 0 {
		t.Fatal("unlisted matrix advertised")
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

func TestRESTCandidateCountDoesNotIncreaseAllocations(t *testing.T) {
	allocations := func(count int) float64 {
		templates := make([]wmts.Template, count)
		for i := range templates {
			templates[i] = wmts.Template{Pattern: fmt.Sprintf("/tiles/{Layer}/{TileMatrix}/{TileRow}/{TileCol}/r%d.png", i), Operation: owscore.GetTile, Fixed: map[string]string{"FORMAT": "image/png", "STYLE": "default", "TILEMATRIXSET": "local"}}
		}
		router, err := wmts.NewRouter(templates, owscore.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("/tiles/world/L00/0/0/r%d.png", count-1)
		return testing.AllocsPerRun(100, func() {
			if _, err := router.Match(path, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
	one, many := allocations(1), allocations(256)
	if many > one+2 {
		t.Fatalf("allocations grow with unmatched routes: one=%.1f many=%.1f", one, many)
	}
}

func FuzzRESTMatch(f *testing.F) {
	router, err := wmts.NewRouter([]wmts.Template{{Pattern: "/tiles/pre-{Layer}-end/{TileMatrix}/{TileCol}/{TileRow}.png", Operation: owscore.GetTile, Fixed: map[string]string{"STYLE": "default", "TILEMATRIXSET": "local", "FORMAT": "image/png"}}}, owscore.Limits{MaxQueryBytes: 4096, MaxPathBytes: 4096})
	if err != nil {
		f.Fatal(err)
	}
	for _, path := range []string{"/tiles/pre-A%2FB+C-end/L00/0/0.png", "/tiles/pre-%-end/L00/0/0.png", "/tiles/pre--end/L00/0/0.png", ""} {
		f.Add(path, "")
	}
	f.Fuzz(func(t *testing.T, path, query string) {
		r, err := router.Match(path, query)
		if err == nil && (r.Tile == nil || r.Tile.Layer == "") {
			t.Fatal("matched without tile identity")
		}
	})
}
