// Package wms implements WMS URL requests independently of any Web framework.
package wms

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/kvp"
	"github.com/mapseekai/ogc-api/owscore"
)

// AxisOrder describes how a two-dimensional wire BBOX maps to business X/Y coordinates.
type AxisOrder uint8

const (
	UnknownAxis AxisOrder = iota
	XY
	YX
)

// AxisResolver returns axis metadata for a CRS or UnknownAxis when unresolved.
// It must be concurrency-safe when shared and must not perform reprojection.
type AxisResolver func(crs string) AxisOrder

// Options configures pure WMS parsing. Empty Versions enables every implemented version.
// AxisResolver supplements built-in metadata; server mode uses the catalog axis resolver.
type Options struct {
	Versions     []string
	Limits       owscore.Limits
	AxisResolver AxisResolver
}

// Versions returns an independent slice of implemented WMS versions in ascending order.
func Versions() []string { return []string{"1.0.0", "1.1.0", "1.1.1", "1.3.0"} }

// Validate rejects invalid limits and unsupported or duplicate configured versions.
func (o Options) Validate() error {
	if err := o.Limits.Validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, v := range o.Versions {
		if !slices.Contains(Versions(), v) || seen[v] {
			return fmt.Errorf("unsupported or repeated WMS version %q", v)
		}
		seen[v] = true
	}
	return nil
}

// CapabilitiesRequest preserves version negotiation and capability-document parameters.
// Format and UpdateSequence may be absent; Parameters retains original query semantics.
type CapabilitiesRequest struct {
	owscore.Meta
	Format         string
	UpdateSequence string
	Parameters     kvp.Params
}

// MapRequest is a typed, ordered WMS map selection; it does not contain rendered data.
// Bounds uses business X/Y only when Normalized is true. WireBBox is always unchanged.
// Catalog validation fills LayerDimensions before the business handler is called.
type MapRequest struct {
	owscore.Meta
	Layers             []string
	Styles             []string
	CRS                string
	WireBBox           [4]float64
	Bounds             [4]float64
	Normalized         bool
	Width, Height      uint64
	WireFormat, Format string
	Transparent        bool
	Background         uint32
	Exceptions         string
	Parameters         kvp.Params
	// LayerDimensions contains independently resolved defaults for each layer.
	LayerDimensions map[string]map[string]string
}

// FeatureInfoRequest extends a map selection with query layers and pixel coordinates.
// PixelColumn and PixelRow normalize X/Y or I/J names; no geographic conversion is performed.
type FeatureInfoRequest struct {
	MapRequest
	QueryLayers                []string
	PixelColumn, PixelRow      uint64
	WireInfoFormat, InfoFormat string
	FeatureCount               uint64
}

// Request contains protocol metadata and exactly one operation payload on successful parsing.
// On error, only metadata is usable for error encoding; discard any partial payload.
type Request struct {
	owscore.Meta
	Capabilities *CapabilitiesRequest
	Map          *MapRequest
	FeatureInfo  *FeatureInfoRequest
}

// Parse validates and decodes a raw WMS query using the requested limits and versions.
// It returns metadata even on failure and performs no catalog lookup or business I/O.
func Parse(raw string, opts Options) (*Request, error) {
	p, err := kvp.Parse(raw, opts.Limits)
	if err != nil {
		return &Request{Meta: owscore.Meta{Protocol: owscore.WMS, Binding: owscore.KVP}}, err
	}
	return ParseParams(p, opts)
}

