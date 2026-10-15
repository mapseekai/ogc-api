package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/processes"
)

func TestEchoJobLifecycleAndOwnership(t *testing.T) {
	jobs := &echoJobs{inputURL: "https://example.invalid/processapi/input"}
	service, err := jobs.service().Prepare(64 << 10)
	if err != nil {
		t.Fatal(err)
	}
	request, err := service.Normalize(processes.Execute{Inputs: map[string]json.RawMessage{"message": json.RawMessage(`"hello"`)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Validate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	status, err := jobs.Submit(context.Background(), request)
	if err != nil || status.Status != "successful" {
		t.Fatal(status, err)
	}
	request.Inputs["message"][1] = 'X'
	for range 2 {
		result, err := jobs.Results(context.Background(), status.JobID)
		if err != nil || string(result.Results["message"].JSON) != `"hello"` {
			t.Fatal(result, err)
		}
		text, err := io.ReadAll(result.Results["copy"].Content.Body)
		if len(result.Results) != 2 || err != nil || string(text) != "hello" || result.Results["copy"].Content.ContentType != "text/plain" {
			t.Fatal("copy output does not echo the input", result)
		}
		if err := result.Results.Close(); err != nil {
			t.Fatal(err)
		}
		result.Request.Outputs["message"] = processes.Output{TransmissionMode: "reference"}
		result.Results["message"].JSON[1] = 'Y'
	}
	page, err := jobs.ListJobs(context.Background(), processes.JobQuery{Limit: 1})
	if err != nil || len(page.Jobs) != 1 {
		t.Fatal(page, err)
	}
	if _, err := jobs.Status(context.Background(), "missing"); !errors.Is(err, common.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := jobs.ListJobs(context.Background(), processes.JobQuery{Limit: 1, Cursor: "-1"}); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := jobs.Submit(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	request.Inputs["message"] = json.RawMessage(`42`)
	if err := service.Validate(context.Background(), request); err == nil {
		t.Fatal("invalid input accepted")
	}
	for _, raw := range []string{`{"value":"qualified"}`, `{"href":"https://example.invalid/processapi/input"}`} {
		request.Inputs["message"] = json.RawMessage(raw)
		if err := service.Validate(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		result, err := service.Executor.Execute(context.Background(), request)
		if err != nil || !json.Valid(result["message"].JSON) {
			t.Fatal(result, err)
		}
	}
	request.Inputs["message"] = json.RawMessage(`{"href":"http://127.0.0.1/private"}`)
	if err := service.Validate(context.Background(), request); err == nil {
		t.Fatal("reference policy bypassed")
	}
}

func TestEchoPendingResults(t *testing.T) {
	jobs := &echoJobs{}
	request := processes.Execute{Inputs: map[string]json.RawMessage{"message": json.RawMessage(`"hello"`), "pause": json.RawMessage(`5`)}, Outputs: map[string]processes.Output{"message": {TransmissionMode: "value"}}}
	status, err := jobs.Submit(context.Background(), request)
	if err != nil || status.Status != "running" {
		t.Fatal(status, err)
	}
	if _, err := jobs.Results(context.Background(), status.JobID); !errors.Is(err, common.ErrNotFound) {
		t.Fatal("pending result became visible", err)
	}
	jobs.mu.Lock()
	jobs.jobs[0].ready = time.Now().Add(-time.Second)
	jobs.mu.Unlock()
	status, err = jobs.Status(context.Background(), status.JobID)
	if err != nil || status.Status != "successful" {
		t.Fatal(status, err)
	}
	if _, err := jobs.Results(context.Background(), status.JobID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := jobs.executeSync(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, value := range []string{`null`, `6`, `-1`, `0.5`, `"1"`} {
		request.Inputs["pause"] = json.RawMessage(value)
		if _, err := echoPause(request); err == nil {
			t.Fatalf("invalid pause %s accepted", value)
		}
	}
}

func TestEchoFormattedTextInput(t *testing.T) {
	jobs := &echoJobs{}
	service, err := jobs.service().Prepare(64 << 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"text copy"`, `{"type":"wrong/type"}`, `42`, `null`} {
		request, err := service.Normalize(processes.Execute{Inputs: map[string]json.RawMessage{"message": json.RawMessage(`"hello"`), "text": json.RawMessage(value)}})
		if err != nil {
			t.Fatal(err)
		}
		err = service.Validate(context.Background(), request)
		if value != `"text copy"` {
			if err == nil {
				t.Fatal("invalid text input accepted", value)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		results, err := service.Executor.Execute(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(results["copy"].Content.Body)
		if err != nil || string(data) != "text copy" || string(results["message"].JSON) != `"hello"` {
			t.Fatal(string(data), err)
		}
		if err := results.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
