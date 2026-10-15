// Package wmts implements WMTS 1.0.0 KVP and REST requests.
package wmts

import (
	"slices"

	"github.com/mapseekai/ogc-api/kvp"
	"github.com/mapseekai/ogc-api/owscore"
)

// CapabilitiesRequest records WMTS negotiation, selected sections and revision parameters.
// It retains raw query fields; it does not contain a generated document.
type CapabilitiesRequest struct {
	owscore.Meta
	AcceptVersions, Sections, AcceptFormats []string
	UpdateSequence                          string
	Parameters                              kvp.Params
}

// TileRequest selects a tile by string matrix identifiers and unsigned row/column indices.
// Catalog validation fills Dimensions; the parser alone cannot validate matrix bounds.
type TileRequest struct {
	owscore.Meta
	Layer, Style, Format      string
	TileMatrixSet, TileMatrix string
	TileRow, TileCol          uint64
	Parameters                kvp.Params
	Dimensions                map[string]string
}

// FeatureInfoRequest adds pixel column/row and result format to a tile selection.
// FORMAT identifies the queried tile; INFOFORMAT describes the returned query data.
type FeatureInfoRequest struct {
	TileRequest
	PixelColumn, PixelRow uint64
	InfoFormat            string
}

// Request carries metadata and exactly one operation payload after successful parsing.
// On error, use only metadata for diagnostics and discard any partial payload.
type Request struct {
	owscore.Meta
	Capabilities *CapabilitiesRequest
	Tile         *TileRequest
	FeatureInfo  *FeatureInfoRequest
}

// Parse copies and validates a raw WMTS KVP query without consulting a catalog.
// Zero limit fields use defaults; the returned metadata remains available on error.
func Parse(raw string, limits owscore.Limits) (*Request, error) {
	p, err := kvp.Parse(raw, limits)
	if err != nil {
		return &Request{Meta: owscore.Meta{Protocol: owscore.WMTS, Binding: owscore.KVP, Version: "1.0.0"}}, err
	}
	return ParseParams(p, owscore.KVP)
}

// ParseParams interprets fields whose raw input limits were already enforced by kvp.Parse.
// REST callers must merge their trusted route fields first. Binding records error-response semantics.
func ParseParams(p kvp.Params, binding owscore.Binding) (*Request, error) {
	req := &Request{Meta: owscore.Meta{Protocol: owscore.WMTS, Binding: binding, Version: "1.0.0"}}
	r := kvp.Reader{Params: p}
	if service := r.String("SERVICE", true, false); service != "WMTS" {
		r.SetError(owscore.Invalid("SERVICE"))
	}
	req.Operation = owscore.Operation(r.String("REQUEST", true, false))
	if req.Operation != owscore.GetCapabilities {
		req.RequestedVersion = r.String("VERSION", true, false)
		if req.RequestedVersion != "" && req.RequestedVersion != "1.0.0" {
			r.SetError(owscore.Fail(owscore.VersionMismatch, "VERSION", "Unsupported WMTS version"))
		}
	}
	if req.Operation == owscore.GetCapabilities {
		c := &CapabilitiesRequest{Meta: req.Meta, Parameters: p}
		c.AcceptVersions = r.List("ACCEPTVERSIONS", false)
		if p.Has("ACCEPTVERSIONS") && !slices.Contains(c.AcceptVersions, "1.0.0") {
			r.SetError(owscore.Fail(owscore.VersionMismatch, "ACCEPTVERSIONS", "No supported version was requested"))
		}
		c.Sections = r.List("SECTIONS", false)
		for i, s := range c.Sections {
			if s == "all" {
				c.Sections[i] = "All"
				continue
			}
			if !slices.Contains([]string{"ServiceIdentification", "ServiceProvider", "OperationsMetadata", "Contents", "Themes", "All", ""}, s) {
				r.SetError(owscore.Invalid("SECTIONS"))
			}
		}
		c.AcceptFormats = r.List("ACCEPTFORMATS", false)
		if p.Has("ACCEPTFORMATS") && !slices.Contains(c.AcceptFormats, "text/xml") {
			r.SetError(owscore.Invalid("ACCEPTFORMATS"))
		}
		c.UpdateSequence = r.String("UPDATESEQUENCE", false, true)
		req.Capabilities = c
		return req, r.Err
	}
	if req.Operation != owscore.GetTile && req.Operation != owscore.GetFeatureInfo {
		r.SetError(owscore.Invalid("REQUEST"))
		return req, r.Err
	}
	tile := TileRequest{Meta: req.Meta, Parameters: p}
	tile.Layer = r.String("LAYER", true, false)
	tile.Style = r.String("STYLE", true, false)
	tile.Format = r.String("FORMAT", binding == owscore.KVP || req.Operation == owscore.GetTile, false)
	tile.TileMatrixSet = r.String("TILEMATRIXSET", true, false)
	tile.TileMatrix = r.String("TILEMATRIX", true, false)
	tile.TileRow = r.Uint("TILEROW", true, 0)
	tile.TileCol = r.Uint("TILECOL", true, 0)
	if req.Operation == owscore.GetTile {
		req.Tile = &tile
		return req, r.Err
	}
	info := &FeatureInfoRequest{TileRequest: tile}
	info.PixelColumn = r.Uint("I", true, 0)
	info.PixelRow = r.Uint("J", true, 0)
	info.InfoFormat = r.String("INFOFORMAT", true, false)
	req.FeatureInfo = info
	return req, r.Err
}
