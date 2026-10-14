package api

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"slices"
	"strings"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/internal/jsonutil"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

func (e *Engine) executeProcess(ctx context.Context, input Input, snapshot *Snapshot, id string, values common.Query) (*response.Plan, string, error) {
	const operation = "processes.execute"
	service, ok := snapshot.processes[id]
	if !ok {
		return nil, operation, processError(404, "no-such-process", "Process is not available")
	}
	if input.Method != "POST" {
		return methodPlan("POST"), operation, nil
	}
	if err := values.Only(); err != nil {
		return nil, operation, err
	}
	media, _, err := mime.ParseMediaType(input.ContentType)
	if err != nil || media != common.JSON {
		return nil, operation, &common.Error{Status: 415, Code: "UnsupportedMediaType", Description: "Execution requires application/json"}
	}
	request, err := processes.Decode(ctx, input.Body, e.config.Limits.MaxBodyBytes)
	if err != nil {
		return nil, operation, err
	}
	request, err = service.Normalize(request)
	if err != nil {
		return nil, operation, err
	}
	asynchronous := service.Async(input.Prefer)
	if asynchronous || request.Response == "document" {
		if _, err := common.Negotiate(input.Accept, "", []string{common.JSON}); err != nil {
			return nil, operation, err
		}
	}
	if err := service.Validate(ctx, request); err != nil {
		return nil, operation, err
	}
	if err := ctx.Err(); err != nil {
		return nil, operation, err
	}
	if asynchronous {
		status, err := service.Submitter.Submit(ctx, request)
		if err != nil {
			return nil, operation, err
		}
		if status.ProcessID == "" {
			status.ProcessID = id
		}
		if status.Type == "" {
			status.Type = "process"
		}
		if status.ProcessID != id {
			return nil, operation, fmt.Errorf("job service returned a different process")
		}
		if err := processes.CheckStatus(status); err != nil {
			return nil, operation, err
		}
		status = statusLinks(snapshot, status)
		plan, err := jsonPlan(201, status, e.config.Limits.MaxDocumentBytes)
		if err != nil {
			return nil, operation, err
		}
		plan.Headers["Location"] = snapshot.baseURL + "/jobs/" + url.PathEscape(status.JobID)
		if strings.Contains(strings.ToLower(input.Prefer), "respond-async") {
			plan.Headers["Preference-Applied"] = "respond-async"
		}
		return plan, operation, nil
	}
	results, err := service.Executor.Execute(ctx, request)
	if err != nil {
		return nil, operation, errors.Join(err, results.Close())
	}
	plan, err := e.processResults(ctx, input.Accept, request, results)
	return plan, operation, err
}

