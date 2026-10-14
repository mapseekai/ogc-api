package wmts

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/mapseekai/ogc-api/kvp"
	"github.com/mapseekai/ogc-api/owscore"
)

// Template supports one simple {identifier} slot per path segment, optionally
// surrounded by literals. It intentionally is not an RFC 6570 expression engine.
type Template struct {
	Pattern   string
	Operation owscore.Operation
	Fixed     map[string]string
	// Dimensions are the explicitly declared additional path/query identifiers.
	Dimensions []string
}

// Router owns compiled, unambiguous resource templates and may be shared between requests.
// It performs no network I/O and does not mutate caller templates after construction.
type Router struct {
	routes []route
	limits owscore.Limits
}

// ResourceURL describes a client-visible template derived from the same route used for matching.
type ResourceURL struct{ Format, ResourceType, Template string }

// NewRouter copies templates and rejects ambiguous paths, missing fields and reserved dimensions.
// It validates route structure, not dynamic catalog values; Resources performs catalog filtering.
func NewRouter(templates []Template, limits owscore.Limits) (*Router, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	out := &Router{limits: limits.Defaults()}
	for _, template := range templates {
		r, err := compile(template)
		if err != nil {
			return nil, err
		}
		for _, previous := range out.routes {
			if overlap(previous, r) {
				return nil, fmt.Errorf("ambiguous REST templates %q and %q", previous.template.Pattern, r.template.Pattern)
			}
		}
		out.routes = append(out.routes, r)
	}
	return out, nil
}

// Match parses an escaped absolute path and raw query, enforcing both input budgets.
// Path segments are decoded once; a plus sign is not a space. Conflicting query values fail.
func (router *Router) Match(escapedPath, rawQuery string) (*Request, error) {
	empty := &Request{Meta: owscore.Meta{Protocol: owscore.WMTS, Version: "1.0.0", Binding: owscore.REST}}
	if len(escapedPath) > router.limits.MaxPathBytes {
		return empty, owscore.Fail(owscore.TooLarge, "PATH", "Path exceeds the configured limit")
	}
	p, err := kvp.Parse(rawQuery, router.limits)
	if err != nil {
		return empty, err
	}
	return router.MatchParams(escapedPath, p)
}

