package wms

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/mapseekai/ogc-api/kvp"
	"github.com/mapseekai/ogc-api/owscore"
)

// Layer describes one named WMS layer and its supported CRS, styles and dimensions.
// GeographicBounds is west, south, east, north in degrees, independent of request axis order.
type Layer struct {
	Title, Abstract  string
	CRS              []string
	GeographicBounds [4]float64
	Styles           map[string]owscore.Style
	Queryable        bool
	Dimensions       map[string]owscore.Dimension
}

// Catalog is mutable service configuration until validated and copied into a server snapshot.
// Do not mutate its maps or slices concurrently with validation or encoding.
type Catalog struct {
	Service              owscore.Service
	Layers               map[string]Layer
	Formats, InfoFormats []string
	// Axes supplements built-in CRS metadata; it performs no reprojection.
	Axes           map[string]AxisOrder
	UpdateSequence string
}

// Axis returns configured axis metadata, then falls back to BuiltinAxis.
// It performs no coordinate transformation.
func (c Catalog) Axis(crs string) AxisOrder {
	if axis := c.Axes[crs]; axis != UnknownAxis {
		return axis
	}
	return BuiltinAxis(crs)
}

// Clone deep-copies catalog maps and slices while sharing dimension validator functions.
func (c Catalog) Clone() Catalog {
	c.Service.Keywords = slices.Clone(c.Service.Keywords)
	c.Formats = slices.Clone(c.Formats)
	c.InfoFormats = slices.Clone(c.InfoFormats)
	c.Axes = maps.Clone(c.Axes)
	c.Layers = maps.Clone(c.Layers)
	for name, l := range c.Layers {
		l.CRS = slices.Clone(l.CRS)
		l.Styles = maps.Clone(l.Styles)
		l.Dimensions = owscore.CloneDimensions(l.Dimensions)
		c.Layers[name] = l
	}
	return c
}

// Check validates catalog declarations and supported axis mappings without mutating them.
// It does not invoke business handlers or dimension validators.
func (c Catalog) Check() error {
	if c.Service.Title == "" || len(c.Formats) == 0 {
		return fmt.Errorf("WMS title and map formats are required")
	}
	if err := owscore.ValidateFormats(c.Formats); err != nil {
		return err
	}
	if err := owscore.ValidateFormats(c.InfoFormats); err != nil {
		return err
	}
	for name, l := range c.Layers {
		if name == "" || l.Title == "" || len(l.CRS) == 0 {
			return fmt.Errorf("invalid WMS layer %q", name)
		}
		for _, crs := range l.CRS {
			axis := c.Axis(crs)
			if axis != XY && axis != YX {
				return fmt.Errorf("missing two-dimensional axis metadata for %q", crs)
			}
		}
		b := l.GeographicBounds
		for _, n := range b {
			if math.IsInf(n, 0) || math.IsNaN(n) {
				return fmt.Errorf("invalid geographic bounds")
			}
		}
		if b[0] >= b[2] || b[1] >= b[3] || b[0] < -180 || b[2] > 180 || b[1] < -90 || b[3] > 90 {
			return fmt.Errorf("invalid geographic bounds for %q", name)
		}
		if l.Queryable && len(c.InfoFormats) == 0 {
			return fmt.Errorf("queryable layer requires information formats")
		}
		for style, s := range l.Styles {
			if style == "" || s.Title == "" {
				return fmt.Errorf("invalid named style on %q", name)
			}
		}
		names := map[string]bool{}
		for dim, d := range l.Dimensions {
			key := kvp.Key(dim)
			if dim == "" || names[key] || strings.HasPrefix(key, "DIM_") {
				return fmt.Errorf("invalid or duplicate dimension %q", dim)
			}
			names[key] = true
			if d.Units == "" {
				return fmt.Errorf("WMS dimension %q requires units", dim)
			}
			if err := d.Check(); err != nil {
				return fmt.Errorf("dimension %s: %w", dim, err)
			}
		}
	}
	return nil
}

// ValidateMap checks a parsed request against this validated catalog.
// On success it fills r.LayerDimensions with per-layer defaults and resolved values.
// The request must be exclusively owned; discard partial changes after an error.
func (c Catalog) ValidateMap(ctx context.Context, r *MapRequest) error {
	if r == nil || len(r.Layers) == 0 || len(r.Styles) != len(r.Layers) {
		return owscore.Invalid("LAYERS")
	}
	if !slices.Contains(c.Formats, r.Format) {
		return owscore.Fail(owscore.InvalidFormat, "FORMAT", "Map format is not available")
	}
	if !r.Normalized {
		return owscore.Fail(owscore.InvalidCRS, "CRS", "Coordinate axis metadata is not available")
	}
	r.LayerDimensions = make(map[string]map[string]string, len(r.Layers))
	for i, name := range r.Layers {
		layer, ok := c.Layers[name]
		if !ok {
			return owscore.Fail(owscore.LayerNotDefined, "LAYERS", "Layer is not available")
		}
		if !slices.Contains(layer.CRS, r.CRS) {
			return owscore.Fail(owscore.InvalidCRS, "CRS", "CRS is not available for the requested layer")
		}
		if style := r.Styles[i]; style != "" {
			if _, ok := layer.Styles[style]; !ok {
				return owscore.Fail(owscore.StyleNotDefined, "STYLES", "Style is not available")
			}
		}
		values := map[string]string{}
		for dim, d := range layer.Dimensions {
			key := kvp.Key(dim)
			if key != "TIME" && key != "ELEVATION" {
				key = "DIM_" + key
			}
			value, present, err := r.Parameters.One(key)
			if err != nil {
				return err
			}
			value, err = d.Resolve(ctx, key, value, present)
			if err != nil {
				return err
			}
			values[dim] = value
		}
		r.LayerDimensions[name] = values
	}
	return nil
}

// ValidateInfo validates a non-nil, parsed feature-info request against the catalog.
// It fills LayerDimensions and may select a default InfoFormat. It does not repeat
// parser-level pixel and query-layer checks; discard the request after an error.
func (c Catalog) ValidateInfo(ctx context.Context, r *FeatureInfoRequest) error {
	if err := c.ValidateMap(ctx, &r.MapRequest); err != nil {
		return err
	}
	for _, name := range r.QueryLayers {
		if !c.Layers[name].Queryable {
			return owscore.Fail(owscore.LayerNotQueryable, "QUERY_LAYERS", "Layer is not queryable")
		}
	}
	if r.Version == "1.0.0" && strings.HasPrefix(r.WireInfoFormat, "GML.") {
		return owscore.Fail(owscore.InvalidFormat, "INFO_FORMAT", "Legacy GML profiles are not advertised by this catalog")
	}
	if r.InfoFormat == "" && len(c.InfoFormats) > 0 {
		r.InfoFormat = c.InfoFormats[0]
	}
	if !slices.Contains(c.InfoFormats, r.InfoFormat) {
		return owscore.Fail(owscore.InvalidFormat, "INFO_FORMAT", "Information format is not available")
	}
	return nil
}
