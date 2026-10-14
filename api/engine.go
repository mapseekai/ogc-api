package api

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/internal/jsonutil"
	"github.com/mapseekai/ogc-api/response"
)

// Handle converts an API request to an owned response plan. The host MUST call Finish
// after sending or suppressing the body, including HEAD, 304, cancellation and write errors.
// Context remains live until Finish. A returned Go error means no response could be built.
// Expected client errors become problem responses. Internal errors remain diagnostic only.
func (e *Engine) Handle(parent context.Context, input Input) (plan *response.Plan, resultErr error) {
	if !e.gate.Acquire() {
		e.observe(Event{Stage: "reject", Operation: "admission", Scope: input.Scope, Status: 503, Error: common.ErrUnavailable})
		return errorPlan(common.ErrUnavailable)
	}
	ctx, cancel := response.Timeout(parent, e.config.Timeout)
	handedOff := false
	defer func() {
		if !handedOff {
			defer e.gate.Release()
			defer cancel()
			if plan != nil && plan.Body != nil {
				_ = plan.Body.Close()
			}
		}
	}()
	var started time.Time
	if e.config.Observer != nil {
		started = time.Now()
	}
	operation := "request"
	var diagnostic error
	e.observe(Event{Stage: "start", Scope: input.Scope})
	if err := ctx.Err(); err != nil {
		diagnostic = err
	} else {
		plan, operation, diagnostic = e.dispatch(ctx, input)
	}
	if e.config.Observer != nil {
		e.observe(Event{Stage: "business", Operation: operation, Scope: input.Scope, Elapsed: time.Since(started), Error: diagnostic})
	}
	if diagnostic == nil {
		diagnostic = ctx.Err()
	}
	if diagnostic != nil {
		if plan != nil {
			diagnostic = errors.Join(diagnostic, plan.Body.Close())
			plan = nil
		}
		var err error
		plan, err = errorPlan(diagnostic)
		if err != nil {
			return nil, err
		}
		cancel()
		ctx, cancel = response.Timeout(parent, time.Second)
	}
	if plan == nil {
		return nil, errors.New("API operation returned no response")
	}
	if (input.Method == "GET" || input.Method == "HEAD") && plan.Status == 200 && matchesETag(input.IfNoneMatch, plan.Headers["ETag"]) {
		if err := plan.Body.Close(); err != nil {
			return nil, err
		}
		plan.Body = io.NopCloser(strings.NewReader(""))
		plan.Length = 0
		plan.Status = 304
		delete(plan.Headers, "Content-Length")
		delete(plan.Headers, "Content-Type")
		delete(plan.Headers, "Content-Encoding")
	}
	if input.Method == "GET" || input.Method == "HEAD" {
		plan.Headers["Vary"] = "Accept"
	}
	plan.Body = &contextBody{Reader: response.Reader{Context: ctx, Source: plan.Body, Length: plan.Length}, Closer: plan.Body}
	var finished func(int64, error)
	if e.config.Observer != nil {
		finished = func(n int64, err error) {
			e.observe(Event{Stage: "finish", Operation: operation, Scope: input.Scope, Status: plan.Status, Elapsed: time.Since(started), BytesRead: n, Error: errors.Join(diagnostic, err)})
		}
	}
	plan.BindLifetime(func() { defer e.gate.Release(); cancel() }, finished)
	if e.config.Observer != nil {
		e.observe(Event{Stage: "handoff", Operation: operation, Scope: input.Scope, Status: plan.Status, Elapsed: time.Since(started), Error: diagnostic})
	}
	handedOff = true
	return plan, nil
}

type contextBody struct {
	response.Reader
	io.Closer
}

func (e *Engine) observe(event Event) {
	if e.config.Observer != nil {
		e.config.Observer(event)
	}
}

func (e *Engine) dispatch(ctx context.Context, input Input) (*response.Plan, string, error) {
	limits := e.config.Limits
	if len(input.EscapedPath) > limits.MaxPathBytes || len(input.RawQuery) > limits.MaxQueryBytes {
		return nil, "request", &common.Error{Status: 414, Code: "RequestTooLarge", Description: "Request URI exceeds the configured limit"}
	}
	if len(input.Accept)+len(input.IfNoneMatch)+len(input.ContentType)+len(input.Prefer) > limits.MaxHeaderBytes {
		return nil, "request", &common.Error{Status: 431, Code: "RequestTooLarge", Description: "Request headers exceed the configured limit"}
	}
	var values common.Query
	if input.RawQuery != "" {
		parsed, err := common.ParseQuery(input.RawQuery, limits.MaxQueryBytes, limits.MaxParameters)
		if err != nil {
			return nil, "request", err
		}
		values = parsed
	}
	if format, ok := values["f"]; ok && format == "" {
		return nil, "request", common.Invalid("f")
	}
	snapshot, err := e.config.Catalog(ctx, input.Scope)
	if err != nil {
		return nil, "catalog", err
	}
	if snapshot == nil || snapshot.baseURL == "" {
		return nil, "catalog", errors.New("uninitialized API snapshot")
	}
	path, err := localPath(input.EscapedPath, snapshot.basePath)
	if err != nil {
		return nil, "routing", err
	}
	if doc, ok := snapshot.documents[path]; ok {
		plan, err := metadataResponse(input, values, snapshot, doc)
		return plan, "metadata", err
	}
	parts, err := resourcePath(path, "")
	if err != nil {
		return nil, "routing", err
	}
	if strings.Contains(path, "%") {
		if doc, ok := snapshot.documents[canonicalPath(parts)]; ok {
			plan, err := metadataResponse(input, values, snapshot, doc)
			return plan, "metadata", err
		}
	}
	if len(parts) > 0 && parts[0] == "processes" && len(parts) == 3 && parts[2] == "execution" {
		return e.executeProcess(ctx, input, snapshot, parts[1], values)
	}
	if len(parts) > 0 && parts[0] == "jobs" {
		return e.jobResponse(ctx, input, snapshot, parts, values)
	}

	if len(parts) >= 3 && parts[0] == "collections" {
		collection, ok := snapshot.collections[parts[1]]
		if !ok {
			return nil, "collection", common.ErrNotFound
		}
		if input.Method != "GET" && input.Method != "HEAD" {
			return methodPlan("GET, HEAD"), "collection", nil
		}
		switch parts[2] {
		case "items":
			return e.featureResponse(ctx, input, snapshot, collection, parts, values)
		case "tiles":
			return e.tileResponse(ctx, input, collection, parts, values)
		case "map":
			return e.mapResponse(ctx, input, collection, parts, values)
		case "coverage":
			return e.coverageResponse(ctx, input, collection, parts, values)
		}
	}
	if len(parts) == 2 && parts[0] == "processes" {
		return nil, "processes.description", processError(404, "no-such-process", "Process is not available")
	}
	return nil, "routing", common.ErrNotFound
}

