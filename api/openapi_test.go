package api_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/processes"
)

func TestPaginationOpenAPIIsOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c := directoryCatalog(nil, "roads")
		c.Collections[0].Features.Options.OffsetPagination = enabled
		if !enabled {
			c.CollectionPaging = nil
		}
		e := engine(t, c, 1)
		status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/api"})
		var doc struct {
			Paths map[string]struct {
				Get struct {
					Parameters []struct {
						Name   string
						Schema struct{ Default, Maximum uint64 }
					}
				}
			}
			Components struct {
				Schemas map[string]struct{ Properties map[string]json.RawMessage }
			}
		}
		if status != 200 || json.Unmarshal(data, &doc) != nil {
			t.Fatal(status, string(data))
		}
		for _, path := range []string{"/collections", "/collections/{collectionId}/items"} {
			found := false
			for _, p := range doc.Paths[path].Get.Parameters {
				if p.Name == "offset" {
					found = true
				}
				if path == "/collections" && p.Name == "limit" && (p.Schema.Default != 2 || p.Schema.Maximum != 4) {
					t.Fatal("directory limits differ from advertised policy", p)
				}
			}
			if found != enabled {
				t.Fatal("offset advertised outside enabled profile", path, enabled, string(data))
			}
		}
		if len(doc.Components.Schemas["Collections"].Properties["numberReturned"]) == 0 || len(doc.Components.Schemas["Collections"].Properties["numberMatched"]) == 0 {
			t.Fatal("paged response counts missing from schema")
		}
	}
}

