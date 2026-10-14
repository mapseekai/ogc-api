package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/tiles"
)

func TestSnapshotRejectsUnroutableMatrixID(t *testing.T) {
	for _, id := range []string{".", "..", "bad\n"} {
		t.Run(fmt.Sprintf("invalid_%q", id), func(t *testing.T) {
			c := catalog(&memoryFeatures{}, tileReader("tile"))
			c.MatrixSets[0].Matrices[0].ID = id
			snapshot, err := api.NewSnapshot(c)
			if err == nil {
				t.Errorf("snapshot accepted matrix ID %q that resourcePath rejects (snapshot nonnil=%v)", id, snapshot != nil)
			}

		})
	}
}

func TestPublishedIdentifiersRoundTrip(t *testing.T) {
	for _, id := range []string{"L00", "10m", "中文/网格+20", "literal%2Fname"} {
		t.Run("valid_"+id, func(t *testing.T) {
			c := catalog(&memoryFeatures{}, tileReader("tile"))
			c.Collections[0].Metadata.ID = id
			c.MatrixSets[0].ID = id
			c.MatrixSets[0].Matrices[0].ID = id
			c.Collections[0].Tiles[0].MatrixSetID = id
			e := engine(t, c, 1)
			escaped := url.PathEscape(id)
			status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/" + escaped + "/tiles/" + escaped + "/" + escaped + "/0/0"})
			if status != 200 || string(data) != "tile" {
				t.Fatalf("id %q did not round-trip: %d %s", id, status, data)
			}

		})
	}
	for _, id := range []string{"", ".", "..", "bad\n", "bad\t", "bad\x7f", "\xff"} {
		t.Run(fmt.Sprintf("invalid_%q", id), func(t *testing.T) {
			c := catalog(&memoryFeatures{}, tileReader("tile"))
			c.Collections[0].Metadata.ID = id
			if _, err := api.NewSnapshot(c); err == nil {
				t.Errorf("accepted unroutable collection %q", id)
			}

		})
	}
}

func TestMetadataAndProcessDescriptionBudgets(t *testing.T) {
	large := strings.Repeat("x", 4<<20)
	for _, field := range []string{"title", "collection", "process"} {
		t.Run(field, func(t *testing.T) {
			c := catalog(&memoryFeatures{}, tileReader("tile"))
			c.MetadataBudget = 1024
			switch field {
			case "title":
				c.Title = large
			case "collection":
				c.Collections[0].Metadata.Description = large
			case "process":
				s := echoProcess()
				s.Description.Description = large
				c.Processes = []processes.Service{s}
			}
			if _, err := api.NewSnapshot(c); err == nil {
				t.Fatal("metadata budget ignored", field)
			}

		})
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

func TestInFlightResponseKeepsOriginalBackend(t *testing.T) {
	first, err := api.NewSnapshot(catalog(&memoryFeatures{}, tileReader("old")))
	if err != nil {
		t.Fatal(err)
	}
	second, err := api.NewSnapshot(catalog(&memoryFeatures{}, tileReader("new")))
	if err != nil {
		t.Fatal(err)
	}
	var publication api.AtomicCatalog
	if err := publication.Publish(first); err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{Catalog: publication.Load})
	if err != nil {
		t.Fatal(err)
	}
	input := api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0"}
	retained, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := publication.Publish(second); err != nil {
		t.Fatal(err)
	}
	status, data, _ := read(t, e, input)
	if status != 200 || string(data) != "new" {
		t.Fatal(status, string(data))
	}
	old, err := io.ReadAll(retained.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := retained.Finish(int64(len(old)), nil); err != nil {
		t.Fatal(err)
	}
	if string(old) != "old" {
		t.Fatal("in-flight response switched backends")
	}
}

func catalog(p *memoryFeatures, tile tiles.Reader) api.Catalog {
	return api.Catalog{Title: "test", BaseURL: "https://example.test/ogc", Collections: []api.Collection{{Metadata: common.Collection{ID: "roads", Title: "Roads"}, Features: &features.Service{Reader: p}, Tiles: []tiles.Service{{MatrixSetID: "local", DataType: "vector", Formats: []string{"application/vnd.mapbox-vector-tile"}, Reader: tile}}}},
		MatrixSets: []tiles.MatrixSet{{ID: "local", CRS: common.CRS84, Matrices: []tiles.Matrix{{ID: "L00", ScaleDenominator: 1, CellSize: 1, PointOfOrigin: [2]float64{-180, 90}, TileWidth: 512, TileHeight: 256, MatrixWidth: 2, MatrixHeight: 1}}}}}
}
