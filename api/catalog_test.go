package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
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

func TestDeploymentConformanceAssertions(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	class := "http://www.opengis.net/spec/ogcapi-processes-1/1.0/conf/core"
	c.ConformsTo = []string{class, class}
	e := engine(t, c, 1)
	c.ConformsTo[0] = "mutated"
	status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	if status != 200 || strings.Count(string(data), class) != 1 {
		t.Fatalf("assertion not copied or deduplicated: %s", data)
	}
	for _, uri := range []string{"", "relative", "https://user:password@example.invalid", "http://example.invalid/bad\n"} {
		c.ConformsTo = []string{uri}
		if _, err := api.NewSnapshot(c); err == nil {
			t.Fatalf("invalid class URI accepted: %q", uri)
		}
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
	for _, field := range []string{"title", "collection", "process", "coverage"} {
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
			case "coverage":
				c.Collections[0].Coverage = &coverages.Service{Formats: []string{common.JSON}, DomainSet: json.RawMessage(`{"label":"` + large + `"}`), RangeType: json.RawMessage(`{"type":"DataRecord"}`), Reader: coverages.ReaderFunc(func(context.Context, coverages.Request) (response.Payload, error) {
					t.Fatal("publication read coverage data")
					return response.Payload{}, nil
				})}
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

func TestDatasetMapPublicationBoundaries(t *testing.T) {
	service := maps.Service{Formats: []string{"image/png"}, Limits: owscore.Limits{MaxLayers: 1}, Renderer: maps.RendererFunc(func(context.Context, maps.Request) (maps.Result, error) {
		t.Fatal("publication invoked renderer")
		return maps.Result{}, nil
	})}
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Map = &service
	if _, err := api.NewSnapshot(c); err == nil {
		t.Fatal("dataset map without collection maps was published")
	}
	c.Collections[0].Map = &service
	snapshot, err := api.NewSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	service.DefaultBBox = []float64{-1, -1, 1, 1}
	service.Formats[0] = "image/jpeg"
	e, err := api.New(api.Config{Catalog: api.Static(snapshot)})
	if err != nil {
		t.Fatal(err)
	}
	_, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if strings.Contains(string(data), "image/jpeg") || !strings.Contains(string(data), "image/png") {
		t.Fatal("dataset publication shares mutable service metadata", string(data))
	}
	c.Collections = append(c.Collections, api.Collection{Metadata: common.Collection{ID: "other"}, Map: &service})
	if _, err := api.NewSnapshot(c); err == nil {
		t.Fatal("dataset default selection exceeded layer limit")
	}
}

func TestFeatureQueryablesDiscoveryAndSnapshotOwnership(t *testing.T) {
	reader := &retainedFilterReader{}
	c := filterCatalog(reader)
	c.Collections[0].Features.Options.Filter.Languages = []cql2.Language{cql2.Text, cql2.JSON}
	e := engine(t, c, 16)
	status, original, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/queryables", Accept: "application/schema+json"})
	if status != 200 || headers["Content-Type"] != "application/schema+json" {
		t.Fatal(status, headers)
	}
	var schema struct {
		Schema               string `json:"$schema"`
		ID                   string `json:"$id"`
		Type                 string
		AdditionalProperties bool
		Properties           map[string]map[string]any
		Operators            []cql2.Operator `json:"x-ogc-filter-operators"`
	}
	if err := json.Unmarshal(original, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Schema != "https://json-schema.org/draft/2020-12/schema" || schema.ID != "https://example.test/ogc/collections/roads/queryables" || schema.Type != "object" || schema.AdditionalProperties || schema.Properties["count"]["type"] != "integer" || schema.Properties["observed"]["format"] != "date-time" || schema.Properties["geom"]["type"] != nil || schema.Properties["geom"]["format"] != "geometry-linestring" {
		t.Fatal(string(original))
	}
	policy := c.Collections[0].Features.Options.Filter
	policy.Queryables["name"] = features.Queryable{Type: cql2.Number}
	policy.Operators[0] = cql2.Like
	policy.Languages[0] = "sql"
	c.Collections[0].Features.Options.FilterProperties[0] = "mutated"
	status, after, afterHeaders := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/queryables", Accept: "application/schema+json"})
	if status != 200 || string(after) != string(original) || afterHeaders["ETag"] != headers["ETag"] {
		t.Fatal("snapshot metadata changed")
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {`S_INTERSECTS(geom,POINT(1 2)) AND name='x'`}}.Encode()})
	if status != 200 {
		t.Fatal("snapshot parser policy changed", status)
	}
	status, body, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads"})
	if status != 200 || !strings.Contains(string(body), `http://www.opengis.net/def/rel/ogc/1.0/queryables`) || !strings.Contains(string(body), `application/schema+json`) {
		t.Fatal(status, string(body))
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/queryables", IfNoneMatch: headers["ETag"]})
	if status != 304 {
		t.Fatal("queryables cache missing", status)
	}
	for _, input := range []api.Input{
		{Method: "GET", EscapedPath: "/ogc/collections/missing/queryables"},
		{Method: "POST", EscapedPath: "/ogc/collections/roads/queryables"},
		{Method: "GET", EscapedPath: "/ogc/collections/roads/queryables", RawQuery: "filter=true"},
		{Method: "GET", EscapedPath: "/ogc/collections/roads/queryables", Accept: "text/xml"},
	} {
		status, _, _ := read(t, e, input)
		if status < 400 {
			t.Fatal("invalid metadata request accepted", input, status)
		}
	}
	legacy := engine(t, catalog(&memoryFeatures{}, tileReader("tile")), 16)
	status, _, _ = read(t, legacy, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/queryables"})
	if status != 404 {
		t.Fatal("queryables published without a declaration", status)
	}
}

func TestFeatureFilterConformanceRemainsDeploymentAssertion(t *testing.T) {
	c := filterCatalog(&retainedFilterReader{})
	e := engine(t, c, 8)
	_, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	if strings.Contains(string(data), "ogcapi-features-3") || strings.Contains(string(data), "/spec/cql2/") {
		t.Fatal("parser capability was advertised as backend conformance", string(data))
	}
	classes := []string{"http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/queryables", "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/filter", "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/features-filter", "http://www.opengis.net/spec/cql2/1.0/conf/basic-cql2"}
	c.ConformsTo = classes
	e = engine(t, c, 8)
	_, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	for _, class := range classes {
		if !strings.Contains(string(data), class) {
			t.Fatal("deployment assertion missing", class)
		}
	}
}

func TestPart2MetadataOwnershipAndConformanceAssertion(t *testing.T) {
	c := part2Catalog(&crsFeatureReader{})
	epoch := 2020.125
	c.Collections[0].Metadata.StorageCRSCoordinateEpoch = &epoch
	e := engine(t, c, 16)
	epoch = 1900
	c.Collections[0].Features.Options.CRS.Supported[0].URI = "mutated"
	for _, path := range []string{"/ogc/collections", "/ogc/collections/roads"} {
		status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: path})
		var metadata common.Collection
		if path == "/ogc/collections" {
			var list common.Collections
			if err := json.Unmarshal(data, &list); err != nil || len(list.Collections) != 1 {
				t.Fatal(err, string(data))
			}
			metadata = list.Collections[0]
		} else if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if status != 200 || len(metadata.CRS) != 5 || metadata.CRS[0] != apiEPSG3857 || metadata.StorageCRS != apiEPSG3857 || metadata.StorageCRSCoordinateEpoch == nil || *metadata.StorageCRSCoordinateEpoch != 2020.125 {
			t.Fatal("mutable publication input leaked", metadata, status)
		}
	}
	_, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	if strings.Contains(string(data), features.CRSConformance) {
		t.Fatal("a CRS parser automatically claimed server conformance")
	}
	c = part2Catalog(&crsFeatureReader{})
	c.ConformsTo = []string{features.CRSConformance}
	e = engine(t, c, 16)
	_, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	if !strings.Contains(string(data), features.CRSConformance) {
		t.Fatal("explicit verified deployment assertion lost")
	}
}

func TestPart2MetadataRejectsUnsupportedDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*api.Catalog)
	}{
		{"metadata_mismatch", func(c *api.Catalog) { c.Collections[0].Metadata.CRS = []string{common.CRS84} }},
		{"global_pointer_not_advertised", func(c *api.Catalog) { c.Collections[0].Metadata.CRS = []string{"#/crs"} }},
		{"storage_unsupported", func(c *api.Catalog) { c.Collections[0].Metadata.StorageCRS = "urn:example:unknown" }},
		{"epoch_without_storage", func(c *api.Catalog) {
			c.Collections[0].Metadata.StorageCRS = ""
			epoch := 2020.0
			c.Collections[0].Metadata.StorageCRSCoordinateEpoch = &epoch
		}},
		{"nonfinite_epoch", func(c *api.Catalog) {
			epoch := math.NaN()
			c.Collections[0].Metadata.StorageCRSCoordinateEpoch = &epoch
		}},
		{"missing_standard_default", func(c *api.Catalog) {
			c.Collections[0].Features.Options.CRS.Supported = []common.CRS{part2CRS(apiEPSG3857)}
		}},
		{"false_axes", func(c *api.Catalog) { c.Collections[0].Features.Options.CRS.Supported[3].AxisOrder = common.XY }},
		{"filter_not_supported", func(c *api.Catalog) {
			c.Collections[0].Features.Options.CRS.Supported = []common.CRS{part2CRS(common.CRS84), part2CRS(apiEPSG3857)}
		}},
		{"nondefault_discovery_extent", func(c *api.Catalog) {
			c.Collections[0].Metadata.Extent = &common.Extent{Spatial: &common.SpatialExtent{CRS: apiEPSG3857, BBox: [][]float64{{0, 0, 1000000, 1000000}}}}
		}},
		{"extent_dimension", func(c *api.Catalog) {
			c.Collections[0].Metadata.Extent = &common.Extent{Spatial: &common.SpatialExtent{CRS: common.CRS84h, BBox: [][]float64{{0, 0, 1, 1}}}}
		}},
		{"assertion_requires_policy", func(c *api.Catalog) {
			c.Collections[0].Features.Options.CRS = nil
			c.Collections[0].Features.Options.Filter.AdditionalCRS = nil
			c.ConformsTo = []string{features.CRSConformance}
		}},
		{"assertion_requires_reader", func(c *api.Catalog) {
			c.Collections[0].Features = nil
			c.ConformsTo = []string{features.CRSConformance}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := part2Catalog(&crsFeatureReader{})
			tc.change(&c)
			if _, err := api.NewSnapshot(c); err == nil {
				t.Fatal("inconsistent metadata or capability accepted")
			}
		})
	}
}

