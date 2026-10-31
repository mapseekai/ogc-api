package features_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
)

const (
	testEPSG4326 = "http://www.opengis.net/def/crs/EPSG/0/4326"
	testEPSG3857 = "http://www.opengis.net/def/crs/EPSG/0/3857"
	testEPSG4979 = "http://www.opengis.net/def/crs/EPSG/0/4979"
)

func TestCRSQueryIndependentSemantics(t *testing.T) {
	options := featureCRSOptions(t)
	for _, tc := range []struct {
		name   string
		values common.Query
		bbox   string
		output string
	}{
		{"default", common.Query{}, "", common.CRS84},
		{"output_only", common.Query{"crs": testEPSG3857}, "", testEPSG3857},
		{"bbox_default_independent", common.Query{"crs": testEPSG3857, "bbox": "120,40,130,50"}, common.CRS84, testEPSG3857},
		{"latitude_first", common.Query{"crs": testEPSG3857, "bbox-crs": testEPSG4326, "bbox": "40,120,50,130"}, testEPSG4326, testEPSG3857},
		{"latitude_first_wrap", common.Query{"bbox-crs": testEPSG4326, "bbox": "-10,170,10,-170"}, testEPSG4326, common.CRS84},
		{"projected_not_degrees", common.Query{"bbox-crs": testEPSG3857, "bbox": "-20000000,-30000000,20000000,30000000"}, testEPSG3857, common.CRS84},
		{"implicit_height", common.Query{"crs": testEPSG4326, "bbox": "170,-10,-100,-170,10,100"}, common.CRS84h, testEPSG4326},
		{"explicit_height", common.Query{"crs": common.CRS84h, "bbox-crs": testEPSG4979, "bbox": "-10,170,-100,10,-170,100"}, testEPSG4979, common.CRS84h},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, err := features.ParseQuery(tc.values, options)
			if err != nil || query.OutputCRS.URI != tc.output || query.BBoxCRS.URI != tc.bbox {
				t.Fatal(query, err)
			}
			if raw, ok := tc.values["bbox"]; ok {
				want, err := query.BBoxCRS.ParseBBox(raw)
				if err != nil || !reflect.DeepEqual(query.BBox, want) {
					t.Fatal("parser changed native axis order", query.BBox, want, err)
				}
			}
		})
	}
	// Part 1 implicit bbox defaults are dimension based, not an assertion that
	// this 2D output reader offers explicit 3D output or a vertical operation.
	options.CRS.Supported = []common.CRS{featureCRS(t, common.CRS84)}
	query, err := features.ParseQuery(common.Query{"bbox": "0,0,-10,1,1,10"}, options)
	if err != nil || query.BBoxCRS.URI != common.CRS84h || query.OutputCRS.URI != common.CRS84 {
		t.Fatal(query, err)
	}
	if _, err := features.ParseQuery(common.Query{"bbox": "0,0,-10,1,1,10", "bbox-crs": common.CRS84h}, options); err == nil {
		t.Fatal("explicit unadvertised 3D CRS accepted")
	}
}

