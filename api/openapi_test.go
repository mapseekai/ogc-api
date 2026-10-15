package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/processes"
)

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
