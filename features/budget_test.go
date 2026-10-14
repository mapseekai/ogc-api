package features_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func TestFeatureBudgetMatchesWireEncoding(t *testing.T) {
	for _, id := range []any{nil, "", "quoted\"\\<>&中文\u2028", json.Number("1.25"), int64(-7), uint64(math.MaxUint64), 0.00001} {
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
	}
}

func TestFeatureRejectsInvalidIDRepresentations(t *testing.T) {
	for _, id := range []any{true, []string{"id"}, map[string]string{"id": "x"}, math.NaN(), math.Inf(1), "\xff"} {
		if _, err := features.EncodeFeature(features.Feature{ID: id}, 4096); err == nil {
			t.Fatalf("accepted invalid ID %T: %v", id, id)
		}
	}
}

func TestFeatureStreamSmallReadBoundaries(t *testing.T) {
	for _, size := range []int{1, 2, 7, 64} {
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
	}
}
