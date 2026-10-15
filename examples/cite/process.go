package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

// echoJobs is a bounded, example-only job store; it does not model durable production work.
type echoJobs struct {
	mu       sync.Mutex
	jobs     []echoJob
	inputURL string
}

type echoJob struct {
	request processes.Execute
	ready   time.Time
}

func (j *echoJobs) service() processes.Service {
	optional := uint64(0)
	return processes.Service{Description: processes.Description{ID: "echo", Version: "1.0.0", Title: "Echo a string as JSON and text", Inputs: map[string]processes.Input{"message": {Schema: json.RawMessage(`{"type":"string"}`)}, "text": {Title: "Optional plain text for the copy output", MinOccurs: &optional, Schema: json.RawMessage(`{"type":"string","contentMediaType":"text/plain"}`)}, "pause": {MinOccurs: &optional, Schema: json.RawMessage(`{"type":"integer","minimum":0,"maximum":5}`)}}, Outputs: map[string]processes.OutputDescription{"message": {Schema: json.RawMessage(`{"type":"string"}`)}, "copy": {Schema: json.RawMessage(`{"type":"string","contentMediaType":"text/plain"}`)}}},
		Validate: func(ctx context.Context, request processes.Execute) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := j.inputValue(request.Inputs["message"]); err != nil {
				return err
			}
			if raw, ok := request.Inputs["text"]; ok {
				if _, err := j.inputValue(raw); err != nil {
					return common.Invalid("text")
				}
			}
			if _, err := echoPause(request); err != nil {
				return err
			}
			for name, output := range request.Outputs {
				media := common.JSON
				if name == "copy" {
					media = "text/plain"
				}
				if output.Format != nil && (output.Format.MediaType != "" && output.Format.MediaType != media || output.Format.Encoding != "") {
					return common.Invalid("format")
				}
			}
			return nil
		}, Executor: processes.ExecutorFunc(j.executeSync), Submitter: j}
}

func (j *echoJobs) Submit(ctx context.Context, request processes.Execute) (processes.Status, error) {
	if err := ctx.Err(); err != nil {
		return processes.Status{}, err
	}
	if _, err := j.inputValue(request.Inputs["message"]); err != nil {
		return processes.Status{}, err
	}
	// Detach the request before keeping it after submission returns.
	data, err := json.Marshal(request)
	if err != nil {
		return processes.Status{}, err
	}
	var stored processes.Execute
	if err := json.Unmarshal(data, &stored); err != nil {
		return processes.Status{}, err
	}
	pause, err := echoPause(request)
	if err != nil {
		return processes.Status{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.jobs) >= 10000 {
		return processes.Status{}, &common.Error{Status: 503, Code: "ServerBusy", Description: "Fixture job store is full"}
	}
	j.jobs = append(j.jobs, echoJob{request: stored, ready: time.Now().Add(pause)})
	return j.status(len(j.jobs)), nil
}

func (j *echoJobs) Status(ctx context.Context, id string) (processes.Status, error) {
	if err := ctx.Err(); err != nil {
		return processes.Status{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	i, err := strconv.Atoi(id)
	if err != nil || i < 1 || i > len(j.jobs) || strconv.Itoa(i) != id {
		return processes.Status{}, common.ErrNotFound
	}
	return j.status(i), nil
}

func (j *echoJobs) Results(ctx context.Context, id string) (processes.JobResults, error) {
	status, err := j.Status(ctx, id)
	if err != nil {
		return processes.JobResults{}, err
	}
	if status.Status != "successful" {
		return processes.JobResults{}, common.ErrNotFound
	}
	i, _ := strconv.Atoi(id)
	j.mu.Lock()
	defer j.mu.Unlock()
	data, err := json.Marshal(j.jobs[i-1].request)
	if err != nil {
		return processes.JobResults{}, err
	}
	var request processes.Execute
	if err := json.Unmarshal(data, &request); err != nil {
		return processes.JobResults{}, err
	}
	results, err := j.execute(ctx, request)
	return processes.JobResults{Request: request, Results: results}, err
}

func (j *echoJobs) ListJobs(ctx context.Context, query processes.JobQuery) (processes.JobPage, error) {
	if err := ctx.Err(); err != nil {
		return processes.JobPage{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	start := uint64(0)
	if query.Cursor != "" {
		var err error
		start, err = strconv.ParseUint(query.Cursor, 10, 64)
		if err != nil || start > uint64(len(j.jobs)) {
			return processes.JobPage{}, common.Invalid("cursor")
		}
	}
	end := min(uint64(len(j.jobs)), start+query.Limit)
	page := processes.JobPage{Jobs: []processes.Status{}}
	for i := start; i < end; i++ {
		page.Jobs = append(page.Jobs, j.status(int(i+1)))
	}
	if end < uint64(len(j.jobs)) {
		page.NextCursor = strconv.FormatUint(end, 10)
	}
	return page, nil
}

func (j *echoJobs) executeSync(ctx context.Context, request processes.Execute) (processes.Results, error) {
	pause, err := echoPause(request)
	if err != nil {
		return nil, err
	}
	if pause != 0 {
		timer := time.NewTimer(pause)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return j.execute(ctx, request)
}

func (j *echoJobs) execute(ctx context.Context, request processes.Execute) (processes.Results, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make(processes.Results, len(request.Outputs))
	for name := range request.Outputs {
		raw := request.Inputs["message"]
		if text, ok := request.Inputs["text"]; ok && name == "copy" {
			raw = text
		}
		value, err := j.inputValue(raw)
		if err != nil {
			return nil, err
		}
		if name == "copy" {
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return nil, err
			}
			results[name] = processes.Value{Content: response.Bytes("text/plain", []byte(text))}
		} else {
			results[name] = processes.Value{JSON: value}
		}
	}
	return results, nil
}

func (j *echoJobs) inputValue(raw json.RawMessage) (json.RawMessage, error) {
	var text *string
	if err := json.Unmarshal(raw, &text); err == nil && text != nil {
		return slices.Clone(raw), nil
	}
	var input struct {
		Value json.RawMessage `json:"value"`
		Href  string          `json:"href"`
	}
	if err := json.Unmarshal(raw, &input); err == nil {
		if len(input.Value) != 0 && input.Href == "" {
			if err := json.Unmarshal(input.Value, &text); err == nil && text != nil {
				return slices.Clone(input.Value), nil
			}
		}
		// Only the host-owned input resource is allowed; no arbitrary URL is fetched.
		if len(input.Value) == 0 && j.inputURL != "" && input.Href == j.inputURL {
			return json.RawMessage(`"reference sample"`), nil
		}
	}
	return nil, common.Invalid("message")
}

// status requires j.mu; the pure echo result becomes visible after its requested wait.
func (j *echoJobs) status(id int) processes.Status {
	status := "successful"
	if time.Now().Before(j.jobs[id-1].ready) {
		status = "running"
	}
	return processes.Status{JobID: fmt.Sprint(id), ProcessID: "echo", Type: "process", Status: status}
}

func echoPause(request processes.Execute) (time.Duration, error) {
	var seconds *uint64
	if raw, ok := request.Inputs["pause"]; ok {
		if err := json.Unmarshal(raw, &seconds); err != nil || seconds == nil || *seconds > 5 {
			return 0, common.Invalid("pause")
		}
		return time.Duration(*seconds) * time.Second, nil
	}
	return 0, nil
}