func TestFeatureCRS84hWrappedExtentRoundTripAndValidation(t *testing.T) {
	valid := []float64{170, -10, -100, -170, 10, 100}
	for _, uri := range []string{"", common.CRS84h} {
		c := part2Catalog(&crsFeatureReader{})
		bbox := append([]float64(nil), valid...)
		c.Collections[0].Metadata.Extent = &common.Extent{Spatial: &common.SpatialExtent{CRS: uri, BBox: [][]float64{bbox}}}
		e := engine(t, c, 16)
		bbox[0] = 0
		for _, path := range []string{"/ogc/collections", "/ogc/collections/roads"} {
			status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: path})
			var metadata common.Collection
			if path == "/ogc/collections" {
				var list common.Collections
				if err := json.Unmarshal(data, &list); err != nil || len(list.Collections) != 1 {
					t.Fatal(err, string(data))
				}
				metadata = list.Collections[0]
			} else if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if status != 200 || metadata.Extent == nil || metadata.Extent.Spatial == nil || metadata.Extent.Spatial.CRS != uri || len(metadata.Extent.Spatial.BBox) != 1 {
				t.Fatal(status, metadata)
			}
			actual := metadata.Extent.Spatial.BBox[0]
			if len(actual) != 6 {
				t.Fatal("3D extent dimension changed", actual)
			}
			for i, coordinate := range valid {
				if actual[i] != coordinate {
					t.Fatal("native wrapped extent changed or aliased publication", actual, valid)
				}
			}
		}
	}
	for _, bbox := range [][]float64{
		{170, -91, -100, -170, 10, 100},
		{170, 10, -100, -170, -10, 100},
		{170, -10, 100, -170, 10, -100},
		{170, -10, -170, 10},
		{181, -10, -100, -170, 10, 100},
		{170, -10, math.Inf(1), -170, 10, 100},
	} {
		c := part2Catalog(&crsFeatureReader{})
		c.Collections[0].Metadata.Extent = &common.Extent{Spatial: &common.SpatialExtent{CRS: common.CRS84h, BBox: [][]float64{bbox}}}
		if _, err := api.NewSnapshot(c); err == nil {
			t.Fatal("invalid latitude/height/dimension accepted", bbox)
		}
	}
}

