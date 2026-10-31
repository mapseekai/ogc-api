package features_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func TestOutputGeometryUsesDeclaredCRS(t *testing.T) {
	for _, tc := range []struct {
		uri, raw string
		bbox     []float64
		valid    bool
	}{
		{testEPSG3857, `{"type":"Point","coordinates":[13358338.895192828,5621521.486192066]}`, []float64{1e7, 5e6, 2e7, 6e6}, true},
		{testEPSG4326, `{"type":"Point","coordinates":[45,120]}`, []float64{40, 110, 50, 130}, true},
		{testEPSG4326, `{"type":"Point","coordinates":[120,45]}`, nil, false},
		{testEPSG4979, `{"type":"Point","coordinates":[45,120,123]}`, []float64{40, 110, -10, 50, 130, 200}, true},
		{testEPSG4979, `{"type":"Point","coordinates":[45,120]}`, nil, false},
		{common.CRS84h, `{"type":"Point","coordinates":[120,45,123]}`, nil, true},
		{common.CRS84, `{"type":"Point","coordinates":[120,45,123]}`, nil, false},
		{common.CRS84, `{"type":"Point","coordinates":[120,45]}`, []float64{40, 1, 30, 2}, true},
		{testEPSG3857, `{"type":"Point","coordinates":[1,2]}`, []float64{2, 0, 1, 1}, false},
		{testEPSG3857, `{"type":"Point","coordinates":[1,2],"bbox":[2,0,1,1]}`, nil, false},
		{testEPSG3857, `{"type":"LineString","coordinates":[[1000000,2000000],[3000000,4000000]]}`, nil, true},
		{common.CRS84, `{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[1,2]},{"type":"Point","coordinates":[3,4]}]}`, nil, true},
		{common.CRS84, `{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[1,2,3]}]}`, nil, false},
		{common.CRS84, `{"type":"Point","coordinates":[]}`, nil, true},
		{common.CRS84, `{"type":"LineString","coordinates":[]}`, nil, true},
		{common.CRS84, `{"type":"Polygon","coordinates":[]}`, nil, true},
		{common.CRS84, `{"type":"GeometryCollection","geometries":[]}`, nil, true},
		{common.CRS84, `{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[]}]}`, nil, true},
		{common.CRS84, `{"type":"MultiPoint","coordinates":[[]]}`, nil, false},
		{common.CRS84, `{"type":"LineString","coordinates":[[1,2],[]]}`, nil, false},
		{common.CRS84, `{"type":"Polygon","coordinates":[[]]}`, nil, false},
		{common.CRS84, `{"type":"MultiPolygon","coordinates":[[[]]]}`, nil, false},
		{common.CRS84, `{"type":"GeometryCollection","geometries":[{"type":"MultiPoint","coordinates":[[]]}]}`, nil, false},
		{common.CRS84, `{"type":"GeometryCollection"}`, nil, false},
		{common.CRS84, `{"type":"Point","coordinates":[null,2]}`, nil, false},
		{common.CRS84, `{"type":"Point","coordinates":["1",2]}`, nil, false},
		{common.CRS84, `{"type":"Point","coordinates":[true,2]}`, nil, false},
		{common.CRS84, `{"type":"Point","coordinates":null}`, nil, false},
		{testEPSG3857, `{"type":"Point","coordinates":[1e400,2]}`, nil, false},
		{common.CRS84, `{"type":"Unknown","coordinates":[1,2]}`, nil, false},
	} {
		t.Run(tc.uri+tc.raw, func(t *testing.T) {
			feature := features.Feature{CRS: featureCRS(t, tc.uri), Geometry: json.RawMessage(tc.raw), BBox: tc.bbox}
			data, err := features.EncodeFeature(feature, 4096)
			if (err == nil) != tc.valid {
				t.Fatal(string(data), err)
			}
			if tc.valid && strings.Contains(string(data), `"crs"`) {
				t.Fatal("invented deprecated GeoJSON crs member", string(data))
			}
		})
	}
}

func TestOutputGeometryNestingIsBounded(t *testing.T) {
	raw := `{"type":"Point","coordinates":[1,2]}`
	for range 40 {
		raw = `{"type":"GeometryCollection","geometries":[` + raw + `]}`
	}
	if _, err := features.EncodeFeature(features.Feature{CRS: featureCRS(t, common.CRS84), Geometry: json.RawMessage(raw)}, 65536); err == nil {
		t.Fatal("unbounded output geometry recursion")
	}
}