func TestPart2OpenAPIParameterAndRepresentationContracts(t *testing.T) {
	e := engine(t, part2Catalog(&crsFeatureReader{}), 16)
	status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/api"})
	var doc struct {
		Paths map[string]struct {
			Get struct {
				Parameters []struct {
					Name, In string
					Schema   struct {
						Type, Format string
						Enum         []string
					}
				}
				Responses map[string]struct {
					Headers map[string]json.RawMessage
					Content map[string]json.RawMessage
				}
			}
		}
		Components struct {
			Schemas map[string]struct{ Properties map[string]json.RawMessage }
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil || status != 200 {
		t.Fatal(status, err)
	}
	for _, path := range []string{"/collections/{collectionId}/items", "/collections/{collectionId}/items/{featureId}"} {
		operation := doc.Paths[path].Get
		found := map[string]bool{}
		for _, parameter := range operation.Parameters {
			if found[parameter.Name] {
				t.Fatal("duplicate parameter", parameter.Name)
			}
			found[parameter.Name] = true
			if parameter.Name == "crs" || parameter.Name == "bbox-crs" {
				if parameter.In != "query" || parameter.Schema.Type != "string" || parameter.Schema.Format != "uri" || !slices.Contains(parameter.Schema.Enum, apiEPSG3857) {
					t.Fatal("incomplete CRS definition", parameter)
				}
			}
			if parameter.Name == "filter-crs" && (slices.Contains(parameter.Schema.Enum, apiEPSG3857) || !slices.Contains(parameter.Schema.Enum, apiEPSG4326)) {
				t.Fatal("output CRS silently enabled filter capability", parameter)
			}
		}
		if !found["crs"] || len(operation.Responses["200"].Headers["Content-Crs"]) == 0 || len(operation.Responses["400"].Content[common.ProblemJSON]) == 0 {
			t.Fatal("missing output/header/error contract", path)
		}
		if strings.HasSuffix(path, "{featureId}") && (found["bbox-crs"] || found["filter-crs"]) {
			t.Fatal("item operation advertises collection filters")
		}
	}
	for _, name := range []string{"crs", "storageCrs", "storageCrsCoordinateEpoch"} {
		if len(doc.Components.Schemas["Collection"].Properties[name]) == 0 {
			t.Fatal("missing discovery schema", name)
		}
	}
}

func TestTileOperationIDs(t *testing.T) {
	for _, dataType := range []string{"map", "vector", "coverage"} {
		t.Run(dataType, func(t *testing.T) {
			c := catalog(&memoryFeatures{}, tileReader("tile"))
			c.Collections[0].Tiles[0].DataType = dataType
			e := engine(t, c, 1)
			_, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/api"})
			var doc struct {
				Paths map[string]struct {
					Get struct {
						OperationID string `json:"operationId"`
					} `json:"get"`
				} `json:"paths"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			for path, operation := range map[string]string{"/collections/{collectionId}/tiles": "getTileSetsList", "/collections/{collectionId}/tiles/{tileMatrixSetId}": "getTileSet", "/collections/{collectionId}/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}": "getTile"} {
				if id := doc.Paths[path].Get.OperationID; id != "ogc.collection."+dataType+"."+operation {
					t.Fatalf("wrong operation ID %q", id)
				}
			}
		})
	}
}

func TestOpenAPIErrorRepresentations(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Processes = []processes.Service{echoProcess()}
	e := engine(t, c, 1)
	status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/api"})
	var doc struct {
		Paths map[string]struct {
			Post struct {
				Responses map[string]struct {
					Content map[string]json.RawMessage
				}
			}
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil || status != 200 {
		t.Fatal(status, err)
	}
	content := doc.Paths["/processes/{processId}/execution"].Post.Responses["default"].Content
	for _, media := range []string{common.JSON, common.ProblemJSON} {
		if len(content[media]) == 0 {
			t.Fatal("missing exception representation", media, string(data))
		}
	}
}

func TestFeatureOpenAPIQueryBoundsMatchPolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		options  []features.Options
		maximum  float64
		fallback any
	}{
		{"defaults", []features.Options{{}}, 1000, float64(10)},
		{"configured", []features.Options{{DefaultLimit: 2, MaxLimit: 4}}, 4, float64(2)},
		{"different maxima", []features.Options{{DefaultLimit: 2, MaxLimit: 4}, {DefaultLimit: 2, MaxLimit: 8}}, 8, float64(2)},
		{"different defaults", []features.Options{{DefaultLimit: 2, MaxLimit: 4}, {DefaultLimit: 3, MaxLimit: 8}}, 8, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := api.Catalog{Title: "query policy", BaseURL: "https://example.test/ogc"}
			for i, options := range test.options {
				c.Collections = append(c.Collections, api.Collection{Metadata: common.Collection{ID: string(rune('a' + i))}, Features: &features.Service{Reader: &memoryFeatures{}, Options: options}})
			}
			e := engine(t, c, 8)
			_, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/api"})
			var doc struct {
				Paths map[string]struct {
					Get struct {
						Parameters []struct {
							Name   string
							Schema map[string]any
						}
					}
				}
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			schemas := make(map[string]map[string]any)
			for _, parameter := range doc.Paths["/collections/{collectionId}/items"].Get.Parameters {
				schemas[parameter.Name] = parameter.Schema
			}
			limit, bbox := schemas["limit"], schemas["bbox"]
			if limit["minimum"] != float64(1) || limit["maximum"] != test.maximum || limit["default"] != test.fallback {
				t.Fatal("inaccurate published limit policy", limit)
			}
			if bbox["minItems"] != float64(4) || bbox["maxItems"] != float64(6) || len(bbox["oneOf"].([]any)) != 2 {
				t.Fatal("inaccurate bbox dimensions", bbox)
			}
			status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/a/items", RawQuery: "limit=999999"})
			if status != 200 {
				t.Fatal("published maximum must still clamp larger requests", status)
			}
			status, problem, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/a/items", RawQuery: "limit=0"})
			if status != 400 || !strings.Contains(string(problem), `"code":"InvalidParameterValue"`) {
				t.Fatal("published minimum must retain the protocol exception", status, string(problem))
			}
		})
	}
}

func TestFeaturePart3OpenAPIReflectsEnabledCollections(t *testing.T) {
	c := filterCatalog(&retainedFilterReader{})
	c.Collections[0].Features.Options.Filter.Languages = []cql2.Language{cql2.Text}
	c.Collections = append(c.Collections, api.Collection{Metadata: common.Collection{ID: "legacy"}, Features: &features.Service{Reader: &memoryFeatures{}}})
	e := engine(t, c, 8)
	_, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/api"})
	var doc struct {
		Paths map[string]struct {
			Get struct {
				Parameters []struct {
					Name   string
					Schema map[string]any
				}
				Responses map[string]struct{ Content map[string]any }
			}
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	queryables, ok := doc.Paths["/collections/{collectionId}/queryables"]
	if !ok || queryables.Get.Responses["200"].Content["application/schema+json"] == nil {
		t.Fatal("missing queryables operation", string(data))
	}
	if got := queryables.Get.Parameters[0].Schema["enum"].([]any); len(got) != 1 || got[0] != "roads" {
		t.Fatal("wrong queryables collection enum", got)
	}
	parameters := map[string]map[string]any{}
	for _, parameter := range doc.Paths["/collections/{collectionId}/items"].Get.Parameters {
		parameters[parameter.Name] = parameter.Schema
	}
	if parameters["filter"]["type"] != "string" || parameters["filter-lang"]["default"] != "cql2-text" || len(parameters["filter-lang"]["enum"].([]any)) != 1 || parameters["filter-crs"]["format"] != "uri-reference" || len(parameters["filter-crs"]["enum"].([]any)) != 2 {
		t.Fatal("inaccurate filter parameters", parameters)
	}
	_, data, _ = read(t, engine(t, catalog(&memoryFeatures{}, tileReader("tile")), 8), api.Input{Method: "GET", EscapedPath: "/ogc/api"})
	if strings.Contains(string(data), `"name":"filter"`) || strings.Contains(string(data), `/collections/{collectionId}/queryables`) {
		t.Fatal("disabled filters advertised", string(data))
	}
}