func TestMapCRSMetadataDoesNotEnableLegacyFeatureCRS(t *testing.T) {
	for _, withFeatures := range []bool{false, true} {
		for _, defaultCRS := range []string{common.CRS84, apiEPSG3857} {
			c := catalog(&memoryFeatures{}, tileReader("tile"))
			if !withFeatures {
				c.Collections[0].Features = nil
			}
			c.Collections[0].Map = &maps.Service{CRS: []string{common.CRS84, apiEPSG3857}, DefaultCRS: defaultCRS, Formats: []string{"image/png"},
				Renderer: maps.RendererFunc(func(_ context.Context, request maps.Request) (maps.Result, error) {
					return maps.Result{Content: response.Bytes(request.Format, []byte("map")), Width: request.Width, Height: request.Height, BBox: request.BBox, CRS: request.CRS}, nil
				})}
			e := engine(t, c, 16)
			for _, path := range []string{"/ogc/collections", "/ogc/collections/roads"} {
				status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: path})
				var metadata common.Collection
				if path == "/ogc/collections" {
					var list common.Collections
					if err := json.Unmarshal(data, &list); err != nil || len(list.Collections) != 1 {
						t.Fatal(err, string(data))
					}
					metadata = list.Collections[0]
				} else if err := json.Unmarshal(data, &metadata); err != nil {
					t.Fatal(err)
				}
				if status != 200 {
					t.Fatal(status, string(data))
				}
				if withFeatures {
					if len(metadata.CRS) != 1 || metadata.CRS[0] != common.CRS84 || metadata.StorageCRS != "" {
						t.Fatal("map capabilities leaked into legacy feature metadata", metadata)
					}
				} else if len(metadata.CRS) != 2 || metadata.CRS[1] != apiEPSG3857 || defaultCRS == apiEPSG3857 && metadata.StorageCRS != apiEPSG3857 {
					t.Fatal("map-only metadata behavior changed", metadata)
				}
			}
			if withFeatures {
				status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: "crs=" + url.QueryEscape(apiEPSG3857)})
				if status != 400 {
					t.Fatal("map support implicitly enabled Features Part 2", status)
				}
				status, _, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items"})
				if status != 200 || headers["Content-Crs"] != "" {
					t.Fatal("legacy no-CRS request changed", status, headers)
				}
			}
			status, _, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/map", RawQuery: "crs=" + url.QueryEscape(apiEPSG3857)})
			if status != 200 || headers["Content-Crs"] != "<"+apiEPSG3857+">" {
				t.Fatal("independent map CRS request changed", status, headers)
			}
		}
	}
}

func catalog(p *memoryFeatures, tile tiles.Reader) api.Catalog {
	return api.Catalog{Title: "test", BaseURL: "https://example.test/ogc", Collections: []api.Collection{{Metadata: common.Collection{ID: "roads", Title: "Roads"}, Features: &features.Service{Reader: p}, Tiles: []tiles.Service{{MatrixSetID: "local", DataType: "vector", Formats: []string{"application/vnd.mapbox-vector-tile"}, Reader: tile}}}},
		MatrixSets: []tiles.MatrixSet{{ID: "local", CRS: common.CRS84, Matrices: []tiles.Matrix{{ID: "L00", ScaleDenominator: 1, CellSize: 1, PointOfOrigin: [2]float64{-180, 90}, TileWidth: 512, TileHeight: 256, MatrixWidth: 2, MatrixHeight: 1}}}}}
}
