package adaptertest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
)

// APIFactory starts a real HTTP listener and registers cleanup with t.
type APIFactory func(t *testing.T, engine *api.Engine) string

// RunAPI checks the same resource-oriented HTTP contracts on every example host.
func RunAPI(t *testing.T, start APIFactory) {
	t.Helper()
	jobs := &apiJobs{}
	process := processes.Service{
		Description: processes.Description{ID: "echo", Version: "1.0.0", OutputTransmission: []string{"value", "reference"},
			Inputs:  map[string]processes.Input{"message": {Schema: json.RawMessage(`{"type":"string"}`)}},
			Outputs: map[string]processes.OutputDescription{"result": {Schema: json.RawMessage(`{"type":"string"}`)}}},
		Validate: func(_ context.Context, request processes.Execute) error {
			var value *string
			if err := json.Unmarshal(request.Inputs["message"], &value); err != nil || value == nil {
				return common.Invalid("message")
			}
			return nil
		},
		Executor: processes.ExecutorFunc(func(_ context.Context, request processes.Execute) (processes.Results, error) {
			results := processes.Results{}
			for name, output := range request.Outputs {
				if output.TransmissionMode == "reference" {
					results[name] = processes.Value{Reference: &processes.Reference{Href: "https://storage.example.test/result", Type: common.JSON}}
				} else {
					results[name] = processes.Value{JSON: request.Inputs["message"]}
				}
			}
			return results, nil
		}), Submitter: jobs,
	}
	snapshot, err := api.NewSnapshot(api.Catalog{Title: "API transport contracts", BaseURL: "https://public.example.test/ogc",
		MatrixSets: []tiles.MatrixSet{{ID: "local", CRS: common.CRS84, Matrices: []tiles.Matrix{{ID: "L00", ScaleDenominator: 1, CellSize: 1, TileWidth: 256, TileHeight: 256, MatrixWidth: 1, MatrixHeight: 1}}}},
		Collections: []api.Collection{{Metadata: common.Collection{ID: "world"}, Features: &features.Service{Reader: apiFeatures{}},
			Tiles: []tiles.Service{{MatrixSetID: "local", DataType: "map", Formats: []string{"image/png"}, Reader: tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
				return response.Bytes("image/png", []byte("tile")), nil
			})}},
			Map: &maps.Service{Formats: []string{"image/png"}, Renderer: maps.RendererFunc(func(_ context.Context, r maps.Request) (maps.Result, error) {
				return maps.Result{Content: response.Bytes(r.Format, []byte("map")), Width: r.Width, Height: r.Height, CRS: r.CRS, BBox: r.BBox}, nil
			})},
			Coverage: &coverages.Service{Formats: []string{"image/tiff"}, Axes: []string{"x"}, Properties: []string{"temperature"}, Schema: json.RawMessage(`{"type":"object"}`), Reader: coverages.ReaderFunc(func(context.Context, coverages.Request) (response.Payload, error) {
				return response.Bytes("image/tiff", []byte("coverage")), nil
			})},
		}}, Processes: []processes.Service{process}, Jobs: &processes.Jobs{Reader: jobs, Lister: jobs, Dismisser: jobs}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := api.New(api.Config{Catalog: api.Static(snapshot), Limits: api.Limits{MaxBodyBytes: 1024}, MaxConcurrent: 8, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	base := start(t, engine)
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method, path, body string, headers map[string]string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", common.JSON)
		}
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		reply, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(reply.Body)
		closeErr := reply.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(readErr, closeErr)
		}
		return reply, data
	}
	for _, test := range []struct {
		name, method, path, body, contains string
		status                             int
	}{
		{"landing", "GET", "/ogc/", "", "links", 200},
		{"conformance", "GET", "/ogc/conformance", "", "conformsTo", 200},
		{"openapi", "GET", "/ogc/api", "", "openapi", 200},
		{"collections", "GET", "/ogc/collections", "", "world", 200},
		{"items", "GET", "/ogc/collections/world/items?limit=1", "", `"numberReturned":1`, 200},
		{"item", "GET", "/ogc/collections/world/items/one", "", `"id":"one"`, 200},
		{"missing item", "GET", "/ogc/collections/world/items/missing", "", "NotFound", 404},
		{"tile", "GET", "/ogc/collections/world/tiles/local/L00/0/0", "", "tile", 200},
		{"tile bounds", "GET", "/ogc/collections/world/tiles/local/L00/1/0", "", "NotFound", 404},
		{"map", "GET", "/ogc/collections/world/map?width=2&height=2", "", "map", 200},
		{"map limits", "GET", "/ogc/collections/world/map?width=999999", "", "InvalidParameterValue", 400},
		{"coverage", "GET", "/ogc/collections/world/coverage?subset=x(0:1)&properties=temperature", "", "coverage", 200},
		{"schema", "GET", "/ogc/collections/world/schema", "", "object", 200},
		{"raw process", "POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"hello"}}`, `"hello"`, 200},
		{"document process", "POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"hello"},"response":"document"}`, `{"result":"hello"}`, 200},
		{"reference process", "POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"hello"},"outputs":{"result":{"transmissionMode":"reference"}}}`, "", 204},
		{"invalid null", "POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"hello"},"outputs":{"result":{"transmissionMode":null}}}`, "InvalidParameterValue", 400},
		{"body limit", "POST", "/ogc/processes/echo/execution", strings.Repeat(" ", 1025), "RequestTooLarge", 413},
		{"duplicate query", "GET", "/ogc/collections/world/items?limit=1&limit=2", "", "InvalidParameterValue", 400},
		{"head", "HEAD", "/ogc/collections/world/items", "", "", 200},
		{"method", "POST", "/ogc/collections", "", "MethodNotAllowed", 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			reply, data := call(test.method, test.path, test.body, nil)
			if reply.StatusCode != test.status || !strings.Contains(string(data), test.contains) {
				t.Fatalf("status=%d body=%s", reply.StatusCode, data)
			}
			if (test.method == "HEAD" || test.status == 204) && len(data) != 0 {
				t.Fatal("suppressed response has a body")
			}
			if test.status == 204 && reply.Header.Get("Link") == "" {
				t.Fatal("reference response lost Link")
			}
		})
	}
	t.Run("conditional responses", func(t *testing.T) {
		first, _ := call("GET", "/ogc/collections/world", "", nil)
		for _, test := range []struct{ path, match string }{{"/ogc/collections/world", "W/" + first.Header.Get("ETag")}, {"/ogc/collections/world/tiles/local/L00/0/0", "*"}} {
			for _, method := range []string{"GET", "HEAD"} {
				reply, data := call(method, test.path, "", map[string]string{"If-None-Match": test.match})
				if reply.StatusCode != 304 || len(data) != 0 || reply.Header.Get("Content-Type") != "" {
					t.Fatalf("invalid conditional response: %d %v %s", reply.StatusCode, reply.Header, data)
				}
			}
		}
	})
	t.Run("negotiation", func(t *testing.T) {
		reply, _ := call("GET", "/ogc/collections/world/items", "", map[string]string{"Accept": "text/html"})
		if reply.StatusCode != 406 {
			t.Fatal(reply.StatusCode)
		}
		reply, _ = call("POST", "/ogc/processes/echo/execution", `{}`, map[string]string{"Content-Type": "text/plain"})
		if reply.StatusCode != 415 {
			t.Fatal(reply.StatusCode)
		}
	})
	t.Run("async jobs", func(t *testing.T) {
		reply, _ := call("POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"hello"},"response":"document"}`, map[string]string{"Prefer": "respond-async"})
		if reply.StatusCode != 201 || !strings.HasSuffix(reply.Header.Get("Location"), "/jobs/job-1") {
			t.Fatal(reply.StatusCode, reply.Header)
		}
		reply, _ = call("GET", "/ogc/jobs/job-1/results", "", nil)
		if reply.StatusCode != 404 {
			t.Fatal(reply.StatusCode)
		}
		jobs.mu.Lock()
		jobs.status.Status = "successful"
		jobs.mu.Unlock()
		for _, path := range []string{"/ogc/jobs", "/ogc/jobs/job-1", "/ogc/jobs/job-1/results"} {
			reply, _ = call("GET", path, "", nil)
			if reply.StatusCode != 200 {
				t.Fatal(path, reply.StatusCode)
			}
		}
		reply, _ = call("DELETE", "/ogc/jobs/job-1", "", nil)
		if reply.StatusCode != 200 {
			t.Fatal(reply.StatusCode)
		}
	})
}

type apiFeatures struct{}
type apiCursor struct{ remaining uint64 }

func (apiFeatures) Items(_ context.Context, r features.Request) (features.Page, error) {
	return features.Page{Items: &apiCursor{remaining: min(2, r.Query.Limit)}}, nil
}
func (apiFeatures) Item(_ context.Context, r features.Request) (features.Feature, error) {
	if r.ItemID != "one" {
		return features.Feature{}, common.ErrNotFound
	}
	return features.Feature{ID: "one", Properties: json.RawMessage(`{"name":"one"}`)}, nil
}
func (c *apiCursor) Next(ctx context.Context) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if c.remaining == 0 {
		return features.Feature{}, io.EOF
	}
	c.remaining--
	return features.Feature{ID: c.remaining, Properties: json.RawMessage(`{}`)}, nil
}
func (c *apiCursor) Close() error { c.remaining = 0; return nil }

type apiJobs struct {
	mu        sync.Mutex
	status    processes.Status
	selection processes.Execute
}

func (j *apiJobs) Submit(_ context.Context, r processes.Execute) (processes.Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.selection = r
	j.status = processes.Status{JobID: "job-1", ProcessID: "echo", Type: "process", Status: "accepted"}
	return j.status, nil
}
func (j *apiJobs) Status(_ context.Context, id string) (processes.Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if id != j.status.JobID {
		return processes.Status{}, common.ErrNotFound
	}
	return j.status, nil
}
func (j *apiJobs) Results(_ context.Context, id string) (processes.JobResults, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if id != j.status.JobID {
		return processes.JobResults{}, common.ErrNotFound
	}
	selection := j.selection
	selection.Inputs = nil
	selection.Outputs = map[string]processes.Output{"result": {TransmissionMode: "value"}}
	return processes.JobResults{Request: selection, Results: processes.Results{"result": {JSON: json.RawMessage(`"done"`)}}}, nil
}
func (j *apiJobs) Dismiss(_ context.Context, id string) (processes.Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if id != j.status.JobID {
		return processes.Status{}, common.ErrNotFound
	}
	j.status.Status = "dismissed"
	return j.status, nil
}
func (j *apiJobs) ListJobs(context.Context, processes.JobQuery) (processes.JobPage, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return processes.JobPage{Jobs: []processes.Status{j.status}}, nil
}
