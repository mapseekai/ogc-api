package main

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
)

func TestFixtureReaderCancellationAndOwnership(t *testing.T) {
	datasets, err := twoDimensionalDatasets()
	if err != nil {
		t.Fatal(err)
	}
	reader := fixtureReader{dataset: datasets[1]}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Items(ctx, features.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := reader.Item(ctx, features.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	query, err := features.ParseQuery(common.Query{"limit": "1", "crs": epsg4326}, features.Options{CRS: &features.CRSOptions{Supported: datasets[1].supported}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.Items(context.Background(), features.Request{CollectionID: "regional", Query: query})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	if page.CRS.URI != epsg4326 || page.NextCursor != "1" || page.NumberMatched == nil || *page.NumberMatched != 4 {
		t.Fatal(page)
	}
	if _, err := page.Items.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("iterator ignored cancellation", err)
	}
	item, err := page.Items.Next(context.Background())
	if err != nil || item.CRS.URI != epsg4326 {
		t.Fatal(item, err)
	}
	if _, err := page.Items.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatal("iterator exceeded limit", err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := page.Items.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatal("closed iterator retained content", err)
	}
}

func TestFixtureBasicFilterIsActuallyApplied(t *testing.T) {
	policy := cql2.Options{Properties: map[string]cql2.Type{"population": cql2.Integer, "active": cql2.Boolean, "nullable": cql2.String}}
	for _, tc := range []struct {
		filter string
		want   any
	}{
		{"population=9007199254740993", true},
		{"population=9007199254740992", false},
		{"nullable IS NULL", true},
		{"nullable='x'", nil},
		{"NOT nullable='x'", nil},
		{"nullable='x' OR active=true", true},
	} {
		expression, err := cql2.Parse(tc.filter, cql2.Text, policy)
		if err != nil {
			t.Fatal(err)
		}
		got, err := evaluate(expression, 0)
		if err != nil || got != tc.want {
			t.Fatal(tc.filter, got, tc.want, err)
		}
	}
}
