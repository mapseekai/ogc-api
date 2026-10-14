package features_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

type cursor struct {
	count     int
	calls     int
	closed    int
	failure   error
	malformed bool
}

func (c *cursor) Next(ctx context.Context) (features.Feature, error) {
	c.calls++
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if c.count == 0 {
		if c.failure != nil {
			return features.Feature{}, c.failure
		}
		return features.Feature{}, io.EOF
	}
	c.count--
	if c.malformed {
		return features.Feature{Properties: json.RawMessage(`[]`)}, nil
	}
	return features.Feature{ID: "a", Geometry: json.RawMessage(`{"type":"Point","coordinates":[1,2]}`), Properties: json.RawMessage(`{}`)}, nil
}

func (c *cursor) Close() error { c.closed++; return nil }

func TestCollectionStreamLifecycle(t *testing.T) {
	for _, count := range []int{0, 1, 100} {
		c := &cursor{count: count}
		matched := uint64(count)
		content, err := features.Stream(context.Background(), features.Page{Items: c, NumberMatched: &matched}, []common.Link{}, 100, 1024)
		if err != nil {
			t.Fatal(err)
		}
		if c.calls != 0 {
			t.Fatal("stream constructor consumed the iterator")
		}
		data, err := io.ReadAll(content.Body)
		closeErr := content.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		if err := content.Close(); err != nil {
			t.Fatal(err)
		}
		if c.closed != 1 {
			t.Fatal("iterator not closed exactly once")
		}
		var collection struct {
			Type           string             `json:"type"`
			Features       []features.Feature `json:"features"`
			NumberReturned uint64             `json:"numberReturned"`
			NumberMatched  uint64             `json:"numberMatched"`
		}
		if err := json.Unmarshal(data, &collection); err != nil {
			t.Fatal(err, string(data))
		}
		if collection.Type != "FeatureCollection" || len(collection.Features) != count || collection.NumberReturned != uint64(count) || collection.NumberMatched != uint64(count) {
			t.Fatalf("invalid feature collection: %s", data)
		}
	}
}

func TestCollectionStreamErrors(t *testing.T) {
	cause := errors.New("backend stream failed")
	tests := []struct {
		name     string
		cursor   *cursor
		limit    uint64
		maxBytes int
		cause    error
	}{
		{"late backend error", &cursor{count: 1, failure: cause}, 10, 1024, cause},
		{"provider limit violation", &cursor{count: 2}, 1, 1024, nil},
		{"invalid properties", &cursor{count: 1, malformed: true}, 1, 1024, nil},
		{"oversized feature", &cursor{count: 1}, 1, 10, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := features.Stream(context.Background(), features.Page{Items: tt.cursor}, []common.Link{}, tt.limit, tt.maxBytes)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(content.Body)
			if err == nil {
				t.Fatal("invalid stream succeeded", string(data))
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Fatal(err)
			}
			if json.Valid(data) {
				t.Fatal("failed result presented as a complete JSON document")
			}
			if err := content.Close(); err != nil {
				t.Fatal(err)
			}
			if tt.cursor.closed != 1 {
				t.Fatal("iterator leaked")
			}
		})
	}
}

func TestCollectionHeadAndCancellation(t *testing.T) {
	c := &cursor{count: 10}
	ctx, cancel := context.WithCancel(context.Background())
	content, err := features.Stream(ctx, features.Page{Items: c}, nil, 10, 1024)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := io.ReadAll(content.Body); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := content.Close(); err != nil {
		t.Fatal(err)
	}
	if c.calls != 0 || c.closed != 1 {
		t.Fatal("HEAD/cancel performed unnecessary reads or leaked")
	}
}

func TestFeatureQueries(t *testing.T) {
	options := features.Options{DefaultLimit: 2, MaxLimit: 5, FilterProperties: []string{"name"}}
	query, err := features.ParseQuery(common.Query{"name": "a", "limit": "3", "bbox": "170,-10,-170,10"}, options)
	if err != nil || query.Properties["name"] != "a" || query.Limit != 3 {
		t.Fatal(query, err)
	}
	for _, bad := range []common.Query{{"limit": "0"}, {"limit": "-1"}, {"crs": common.CRS84}, {"datetime": "invalid"}} {
		if _, err := features.ParseQuery(bad, options); err == nil {
			t.Fatal("accepted invalid query", bad)
		}
	}
	for _, raw := range []string{"6", "18446744073709551616000"} {
		query, err := features.ParseQuery(common.Query{"limit": raw}, options)
		if err != nil || query.Limit != 5 {
			t.Fatal("feature limit must be clamped", query, err)
		}
	}
	if err := (features.Options{FilterProperties: []string{"limit"}}).Validate(); err == nil {
		t.Fatal("reserved filter accepted")
	}
}

func BenchmarkCollectionStream(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := &cursor{count: 100}
		content, err := features.Stream(context.Background(), features.Page{Items: c}, []common.Link{}, 100, 1024)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, content.Body); err != nil {
			b.Fatal(err)
		}
		if err := content.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzFeature(f *testing.F) {
	for _, seed := range []string{`{}`, `null`, `{"name":"road"}`, strings.Repeat("a", 100)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		data, err := features.EncodeFeature(features.Feature{Properties: json.RawMessage(raw)}, 4096)
		if err == nil && !json.Valid(data) {
			t.Fatal("invalid output")
		}
	})
}