func TestCRSQueryErrorsAndLegacyPolicy(t *testing.T) {
	options := featureCRSOptions(t)
	for _, values := range []common.Query{
		{"crs": ""}, {"crs": "EPSG:4326"}, {"crs": "4326"}, {"crs": "http://www.opengis.net/def/crs/EPSG/0/999999"},
		{"bbox-crs": testEPSG3857}, {"bbox-crs": "", "bbox": "0,0,1,1"}, {"bbox-crs": "EPSG:3857", "bbox": "0,0,1,1"},
		{"bbox-crs": testEPSG3857, "bbox": "2,0,1,1"}, {"bbox-crs": testEPSG3857, "bbox": "0,0,NaN,1"},
		{"bbox-crs": testEPSG4326, "bbox": "100,1,110,2"}, {"bbox-crs": testEPSG4326, "bbox": "10,0,-10,1"},
		{"bbox-crs": common.CRS84h, "bbox": "0,0,1,1"}, {"bbox-crs": common.CRS84, "bbox": "0,0,0,1,1,1"},
		{"bbox-crs": testEPSG4979, "bbox": "0,0,10,1,1,-10"},
		{"crs": testEPSG3857, "unknown": "x"},
	} {
		if _, err := features.ParseQuery(values, options); err == nil || common.PublicError(err).Status != 400 {
			t.Fatal("bad CRS request accepted", values, err)
		}
	}
	for _, raw := range []string{"crs=a&crs=a", "bbox-crs=a&bbox-crs=b", "crs=a&%63rs=b", "bbox-crs=%ZZ"} {
		if _, err := common.ParseQuery(raw, 65536, 128); err == nil {
			t.Fatal("duplicate/invalid encoded parameter", raw)
		}
	}
	for _, values := range []common.Query{{"crs": common.CRS84}, {"bbox": "0,0,1,1", "bbox-crs": common.CRS84}} {
		if _, err := features.ParseQuery(values, features.Options{}); err == nil {
			t.Fatal("Part 2 automatically enabled for a legacy reader")
		}
	}
	for _, values := range []common.Query{{"bbox": "0,0,1,1"}, {"bbox-crs": common.CRS84}, {"limit": "1"}, {"filter": "true"}, {"crs": ""}} {
		if _, err := features.ParseItemQuery(values, options); err == nil {
			t.Fatal("single item accepted collection predicates", values)
		}
	}
	query, err := features.ParseItemQuery(common.Query{"crs": testEPSG4326, "f": common.GeoJSON}, options)
	if err != nil || query.OutputCRS.AxisOrder != common.YX || query.OutputCRS.URI != testEPSG4326 || query.Limit != 0 || query.BBox != nil {
		t.Fatal(query, err)
	}
}

func TestCRSPolicyValidationOwnershipAndDefault(t *testing.T) {
	options := featureCRSOptions(t)
	prepared := options.Defaults()
	options.CRS.Supported[0].URI = "mutated"
	if prepared.CRS.Supported[0].URI != testEPSG3857 {
		t.Fatal("CRS policy aliases publication input")
	}
	prepared.CRS.DefaultCRS = common.CRS84h
	query, err := features.ParseQuery(common.Query{}, prepared)
	if err != nil || query.OutputCRS.URI != common.CRS84h {
		t.Fatal(query, err)
	}
	for _, policy := range []features.CRSOptions{
		{},
		{Supported: []common.CRS{featureCRS(t, testEPSG3857)}},
		{Supported: []common.CRS{featureCRS(t, common.CRS84)}, DefaultCRS: testEPSG3857},
		{Supported: []common.CRS{featureCRS(t, common.CRS84), featureCRS(t, common.CRS84)}},
		{Supported: []common.CRS{{URI: common.CRS84, AxisOrder: common.YX, Dimensions: 2, Geographic: true}}},
	} {
		if err := policy.Validate(); err == nil {
			t.Fatal("invalid policy accepted", policy)
		}
	}
}

func TestFilterBBoxAndOutputCRSAreIndependent(t *testing.T) {
	options := featureCRSOptions(t)
	options.Filter = &features.FilterOptions{Queryables: map[string]features.Queryable{"geom": {Type: cql2.GeometryType}}, Operators: []cql2.Operator{cql2.SIntersects}, AdditionalCRS: []common.CRS{featureCRS(t, testEPSG4326)}}
	values := common.Query{"crs": testEPSG3857, "bbox": "120,40,130,50", "filter-crs": testEPSG4326, "filter": "S_INTERSECTS(geom,POINT(45 125))"}
	query, err := features.ParseQuery(values, options)
	if err != nil || query.OutputCRS.URI != testEPSG3857 || query.BBoxCRS.URI != common.CRS84 || query.Filter.CRS != testEPSG4326 {
		t.Fatal(query, err)
	}
	geometry := query.Filter.Expression.Args[1].Literal.Geometry
	if geometry.CRSDefinition.AxisOrder != common.YX || string(geometry.Coordinates.Position[0]) != "45" || string(geometry.Coordinates.Position[1]) != "125" {
		t.Fatal("filter was reinterpreted/reprojected", geometry)
	}
	values["filter"] = `{"op":"s_intersects","args":[{"property":"geom"},{"type":"Point","coordinates":[45,125]}]}`
	values["filter-lang"] = "cql2-json"
	other, err := features.ParseQuery(values, options)
	if err != nil || !reflect.DeepEqual(other.Filter.Expression, query.Filter.Expression) {
		t.Fatal("non-default CRS encodings diverged", other, err)
	}
	options.Filter.AdditionalCRS[0].URI = "mutated"
	if geometry.CRS != testEPSG4326 || geometry.CRSDefinition.URI != testEPSG4326 {
		t.Fatal("returned AST aliases mutable policy")
	}
	options.Filter.AdditionalCRS = nil
	if _, err := features.ParseQuery(values, options); err == nil {
		t.Fatal("output capability silently enabled filter operations")
	}
	options.Filter.AdditionalCRS = []common.CRS{featureCRS(t, testEPSG4326)}
	options.CRS = nil
	if options.Validate() == nil {
		t.Fatal("additional filter CRS enabled without Part 2 declaration")
	}
}