func metadataResponse(input Input, values common.Query, snapshot *Snapshot, doc document) (*response.Plan, error) {
	if input.Method != "GET" && input.Method != "HEAD" {
		return methodPlan("GET, HEAD"), nil
	}
	if err := values.Only("f"); err != nil {
		return nil, err
	}
	offered := []string{doc.media}
	if doc.media != common.JSON {
		offered = append(offered, common.JSON)
	}
	media, err := common.Negotiate(input.Accept, values["f"], offered)
	if err != nil {
		return nil, err
	}
	payload := response.Bytes(media, doc.data)
	payload.Cache.ETag = doc.etag
	payload.Cache.CacheControl = snapshot.metadataCacheControl
	return response.New(200, payload), nil
}

func localPath(path, prefix string) (string, error) {
	if prefix != "" {
		if path == prefix {
			path = "/"
		} else if strings.HasPrefix(path, prefix+"/") {
			path = strings.TrimPrefix(path, prefix)
		} else {
			return "", common.ErrNotFound
		}
	}
	if path == "" {
		path = "/"
	}
	return path, nil
}

func resourcePath(path, prefix string) ([]string, error) {
	var err error
	path, err = localPath(path, prefix)
	if err != nil {
		return nil, err
	}
	if path == "/" || path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Count(path, "/") > 8 {
		return nil, common.ErrNotFound
	}
	parts := strings.Split(path[1:], "/")
	for i, part := range parts {
		value, err := url.PathUnescape(part)
		if err != nil || !common.ValidID(value) {
			return nil, common.Invalid("path")
		}
		parts[i] = value
	}
	return parts, nil
}

func canonicalPath(parts []string) string {
	if len(parts) == 0 {
		return "/"
	}
	encoded := make([]string, len(parts))
	for i, part := range parts {
		encoded[i] = url.PathEscape(part)
	}
	return "/" + strings.Join(encoded, "/")
}

func errorPlan(err error) (*response.Plan, error) {
	public := common.PublicError(err)
	data, encodeErr := jsonutil.Marshal(public.Problem(), 64<<10)
	if encodeErr != nil {
		return nil, encodeErr
	}
	plan := response.New(public.Status, response.Bytes(common.ProblemJSON, data))
	plan.ProtocolError = err
	if public.Status == 503 {
		plan.Headers["Retry-After"] = "1"
	}
	return plan, nil
}

func methodPlan(allow string) *response.Plan {
	plan := response.New(405, response.Bytes(common.ProblemJSON, []byte(`{"type":"about:blank","title":"Method Not Allowed","status":405,"code":"MethodNotAllowed","description":"Method is not available"}`)))
	plan.Headers["Allow"] = allow
	return plan
}

func payloadPlan(payload response.Payload, expected string) (*response.Plan, error) {
	if err := payload.Validate(expected); err != nil {
		return nil, errors.Join(err, payload.Close())
	}
	return response.New(200, payload), nil
}

func matchesETag(raw, etag string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "*" {
		return true
	}
	if raw == "" || etag == "" {
		return false
	}
	target := strings.TrimPrefix(etag, "W/")
	for raw != "" {
		raw = strings.TrimSpace(raw)
		raw = strings.TrimPrefix(raw, "W/")
		if len(raw) == 0 || raw[0] != '"' {
			return false
		}
		// Opaque tags may contain commas; backslashes do not escape quotation marks in ETags.
		end := strings.IndexByte(raw[1:], '"')
		if end < 0 {
			return false
		}
		end += 2
		candidate := raw[:end]
		tail := strings.TrimSpace(raw[end:])
		if tail != "" && tail[0] != ',' {
			return false
		}
		if candidate == target {
			return true
		}
		if tail == "" {
			return false
		}
		raw = tail[1:]
	}
	return false
}
