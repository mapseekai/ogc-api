package processes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"slices"
	"strings"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/internal/jsonutil"
)

// Decode parses one bounded execution request. It rejects duplicate keys and unknown
// top-level options instead of silently changing the requested execution semantics.
func Decode(ctx context.Context, body io.Reader, maxBytes int64) (Execute, error) {
	var fields map[string]json.RawMessage
	if err := jsonutil.Decode(ctx, body, maxBytes, &fields); err != nil {
		if ctx.Err() != nil {
			return Execute{}, ctx.Err()
		}
		if errors.Is(err, jsonutil.ErrTooLarge) {
			return Execute{}, &common.Error{Status: 413, Code: "RequestTooLarge", Description: "Execution document exceeds the configured limit", Cause: err}
		}
		return Execute{}, &common.Error{Status: 400, Code: "InvalidParameterValue", Description: "Invalid execution document", Cause: err}
	}
	for name := range fields {
		if name != "inputs" && name != "outputs" && name != "response" {
			return Execute{}, common.Invalid("execution document")
		}
	}
	var execute Execute
	if raw, ok := fields["inputs"]; ok {
		if err := json.Unmarshal(raw, &execute.Inputs); err != nil || execute.Inputs == nil {
			return Execute{}, common.Invalid("inputs")
		}
	}
	if raw, ok := fields["response"]; ok {
		if err := json.Unmarshal(raw, &execute.Response); err != nil || execute.Response != "raw" && execute.Response != "document" {
			return Execute{}, common.Invalid("response")
		}
	}
	if raw, ok := fields["outputs"]; ok {
		var outputs map[string]json.RawMessage
		if err := json.Unmarshal(raw, &outputs); err != nil || outputs == nil {
			return Execute{}, common.Invalid("outputs")
		}
		execute.Outputs = make(map[string]Output, len(outputs))
		for name, data := range outputs {
			output, err := decodeOutput(data)
			if err != nil {
				return Execute{}, err
			}
			execute.Outputs[name] = output
		}
	}
	return execute, nil
}

func decodeOutput(raw json.RawMessage) (Output, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Output{}, common.Invalid("outputs")
	}
	for name := range fields {
		if name != "format" && name != "transmissionMode" {
			return Output{}, common.Invalid("outputs")
		}
	}
	if raw, ok := fields["format"]; ok {
		var format map[string]json.RawMessage
		if err := json.Unmarshal(raw, &format); err != nil || format == nil {
			return Output{}, common.Invalid("format")
		}
		for name, value := range format {
			if (name == "mediaType" || name == "encoding") && !jsonString(value) {
				return Output{}, common.Invalid(name)
			}
			if name == "schema" {
				trimmed := strings.TrimSpace(string(value))
				if !strings.HasPrefix(trimmed, "{") && !jsonString(value) {
					return Output{}, common.Invalid(name)
				}
			}
			if name != "mediaType" && name != "encoding" && name != "schema" {
				return Output{}, common.Invalid("format")
			}
		}
	}
	if value, present := fields["transmissionMode"]; present && !jsonString(value) {
		return Output{}, common.Invalid("transmissionMode")
	}
	var output Output
	if err := json.Unmarshal(raw, &output); err != nil {
		return Output{}, common.Invalid("outputs")
	}
	if _, present := fields["transmissionMode"]; present && output.TransmissionMode != "value" && output.TransmissionMode != "reference" {
		return Output{}, common.Invalid("transmissionMode")
	}
	return output, nil
}

