// Package api serves resource-oriented OGC APIs independently of HTTP frameworks and GIS engines.
package api

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/mapseekai/ogc-api/internal/admission"
)

// Input contains borrowed transport data. EscapedPath is the original URL path.
// Scope is supplied by trusted host routing, never copied from a tenant query parameter.
// The host owns Body and configures network read/write deadlines; Handle never closes it.
// Retain or copy framework-borrowed strings until Plan.Finish, since lazy streams may use Scope.
type Input struct {
	Method      string
	EscapedPath string
	RawQuery    string
	Accept      string
	IfNoneMatch string
	ContentType string
	Prefer      string
	Body        io.Reader
	Scope       string
}

// Limits bounds parsing and per-item encoding, not the total size of a streamed response.
type Limits struct {
	MaxQueryBytes    int
	MaxPathBytes     int
	MaxParameters    int
	MaxHeaderBytes   int
	MaxBodyBytes     int64
	MaxFeatureBytes  int
	MaxDocumentBytes int
}

// Defaults returns finite deployment defaults without changing the receiver.
func (l Limits) Defaults() Limits {
	if l.MaxQueryBytes == 0 {
		l.MaxQueryBytes = 64 << 10
	}
	if l.MaxPathBytes == 0 {
		l.MaxPathBytes = 16 << 10
	}
	if l.MaxParameters == 0 {
		l.MaxParameters = 128
	}
	if l.MaxHeaderBytes == 0 {
		l.MaxHeaderBytes = 16 << 10
	}
	if l.MaxBodyBytes == 0 {
		l.MaxBodyBytes = 1 << 20
	}
	if l.MaxFeatureBytes == 0 {
		l.MaxFeatureBytes = 1 << 20
	}
	if l.MaxDocumentBytes == 0 {
		l.MaxDocumentBytes = 8 << 20
	}
	return l
}

// Event records synchronous request lifecycle observations; BytesRead is not a client acknowledgement.
type Event struct {
	Stage     string
	Operation string
	Scope     string
	Status    int
	Elapsed   time.Duration
	BytesRead int64
	Error     error
}

// Observer must be concurrency-safe and non-blocking. Panics propagate after owned content is released.
type Observer func(Event)

// Config contains immutable engine policy. Catalog and Observer remain caller-owned functions.
// MaxConcurrent zero uses 1024 active responses. Slots remain held until Plan.Finish,
// including streaming responses; overload is rejected immediately without a waiting queue.
type Config struct {
	Catalog       CatalogProvider
	Limits        Limits
	MaxConcurrent int
	Timeout       time.Duration
	Observer      Observer
}

// Engine is safe for concurrent Handle calls; providers protect their own shared state.
// It opens no sockets, starts no goroutines and contains no data source implementations.
type Engine struct {
	config Config
	gate   admission.Gate
}

// New validates immutable policy without invoking providers or performing network access.
func New(config Config) (*Engine, error) {
	if config.Catalog == nil {
		return nil, fmt.Errorf("an API catalog provider is required")
	}
	if config.Timeout < 0 || config.MaxConcurrent < 0 {
		return nil, fmt.Errorf("negative API timeout or concurrency limit")
	}
	limits := config.Limits.Defaults()
	if limits.MaxQueryBytes < 1 || limits.MaxPathBytes < 1 || limits.MaxParameters < 1 || limits.MaxHeaderBytes < 1 || limits.MaxBodyBytes < 1 || limits.MaxFeatureBytes < 1 || limits.MaxDocumentBytes < 1 {
		return nil, fmt.Errorf("API resource limits must be positive")
	}
	config.Limits = limits
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 1024
	}
	return &Engine{config: config, gate: admission.New(config.MaxConcurrent)}, nil
}

// CatalogProvider returns one immutable, authorization-filtered catalog per request.
// Metadata and backend bindings must come from that same snapshot to prevent split updates.
type CatalogProvider func(context.Context, string) (*Snapshot, error)
