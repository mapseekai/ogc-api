package features_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
)

func TestQueryableSchemaTypesAndOwnership(t *testing.T) {
	policy := features.FilterOptions{Queryables: map[string]features.Queryable{
		"name": {Type: cql2.String, Title: "Road name"}, "height": {Type: cql2.Number}, "count": {Type: cql2.Integer}, "active": {Type: cql2.Boolean},
		"day": {Type: cql2.Date}, "observed": {Type: cql2.Timestamp}, "geom": {Type: cql2.GeometryType, Format: "geometry-linestring", Role: "primary-geometry"},
	}, Operators: []cql2.Operator{cql2.SIntersects}}
	prepared := policy.Defaults()
	policy.Queryables["name"] = features.Queryable{Type: cql2.Number}
	policy.Operators[0] = cql2.Like
	schema, err := prepared.Schema("https://example.test/collections/roads/queryables", "Roads", "Road network")
	if err != nil {
		t.Fatal(err)
	}
	if schema.Schema != "https://json-schema.org/draft/2020-12/schema" || schema.ID != "https://example.test/collections/roads/queryables" || schema.Type != "object" || schema.AdditionalProperties {
		t.Fatal(schema)
	}
	if schema.Properties["name"].Type != "string" || schema.Properties["name"].Title != "Road name" || schema.Properties["day"].Format != "date" || schema.Properties["observed"].Format != "date-time" || schema.Properties["geom"].Type != "" || schema.Properties["geom"].Format != "geometry-linestring" {
		t.Fatal(schema.Properties)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	geometry := wire["properties"].(map[string]any)["geom"].(map[string]any)
	if _, ok := geometry["type"]; ok {
		t.Fatal("spatial schema must omit type")
	}
	if _, ok := geometry["$ref"]; ok {
		t.Fatal("spatial schema must omit ref")
	}
	schema.Properties["name"] = features.QueryableSchema{Type: "integer"}
	schema.Languages[0] = cql2.JSON
	again, err := prepared.Schema(schema.ID, "", "")
	if err != nil || again.Properties["name"].Type != "string" || again.Languages[0] != cql2.Text {
		t.Fatal("schema mutation changed policy", err)
	}
	if !reflect.DeepEqual(prepared.Operators, []cql2.Operator{cql2.SIntersects}) {
		t.Fatal("caller mutation changed prepared policy")
	}
}

func TestFilterPolicyRejectsUnsupportedDeclarations(t *testing.T) {
	for _, policy := range []features.FilterOptions{
		{},
		{Queryables: map[string]features.Queryable{"name": {Type: "object"}}},
		// The Features queryables schema currently supports temporal instants,
		// not interval-valued properties; never mislabel one as a timestamp.
		{Queryables: map[string]features.Queryable{"period": {Type: cql2.IntervalType}}},
		{Queryables: map[string]features.Queryable{"geom": {Type: cql2.GeometryType, Format: "geometry-invalid"}}},
		{Queryables: map[string]features.Queryable{"day": {Type: cql2.Date, Format: "date-time"}}},
		{Queryables: map[string]features.Queryable{"name": {Type: cql2.String, Format: "date"}}},
		{Queryables: map[string]features.Queryable{"n": {Type: cql2.Number, Format: "uuid"}}},
		{Operators: []cql2.Operator{"sql"}}, {Operators: []cql2.Operator{cql2.Like, cql2.Like}},
		{Languages: []cql2.Language{"cql"}}, {Languages: []cql2.Language{cql2.Text, cql2.Text}}, {Languages: []cql2.Language{cql2.JSON}},
		{Limits: cql2.Limits{MaxNodes: 5000}},
	} {
		if err := policy.Validate(); err == nil {
			t.Fatal("invalid policy accepted", policy)
		}
	}
}
