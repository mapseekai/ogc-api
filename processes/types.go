// Package processes defines OGC API Processes execution and external job-service contracts.
// It contains no scheduler, worker pool, database or automatic URL fetcher.
package processes

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/response"
)

// Format describes the media type, encoding and schema of a process input or output.
type Format struct {
	MediaType string          `json:"mediaType,omitempty"`
	Encoding  string          `json:"encoding,omitempty"`
	Schema    json.RawMessage `json:"schema,omitempty"`
}

// Input describes one named input. MinOccurs nil defaults to one; MaxOccurs may be
// an integer or "unbounded". Validate checks instance values against Schema externally.
type Input struct {
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	MinOccurs   *uint64         `json:"minOccurs,omitempty"`
	MaxOccurs   any             `json:"maxOccurs,omitempty"`
}

// OutputDescription specifies the schema of one process result.
type OutputDescription struct {
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
}

// Description is public process metadata. Snapshot publication derives JobControlOptions
// from bound capabilities instead of trusting manually advertised execution modes.
type Description struct {
	ID                 string                       `json:"id"`
	Version            string                       `json:"version"`
	Title              string                       `json:"title,omitempty"`
	Description        string                       `json:"description,omitempty"`
	JobControlOptions  []string                     `json:"jobControlOptions"`
	OutputTransmission []string                     `json:"outputTransmission"`
	Inputs             map[string]Input             `json:"inputs,omitempty"`
	Outputs            map[string]OutputDescription `json:"outputs,omitempty"`
	Links              []common.Link                `json:"links"`
}

// Output selects a format and a value/reference transmission mode.
type Output struct {
	Format           *Format `json:"format,omitempty"`
	TransmissionMode string  `json:"transmissionMode,omitempty"`
}

// Execute is the Processes Part 1 execution document. Response defaults to "raw".
// Inputs preserve inline/qualified/reference encodings. The library never dereferences
// input URLs; an external validator/executor applies the deployment's URL policy.
type Execute struct {
	Inputs   map[string]json.RawMessage `json:"inputs,omitempty"`
	Outputs  map[string]Output          `json:"outputs,omitempty"`
	Response string                     `json:"response,omitempty"`
}

// Executor completes synchronous processing. Results transfer ownership even on error.
// Shared implementations must be concurrency-safe and honor cancellation.
type Executor interface {
	Execute(context.Context, Execute) (Results, error)
}

// ExecutorFunc adapts a synchronous operation without introducing a plugin registry.
type ExecutorFunc func(context.Context, Execute) (Results, error)

// Execute calls f and transfers returned result ownership to the caller.
func (f ExecutorFunc) Execute(ctx context.Context, request Execute) (Results, error) {
	return f(ctx, request)
}

// Submitter creates an externally managed asynchronous job. The returned status must
// contain a stable JobID. Durable job work must not retain the submission request context.
type Submitter interface {
	Submit(context.Context, Execute) (Status, error)
}

// SubmitterFunc adapts a remote or durable job-submission operation.
type SubmitterFunc func(context.Context, Execute) (Status, error)

// Submit invokes the configured external submission implementation.
func (f SubmitterFunc) Submit(ctx context.Context, request Execute) (Status, error) {
	return f(ctx, request)
}

// Validator verifies input schemas, cardinalities and output formats before execution.
// Validation is required for every process and must not execute the process or retain request aliases.
// Failures should use client-safe common.Error values; arbitrary errors remain private.
type Validator func(context.Context, Execute) error

// Service binds one process to independent validation, synchronous and asynchronous capabilities.
// At least one execution capability and a Validator are required.
type Service struct {
	Description Description
	Validate    Validator
	Executor    Executor
	Submitter   Submitter
}

// Reference points to an externally stored result; no retrieval is performed by this package.
type Reference struct {
	Href  string `json:"href"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// Value contains exactly one JSON value, binary/stream content or external reference.
// JSON is an ordinary output value; document responses qualify object values automatically.
// Content must be built with response.Bytes or Stream. The owner must close all Content fields.
type Value struct {
	JSON      json.RawMessage
	Content   response.Payload
	Reference *Reference
}

// Results maps output identifiers to exclusively owned values.
type Results map[string]Value

// Close attempts every result close, including after a foreign Close panics.
// Ordinary errors are joined; the first encountered panic is rethrown after cleanup.
func (r Results) Close() (result error) {
	var firstPanic any
	for _, value := range r {
		func() {
			defer func() {
				if caught := recover(); caught != nil && firstPanic == nil {
					firstPanic = caught
				}
			}()
			result = errors.Join(result, value.Content.Close())
		}()
	}
	if firstPanic != nil {
		panic(firstPanic)
	}
	return result
}

// Status describes an external job. Type is "process" and Progress is in the range 0..100.
type Status struct {
	JobID     string        `json:"jobID"`
	ProcessID string        `json:"processID,omitempty"`
	Type      string        `json:"type"`
	Status    string        `json:"status"`
	Message   string        `json:"message,omitempty"`
	Created   *time.Time    `json:"created,omitempty"`
	Started   *time.Time    `json:"started,omitempty"`
	Finished  *time.Time    `json:"finished,omitempty"`
	Updated   *time.Time    `json:"updated,omitempty"`
	Progress  *int          `json:"progress,omitempty"`
	Links     []common.Link `json:"links,omitempty"`
}

// JobResults retains the original output selection and response mode with the completed result.
// The external job service must not return execution inputs or private scheduler metadata here.
type JobResults struct {
	Request Execute
	Results Results
}

// JobReader resolves status and results for jobs visible to the current caller.
// Results transfers ownership even on failure. Implementations must enforce authorization.
type JobReader interface {
	Status(context.Context, string) (Status, error)
	Results(context.Context, string) (JobResults, error)
}

// Dismisser asks the external job service to dismiss a job and return its resulting status.
type Dismisser interface {
	Dismiss(context.Context, string) (Status, error)
}

// JobQuery describes bounded pagination for the job-list retrieval profile.
type JobQuery struct {
	Limit  uint64
	Cursor string
}

// JobPage is an exclusively owned job-status page. NextCursor is opaque.
type JobPage struct {
	Jobs       []Status
	NextCursor string
}

// JobLister provides job lists filtered for the current caller before pagination.
type JobLister interface {
	ListJobs(context.Context, JobQuery) (JobPage, error)
}

// Jobs groups optional external job capabilities without requiring a combined large interface.
// Reader is required when asynchronous processing is published.
type Jobs struct {
	Reader    JobReader
	Lister    JobLister
	Dismisser Dismisser
}
