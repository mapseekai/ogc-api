package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
)

type memoryFeatures struct{ closed atomic.Int64 }
type featureCursor struct {
	owner     *memoryFeatures
	remaining int
	closed    bool
}

func (c *featureCursor) Next(ctx context.Context) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if c.remaining == 0 {
		return features.Feature{}, io.EOF
	}
	c.remaining--
	return features.Feature{ID: "one", Geometry: json.RawMessage(`{"type":"Point","coordinates":[1,2]}`), Properties: json.RawMessage(`{"name":"road"}`)}, nil
}
func (c *featureCursor) Close() error {
	if !c.closed {
		c.closed = true
		c.owner.closed.Add(1)
	}
	return nil
}
func (p *memoryFeatures) Items(_ context.Context, _ features.Request) (features.Page, error) {
	return features.Page{Items: &featureCursor{owner: p, remaining: 1}}, nil
}
func (p *memoryFeatures) Item(_ context.Context, _ features.Request) (features.Feature, error) {
	return features.Feature{ID: "one", Properties: json.RawMessage(`{}`)}, nil
}

func catalog(p *memoryFeatures, tile tiles.Reader) api.Catalog {
	return api.Catalog{Title: "test", BaseURL: "https://example.test/ogc", Collections: []api.Collection{{Metadata: common.Collection{ID: "roads", Title: "Roads"}, Features: &features.Service{Reader: p}, Tiles: []tiles.Service{{MatrixSetID: "local", DataType: "vector", Formats: []string{"application/vnd.mapbox-vector-tile"}, Reader: tile}}}},
		MatrixSets: []tiles.MatrixSet{{ID: "local", CRS: common.CRS84, Matrices: []tiles.Matrix{{ID: "L00", ScaleDenominator: 1, CellSize: 1, PointOfOrigin: [2]float64{-180, 90}, TileWidth: 512, TileHeight: 256, MatrixWidth: 2, MatrixHeight: 1}}}}}
}
func tileReader(value string) tiles.Reader {
	return tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
		return response.Bytes("application/vnd.mapbox-vector-tile", []byte(value)), nil
	})
}
func engine(t testing.TB, c api.Catalog, max int) *api.Engine {
	t.Helper()
	snapshot, err := api.NewSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{Catalog: api.Static(snapshot), MaxConcurrent: max})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func read(t testing.TB, e *api.Engine, input api.Input) (int, []byte, map[string]string) {
	t.Helper()
	plan, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(plan.Body)
	finish := plan.Finish(int64(len(data)), err)
	if err != nil || finish != nil {
		t.Fatal(err, finish)
	}
	return plan.Status, data, plan.Headers
}

func TestReadContracts(t *testing.T) {
	p := &memoryFeatures{}
	e := engine(t, catalog(p, tileReader("tile")), 8)
	tests := []struct {
		path, query, accept string
		status              int
	}{
		{"/ogc/", "", "", 200}, {"/ogc/conformance", "", "", 200}, {"/ogc/api", "", "application/json", 200},
		{"/ogc/collections", "", "", 200}, {"/ogc/collections/roads", "", "", 200},
		{"/ogc/collections/roads/items", "bbox=170,-10,-170,10&limit=2", "application/geo+json", 200},
		{"/ogc/collections/roads/items/one", "", "", 200},
		{"/ogc/tileMatrixSets", "", "", 200}, {"/ogc/tileMatrixSets/local", "", "", 200},
		{"/ogc/collections/roads/tiles", "", "", 200}, {"/ogc/collections/roads/tiles/local", "", "", 200},
		{"/ogc/collections/roads/tiles/local/L00/0/1", "", "", 200},
		{"/ogc/collections/roads/tiles/local/L00/1/0", "", "", 404},
		{"/ogc/collections/roads/tiles/local/0/0/0", "", "", 404},
		{"/ogc/collections/roads/items", "limit=1&limit=2", "", 400},
		{"/ogc/collections/roads/items", "filter=ignored", "", 400},
		{"/ogc/collections/roads/items", "limit=1001", "", 200},
		{"/ogc/collections/roads/items", "", "text/html", 406},
		{"/ogc/collections/roads/items", "f=", "", 400},
		{"/ogc/collections/missing/items", "", "", 404},
	}
	for _, tt := range tests {
		t.Run(tt.path+"?"+tt.query, func(t *testing.T) {
			status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: tt.path, RawQuery: tt.query, Accept: tt.accept})
			if status != tt.status {
				t.Fatalf("got %d %s, want %d", status, data, tt.status)
			}
		})
	}
	if p.closed.Load() != 2 {
		t.Fatalf("cursor close count %d", p.closed.Load())
	}
}

