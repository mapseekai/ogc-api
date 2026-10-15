// Package server combines parsers, a trusted catalog and application callbacks.
package server

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/mapseekai/ogc-api/internal/admission"
	"github.com/mapseekai/ogc-api/kvp"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

const (
	Managed CapabilitiesMode = "managed"
	Encoded CapabilitiesMode = "encoded"
)

// Input contains transport-neutral request data valid for the full Handle call.
// Use the raw query and escaped path, not a framework singleton map. Protocol, Binding
// and Scope are trusted routing context; they must not be populated from untrusted aliases.
type Input struct {
	Method      string
	RawQuery    string
	EscapedPath string
	// Accept contains the HTTP Accept field for WMTS REST representation negotiation.
	// An empty value accepts the representation selected by the resource template.
	Accept   string
	Protocol owscore.Protocol
	Binding  owscore.Binding
	Scope    string
}

// Endpoint groups trusted routing metadata for an example or host adapter.
// It does not register routes or start a server; an empty protocol selects SERVICE-based dispatch.
type Endpoint struct {
	Protocol owscore.Protocol
	Binding  owscore.Binding
	Scope    string
}

// Config defines immutable engine policy and trusted public endpoints.
// New copies value configuration and templates, but callback closures remain caller-owned.
// Limits applies to both protocols and takes precedence over WMS.Limits.
// Authentication, CORS, connection limits and content generation belong to the host.
type Config struct {
	// MaxConcurrent bounds complete response lifetimes; zero selects 1024 active responses.
	// Overload returns 503 without queueing or spawning work.
	MaxConcurrent int
	Catalog       CatalogProvider
	Handlers      Handlers
	WMS           wms.Options
	Limits        owscore.Limits
	WMSURL        string
	WMTSURL       string
	RESTBaseURL   string
	RESTTemplates []wmts.Template
	// Timeout covers catalog lookup, callbacks and normal content reads.
	// Zero inherits parent cancellation without adding a deadline. Error documents
	// use a separate one-second send window that still honors parent cancellation.
	Timeout             time.Duration
	AllowHEAD           bool
	CapabilitiesMode    CapabilitiesMode
	EncodedCapabilities func(context.Context, *CapabilitiesRequest) (response.Payload, error)
	// CompareUpdateSequence compares requested against current. It is required
	// when a catalog declares an opaque update sequence.
	CompareUpdateSequence func(requested, current string) (int, error)
	Observer              Observer
}

// Engine shares immutable protocol configuration across concurrent requests.
// Catalog providers, content handlers and observers must protect their own shared state.
type Engine struct {
	gate       admission.Gate
	config     Config
	rest       *wmts.Router
	restPrefix string
}

