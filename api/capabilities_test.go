package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

func echoProcess() processes.Service {
	return processes.Service{
		Description: processes.Description{ID: "echo", Version: "1.0.0", Inputs: map[string]processes.Input{"message": {Schema: json.RawMessage(`{"type":"string"}`)}}, Outputs: map[string]processes.OutputDescription{"result": {Schema: json.RawMessage(`{"type":"string"}`)}}},
		Validate: func(_ context.Context, request processes.Execute) error {
			var message string
			if err := json.Unmarshal(request.Inputs["message"], &message); err != nil {
				return common.Invalid("message")
			}
			return nil
		},
		Executor: processes.ExecutorFunc(func(_ context.Context, request processes.Execute) (processes.Results, error) {
			return processes.Results{"result": {JSON: request.Inputs["message"]}}, nil
		}),
	}
}

type jobService struct {
	mu        sync.Mutex
	status    processes.Status
	selection processes.Execute
	submits   int
}

func (j *jobService) Submit(_ context.Context, request processes.Execute) (processes.Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.submits++
	j.selection = request
	j.status = processes.Status{JobID: "job-1", ProcessID: "echo", Type: "process", Status: "accepted"}
	return j.status, nil
}
func (j *jobService) Status(_ context.Context, id string) (processes.Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if id != j.status.JobID {
		return processes.Status{}, common.ErrNotFound
	}
	return j.status, nil
}
func (j *jobService) Results(_ context.Context, id string) (processes.JobResults, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if id != j.status.JobID {
		return processes.JobResults{}, common.ErrNotFound
	}
	selection := j.selection
	selection.Outputs = map[string]processes.Output{"result": {TransmissionMode: "value"}}
	return processes.JobResults{Request: selection, Results: processes.Results{"result": {JSON: json.RawMessage(`"done"`)}}}, nil
}
func (j *jobService) Dismiss(_ context.Context, id string) (processes.Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if id != j.status.JobID {
		return processes.Status{}, common.ErrNotFound
	}
	j.status.Status = "dismissed"
	return j.status, nil
}
func (j *jobService) ListJobs(_ context.Context, _ processes.JobQuery) (processes.JobPage, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return processes.JobPage{Jobs: []processes.Status{j.status}}, nil
}

func TestSyncProcessEndToEnd(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Processes = []processes.Service{echoProcess()}
	e := engine(t, c, 8)
	for _, path := range []string{"/ogc/processes", "/ogc/processes/echo"} {
		status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: path})
		if status != 200 || !strings.Contains(string(data), "sync-execute") {
			t.Fatal(status, string(data))
		}
		if strings.Contains(string(data), "async-execute") {
			t.Fatal("advertised absent submitter")
		}
	}
	tests := []struct {
		body, contentType, prefer string
		status                    int
		want                      string
	}{
		{`{"inputs":{"message":"hello"}}`, common.JSON, "", 200, `"hello"`},
		{`{"inputs":{"message":"hello"},"response":"document"}`, common.JSON, "", 200, `{"result":"hello"}`},
		{`{"inputs":{"message":"hello"}}`, common.JSON, "respond-async", 200, `"hello"`},
		{`{"inputs":{"message":1}}`, common.JSON, "", 400, "InvalidParameterValue"},
		{`{}`, common.JSON, "", 400, "InvalidParameterValue"},
		{`{"inputs":{"message":"hello"}}`, "text/plain", "", 415, "UnsupportedMediaType"},
		{`{"inputs":{"message":"hello"},"mode":"async"}`, common.JSON, "", 400, "InvalidParameterValue"},
		{`{"inputs":{"message":"hello"},"outputs":{"unknown":{}}}`, common.JSON, "", 400, "InvalidParameterValue"},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			status, data, _ := read(t, e, api.Input{Method: "POST", EscapedPath: "/ogc/processes/echo/execution", Body: strings.NewReader(tt.body), ContentType: tt.contentType, Prefer: tt.prefer})
			if status != tt.status || !strings.Contains(string(data), tt.want) {
				t.Fatal(status, string(data))
			}
		})
	}
	status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/processes/missing"})
	if status != 404 || !strings.Contains(string(data), "/no-such-process") {
		t.Fatal(status, string(data))
	}
}

