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
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

func TestDismissPreservesVisibleProcessIdentity(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Processes = []processes.Service{echoProcess()}
	c.Jobs = &processes.Jobs{Reader: inconsistentProcessJobs{}, Dismisser: inconsistentProcessJobs{}}
	e := engine(t, c, 1)
	status, data, _ := read(t, e, api.Input{Method: "DELETE", EscapedPath: "/ogc/jobs/job-1"})
	if status == 200 {
		t.Fatalf("dismissed status from an unlisted process escaped validation: status=%d body=%s", status, data)
	}
}

func TestProcessResultNegotiationClosesBeforeReturn(t *testing.T) {
	// A single result-construction path handles sync and job reads; exercise an
	// unacceptable synchronous result and verify the stream is closed before return.
	closed := 0
	service := echoProcess()
	service.Executor = processes.ExecutorFunc(func(context.Context, processes.Execute) (processes.Results, error) {
		return processes.Results{"result": {Content: response.Stream(common.JSON, &closeCountingBody{Reader: strings.NewReader(`"x"`), closed: &closed}, 3)}}, nil
	})
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Processes = []processes.Service{service}
	e := engine(t, c, 1)
	status, _, _ := read(t, e, api.Input{Method: "POST", EscapedPath: "/ogc/processes/echo/execution", ContentType: common.JSON, Accept: "image/png", Body: strings.NewReader(`{"inputs":{"message":"x"}}`)})
	if status != 406 || closed != 1 {
		t.Fatalf("status=%d closes=%d", status, closed)
	}
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

type inconsistentProcessJobs struct{}

func (inconsistentProcessJobs) Status(context.Context, string) (processes.Status, error) {
	return processes.Status{JobID: "job-1", ProcessID: "echo", Type: "process", Status: "running"}, nil
}

func (inconsistentProcessJobs) Results(context.Context, string) (processes.JobResults, error) {
	return processes.JobResults{}, nil
}

func (inconsistentProcessJobs) Dismiss(context.Context, string) (processes.Status, error) {
	return processes.Status{JobID: "job-1", ProcessID: "hidden-process", Type: "process", Status: "dismissed", Message: "hidden provider details"}, nil
}

type closeCountingBody struct {
	io.Reader
	closed *int
}

func (b *closeCountingBody) Close() error { *b.closed++; return nil }

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