// New validates engine configuration and compiles routes without opening sockets or fetching catalogs.
// Runtime catalogs are checked when constructed as snapshots. The returned Engine may be
// shared if all supplied callbacks are concurrency-safe.
func New(config Config) (*Engine, error) {
	if config.Catalog == nil {
		return nil, fmt.Errorf("a catalog provider is required")
	}
	if config.MaxConcurrent < 0 {
		return nil, fmt.Errorf("negative concurrency limit")
	}
	if config.Timeout < 0 {
		return nil, fmt.Errorf("negative timeout")
	}
	if err := config.Limits.Validate(); err != nil {
		return nil, err
	}
	config.Limits = config.Limits.Defaults()
	config.WMS.Limits = config.Limits
	config.WMS.Versions = slices.Clone(config.WMS.Versions)
	if err := config.WMS.Validate(); err != nil {
		return nil, err
	}
	if config.WMSURL != "" {
		if err := validateURL(config.WMSURL); err != nil {
			return nil, fmt.Errorf("WMS URL: %w", err)
		}
		if config.Handlers.GetMap == nil {
			return nil, fmt.Errorf("a published WMS requires GetMap")
		}
	}
	if config.WMTSURL != "" {
		if err := validateURL(config.WMTSURL); err != nil {
			return nil, fmt.Errorf("WMTS URL: %w", err)
		}
		if config.Handlers.GetTile == nil {
			return nil, fmt.Errorf("a published WMTS requires GetTile")
		}
	}
	if config.WMSURL == "" && config.WMTSURL == "" {
		return nil, fmt.Errorf("at least one trusted public endpoint URL is required")
	}
	if config.CapabilitiesMode == "" {
		config.CapabilitiesMode = Managed
	}
	if config.CapabilitiesMode != Managed && config.CapabilitiesMode != Encoded {
		return nil, fmt.Errorf("unknown capabilities mode")
	}
	if (config.CapabilitiesMode == Encoded) != (config.EncodedCapabilities != nil) {
		return nil, fmt.Errorf("encoded capabilities mode and callback must be configured together")
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 1024
	}
	e := &Engine{config: config, gate: admission.New(config.MaxConcurrent)}
	if len(config.RESTTemplates) > 0 {
		if config.WMTSURL == "" {
			return nil, fmt.Errorf("REST resources require a WMTS service")
		}
		if err := validateURL(config.RESTBaseURL); err != nil {
			return nil, fmt.Errorf("REST base URL: %w", err)
		}
		base, _ := url.Parse(config.RESTBaseURL)
		if base.RawQuery != "" {
			return nil, fmt.Errorf("REST base URL must not contain a query")
		}
		e.restPrefix = strings.TrimSuffix(base.EscapedPath(), "/")
		router, err := wmts.NewRouter(config.RESTTemplates, config.Limits)
		if err != nil {
			return nil, err
		}
		e.rest = router
	}
	return e, nil
}

// Handlers contains independently optional business callbacks for already validated requests.
// Callbacks receive non-nil contexts and own each request exclusively; shared captured state
// must be concurrency-safe. Returned payloads transfer ownership even alongside an error.
type Handlers struct {
	GetMap             func(context.Context, *wms.MapRequest) (response.Payload, error)
	GetWMSFeatureInfo  func(context.Context, *wms.FeatureInfoRequest) (response.Payload, error)
	GetTile            func(context.Context, *wmts.TileRequest) (response.Payload, error)
	GetWMTSFeatureInfo func(context.Context, *wmts.FeatureInfoRequest) (response.Payload, error)
}

// CapabilitiesRequest supplies the selected version and one protocol-specific request
// to an encoded-capabilities callback. Treat it as read-only during the callback.
type CapabilitiesRequest struct {
	owscore.Meta
	WMS  *wms.CapabilitiesRequest
	WMTS *wmts.CapabilitiesRequest
}

// CapabilitiesMode selects catalog-generated XML or a validated pre-encoded business response.
type CapabilitiesMode string

// Event records one synchronous lifecycle observation. BytesRead counts server-side reads,
// not client acknowledgements. Error is a processing/transport error; ProtocolError is independent.
type Event struct {
	ProtocolError error
	Stage         string
	Meta          owscore.Meta
	Status        int
	Elapsed       time.Duration
	BytesRead     int64
	Error         error
}

// Observer receives synchronous events and must be concurrency-safe and non-blocking.
// Panics propagate; before handoff, the engine closes owned content without emitting
// another observer event. A finish event occurs only for a plan actually handed off.
type Observer func(Event)

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\x00") {
		return fmt.Errorf("expected a trusted absolute HTTP(S) URL without credentials or fragment")
	}
	p, err := kvp.Parse(u.RawQuery, owscore.Limits{})
	if err != nil {
		return err
	}
	for key := range p.All() {
		if slices.Contains([]string{"SERVICE", "REQUEST", "VERSION", "WMTVER", "TOKEN", "ACCESS_TOKEN", "API_KEY", "APIKEY", "KEY", "SIGNATURE", "AUTHORIZATION", "PASSWORD"}, key) || strings.HasPrefix(key, "X-AMZ-") {
			return fmt.Errorf("public URL contains a reserved or credential-like query field")
		}
	}
	return nil
}
