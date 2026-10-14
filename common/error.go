package common

import (
	"context"
	"errors"
	"net/http"
)

// Backend error categories remain stable across provider implementations.
var (
	ErrNotFound       = errors.New("resource not found")
	ErrForbidden      = errors.New("access forbidden")
	ErrUnavailable    = errors.New("service unavailable")
	ErrConflict       = errors.New("resource conflict")
	ErrNotImplemented = errors.New("operation not implemented")
)

// Error carries a client-safe protocol error and a private diagnostic cause.
// Only Description may be disclosed to clients; it must not contain credentials or queries.
type Error struct {
	// Type optionally identifies a standard-specific exception URI. Empty uses about:blank.
	Type        string
	Status      int
	Code        string
	Description string
	Cause       error
}

// Error returns the public description without exposing Cause.
func (e *Error) Error() string { return e.Code + ": " + e.Description }

// Unwrap retains the private error for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// Invalid reports an invalid standard parameter without echoing its untrusted value.
func Invalid(name string) *Error {
	return &Error{Status: 400, Code: "InvalidParameterValue", Description: "Invalid parameter: " + name}
}

// PublicError maps backend failures to safe protocol categories.
func PublicError(err error) *Error {
	var public *Error
	if errors.As(err, &public) {
		if public == nil {
			err = errors.New("provider returned a typed nil protocol error")
		} else if public.Status >= 400 && public.Status <= 599 {
			return public
		}
	}
	status, code := 500, "NoApplicableCode"
	switch {
	case errors.Is(err, ErrNotFound):
		status, code = 404, "NotFound"
	case errors.Is(err, ErrForbidden):
		status, code = 403, "Forbidden"
	case errors.Is(err, ErrUnavailable):
		status, code = 503, "ServerBusy"
	case errors.Is(err, ErrConflict):
		status, code = 409, "Conflict"
	case errors.Is(err, ErrNotImplemented):
		status, code = 501, "NotImplemented"
	case errors.Is(err, context.DeadlineExceeded):
		status, code = 504, "Timeout"
	case errors.Is(err, context.Canceled):
		status, code = 408, "RequestCancelled"
	}
	return &Error{Status: status, Code: code, Description: http.StatusText(status), Cause: err}
}

// Problem includes RFC 9457 fields and the OGC code/description exception fields.
// Cause is deliberately absent from this wire representation.
type Problem struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Detail      string `json:"detail"`
	Code        string `json:"code"`
	Description string `json:"description"`
}

// Problem returns the public JSON error representation.
func (e *Error) Problem() Problem {
	kind := e.Type
	if kind == "" {
		kind = "about:blank"
	}
	return Problem{Type: kind, Title: http.StatusText(e.Status), Status: e.Status,
		Detail: e.Description, Code: e.Code, Description: e.Description}
}