// MatchParams reuses parsed query fields when a host has already scanned the URL.
func (router *Router) MatchParams(escapedPath string, p kvp.Params) (*Request, error) {
	empty := &Request{Meta: owscore.Meta{Protocol: owscore.WMTS, Version: "1.0.0", Binding: owscore.REST}}
	if len(escapedPath) > router.limits.MaxPathBytes {
		return empty, owscore.Fail(owscore.TooLarge, "PATH", "Path exceeds the configured limit")
	}
	if !strings.HasPrefix(escapedPath, "/") {
		return empty, owscore.Invalid("PATH")
	}
	parts := strings.Split(escapedPath, "/")[1:]
	for _, r := range router.routes {
		if len(parts) != len(r.segments) {
			continue
		}
		matched := true
		for i, s := range r.segments {
			if _, ok := s.match(parts[i]); !ok {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		// Allocate and decode only after the entire candidate path matches.
		fields := maps.Clone(r.fixed)
		for i, s := range r.segments {
			if s.key == "" {
				continue
			}
			raw, _ := s.match(parts[i])
			value, err := url.PathUnescape(raw)
			if err != nil {
				return empty, owscore.Invalid(s.key)
			}
			fields[s.key] = value
		}
		empty.Operation = r.template.Operation
		bound, e := p.Merge(fields)
		if e != nil {
			return empty, e
		}
		return ParseParams(bound, owscore.REST)
	}
	return empty, owscore.Fail(owscore.NotFound, "PATH", "Resource does not exist")
}

// Resources derives templates usable by a named layer in a previously checked catalog.
// Fixed matrix identifiers, tile limits, pixels and enumerated dimensions must admit
// at least one valid selection. Custom dimension validators run only on real requests.
// Missing layers return no resources. The method does not mutate the catalog.
func (router *Router) Resources(layerID string, catalog Catalog, baseURL string, featureInfo bool) []ResourceURL {
	layer, ok := catalog.Layers[layerID]
	if !ok {
		return nil
	}
	var out []ResourceURL
	for _, r := range router.routes {
		if r.template.Operation == owscore.GetCapabilities {
			continue
		}
		if r.template.Operation == owscore.GetFeatureInfo && !featureInfo {
			continue
		}
		if value, ok := r.fixed["LAYER"]; ok && value != layerID {
			continue
		}
		if !resourceApplies(r, layer, catalog) {
			continue
		}
		parts := make([]string, len(r.segments))
		for i, s := range r.segments {
			if s.key == "" {
				parts[i] = s.literal
			} else if s.key == "LAYER" {
				parts[i] = s.prefix + url.PathEscape(layerID) + s.suffix
			} else {
				parts[i] = s.prefix + "{" + s.name + "}" + s.suffix
			}
		}
		resource := ResourceURL{Format: r.fixed["FORMAT"], ResourceType: "tile", Template: strings.TrimSuffix(baseURL, "/") + "/" + strings.Join(parts, "/")}
		if r.template.Operation == owscore.GetFeatureInfo {
			resource.Format = r.fixed["INFOFORMAT"]
			resource.ResourceType = "FeatureInfo"
		}
		out = append(out, resource)
	}
	return out
}

// MetadataPaths returns a new slice of fixed capability-resource paths relative to the public base URL.
func (router *Router) MetadataPaths() []string {
	var paths []string
	for _, r := range router.routes {
		if r.template.Operation == owscore.GetCapabilities {
			paths = append(paths, r.template.Pattern)
		}
	}
	return paths
}

type route struct {
	template Template
	segments []segment
	fixed    map[string]string
}

type segment struct {
	literal, prefix, suffix, key, name string
}

func compile(t Template) (route, error) {
	r := route{template: t, fixed: map[string]string{"SERVICE": "WMTS", "VERSION": "1.0.0", "REQUEST": string(t.Operation)}}
	if !slices.Contains([]owscore.Operation{owscore.GetCapabilities, owscore.GetTile, owscore.GetFeatureInfo}, t.Operation) {
		return r, fmt.Errorf("invalid REST operation")
	}
	if !strings.HasPrefix(t.Pattern, "/") || strings.ContainsAny(t.Pattern, "?#\\\r\n\x00") {
		return r, fmt.Errorf("REST template must be an absolute path without a query or fragment")
	}
	dimensions := make(map[string]bool, len(t.Dimensions))
	for _, d := range t.Dimensions {
		key := kvp.Key(d)
		if key == "" || reservedParameter(key) || dimensions[key] {
			return r, fmt.Errorf("conflicting dimension %q", d)
		}
		dimensions[key] = true
	}
	for k, v := range t.Fixed {
		key := kvp.Key(k)
		if (!reservedParameter(key) && !dimensions[key]) || v == "" {
			return r, fmt.Errorf("invalid fixed REST field %q", k)
		}
		if previous, ok := r.fixed[key]; ok && previous != v {
			return r, fmt.Errorf("conflicting fixed REST field %q", k)
		}
		r.fixed[key] = v
	}
	present := map[string]bool{}
	for k := range r.fixed {
		present[k] = true
	}
	for _, part := range strings.Split(t.Pattern, "/")[1:] {
		s := segment{literal: part}
		if strings.ContainsAny(part, "{}") {
			start := strings.IndexByte(part, '{')
			end := strings.IndexByte(part, '}')
			if start < 0 || end < start || strings.Count(part, "{") != 1 || strings.Count(part, "}") != 1 {
				return r, fmt.Errorf("invalid REST placeholder")
			}
			name := part[start+1 : end]
			key := kvp.Key(name)
			if (!reservedParameter(key) && !dimensions[key]) || present[key] {
				return r, fmt.Errorf("duplicate or undeclared REST placeholder %q", name)
			}
			s = segment{prefix: part[:start], suffix: part[end+1:], key: key, name: name}
			present[key] = true
		} else if _, e := url.PathUnescape(part); e != nil {
			return r, fmt.Errorf("invalid escaped REST literal")
		}
		r.segments = append(r.segments, s)
	}
	required := []string{}
	if t.Operation != owscore.GetCapabilities {
		required = []string{"LAYER", "STYLE", "TILEMATRIXSET", "TILEMATRIX", "TILEROW", "TILECOL"}
	}
	if t.Operation == owscore.GetTile {
		required = append(required, "FORMAT")
	}
	if t.Operation == owscore.GetFeatureInfo {
		required = append(required, "INFOFORMAT", "I", "J")
	}
	if t.Operation == owscore.GetTile && r.fixed["FORMAT"] == "" {
		return r, fmt.Errorf("tile templates require a fixed FORMAT; use one template per media type")
	}
	if t.Operation == owscore.GetFeatureInfo && r.fixed["INFOFORMAT"] == "" {
		return r, fmt.Errorf("feature-info templates require a fixed INFOFORMAT")
	}
	for _, key := range required {
		if !present[key] {
			return r, fmt.Errorf("missing REST field %s", key)
		}
	}
	if t.Operation == owscore.GetCapabilities {
		for _, s := range r.segments {
			if s.key != "" {
				return r, fmt.Errorf("metadata resource must have a fixed path")
			}
		}
	}
	r.template.Fixed = maps.Clone(t.Fixed)
	r.template.Dimensions = slices.Clone(t.Dimensions)
	return r, nil
}

func overlap(a, b route) bool {
	if len(a.segments) != len(b.segments) {
		return false
	}
	for i, x := range a.segments {
		y := b.segments[i]
		switch {
		case x.key == "" && y.key == "":
			if x.literal != y.literal {
				return false
			}
		case x.key == "":
			if _, ok := y.match(x.literal); !ok {
				return false
			}
		case y.key == "":
			if _, ok := x.match(y.literal); !ok {
				return false
			}
		default:
			if !strings.HasPrefix(x.prefix, y.prefix) && !strings.HasPrefix(y.prefix, x.prefix) {
				return false
			}
			if !strings.HasSuffix(x.suffix, y.suffix) && !strings.HasSuffix(y.suffix, x.suffix) {
				return false
			}
		}
	}
	return true
}

// A shared template must not advertise fixed values absent from this layer.
func resourceApplies(r route, layer Layer, catalog Catalog) bool {
	formats, field := layer.Formats, "FORMAT"
	if r.template.Operation == owscore.GetFeatureInfo {
		formats, field = layer.InfoFormats, "INFOFORMAT"
	}
	if !slices.Contains(formats, r.fixed[field]) {
		return false
	}
	if style, ok := r.fixed["STYLE"]; ok {
		if _, exists := layer.Styles[style]; !exists {
			return false
		}
	}
	if set, ok := r.fixed["TILEMATRIXSET"]; ok {
		if _, exists := layer.MatrixSets[set]; !exists {
			return false
		}
	}
	for _, name := range r.template.Dimensions {
		found := false
		for declared, dimension := range layer.Dimensions {
			if kvp.Key(name) != kvp.Key(declared) {
				continue
			}
			found = true
			// Custom validators belong to requests, not metadata generation.
			if value, fixed := r.fixed[kvp.Key(name)]; fixed && dimension.Validate == nil {
				if _, err := dimension.Resolve(context.Background(), name, value, value != "default"); err != nil {
					return false
				}
			}
			break
		}
		if !found {
			return false
		}
	}
	for setID := range layer.MatrixSets {
		if fixed, ok := r.fixed["TILEMATRIXSET"]; ok && fixed != setID {
			continue
		}
		for matrixID := range catalog.MatrixSets[setID].Matrices {
			if fixed, ok := r.fixed["TILEMATRIX"]; ok && fixed != matrixID {
				continue
			}
			matrix, bounds, err := catalog.matrixFor(layer, setID, matrixID)
			if err != nil {
				continue
			}
			row, rowErr := fixedPosition(r.fixed, "TILEROW", bounds.MinTileRow)
			col, colErr := fixedPosition(r.fixed, "TILECOL", bounds.MinTileCol)
			if rowErr != nil || colErr != nil || validateTilePosition(bounds, row, col) != nil {
				continue
			}
			if r.template.Operation == owscore.GetFeatureInfo {
				i, iErr := fixedPosition(r.fixed, "I", 0)
				j, jErr := fixedPosition(r.fixed, "J", 0)
				if iErr != nil || jErr != nil || validatePixelPosition(matrix, i, j) != nil {
					continue
				}
			}
			return true
		}
	}
	return false
}

func fixedPosition(fields map[string]string, key string, fallback uint64) (uint64, error) {
	if value, ok := fields[key]; ok {
		return kvp.ParseUint(value)
	}
	return fallback, nil
}

func (s segment) match(part string) (string, bool) {
	if s.key == "" {
		return "", part == s.literal
	}
	if len(part) <= len(s.prefix)+len(s.suffix) || !strings.HasPrefix(part, s.prefix) || !strings.HasSuffix(part, s.suffix) {
		return "", false
	}
	return part[len(s.prefix) : len(part)-len(s.suffix)], true
}
