package features_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func TestFeatureBudgetMatchesWireEncoding(t *testing.T) {
	for _, id := range []any{nil, "", "quoted\"\\<>&中文\u2028", json.Number("1.25"), int64(-7), uint64(math.MaxUint64), 0.00001} {
		t.Run(fmt.Sprintf("%T_%v", id, id), func(t *testing.T) {
			for _, linked := range []bool{false, true} {
				feature := features.Feature{Type: "Feature", ID: id,
					Geometry:   json.RawMessage(`{"type":"Point","coordinates":[1,2]}`),
					Properties: json.RawMessage(`{"name":"<>&"}`), BBox: []float64{1, 2, 1, 2}}
				if linked {
					feature.Links = []common.Link{{Href: "https://example.test/{id}", Rel: "item", Type: common.GeoJSON, Title: "\"<>&中文", Templated: true}, {Href: "/next", Rel: "next"}}
				}
				want, err := json.Marshal(feature)
				if err != nil {
					t.Fatal(err)
				}
				got, err := features.EncodeFeature(feature, len(want))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("id=%v linked=%t: exact budget failed: %v\ngot %s\nwant %s", id, linked, err, got, want)
				}
				if _, err := features.EncodeFeature(feature, len(want)-1); err == nil {
					t.Fatal("undersized feature budget accepted")
				}
			}

		})
	}
}

func TestFeatureRejectsInvalidIDRepresentations(t *testing.T) {
	for _, id := range []any{true, []string{"id"}, map[string]string{"id": "x"}, math.NaN(), math.Inf(1), "\xff"} {
		t.Run(fmt.Sprintf("%T_%v", id, id), func(t *testing.T) {
			if _, err := features.EncodeFeature(features.Feature{ID: id}, 4096); err == nil {
				t.Fatalf("accepted invalid ID %T: %v", id, id)
			}

		})
	}
}

func TestFeatureStreamSmallReadBoundaries(t *testing.T) {
	for _, size := range []int{1, 2, 7, 64} {
		t.Run(fmt.Sprintf("read_%d", size), func(t *testing.T) {
			items := &cursor{count: 5}
			content, err := features.Stream(context.Background(), features.Page{Items: items}, []common.Link{}, 5, 1024)
			if err != nil {
				t.Fatal(err)
			}
			var data bytes.Buffer
			buffer := make([]byte, size)
			for {
				n, err := content.Body.Read(buffer)
				data.Write(buffer[:n])
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := content.Close(); err != nil {
				t.Fatal(err)
			}
			var collection struct {
				Features       []features.Feature `json:"features"`
				NumberReturned int                `json:"numberReturned"`
			}
			if err := json.Unmarshal(data.Bytes(), &collection); err != nil || len(collection.Features) != 5 || collection.NumberReturned != 5 || items.closed != 1 {
				t.Fatalf("read size %d: invalid collection or cleanup: %v %s", size, err, data.Bytes())
			}

		})
	}
}

func TestCollectionStreamLifecycle(t *testing.T) {
	for _, count := range []int{0, 1, 100} {
		t.Run(fmt.Sprintf("features_%d", count), func(t *testing.T) {
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

		})
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

func BenchmarkRejectOversizedFeature(b *testing.B) {
	text := strings.Repeat("x", 4<<20)
	for _, which := range []string{"id", "link", "properties"} {
		b.Run(which, func(b *testing.B) {
			f := features.Feature{}
			switch which {
			case "id":
				f.ID = text
			case "link":
				f.Links = []common.Link{{Href: text, Rel: "related"}}
			case "properties":
				f.Properties = json.RawMessage(`{"x":"` + text + `"}`)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := features.EncodeFeature(f, 1024); err == nil {
					b.Fatal("budget should reject fixture")
				}
			}
		})
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