func TestExternalAsyncJobs(t *testing.T) {
	jobs := &jobService{}
	service := echoProcess()
	service.Submitter = jobs
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Processes = []processes.Service{service}
	c.Jobs = &processes.Jobs{Reader: jobs, Lister: jobs, Dismisser: jobs}
	e := engine(t, c, 8)
	status, data, headers := read(t, e, api.Input{Method: "POST", EscapedPath: "/ogc/processes/echo/execution", ContentType: common.JSON, Prefer: "respond-async", Body: strings.NewReader(`{"inputs":{"message":"hello"},"response":"document"}`)})
	if status != 201 || !strings.Contains(string(data), "job-1") || headers["Location"] != "https://example.test/ogc/jobs/job-1" {
		t.Fatal(status, string(data), headers)
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/jobs/job-1/results"})
	if status != 404 || !strings.Contains(string(data), "/result-not-ready") {
		t.Fatal(status, string(data))
	}
	jobs.mu.Lock()
	jobs.status.Status = "successful"
	jobs.mu.Unlock()
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/jobs/job-1/results"})
	if status != 200 || string(data) != `{"result":"done"}` {
		t.Fatal(status, string(data))
	}
	for _, path := range []string{"/ogc/jobs", "/ogc/jobs/job-1"} {
		status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: path})
		if status != 200 {
			t.Fatal(path, status)
		}
	}
	status, data, _ = read(t, e, api.Input{Method: "DELETE", EscapedPath: "/ogc/jobs/job-1"})
	if status != 200 || !strings.Contains(string(data), "dismissed") {
		t.Fatal(status, string(data))
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/jobs/unknown/results"})
	if status != 404 || !strings.Contains(string(data), "/no-such-job") {
		t.Fatal(status, string(data))
	}
}

func TestMapAndCoverageEndToEnd(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	var rendered maps.Request
	c.Collections[0].Map = &maps.Service{Formats: []string{"image/png"}, Renderer: maps.RendererFunc(func(_ context.Context, request maps.Request) (maps.Result, error) {
		rendered = request
		return maps.Result{Content: response.Bytes("image/png", []byte("map")), Width: request.Width, Height: request.Height, BBox: request.BBox, CRS: request.CRS}, nil
	})}
	var covered coverages.Request
	c.Collections[0].Coverage = &coverages.Service{Formats: []string{"image/tiff"}, Axes: []string{"x"}, Properties: []string{"temperature"}, Schema: json.RawMessage(`{"type":"object"}`), Reader: coverages.ReaderFunc(func(_ context.Context, request coverages.Request) (response.Payload, error) {
		covered = request
		return response.Bytes("image/tiff", []byte("coverage")), nil
	})}
	e := engine(t, c, 8)
	status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/map", RawQuery: "bbox=0,0,10,10&width=100&height=100"})
	if status != 200 || string(data) != "map" || rendered.Layers[0].ID != "roads" || headers["Content-Bbox"] != "0,0,10,10" {
		t.Fatal(status, string(data), headers)
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/coverage", RawQuery: "subset=x(1:2)&properties=temperature"})
	if status != 200 || string(data) != "coverage" || covered.Subsets[0].Axis != "x" || covered.CollectionID != "roads" {
		t.Fatal(status, string(data), covered)
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/schema"})
	if status != 200 || !json.Valid(data) {
		t.Fatal(status, string(data))
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	if status != 200 || strings.Contains(string(data), "coverages") || strings.Contains(string(data), "maps-1") {
		t.Fatal("profile routes must not claim unimplemented conformance", string(data))
	}
}

func TestHeadClosesWithoutIterating(t *testing.T) {
	provider := &memoryFeatures{}
	e := engine(t, catalog(provider, tileReader("tile")), 1)
	plan, err := e.Handle(context.Background(), api.Input{Method: "HEAD", EscapedPath: "/ogc/collections/roads/items"})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if provider.closed.Load() != 1 {
		t.Fatal("HEAD did not close result")
	}
	next, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, next.Body); err != nil {
		t.Fatal(err)
	}
	if err := next.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if next.Status != 200 {
		t.Fatal("HEAD leaked admission")
	}
}
