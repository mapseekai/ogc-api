package features_test

import (
	"fmt"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
)

func TestFeatureQueries(t *testing.T) {
	options := features.Options{DefaultLimit: 2, MaxLimit: 5, FilterProperties: []string{"name"}}
	query, err := features.ParseQuery(common.Query{"name": "a", "limit": "3", "bbox": "170,-10,-170,10"}, options)
	if err != nil || query.Properties["name"] != "a" || query.Limit != 3 {
		t.Fatal(query, err)
	}
	for _, bad := range []common.Query{{"limit": "0"}, {"limit": "-1"}, {"crs": common.CRS84}, {"datetime": "invalid"}} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			if _, err := features.ParseQuery(bad, options); err == nil {
				t.Fatal("accepted invalid query", bad)
			}

		})
	}
	for _, raw := range []string{"6", "18446744073709551616000"} {
		t.Run("clamp_"+raw, func(t *testing.T) {
			query, err := features.ParseQuery(common.Query{"limit": raw}, options)
			if err != nil || query.Limit != 5 {
				t.Fatal("feature limit must be clamped", query, err)
			}

		})
	}
	if err := (features.Options{FilterProperties: []string{"limit"}}).Validate(); err == nil {
		t.Fatal("reserved filter accepted")
	}
}

func TestFeatureFilterQueryMixingAndParameterPolicy(t *testing.T) {
	options := features.Options{FilterProperties: []string{"name"}, Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{"name": {Type: cql2.String}, "count": {Type: cql2.Integer}, "geom": {Type: cql2.GeometryType}}, Operators: []cql2.Operator{cql2.SIntersects}}}
	values := common.Query{"bbox": "170,-10,-170,10", "datetime": "2024-01-01T00:00:00Z/..", "limit": "2", "cursor": "opaque", "name": "legacy", "filter": "count>=9007199254740993"}
	query, err := features.ParseQuery(values, options)
	if err != nil {
		t.Fatal(err)
	}
	if query.Filter == nil || query.Filter.Language != cql2.Text || query.Filter.Expression.Operator != cql2.GreaterEqual || query.Filter.Expression.Args[0].PropertyType != cql2.Integer || string(query.Filter.Expression.Args[1].Literal.Number) != "9007199254740993" || len(query.BBox) != 4 || query.BBox[0] != 170 || query.BBox[2] != -170 || query.Datetime.Start == nil || query.Datetime.End != nil || query.Properties["name"] != "legacy" || query.Cursor != "opaque" {
		t.Fatal(query)
	}
	values["name"] = "changed"
	query.Properties["name"] = "owned"
	if values["name"] != "changed" {
		t.Fatal("property map aliases input")
	}
	for _, input := range []common.Query{
		{"filter": ""}, {"filter": "  "}, {"filter-lang": "cql2-text"}, {"filter-crs": common.CRS84},
		{"filter": "count=1", "filter-lang": ""}, {"filter": "count=1", "filter-lang": "sql"}, {"filter": "count=1", "filter-lang": "CQL2-TEXT"},
		{"filter": "count=1", "filter-crs": ""}, {"filter": "count=1", "filter-crs": "http://www.opengis.net/def/crs/EPSG/0/3857"},
		{"filter": "unknown=1"}, {"filter": "count='1'"}, {"filter": "name LIKE 'a%'"}, {"filter": "S_INTERSECTS(geom,POINT(1 2 3))", "filter-crs": common.CRS84},
		{"filter": "count=1", "unknown": "x"},
	} {
		if _, err := features.ParseQuery(input, options); err == nil || common.PublicError(err).Status != 400 {
			t.Fatal("invalid filter was accepted or misclassified", input, err)
		}
	}
	for _, raw := range []string{"filter=count%3D1&filter=count%3D2", "filter=count%3D1&filter-lang=cql2-text&filter-lang=cql2-json", "filter=count%3D1&filter-crs=a&filter-crs=b"} {
		if _, err := common.ParseQuery(raw, 65536, 128); err == nil {
			t.Fatal("duplicate filter parameter accepted")
		}
	}
	if _, err := features.ParseQuery(common.Query{"filter": "count=1"}, features.Options{}); err == nil {
		t.Fatal("CQL2 implicitly enabled")
	}
	if err := (features.Options{FilterProperties: []string{"filter-crs"}}).Validate(); err == nil {
		t.Fatal("filter-crs reserved-name gap")
	}
	if _, err := features.ParseQuery(common.Query{"filter-crs": "x"}, features.Options{FilterProperties: []string{"filter-crs"}}); err == nil {
		t.Fatal("invalid direct policy accepted")
	}
	if err := (features.Options{FilterProperties: []string{"name"}, Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{"name": {Type: cql2.Number}}}}).Validate(); err == nil {
		t.Fatal("legacy string predicate type changed")
	}
}

