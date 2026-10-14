package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

func TestPublishedIdentifiersRoundTrip(t *testing.T) {
	for _, id := range []string{"L00", "10m", "中文/网格+20", "literal%2Fname"} {
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
	}
	for _, id := range []string{"", ".", "..", "bad\n", "bad\t", "bad\x7f", "\xff"} {
		c := catalog(&memoryFeatures{}, tileReader("tile"))
		c.Collections[0].Metadata.ID = id
		if _, err := api.NewSnapshot(c); err == nil {
			t.Errorf("accepted unroutable collection %q", id)
		}
	}
}

func TestWildcardConditionalPreservesAuthorizationAndCleanup(t *testing.T) {
	snapshot, err := api.NewSnapshot(catalog(&memoryFeatures{}, tileReader("tile")))
	if err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{MaxConcurrent: 1, Catalog: func(_ context.Context, scope string) (*api.Snapshot, error) {
		if scope != "allowed" {
			return nil, common.ErrForbidden
		}
		return snapshot, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		scope, path, accept string
		status              int
	}{
		{"denied", "/ogc/collections/roads/tiles/local/L00/0/0", "", 403},
		{"allowed", "/ogc/collections/missing/items", "", 404},
		{"allowed", "/ogc/collections/roads/items", "text/html", 406},
		{"allowed", "/ogc/collections/roads/tiles/local/L00/0/0", "", 304},
	} {
		status, data, _ := read(t, e, api.Input{Method: "GET", Scope: test.scope, EscapedPath: test.path, Accept: test.accept, IfNoneMatch: "*"})
		if status != test.status {
			t.Fatalf("conditional changed result: %d want %d: %s", status, test.status, data)
		}
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", Scope: "allowed", EscapedPath: "/ogc/"})
	if status != 200 {
		t.Fatal("conditional response leaked quota")
	}
}

func TestMetadataAndProcessDescriptionBudgets(t *testing.T) {
	large := strings.Repeat("x", 4<<20)
	for _, field := range []string{"title", "collection", "process"} {
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
	}
}

func TestProcessResultNegotiationClosesBeforeReturn(t *testing.T) {
	// A single result-construction path handles sync and job reads; exercise an
	// unacceptable synchronous result and verify the stream is closed before return.
	closed := 0
	service := echoProcess()
	service.Executor = processes.ExecutorFunc(func(context.Context, processes.Execute) (processes.Results, error) {
		return processes.Results{"result": {Content: response.Stream(common.JSON, &boundaryBody{Reader: strings.NewReader(`"x"`), closed: &closed}, 3)}}, nil
	})
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Processes = []processes.Service{service}
	e := engine(t, c, 1)
	status, _, _ := read(t, e, api.Input{Method: "POST", EscapedPath: "/ogc/processes/echo/execution", ContentType: common.JSON, Accept: "image/png", Body: strings.NewReader(`{"inputs":{"message":"x"}}`)})
	if status != 406 || closed != 1 {
		t.Fatalf("status=%d closes=%d", status, closed)
	}
}

type boundaryBody struct {
	io.Reader
	closed *int
}

func (b *boundaryBody) Close() error { *b.closed++; return nil }

func TestMetadataEncodingCannotMutateProcessCardinality(t *testing.T) {
	service := echoProcess()
	input := service.Description.Inputs["message"]
	input.MaxOccurs = json.Number("18446744073709551615")
	service.Description.Inputs["message"] = input
	prepared, err := service.Prepare(4096)
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared.Description.Inputs["message"].MaxOccurs; got != json.Number("18446744073709551615") {
		t.Fatalf("integer lost precision: %v", got)
	}
}

type failedFeaturePage struct {
	closed  *int
	failure error
}

func (p failedFeaturePage) Items(context.Context, features.Request) (features.Page, error) {
	return features.Page{Items: p}, common.ErrNotFound
}
func (p failedFeaturePage) Item(context.Context, features.Request) (features.Feature, error) {
	return features.Feature{}, common.ErrNotFound
}
func (p failedFeaturePage) Next(context.Context) (features.Feature, error) {
	return features.Feature{}, io.EOF
}
func (p failedFeaturePage) Close() error { *p.closed++; return p.failure }

func TestFeatureErrorRetainsCleanupDiagnostic(t *testing.T) {
	closes := 0
	closeErr := errors.New("cursor cleanup failed")
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Collections[0].Features.Reader = failedFeaturePage{closed: &closes, failure: closeErr}
	e := engine(t, c, 1)
	plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Finish(0, nil)
	if closes != 1 || plan.Status != 404 || !errors.Is(plan.ProtocolError, closeErr) {
		t.Fatalf("lost ownership diagnostic: closes=%d status=%d error=%v", closes, plan.Status, plan.ProtocolError)
	}
}