func (e *Engine) jobResponse(ctx context.Context, input Input, snapshot *Snapshot, parts []string, values common.Query) (*response.Plan, string, error) {
	const operation = "processes.job"
	if snapshot.jobs == nil || len(parts) > 3 || len(parts) == 3 && parts[2] != "results" {
		return nil, operation, processError(404, "no-such-job", "Job is not available")
	}
	if len(parts) == 1 {
		return e.jobList(ctx, input, snapshot, values)
	}
	allowed := "GET, HEAD"
	if len(parts) == 2 && snapshot.jobs.Dismisser != nil {
		allowed += ", DELETE"
	}
	if input.Method != "GET" && input.Method != "HEAD" && !(input.Method == "DELETE" && len(parts) == 2 && snapshot.jobs.Dismisser != nil) {
		return methodPlan(allowed), operation, nil
	}
	if err := values.Only(); err != nil {
		return nil, operation, err
	}
	status, err := snapshot.jobs.Reader.Status(ctx, parts[1])
	if errors.Is(err, common.ErrNotFound) {
		return nil, operation, processError(404, "no-such-job", "Job is not available")
	}
	if err != nil {
		return nil, operation, err
	}
	if err := processes.CheckStatus(status); err != nil {
		return nil, operation, err
	}
	if status.JobID != parts[1] {
		return nil, operation, fmt.Errorf("job reader returned a different job")
	}
	if _, visible := snapshot.processes[status.ProcessID]; !visible {
		return nil, operation, processError(404, "no-such-job", "Job is not available")
	}
	if input.Method == "DELETE" {
		processID := status.ProcessID
		status, err = snapshot.jobs.Dismisser.Dismiss(ctx, parts[1])
		if err != nil {
			return nil, operation, err
		}
		if err := processes.CheckStatus(status); err != nil {
			return nil, operation, err
		}
		if status.JobID != parts[1] || status.ProcessID != processID || status.Status != "dismissed" {
			return nil, operation, fmt.Errorf("job service did not return the dismissed job")
		}
	}
	if len(parts) == 2 {
		if _, err := common.Negotiate(input.Accept, "", []string{common.JSON}); err != nil {
			return nil, operation, err
		}
		plan, err := jsonPlan(200, statusLinks(snapshot, status), e.config.Limits.MaxDocumentBytes)
		return plan, operation, err
	}
	if status.Status == "accepted" || status.Status == "running" {
		return nil, operation, processError(404, "result-not-ready", "Job results are not ready")
	}
	if status.Status == "dismissed" {
		return nil, operation, processError(404, "no-such-job", "Job has been dismissed")
	}
	result, err := snapshot.jobs.Reader.Results(ctx, parts[1])
	if err != nil {
		return nil, operation, errors.Join(err, result.Results.Close())
	}
	if status.Status == "failed" {
		return nil, operation, errors.Join(fmt.Errorf("failed job returned results without its diagnostic error"), result.Results.Close())
	}
	service := snapshot.processes[status.ProcessID]
	// The persisted selection is checked without requiring execution inputs on a result read.
	request := result.Request
	request.Inputs = nil
	selectionService := service
	description := selectionService.Description
	description.Inputs = nil
	selectionService.Description = description
	request, err = selectionService.Normalize(request)
	if err != nil {
		return nil, operation, errors.Join(fmt.Errorf("invalid stored job output selection: %w", err), result.Results.Close())
	}
	plan, err := e.processResults(ctx, input.Accept, request, result.Results)
	return plan, operation, err
}

func (e *Engine) jobList(ctx context.Context, input Input, snapshot *Snapshot, values common.Query) (*response.Plan, string, error) {
	const operation = "processes.jobs"
	if snapshot.jobs.Lister == nil {
		return nil, operation, common.ErrNotFound
	}
	if input.Method != "GET" && input.Method != "HEAD" {
		return methodPlan("GET, HEAD"), operation, nil
	}
	if err := values.Only("limit", "cursor"); err != nil {
		return nil, operation, err
	}
	if _, err := common.Negotiate(input.Accept, "", []string{common.JSON}); err != nil {
		return nil, operation, err
	}
	limit, err := values.Integer("limit", 10, 1, 1000)
	if err != nil {
		return nil, operation, err
	}
	page, err := snapshot.jobs.Lister.ListJobs(ctx, processes.JobQuery{Limit: limit, Cursor: values["cursor"]})
	if err != nil {
		return nil, operation, err
	}
	if uint64(len(page.Jobs)) > limit {
		return nil, operation, fmt.Errorf("job service exceeded requested page size")
	}
	if page.Jobs == nil {
		page.Jobs = []processes.Status{}
	}
	for i, status := range page.Jobs {
		if err := processes.CheckStatus(status); err != nil {
			return nil, operation, err
		}
		if _, visible := snapshot.processes[status.ProcessID]; !visible {
			return nil, operation, fmt.Errorf("job service returned a job outside this publication")
		}
		page.Jobs[i] = statusLinks(snapshot, status)
	}
	links := []common.Link{snapshot.link("/jobs", "self", common.JSON)}
	if page.NextCursor != "" {
		query := url.Values{"cursor": {page.NextCursor}, "limit": {fmt.Sprint(limit)}}.Encode()
		if len(query) > e.config.Limits.MaxQueryBytes {
			return nil, operation, fmt.Errorf("job cursor exceeds query limit")
		}
		links = append(links, snapshot.link("/jobs?"+query, "next", common.JSON))
	}
	plan, err := jsonPlan(200, struct {
		Jobs  []processes.Status `json:"jobs"`
		Links []common.Link      `json:"links"`
	}{page.Jobs, links}, e.config.Limits.MaxDocumentBytes)
	return plan, operation, err
}