func TestStreamAdmissionAndCancellation(t *testing.T) {
	p := &memoryFeatures{}
	e := engine(t, catalog(p, tileReader("tile")), 1)
	ctx, cancel := context.WithCancel(context.Background())
	plan, err := e.Handle(ctx, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items"})
	if err != nil {
		t.Fatal(err)
	}
	status, _, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if status != 503 || headers["Retry-After"] != "1" {
		t.Fatal(status, headers)
	}
	cancel()
	if _, err := io.ReadAll(plan.Body); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := plan.Finish(0, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := plan.Finish(0, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if p.closed.Load() != 1 {
		t.Fatal("cursor not closed exactly once")
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if status != 200 {
		t.Fatal("admission slot leaked")
	}
}

func TestMetadataConditionalAndIsolation(t *testing.T) {
	p := &memoryFeatures{}
	c := catalog(p, tileReader("tile"))
	e := engine(t, c, 1)
	c.Collections[0].Metadata.Title = "mutated"
	c.MatrixSets[0].Matrices[0].ID = "wrong"
	_, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads"})
	if strings.Contains(string(data), "mutated") {
		t.Fatal("metadata alias")
	}
	status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads", IfNoneMatch: "W/" + headers["ETag"]})
	if status != 304 || len(data) != 0 {
		t.Fatal(status, string(data))
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0"})
	if status != 200 {
		t.Fatal(status)
	}
}

func TestAtomicReplacementAndParallelReads(t *testing.T) {
	p := &memoryFeatures{}
	a, err := api.NewSnapshot(catalog(p, tileReader("A")))
	if err != nil {
		t.Fatal(err)
	}
	b, err := api.NewSnapshot(catalog(p, tileReader("B")))
	if err != nil {
		t.Fatal(err)
	}
	var current api.AtomicCatalog
	if err := current.Publish(a); err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{Catalog: current.Load, MaxConcurrent: 128})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0"})
				if err != nil {
					t.Error(err)
					return
				}
				data, err := io.ReadAll(plan.Body)
				finish := plan.Finish(int64(len(data)), err)
				if plan.Status != 200 || (string(data) != "A" && string(data) != "B") || finish != nil {
					t.Errorf("bad replacement response: %d %s %v", plan.Status, data, finish)
					return
				}
			}
		}()
	}
	for i := 0; i < 500; i++ {
		if err := current.Publish(b); err != nil {
			t.Fatal(err)
		}
		if err := current.Publish(a); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestObserverPanicReleasesSlot(t *testing.T) {
	p := &memoryFeatures{}
	snapshot, err := api.NewSnapshot(catalog(p, tileReader("tile")))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	e, err := api.New(api.Config{Catalog: api.Static(snapshot), MaxConcurrent: 1, Observer: func(event api.Event) {
		if event.Stage == "handoff" && calls.Add(1) == 1 {
			panic("observer")
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic")
			}
		}()
		_, _ = e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items"})
	}()
	if p.closed.Load() != 1 {
		t.Fatal("panic leaked cursor")
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if status != 200 {
		t.Fatal("panic leaked slot")
	}
}

func BenchmarkMetadataParallel(b *testing.B) {
	e := engine(b, catalog(&memoryFeatures{}, tileReader("tile")), 1024)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads"})
			if err != nil {
				b.Error(err)
				return
			}
			n, err := io.Copy(io.Discard, plan.Body)
			if finish := plan.Finish(n, err); finish != nil {
				b.Error(finish)
				return
			}
		}
	})
}
func BenchmarkTileParallel(b *testing.B) {
	e := engine(b, catalog(&memoryFeatures{}, tileReader("tile")), 1024)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0"})
			if err != nil {
				b.Error(err)
				return
			}
			n, err := io.Copy(io.Discard, plan.Body)
			if finish := plan.Finish(n, err); finish != nil {
				b.Error(finish)
				return
			}
		}
	})
}

func TestCommonAndFeaturesDiscoveryRelations(t *testing.T) {
	e := engine(t, catalog(&memoryFeatures{}, tileReader("tile")), 8)
	_, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	var root common.LandingPage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	relations := make(map[string]bool)
	for _, link := range root.Links {
		relations[link.Rel] = true
	}
	// Common1.0 uses the OGC URI; Features1.0 retains the earlier short relation.
	for _, relation := range []string{"service-desc", "conformance", "http://www.opengis.net/def/rel/ogc/1.0/conformance", "data"} {
		if !relations[relation] {
			t.Fatal("missing standard discovery relation", relation)
		}
	}
	_, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	var conformance common.Conformance
	if err := json.Unmarshal(data, &conformance); err != nil {
		t.Fatal(err)
	}
	classes := make(map[string]bool)
	for _, class := range conformance.ConformsTo {
		classes[class] = true
	}
	if !classes["http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/landing-page"] || classes["http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/landingPage"] {
		t.Fatal("invalid Common landing-page class URI")
	}
	if classes["http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/oas30"] {
		t.Fatal("incomplete OAS class advertised")
	}
}
