package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/exception"
	"github.com/mapseekai/ogc-api/kvp"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

// Error documents are in-memory and bounded; they get a separate send window.
const errorResponseTimeout = time.Second

// Handle converts expected protocol errors into response plans. A non-nil Go
// error means no plan could be built. Its text must not be exposed to clients.
// Parent must be non-nil. Normal content keeps the configured request timeout;
// in-memory error document reads get a one-second window under the still-live parent.
// Blocking transport writes still require host connection deadlines.
// Ownership passes to the transport only on a successful return. The transport
// must call Plan.Finish. Callback panics propagate after untransferred resources are cleaned up.
func (e *Engine) Handle(parent context.Context, input Input) (plan *response.Plan, resultErr error) {
	if !e.gate.Acquire() {
		e.observe(Event{Stage: "reject", Meta: owscore.Meta{Protocol: input.Protocol, Binding: input.Binding}, Status: 503, Error: owscore.Fail(owscore.Unavailable, "", "Service capacity is temporarily exhausted")})
		meta := owscore.Meta{Protocol: input.Protocol, Binding: input.Binding}
		if meta.Protocol == owscore.WMS {
			meta.Version = "1.3.0"
		}
		if meta.Protocol == owscore.WMTS {
			meta.Version = "1.0.0"
		}
		plan, err := exception.Encode(meta, owscore.Fail(owscore.Unavailable, "", "Service capacity is temporarily exhausted"))
		if plan != nil {
			plan.Headers["Retry-After"] = "1"
		}
		return plan, err
	}
	ctx, cancel := response.Timeout(parent, e.config.Timeout)
	var payload response.Payload
	handedOff := false
	defer func() {
		if handedOff {
			return
		}
		defer e.gate.Release()
		defer cancel()
		// No transport owns these resources if a callback or observer panics.
		if plan != nil {
			_ = plan.Body.Close()
		} else {
			_ = payload.Close()
		}
	}()
	started := time.Now()
	meta := owscore.Meta{Protocol: input.Protocol, Binding: input.Binding}
	if meta.Binding == "" {
		meta.Binding = owscore.KVP
	}
	if meta.Protocol == owscore.WMS {
		meta.Version = "1.3.0"
	}
	if meta.Protocol == owscore.WMTS {
		meta.Version = "1.0.0"
	}
	defer func() {
		if plan == nil || resultErr != nil {
			return
		}
		if plan.ProtocolError != nil {
			cancel()
			// Parent cancellation still wins; only the exhausted business budget is replaced.
			ctx, cancel = context.WithTimeout(parent, errorResponseTimeout)
		}
		if meta.Protocol == owscore.WMTS && meta.Binding == owscore.REST {
			plan.Headers["Vary"] = "Accept"
		}
		plan.Body = &contextBody{Reader: response.Reader{Context: ctx, Source: plan.Body, Length: plan.Length}, Closer: plan.Body}
		plan.BindLifetime(func() { defer e.gate.Release(); cancel() }, func(n int64, err error) {
			e.observe(Event{Stage: "finish", Meta: meta, ProtocolError: plan.ProtocolError, Status: plan.Status, Elapsed: time.Since(started), BytesRead: n, Error: err})
		})
		e.observe(Event{Stage: "handoff", Meta: meta, ProtocolError: plan.ProtocolError, Status: plan.Status, Elapsed: time.Since(started)})
		handedOff = true
	}()
	e.observe(Event{Stage: "start", Meta: meta})
	if input.Method != "GET" && !(input.Method == "HEAD" && e.config.AllowHEAD) {
		plan = response.New(405, response.Bytes("text/plain; charset=utf-8", []byte("Method not allowed")))
		allow := "GET"
		if e.config.AllowHEAD {
			allow += ", HEAD"
		}
		plan.Headers["Allow"] = allow
		return plan, nil
	}
	if len(input.EscapedPath) > e.config.Limits.MaxPathBytes {
		return exception.Encode(meta, owscore.Fail(owscore.TooLarge, "PATH", "Path exceeds the configured limit"))
	}
	params, err := kvp.Parse(input.RawQuery, e.config.Limits)
	if err != nil {
		return exception.Encode(meta, err)
	}
	if meta.Protocol == "" {
		service, _, err := params.One("SERVICE")
		if err != nil {
			return exception.Encode(meta, err)
		}
		if service != "WMS" && service != "WMTS" {
			return exception.Encode(meta, owscore.Missing("SERVICE"))
		}
		meta.Protocol = owscore.Protocol(service)
	}
	if meta.Binding != owscore.KVP && meta.Binding != owscore.REST {
		return exception.Encode(meta, owscore.Invalid("BINDING"))
	}
	if (meta.Protocol == owscore.WMS && e.config.WMSURL == "") || (meta.Protocol == owscore.WMTS && e.config.WMTSURL == "") {
		return exception.Encode(meta, owscore.Fail(owscore.UnsupportedOperation, "SERVICE", "Service is not available"))
	}
	snapshot, err := e.config.Catalog(ctx, input.Scope)
	if err != nil {
		return exception.Encode(meta, err)
	}
	if snapshot == nil {
		return exception.Encode(meta, errors.New("catalog provider returned nil"))
	}
	var expected string
	switch meta.Protocol {
	case owscore.WMS:
		if meta.Binding != owscore.KVP {
			return exception.Encode(meta, owscore.Invalid("BINDING"))
		}
		if snapshot.wms == nil {
			return exception.Encode(meta, owscore.Fail(owscore.UnsupportedOperation, "SERVICE", "Service is not available"))
		}
		opts := e.config.WMS
		opts.AxisResolver = snapshot.wms.Axis
		request, parseErr := wms.ParseParams(params, opts)
		meta = request.Meta
		if parseErr != nil {
			return e.wmsError(meta, params, parseErr)
		}
		payload, expected, err = e.handleWMS(ctx, snapshot.wms, request)
	case owscore.WMTS:
		if snapshot.wmts == nil {
			return exception.Encode(meta, owscore.Fail(owscore.UnsupportedOperation, "SERVICE", "Service is not available"))
		}
		var request *wmts.Request
		if meta.Binding == owscore.REST {
			if e.rest == nil {
				return exception.Encode(meta, owscore.Fail(owscore.NotFound, "PATH", "Resource does not exist"))
			}
			path := input.EscapedPath
			if e.restPrefix != "" {
				if !strings.HasPrefix(path, e.restPrefix+"/") {
					return exception.Encode(meta, owscore.Fail(owscore.NotFound, "PATH", "Resource does not exist"))
				}
				path = strings.TrimPrefix(path, e.restPrefix)
			}
			request, err = e.rest.MatchParams(path, params)
		} else {
			request, err = wmts.ParseParams(params, owscore.KVP)
		}
		meta = request.Meta
		if err != nil {
			return exception.Encode(meta, err)
		}
		payload, expected, err = e.handleWMTS(ctx, snapshot.wmts, request)
	default:
		return exception.Encode(meta, owscore.Invalid("SERVICE"))
	}
	e.observe(Event{Stage: "business", Meta: meta, Elapsed: time.Since(started), Error: err})
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = payload.Validate(expected)
	}
	if err == nil && meta.Protocol == owscore.WMTS && meta.Binding == owscore.REST {
		if _, negotiationErr := common.Negotiate(input.Accept, "", []string{payload.ContentType}); negotiationErr != nil {
			err = owscore.Fail(owscore.NotAcceptable, "Accept", "Resource representation is not acceptable")
		}
	}
	if err != nil {
		failed := payload
		payload = response.Payload{}
		err = errors.Join(err, failed.Close())
		if meta.Protocol == owscore.WMS {
			return e.wmsError(meta, params, err)
		}
		return exception.Encode(meta, err)
	}
	return response.New(200, payload), nil
}

