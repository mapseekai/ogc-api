package main

import (
	"context"
	"io"
	"testing"

	"github.com/mapseekai/ogc-api/features"
)

func TestGeographicBoundarySamples(t *testing.T) {
	r := sampleFeatures{}
	for _, tc := range []struct {
		bbox  []float64
		count int
	}{{nil, 7}, {[]float64{-1.5, 50, 1.5, 53}, 1}, {[]float64{-80, -5, -70, 5}, 1}, {[]float64{177, 65, -177, 70}, 2}, {[]float64{-180, 85, 180, 90}, 1}, {[]float64{-180, -90, 180, -85}, 1}, {[]float64{-1, -1, 1, 1}, 1}, {[]float64{-1, -1, 1, 1, 1, 2}, 0}} {
		page, err := r.Items(context.Background(), features.Request{Query: features.Query{BBox: tc.bbox, Limit: 100}})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for {
			_, err := page.Items.Next(context.Background())
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			count++
		}
		page.Items.Close()
		if count != tc.count || *page.NumberMatched != uint64(count) {
			t.Fatalf("bbox %v returned %d", tc.bbox, count)
		}
	}
	page, err := r.Items(context.Background(), features.Request{Query: features.Query{Limit: 1}})
	if err != nil || page.NextCursor != "1" {
		t.Fatal(page, err)
	}
	page.Items.Close()
	page, err = r.Items(context.Background(), features.Request{Query: features.Query{Limit: 1, Cursor: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Items.Close()
	item, err := page.Items.Next(context.Background())
	if err != nil || item.ID != "1" {
		t.Fatal(item, err)
	}
	if _, err := r.Items(context.Background(), features.Request{Query: features.Query{Limit: 1, Cursor: "-1"}}); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}
