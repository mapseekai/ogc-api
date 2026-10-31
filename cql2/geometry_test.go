package cql2_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
)

func TestGeometryLiteralParity(t *testing.T) {
	cases := []struct{ name, text, json string }{
		{"point3d", `POINT Z (1 2 3.00)`, `{"type":"Point","coordinates":[1,2,3]}`},
		{"line", `LINESTRING(1 2,3 4)`, `{"type":"LineString","coordinates":[[1,2],[3,4]]}`},
		{"multi_point", `MULTIPOINT((1 2),(3 4))`, `{"type":"MultiPoint","coordinates":[[1,2],[3,4]]}`},
		{"multi_line", `MULTILINESTRING((1 2,3 4),(2 3,4 5))`, `{"type":"MultiLineString","coordinates":[[[1,2],[3,4]],[[2,3],[4,5]]]}`},
		{"multi_polygon", `MULTIPOLYGON(((0 0,1 0,1 1,0 0)))`, `{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]]]]}`},
		{"collection", `GEOMETRYCOLLECTION(POINT(1 2),LINESTRING(1 2,3 4))`, `{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[1,2]},{"type":"LineString","coordinates":[[1,2],[3,4]]}]}`},
		{"bbox3d", `BBOX(170,-10,-100,-170,10,100)`, `{"bbox":[170,-10,-100,-170,10,100]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := cql2.Parse(`S_INTERSECTS(geom,`+tc.text+`)`, cql2.Text, profile())
			if err != nil {
				t.Fatal(err)
			}
			b, err := cql2.Parse(`{"op":"s_intersects","args":[{"property":"geom"},`+tc.json+`]}`, cql2.JSON, profile())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatal("different geometry representations", a, b)
			}
		})
	}
}

func TestDeclaredGeometryCRSNativeAxesAndPrecision(t *testing.T) {
	for _, tc := range []struct {
		uri, text, json string
		valid           bool
	}{
		{"http://www.opengis.net/def/crs/EPSG/0/4326", `POINT(45 125)`, `{"type":"Point","coordinates":[45,125]}`, true},
		{"http://www.opengis.net/def/crs/EPSG/0/4326", `POINT(125 45)`, `{"type":"Point","coordinates":[125,45]}`, false},
		{"http://www.opengis.net/def/crs/EPSG/0/4326", `BBOX(-10,170,10,-170)`, `{"bbox":[-10,170,10,-170]}`, true},
		{"http://www.opengis.net/def/crs/EPSG/0/4326", `BBOX(10,1,-10,2)`, `{"bbox":[10,1,-10,2]}`, false},
		{"http://www.opengis.net/def/crs/EPSG/0/3857", `POINT(9007199254740993.0000000001 5621521.486192066)`, `{"type":"Point","coordinates":[9007199254740993.0000000001,5621521.486192066]}`, true},
		{"http://www.opengis.net/def/crs/EPSG/0/3857", `BBOX(1000000,2000000,3000000,4000000)`, `{"bbox":[1000000,2000000,3000000,4000000]}`, true},
		{"http://www.opengis.net/def/crs/EPSG/0/3857", `BBOX(3000000,2000000,1000000,4000000)`, `{"bbox":[3000000,2000000,1000000,4000000]}`, false},
		{"http://www.opengis.net/def/crs/EPSG/0/4979", `POINT(45 125 100)`, `{"type":"Point","coordinates":[45,125,100]}`, true},
		{"http://www.opengis.net/def/crs/EPSG/0/4979", `POINT(45 125)`, `{"type":"Point","coordinates":[45,125]}`, false},
	} {
		t.Run(tc.uri+tc.text, func(t *testing.T) {
			definition, ok := common.CRSByURI(tc.uri)
			if !ok {
				t.Fatal(tc.uri)
			}
			options := profile()
			options.CRS, options.CRSDefinition = tc.uri, definition
			a, textErr := cql2.Parse(`S_INTERSECTS(geom,`+tc.text+`)`, cql2.Text, options)
			b, jsonErr := cql2.Parse(`{"op":"s_intersects","args":[{"property":"geom"},`+tc.json+`]}`, cql2.JSON, options)
			if (textErr == nil) != tc.valid || (jsonErr == nil) != tc.valid {
				t.Fatal(textErr, jsonErr)
			}
			if tc.valid && !reflect.DeepEqual(a, b) {
				t.Fatal("declared CRS encodings diverged", a, b)
			}
			if tc.valid {
				geometry := a.Args[1].Literal.Geometry
				options.CRSDefinition.URI = "changed"
				if geometry.CRS != tc.uri || geometry.CRSDefinition != definition {
					t.Fatal("retained CRS metadata changed", geometry)
				}
			}
		})
	}
	options := profile()
	options.CRS = "http://www.opengis.net/def/crs/EPSG/0/3857"
	if options.Validate() == nil {
		t.Fatal("non-default CRS accepted without axis/dimension declaration")
	}
	options.CRSDefinition, _ = common.CRSByURI(common.CRS84)
	if options.Validate() == nil {
		t.Fatal("mismatched CRS metadata accepted")
	}
}

func TestGeometryValidation(t *testing.T) {
	for _, geometry := range []string{`POINT(181 0)`, `POINT(0 -91)`, `POINT(1)`, `POINT(1 2 3 4)`, `POINT Z(1 2)`, `POINT(1-2)`, `LINESTRING(1 2)`, `LINESTRING(1 2,3 4 5)`, `POLYGON((0 0,1 0,1 1))`, `POLYGON((0 0,1 0,1 1,2 2))`, `BBOX(0,10,1,-10)`, `BBOX(0,0,10,1,1,-10)`, `GEOMETRYCOLLECTION(POINT(1 2),POINT(1 2 3))`, `POINT EMPTY`, `MULTIPOINT(1 2,3 4)`, `GEOMETRYCOLLECTION(POINT(1 2))`, `GEOMETRYCOLLECTION(POINT(1 2),BBOX(0,0,1,1))`, `GEOMETRYCOLLECTION(POINT(1 2),GEOMETRYCOLLECTION(POINT(1 2),POINT(3 4)))`, `BBOX(1,2,3)`, `POLYGON(())`} {
		t.Run(geometry, func(t *testing.T) {
			if _, err := cql2.Parse(`S_INTERSECTS(geom,`+geometry+`)`, cql2.Text, profile()); !errors.Is(err, cql2.ErrInvalid) {
				t.Fatal("invalid geometry accepted", err)
			}
		})
	}
	for _, geometry := range []string{`{"type":"Point","coordinates":[1,2],"crs":"EPSG:3857"}`, `{"type":"point","coordinates":[1,2]}`, `{"type":"Point","coordinates":["1",2]}`, `{"type":"Point","coordinates":[1,2],"bbox":[0,0,1,1,2,2]}`, `{"bbox":[1,2,3]}`, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[2,2]]]}`, `{"type":"GeometryCollection","geometries":[]}`} {
		if _, err := cql2.Parse(`{"op":"s_intersects","args":[{"property":"geom"},`+geometry+`]}`, cql2.JSON, profile()); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatal("invalid JSON geometry accepted", err)
		}
	}
}

func TestGeometryCRSDefaultsAndExplicitDimensions(t *testing.T) {
	for _, tc := range []struct {
		input, crs string
		valid      bool
		expected   string
	}{
		{`POINT(1 2)`, "", true, common.CRS84}, {`POINT(1 2 3)`, "", true, common.CRS84h},
		{`POINT(1 2)`, common.CRS84, true, common.CRS84}, {`POINT(1 2 3)`, common.CRS84h, true, common.CRS84h},
		{`POINT(1 2)`, common.CRS84h, false, ""}, {`POINT(1 2 3)`, common.CRS84, false, ""},
		{`POINT(1 2)`, "http://www.opengis.net/def/crs/EPSG/0/3857", false, ""},
	} {
		t.Run(tc.input+tc.crs, func(t *testing.T) {
			o := profile()
			o.CRS = tc.crs
			e, err := cql2.Parse(`S_INTERSECTS(geom,`+tc.input+`)`, cql2.Text, o)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if err == nil && e.Args[1].Literal.Geometry.CRS != tc.expected {
				t.Fatal(e.Args[1].Literal.Geometry.CRS)
			}
		})
	}
}