// ParseParams returns the identified protocol/version even when a later field fails.
func ParseParams(p kvp.Params, opts Options) (*Request, error) {
	req := &Request{Meta: owscore.Meta{Protocol: owscore.WMS, Binding: owscore.KVP, Version: "1.3.0"}}
	if err := opts.Validate(); err != nil {
		return req, err
	}
	versions := slices.Clone(opts.Versions)
	if len(versions) == 0 {
		versions = Versions()
	}
	slices.SortFunc(versions, func(a, b string) int {
		av, _ := owscore.ParseVersion(a)
		bv, _ := owscore.ParseVersion(b)
		return av.Compare(bv)
	})
	req.Version = versions[len(versions)-1]
	r := kvp.Reader{Params: p}
	service := r.String("SERVICE", false, false)
	if service != "" && service != "WMS" {
		r.SetError(owscore.Invalid("SERVICE"))
	}
	op := r.String("REQUEST", true, false)
	version := r.String("VERSION", false, false)
	old := r.String("WMTVER", false, false)
	if version != "" && old != "" && version != old {
		r.SetError(owscore.Invalid("WMTVER"))
	}
	if version == "" {
		version = old
	}
	req.RequestedVersion = version
	switch op {
	case "GetCapabilities", "capabilities":
		req.Operation = owscore.GetCapabilities
	case "GetMap", "map":
		req.Operation = owscore.GetMap
	case "GetFeatureInfo", "feature_info":
		req.Operation = owscore.GetFeatureInfo
	default:
		r.SetError(owscore.Fail(owscore.UnsupportedOperation, "REQUEST", "Unsupported WMS operation"))
	}
	if version != "" {
		parsed, err := owscore.ParseVersion(version)
		r.SetError(err)
		if err == nil && req.Operation == owscore.GetCapabilities {
			req.Version = versions[0]
			for _, v := range versions {
				candidate, _ := owscore.ParseVersion(v)
				if candidate.Compare(parsed) <= 0 {
					req.Version = v
				}
			}
		} else if slices.Contains(versions, version) {
			req.Version = version
		} else if err == nil {
			r.SetError(owscore.Fail(owscore.VersionMismatch, "VERSION", "Unsupported WMS version"))
		}
	} else if req.Operation != owscore.GetCapabilities {
		r.SetError(owscore.Missing("VERSION"))
	}
	if req.Operation != owscore.GetCapabilities {
		if req.Version == "1.0.0" {
			if !p.Has("WMTVER") {
				r.SetError(owscore.Missing("WMTVER"))
			}
			expected := "map"
			if req.Operation == owscore.GetFeatureInfo {
				expected = "feature_info"
			}
			if op != expected {
				r.SetError(owscore.Invalid("REQUEST"))
			}
		} else if op != string(req.Operation) {
			r.SetError(owscore.Invalid("REQUEST"))
		}
	}
	if req.Operation == owscore.GetCapabilities {
		if op == "GetCapabilities" && old == "" && service == "" {
			r.SetError(owscore.Missing("SERVICE"))
		}
		req.Capabilities = &CapabilitiesRequest{Meta: req.Meta, Format: r.String("FORMAT", false, false), UpdateSequence: r.String("UPDATESEQUENCE", false, true), Parameters: p}
		return req, r.Err
	}
	if r.Err != nil {
		return req, r.Err
	}
	m, err := parseMap(p, req.Meta, opts)
	if err != nil {
		return req, err
	}
	if req.Operation == owscore.GetMap {
		req.Map = m
		return req, nil
	}
	info := &FeatureInfoRequest{MapRequest: *m}
	info.QueryLayers = r.ListLimit("QUERY_LAYERS", true, opts.Limits.Defaults().MaxLayers)
	if r.Err != nil {
		return req, r.Err
	}
	if len(info.QueryLayers) == 0 {
		r.SetError(owscore.Missing("QUERY_LAYERS"))
	}
	for _, layer := range info.QueryLayers {
		if layer == "" || !slices.Contains(m.Layers, layer) {
			r.SetError(owscore.Invalid("QUERY_LAYERS"))
		}
	}
	col, row := "X", "Y"
	if req.Version == "1.3.0" {
		col, row = "I", "J"
	}
	info.PixelColumn = r.Uint(col, true, 0)
	info.PixelRow = r.Uint(row, true, 0)
	if info.PixelColumn >= m.Width {
		r.SetError(owscore.Fail(owscore.PointOutOfRange, col, "Pixel column is outside the map"))
	}
	if info.PixelRow >= m.Height {
		r.SetError(owscore.Fail(owscore.PointOutOfRange, row, "Pixel row is outside the map"))
	}
	info.WireInfoFormat = r.String("INFO_FORMAT", req.Version == "1.3.0", false)
	info.InfoFormat = info.WireInfoFormat
	if req.Version == "1.0.0" {
		if info.WireInfoFormat == "" {
			info.WireInfoFormat = "MIME"
		}
		if info.WireInfoFormat == "MIME" {
			info.InfoFormat = ""
		} else if !slices.Contains([]string{"GML.1", "GML.2", "GML.3"}, info.WireInfoFormat) {
			r.SetError(owscore.Fail(owscore.InvalidFormat, "INFO_FORMAT", "Unsupported legacy information format"))
		}
	}
	info.FeatureCount = 1
	if value, present, e := p.One("FEATURE_COUNT"); e != nil {
		r.SetError(e)
	} else if present {
		n, e := kvp.ParseUint(value)
		if e == nil && n > 0 {
			info.FeatureCount = n
		} else if req.Version != "1.3.0" {
			r.SetError(owscore.Invalid("FEATURE_COUNT"))
		}
		// WMS 1.3.0 section 7.4.3.6 explicitly defaults invalid counts to one.
	}
	req.FeatureInfo = info
	return req, r.Err
}
func parseMap(p kvp.Params, meta owscore.Meta, opts Options) (*MapRequest, error) {
	r := kvp.Reader{Params: p}
	limits := opts.Limits.Defaults()
	m := &MapRequest{Meta: meta, Parameters: p, Background: 0xFFFFFF}
	m.Layers = r.ListLimit("LAYERS", true, limits.MaxLayers)
	if r.Err != nil {
		return m, r.Err
	}
	m.Styles = r.ListLimit("STYLES", true, limits.MaxLayers)
	if r.Err != nil {
		return m, r.Err
	}
	for _, layer := range m.Layers {
		if layer == "" {
			r.SetError(owscore.Invalid("LAYERS"))
		}
	}
	if len(m.Styles) == 1 && m.Styles[0] == "" {
		m.Styles = make([]string, len(m.Layers))
	}
	if len(m.Styles) != len(m.Layers) {
		r.SetError(owscore.Invalid("STYLES"))
	}
	crsParam := "SRS"
	if meta.Version == "1.3.0" {
		crsParam = "CRS"
	}
	m.CRS = r.String(crsParam, true, false)
	bbox := strings.SplitN(r.String("BBOX", true, false), ",", 5)
	if len(bbox) != 4 {
		r.SetError(owscore.Invalid("BBOX"))
	} else {
		for i, v := range bbox {
			n, e := strconv.ParseFloat(v, 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || strings.ContainsAny(v, "xXpP_") {
				r.SetError(owscore.Invalid("BBOX"))
			}
			m.WireBBox[i] = n
		}
		if m.WireBBox[0] >= m.WireBBox[2] || m.WireBBox[1] >= m.WireBBox[3] {
			r.SetError(owscore.Invalid("BBOX"))
		}
	}
	m.Bounds = m.WireBBox
	axis := XY
	if meta.Version == "1.3.0" {
		axis = BuiltinAxis(m.CRS)
		if opts.AxisResolver != nil {
			if resolved := opts.AxisResolver(m.CRS); resolved != UnknownAxis {
				axis = resolved
			}
		}
	}
	m.Normalized = axis != UnknownAxis
	if axis == YX {
		m.Bounds = [4]float64{m.WireBBox[1], m.WireBBox[0], m.WireBBox[3], m.WireBBox[2]}
	}
	m.Width = r.Uint("WIDTH", true, 0)
	m.Height = r.Uint("HEIGHT", true, 0)
	if m.Width == 0 || m.Width > limits.MaxImageWidth {
		r.SetError(owscore.Invalid("WIDTH"))
	}
	if m.Height == 0 || m.Height > limits.MaxImageHeight {
		r.SetError(owscore.Invalid("HEIGHT"))
	}
	if m.Height > 0 && m.Width > limits.MaxImagePixels/m.Height {
		r.SetError(owscore.Fail(owscore.TooLarge, "WIDTH", "Image pixel count exceeds the configured limit"))
	}
	m.WireFormat = r.String("FORMAT", true, false)
	m.Format = m.WireFormat
	if meta.Version == "1.0.0" {
		var ok bool
		m.Format, ok = LegacyMediaType(m.WireFormat)
		if !ok {
			r.SetError(owscore.Fail(owscore.InvalidFormat, "FORMAT", "Unsupported legacy format name"))
		}
	}
	if value := r.String("TRANSPARENT", false, false); value != "" {
		if value != "TRUE" && value != "FALSE" {
			r.SetError(owscore.Invalid("TRANSPARENT"))
		}
		m.Transparent = value == "TRUE"
	}
	if value := r.String("BGCOLOR", false, false); value != "" {
		if len(value) != 8 || !strings.HasPrefix(value, "0x") {
			r.SetError(owscore.Invalid("BGCOLOR"))
		} else {
			n, e := strconv.ParseUint(value[2:], 16, 24)
			if e != nil {
				r.SetError(owscore.Invalid("BGCOLOR"))
			}
			m.Background = uint32(n)
		}
	}
	m.Exceptions = r.String("EXCEPTIONS", false, false)
	if m.Exceptions == "" {
		m.Exceptions = ExceptionFormat(meta.Version)
	}
	return m, r.Err
}

// BuiltinAxis recognizes a small documented set of CRS identifiers, returning UnknownAxis otherwise.
func BuiltinAxis(crs string) AxisOrder {
	switch crs {
	case "EPSG:4326", "urn:ogc:def:crs:EPSG::4326", "http://www.opengis.net/def/crs/EPSG/0/4326":
		return YX
	case "CRS:84", "EPSG:3857", "urn:ogc:def:crs:OGC:1.3:CRS84":
		return XY
	default:
		return UnknownAxis
	}
}

// ExceptionFormat returns the default exception format for a supported WMS version.
// WMS 1.0.0 defaults to INIMAGE; this library does not render exception images.
func ExceptionFormat(version string) string {
	switch version {
	case "1.0.0":
		return "INIMAGE"
	case "1.3.0":
		return "XML"
	default:
		return "application/vnd.ogc.se_xml"
	}
}

// XMLExceptionFormat returns the XML exception request value for a supported WMS version.
func XMLExceptionFormat(version string) string {
	if version == "1.0.0" {
		return "WMS_XML"
	}
	return ExceptionFormat(version)
}

// LegacyMediaType maps a recognized WMS 1.0.0 format name to its MIME type.
// The boolean is false for unrecognized names.
func LegacyMediaType(name string) (string, bool) {
	switch name {
	case "PNG":
		return "image/png", true
	case "JPEG":
		return "image/jpeg", true
	case "GIF":
		return "image/gif", true
	case "TIFF", "GeoTIFF":
		return "image/tiff", true
	case "PPM":
		return "image/x-portable-pixmap", true
	case "WBMP":
		return "image/vnd.wap.wbmp", true
	case "WebCGM":
		return "image/cgm", true
	case "SVG":
		return "image/svg+xml", true
	case "GML.1", "GML.2", "GML.3":
		return "application/vnd.ogc.gml", true
	}
	return "", false
}

// LegacyFormat returns the preferred WMS 1.0.0 name for a MIME type, or an empty string.
// Several legacy profiles can share one MIME type; this is not an exact inverse.
func LegacyFormat(media string) string {
	for _, name := range []string{"PNG", "JPEG", "GIF", "TIFF", "PPM", "WBMP", "WebCGM", "SVG", "GML.1"} {
		if m, _ := LegacyMediaType(name); m == media {
			return name
		}
	}
	return ""
}