func FuzzCRSQuery(f *testing.F) {
	for _, raw := range []string{"crs=" + testEPSG3857 + "&bbox=-1000000,-1000000,1000000,1000000&bbox-crs=" + testEPSG3857, "bbox=0,0,-10,1,1,10", "crs=a&crs=b", "bbox-crs=" + testEPSG4326 + "&bbox=-10,170,10,-170"} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		values, err := common.ParseQuery(raw, 65536, 128)
		if err != nil {
			return
		}
		options := featureCRSOptions(t)
		query, err := features.ParseQuery(values, options)
		if err != nil {
			return
		}
		before, _ := json.Marshal(query)
		for key := range values {
			values[key] = "changed"
		}
		options.CRS.Supported[0].URI = "changed"
		after, _ := json.Marshal(query)
		if string(before) != string(after) || query.OutputCRS.Validate() != nil || len(query.BBox) != 0 && query.BBoxCRS.ValidateBBox(query.BBox) != nil {
			t.Fatal("retained CRS query mutated or invalid")
		}
	})
}

func BenchmarkCRSQuery(b *testing.B) {
	options := features.Options{CRS: &features.CRSOptions{Supported: []common.CRS{mustBenchmarkCRS(common.CRS84), mustBenchmarkCRS(testEPSG3857)}}}
	values := common.Query{"crs": testEPSG3857, "bbox-crs": testEPSG3857, "bbox": "-20000000,-30000000,20000000,30000000"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := features.ParseQuery(values, options); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCRSQueryCombinedParallel(b *testing.B) {
	projected := mustBenchmarkCRS(testEPSG3857)
	options := features.Options{CRS: &features.CRSOptions{Supported: []common.CRS{mustBenchmarkCRS(common.CRS84), projected}}, FilterProperties: []string{"name"},
		Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{"name": {Type: cql2.String}, "n": {Type: cql2.Integer}, "geom": {Type: cql2.GeometryType}}, Operators: []cql2.Operator{cql2.SIntersects}, AdditionalCRS: []common.CRS{projected}}}
	values := common.Query{"crs": testEPSG3857, "bbox": "-10,-10,10,10", "datetime": "2024-01-01T00:00:00Z/..", "name": "legacy", "filter-crs": testEPSG3857,
		"filter": "(n>=9007199254740993 AND name IS NOT NULL) OR S_INTERSECTS(geom,POINT(1000000 2000000))"}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := features.ParseQuery(values, options); err != nil {
				b.Error(err)
			}
		}
	})
}

func featureCRSOptions(t *testing.T) features.Options {
	t.Helper()
	return features.Options{CRS: &features.CRSOptions{Supported: []common.CRS{featureCRS(t, testEPSG3857), featureCRS(t, common.CRS84), featureCRS(t, common.CRS84h), featureCRS(t, testEPSG4326), featureCRS(t, testEPSG4979)}}}
}

func featureCRS(t *testing.T, uri string) common.CRS {
	t.Helper()
	crs, ok := common.CRSByURI(uri)
	if !ok {
		t.Fatal("missing test CRS metadata", uri)
	}
	return crs
}

func mustBenchmarkCRS(uri string) common.CRS {
	crs, _ := common.CRSByURI(uri)
	return crs
}