func TestFeatureFilterJSONAndCRS(t *testing.T) {
	options := features.Options{Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{"geom": {Type: cql2.GeometryType}}, Operators: []cql2.Operator{cql2.SIntersects}}}
	query, err := features.ParseQuery(common.Query{"filter": `{"op":"s_intersects","args":[{"property":"geom"},{"type":"Point","coordinates":[1,2,3]}]}`, "filter-lang": "cql2-json"}, options)
	if err != nil {
		t.Fatal(err)
	}
	if query.Filter.Expression.Args[1].Literal.Geometry.CRS != common.CRS84h || query.Filter.CRS != "" {
		t.Fatal(query.Filter)
	}
	query, err = features.ParseQuery(common.Query{"filter": "S_INTERSECTS(geom,POINT(1 2))", "filter-crs": common.CRS84}, options)
	if err != nil || query.Filter.CRS != common.CRS84 {
		t.Fatal(query.Filter, err)
	}
	options.Filter.Languages = []cql2.Language{cql2.Text}
	if _, err := features.ParseQuery(common.Query{"filter": "true", "filter-lang": "cql2-json"}, options); err == nil {
		t.Fatal("unsupported collection language accepted")
	}
}

func TestFilterURLDecodedOnce(t *testing.T) {
	options := features.Options{Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{"name": {Type: cql2.String}}}}
	values, err := common.ParseQuery("filter=name%3D%27a%2Bb%2527%E9%9B%AA%27", 65536, 128)
	if err != nil {
		t.Fatal(err)
	}
	query, err := features.ParseQuery(values, options)
	if err != nil {
		t.Fatal(err)
	}
	if got := query.Filter.Expression.Args[1].Literal.Text; got != "a+b%27雪" {
		t.Fatalf("double-decoded literal %q", got)
	}
}

func FuzzParseQuery(f *testing.F) {
	for _, raw := range []string{"filter=name%3D%27a%27", "filter=%7B%22op%22%3A%22%3D%22%2C%22args%22%3A%5B%7B%22property%22%3A%22name%22%7D%2C%22a%22%5D%7D&filter-lang=cql2-json", "filter=name%20IS%20NULL&bbox=170,-10,-170,10", "filter-lang=cql2-text", "filter=&filter=1"} {
		f.Add(raw)
	}
	options := features.Options{FilterProperties: []string{"name"}, Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{"name": {Type: cql2.String}}}}
	f.Fuzz(func(t *testing.T, raw string) {
		values, err := common.ParseQuery(raw, 65536, 128)
		if err != nil {
			return
		}
		query, err := features.ParseQuery(values, options)
		if err == nil && values["filter"] != "" && query.Filter == nil {
			t.Fatal("filter was dropped")
		}
	})
}

func ExampleParseQuery() {
	options := features.Options{Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{"population": {Type: cql2.Integer}}}}
	values, _ := common.ParseQuery("filter=population%3E%3D9007199254740993&limit=5", 65536, 128)
	query, err := features.ParseQuery(values, options)
	if err != nil {
		panic(err)
	}
	fmt.Println(query.Filter.Expression.Operator, query.Filter.Expression.Args[0].Property, query.Filter.Expression.Args[1].Literal.Number, query.Limit)
	// The external Reader visits this AST and ANDs it with the other Query fields.
	// Output: >= population 9007199254740993 5
}