// processResults takes ownership on every path, for both execution and job reads.
func (e *Engine) processResults(ctx context.Context, accept string, request processes.Execute, results processes.Results) (*response.Plan, error) {
	plan, err := processes.EncodeResults(ctx, request, results, e.config.Limits.MaxDocumentBytes)
	if err != nil {
		if plan != nil {
			err = errors.Join(err, plan.Finish(0, err))
		}
		return nil, err
	}
	if plan.Status != 204 {
		if _, err := common.Negotiate(accept, "", []string{plan.Headers["Content-Type"]}); err != nil {
			return nil, errors.Join(err, plan.Finish(0, err))
		}
	}
	return plan, nil
}

func (s *Snapshot) prepareProcesses(services []processes.Service, jobs *processes.Jobs, budget *int, add func(string, any) error) error {
	s.processes = make(map[string]processes.Service, len(services))
	if jobs != nil {
		copy := *jobs
		if copy.Reader == nil {
			return fmt.Errorf("job reader is required")
		}
		s.jobs = &copy
	}
	summaries := make([]processes.Description, 0, len(services))
	for _, candidate := range services {
		service, err := candidate.Prepare(*budget)
		if err != nil {
			return err
		}
		id := service.Description.ID
		if _, ok := s.processes[id]; ok {
			return fmt.Errorf("duplicate process id")
		}
		if service.Submitter != nil && s.jobs == nil {
			return fmt.Errorf("asynchronous process requires an external job reader")
		}
		path := "/processes/" + url.PathEscape(id)
		service.Description.Links = append(service.Description.Links,
			s.link(path, "self", common.JSON),
			s.link(path+"/execution", "http://www.opengis.net/def/rel/ogc/1.0/execute", common.JSON))
		if err := add(path, service.Description); err != nil {
			return err
		}
		s.processes[id] = service
		summary := service.Description
		summary.Inputs, summary.Outputs = nil, nil
		summaries = append(summaries, summary)
	}
	if len(services) == 0 {
		return nil
	}
	slices.SortFunc(summaries, func(a, b processes.Description) int { return strings.Compare(a.ID, b.ID) })
	return add("/processes", struct {
		Processes []processes.Description `json:"processes"`
		Links     []common.Link           `json:"links"`
	}{summaries, []common.Link{s.link("/processes", "self", common.JSON)}})
}

func statusLinks(snapshot *Snapshot, status processes.Status) processes.Status {
	path := "/jobs/" + url.PathEscape(status.JobID)
	status.Links = append(slices.Clone(status.Links), snapshot.link(path, "self", common.JSON))
	if status.Status == "successful" {
		status.Links = append(status.Links, snapshot.link(path+"/results", "http://www.opengis.net/def/rel/ogc/1.0/results", ""))
	}
	return status
}

func jsonPlan(status int, value any, maxBytes int) (*response.Plan, error) {
	data, err := jsonutil.Marshal(value, maxBytes)
	if err != nil {
		return nil, err
	}
	return response.New(status, response.Bytes(common.JSON, data)), nil
}

func processError(status int, code, description string) *common.Error {
	return &common.Error{Type: "http://www.opengis.net/def/exceptions/ogcapi-processes-1/1.0/" + code, Status: status, Code: code, Description: description}
}