func (e *Engine) handleWMS(ctx context.Context, catalog *wms.Catalog, request *wms.Request) (response.Payload, string, error) {
	var empty response.Payload
	switch request.Operation {
	case owscore.GetCapabilities:
		r := request.Capabilities
		expected := wms.CapabilitiesMediaType(r.Version)
		if r.Version != "1.3.0" && r.Format != "" && r.Format != expected && !(r.Version == "1.0.0" && r.Format == "WMS_XML") {
			return empty, "", owscore.Fail(owscore.InvalidFormat, "FORMAT", "Capabilities format is not available")
		}
		if _, err := e.checkSequence(owscore.WMS, r.UpdateSequence, catalog.UpdateSequence); err != nil {
			return empty, "", err
		}
		if e.config.CapabilitiesMode == Encoded {
			return e.encoded(ctx, &CapabilitiesRequest{Meta: r.Meta, WMS: r}, expected)
		}
		data, err := wms.EncodeCapabilities(*catalog, wms.CapabilitiesOptions{
			Version:     r.Version,
			Endpoint:    e.config.WMSURL,
			FeatureInfo: e.config.Handlers.GetWMSFeatureInfo != nil,
			Limits:      e.config.Limits,
		})
		return response.Bytes(expected, data), expected, err
	case owscore.GetMap:
		r := request.Map
		if err := validateWMSExceptions(r); err != nil {
			return empty, "", err
		}
		if err := catalog.ValidateMap(ctx, r); err != nil {
			return empty, "", err
		}
		if e.config.Handlers.GetMap == nil {
			return empty, "", owscore.Fail(owscore.UnsupportedOperation, "REQUEST", "Map operation is unavailable")
		}
		payload, err := e.config.Handlers.GetMap(ctx, r)
		return payload, r.Format, err
	case owscore.GetFeatureInfo:
		r := request.FeatureInfo
		if err := validateWMSExceptions(&r.MapRequest); err != nil {
			return empty, "", err
		}
		if err := catalog.ValidateInfo(ctx, r); err != nil {
			return empty, "", err
		}
		if e.config.Handlers.GetWMSFeatureInfo == nil {
			return empty, "", owscore.Fail(owscore.UnsupportedOperation, "REQUEST", "Feature information is unavailable")
		}
		payload, err := e.config.Handlers.GetWMSFeatureInfo(ctx, r)
		return payload, r.InfoFormat, err
	}
	return empty, "", owscore.Fail(owscore.UnsupportedOperation, "REQUEST", "Operation is unavailable")
}