// Prepare validates and copies a service declaration. Bindings are retained as interfaces;
// description maps and schema bytes are detached through a bounded cold-path JSON copy.
func (s Service) Prepare(maxDescriptionBytes int) (Service, error) {
	if !safeID(s.Description.ID) || s.Description.Version == "" || s.Validate == nil || s.Executor == nil && s.Submitter == nil {
		return Service{}, fmt.Errorf("process id, version, validation and execution capability are required")
	}
	data, err := jsonutil.Marshal(s.Description, maxDescriptionBytes)
	if err != nil {
		return Service{}, err
	}
	var description Description
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&description); err != nil {
		return Service{}, err
	}
	if len(description.Inputs) > 256 || len(description.Outputs) > 256 {
		return Service{}, fmt.Errorf("too many process inputs or outputs")
	}
	for name, input := range description.Inputs {
		if !safeID(name) || !json.Valid(input.Schema) {
			return Service{}, fmt.Errorf("invalid process input description")
		}
	}
	for name, output := range description.Outputs {
		if !safeID(name) || !json.Valid(output.Schema) {
			return Service{}, fmt.Errorf("invalid process output description")
		}
	}
	if len(description.OutputTransmission) == 0 {
		description.OutputTransmission = []string{"value"}
	}
	seen := make(map[string]bool)
	for _, mode := range description.OutputTransmission {
		if seen[mode] || mode != "value" && mode != "reference" {
			return Service{}, fmt.Errorf("invalid process output transmission")
		}
		seen[mode] = true
	}
	description.JobControlOptions = nil
	if s.Executor != nil {
		description.JobControlOptions = append(description.JobControlOptions, "sync-execute")
	}
	if s.Submitter != nil {
		description.JobControlOptions = append(description.JobControlOptions, "async-execute")
	}
	if description.Links == nil {
		description.Links = []common.Link{}
	}
	s.Description = description
	return s, nil
}

// Normalize enforces standard option semantics and declared input/output names.
// It returns a request-owned value; the provided Execute must not be used concurrently.
// Schema validation is deliberately performed separately by Service.Validate before dispatch.
func (s Service) Normalize(request Execute) (Execute, error) {
	if request.Response == "" {
		request.Response = "raw"
	}
	if request.Response != "raw" && request.Response != "document" {
		return Execute{}, common.Invalid("response")
	}
	for name := range request.Inputs {
		if _, ok := s.Description.Inputs[name]; !ok {
			return Execute{}, common.Invalid("inputs")
		}
	}
	for name, description := range s.Description.Inputs {
		required := description.MinOccurs == nil || *description.MinOccurs > 0
		if _, ok := request.Inputs[name]; !ok && required {
			return Execute{}, common.Invalid("inputs")
		}
	}
	if request.Outputs == nil {
		request.Outputs = make(map[string]Output, len(s.Description.Outputs))
		for name := range s.Description.Outputs {
			request.Outputs[name] = Output{}
		}
	}
	for name, output := range request.Outputs {
		if _, ok := s.Description.Outputs[name]; !ok {
			return Execute{}, common.Invalid("outputs")
		}
		if output.TransmissionMode == "" {
			output.TransmissionMode = s.Description.OutputTransmission[0]
		}
		if !slices.Contains(s.Description.OutputTransmission, output.TransmissionMode) {
			return Execute{}, common.Invalid("transmissionMode")
		}
		if output.Format != nil && output.Format.MediaType != "" {
			if _, _, err := mime.ParseMediaType(output.Format.MediaType); err != nil {
				return Execute{}, common.Invalid("format")
			}
		}
		request.Outputs[name] = output
	}
	return request, nil
}

// Async determines execution mode from actual capabilities and the respond-async preference.
// Unsupported preferences are ignored as required for HTTP Prefer; wait never creates a worker.
func (s Service) Async(prefer string) bool {
	if s.Submitter == nil {
		return false
	}
	if s.Executor == nil {
		return true
	}
	for _, preference := range strings.Split(prefer, ",") {
		token, _, _ := strings.Cut(strings.TrimSpace(preference), ";")
		if strings.EqualFold(strings.TrimSpace(token), "respond-async") {
			return true
		}
	}
	return false
}

// CheckStatus validates a backend status before it is exposed over the protocol.
func CheckStatus(status Status) error {
	if !safeID(status.JobID) || !safeID(status.ProcessID) || status.Type != "process" {
		return fmt.Errorf("invalid external job identity")
	}
	if !slices.Contains([]string{"accepted", "running", "successful", "failed", "dismissed"}, status.Status) {
		return fmt.Errorf("invalid external job status")
	}
	if status.Progress != nil && (*status.Progress < 0 || *status.Progress > 100) {
		return fmt.Errorf("invalid job progress")
	}
	return nil
}

func safeID(value string) bool {
	return common.ValidID(value) && !strings.ContainsAny(value, "<>\"")
}

func jsonString(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '"'
}