func (e *Engine) handleWMTS(ctx context.Context, catalog *wmts.Catalog, request *wmts.Request) (response.Payload, string, error) {
	var empty response.Payload
	switch request.Operation {
	case owscore.GetCapabilities:
		r := request.Capabilities
		minimal, err := e.checkSequence(owscore.WMTS, r.UpdateSequence, catalog.UpdateSequence)
		if err != nil {
			return empty, "", err
		}
		if e.config.CapabilitiesMode == Encoded && !minimal {
			return e.encoded(ctx, &CapabilitiesRequest{Meta: r.Meta, WMTS: r}, "text/xml")
		}
		data, err := wmts.EncodeCapabilities(*catalog, wmts.CapabilitiesOptions{
			Endpoint:      e.config.WMTSURL,
			PublicBaseURL: e.config.RESTBaseURL,
			FeatureInfo:   e.config.Handlers.GetWMTSFeatureInfo != nil,
			Router:        e.rest,
			Limits:        e.config.Limits,
			Sections:      r.Sections,
			Minimal:       minimal,
		})
		return response.Bytes("text/xml", data), "text/xml", err
	case owscore.GetTile:
		r := request.Tile
		if _, err := catalog.ValidateTile(ctx, r); err != nil {
			return empty, "", err
		}
		if e.config.Handlers.GetTile == nil {
			return empty, "", owscore.Fail(owscore.UnsupportedOperation, "REQUEST", "Tile operation is unavailable")
		}
		payload, err := e.config.Handlers.GetTile(ctx, r)
		return payload, r.Format, err
	case owscore.GetFeatureInfo:
		r := request.FeatureInfo
		if err := catalog.ValidateInfo(ctx, r); err != nil {
			return empty, "", err
		}
		if e.config.Handlers.GetWMTSFeatureInfo == nil {
			return empty, "", owscore.Fail(owscore.UnsupportedOperation, "REQUEST", "Feature information is unavailable")
		}
		payload, err := e.config.Handlers.GetWMTSFeatureInfo(ctx, r)
		return payload, r.InfoFormat, err
	}
	return empty, "", owscore.Fail(owscore.UnsupportedOperation, "REQUEST", "Operation is unavailable")
}

func (e *Engine) checkSequence(protocol owscore.Protocol, requested, current string) (bool, error) {
	if requested == "" || current == "" {
		return false, nil
	}
	if e.config.CompareUpdateSequence == nil {
		return false, fmt.Errorf("catalog declares UpdateSequence without a comparator")
	}
	order, err := e.config.CompareUpdateSequence(requested, current)
	if err != nil {
		return false, owscore.Fail(owscore.InvalidUpdateSequence, "UPDATESEQUENCE", "Update sequence cannot be compared")
	}
	if order > 0 {
		return false, owscore.Fail(owscore.InvalidUpdateSequence, "UPDATESEQUENCE", "Requested update sequence is newer than the service")
	}
	if order == 0 {
		if protocol == owscore.WMTS {
			return true, nil
		}
		return false, owscore.Fail(owscore.CurrentUpdateSequence, "UPDATESEQUENCE", "The service metadata has not changed")
	}
	return false, nil
}

// WMS 1.0.0 allows an empty error response when image exceptions are requested.
// This library does not render error images. Explicit WMS_XML produces WMTException.
func (e *Engine) wmsError(meta owscore.Meta, params kvp.Params, err error) (*response.Plan, error) {
	if meta.Version == "1.0.0" && meta.Operation != owscore.GetCapabilities {
		format, present, parseErr := params.One("EXCEPTIONS")
		if parseErr == nil && (!present || format == "INIMAGE" || format == "BLANK") {
			plan := response.New(exception.Status(meta, owscore.PublicError(err)), response.Bytes("text/plain", nil))
			plan.ProtocolError = owscore.PublicError(err)
			return plan, nil
		}
	}
	return exception.Encode(meta, err)
}

func validateWMSExceptions(r *wms.MapRequest) error {
	if r.Exceptions == wms.XMLExceptionFormat(r.Version) {
		return nil
	}
	if r.Version == "1.0.0" && (r.Exceptions == "INIMAGE" || r.Exceptions == "BLANK") {
		return nil
	}
	return owscore.Fail(owscore.InvalidFormat, "EXCEPTIONS", "Only XML exceptions are available")
}

func (e *Engine) observe(event Event) {
	if e.config.Observer != nil {
		e.config.Observer(event)
	}
}

type contextBody struct {
	response.Reader
	io.Closer
}
