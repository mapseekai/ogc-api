package wmts

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/mapseekai/ogc-api/kvp"
	"github.com/mapseekai/ogc-api/owscore"
)

// Matrix declares one tile grid. Width and height values are counts, not maximum indices.
// TopLeftCorner follows the declared CRS axes; no XYZ or 256-pixel assumptions are made.
type Matrix struct {
	ScaleDenominator          float64
	TopLeftCorner             [2]float64
	TileWidth, TileHeight     uint64
	MatrixWidth, MatrixHeight uint64
}

// MatrixSet groups string-identified matrices in one CRS. Identifiers need not be numeric zooms.
type MatrixSet struct {
	Title, CRS, WellKnownScaleSet string
	Matrices                      map[string]Matrix
}

// MatrixLimits specifies inclusive minimum and maximum tile indices for a layer.
type MatrixLimits struct{ MinTileRow, MaxTileRow, MinTileCol, MaxTileCol uint64 }

// MatrixLink exposes all matrices when Limits is empty, or only its listed ranges otherwise.
type MatrixLink struct{ Limits map[string]MatrixLimits }

// Layer declares supported styles, formats, linked matrix sets and dimensions for one resource.
// DefaultStyle must identify a declared style; empty InfoFormats disables feature-info resources.
type Layer struct {
	Title, Abstract      string
	Styles               map[string]owscore.Style
	DefaultStyle         string
	Formats, InfoFormats []string
	MatrixSets           map[string]MatrixLink
	Dimensions           map[string]owscore.Dimension
}

// Catalog holds mutable WMTS declarations before validation and snapshot publication.
// Do not mutate its maps or slices concurrently with request handling or encoding.
type Catalog struct {
	Service        owscore.Service
	Layers         map[string]Layer
	MatrixSets     map[string]MatrixSet
	UpdateSequence string
}

// Clone deep-copies catalog maps, slices and matrix limits; validator closures remain shared.
func (c Catalog) Clone() Catalog {
	c.Service.Keywords = slices.Clone(c.Service.Keywords)
	c.Layers = maps.Clone(c.Layers)
	c.MatrixSets = maps.Clone(c.MatrixSets)
	for key, set := range c.MatrixSets {
		set.Matrices = maps.Clone(set.Matrices)
		c.MatrixSets[key] = set
	}
	for key, layer := range c.Layers {
		layer.Styles = maps.Clone(layer.Styles)
		layer.Formats = slices.Clone(layer.Formats)
		layer.InfoFormats = slices.Clone(layer.InfoFormats)
		layer.Dimensions = owscore.CloneDimensions(layer.Dimensions)
		layer.MatrixSets = maps.Clone(layer.MatrixSets)
		for id, link := range layer.MatrixSets {
			link.Limits = maps.Clone(link.Limits)
			layer.MatrixSets[id] = link
		}
		c.Layers[key] = layer
	}
	return c
}

// Check validates matrix geometry, layer links, formats, dimensions and reserved names.
// It does not mutate configuration or invoke application validators.
func (c Catalog) Check() error {
	if c.Service.Title == "" {
		return fmt.Errorf("WMTS service title is required")
	}
	for id, set := range c.MatrixSets {
		if id == "" || set.CRS == "" || len(set.Matrices) == 0 {
			return fmt.Errorf("invalid matrix set %q", id)
		}
		for name, m := range set.Matrices {
			if name == "" || m.ScaleDenominator <= 0 || math.IsNaN(m.ScaleDenominator) || math.IsInf(m.ScaleDenominator, 0) || m.TileWidth == 0 || m.TileHeight == 0 || m.MatrixWidth == 0 || m.MatrixHeight == 0 {
				return fmt.Errorf("invalid tile matrix %q", name)
			}
			for _, v := range m.TopLeftCorner {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return fmt.Errorf("invalid tile origin")
				}
			}
		}
	}
	for id, layer := range c.Layers {
		if id == "" || layer.Title == "" || len(layer.Formats) == 0 || len(layer.MatrixSets) == 0 {
			return fmt.Errorf("invalid WMTS layer %q", id)
		}
		if _, ok := layer.Styles[layer.DefaultStyle]; !ok {
			return fmt.Errorf("default style is not declared on %q", id)
		}
		for name, style := range layer.Styles {
			if name == "" || style.Title == "" {
				return fmt.Errorf("invalid WMTS style")
			}
		}
		if err := owscore.ValidateFormats(layer.Formats); err != nil {
			return err
		}
		if err := owscore.ValidateFormats(layer.InfoFormats); err != nil {
			return err
		}
		for name, link := range layer.MatrixSets {
			set, ok := c.MatrixSets[name]
			if !ok {
				return fmt.Errorf("unknown linked matrix set %q", name)
			}
			for matrix, limit := range link.Limits {
				m, ok := set.Matrices[matrix]
				if !ok || limit.MinTileRow > limit.MaxTileRow || limit.MinTileCol > limit.MaxTileCol || limit.MaxTileRow >= m.MatrixHeight || limit.MaxTileCol >= m.MatrixWidth {
					return fmt.Errorf("invalid limits for %q", matrix)
				}
			}
		}
		seen := map[string]bool{}
		for name, d := range layer.Dimensions {
			key := kvp.Key(name)
			if name == "" || seen[key] || reservedParameter(key) {
				return fmt.Errorf("invalid WMTS dimension %q", name)
			}
			seen[key] = true
			if d.Default == "" || d.Default == "default" || d.Default == "current" {
				return fmt.Errorf("WMTS dimension %q requires a concrete default value", name)
			}
			if d.MultipleValues {
				return fmt.Errorf("WMTS dimensions select one value per tile")
			}
			if err := d.Check(); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateTile checks a non-nil parsed request against a previously checked catalog.
// It returns the selected matrix and fills r.Dimensions, including per-layer defaults.
// The request must not be shared concurrently; discard partial changes on error.
func (c Catalog) ValidateTile(ctx context.Context, r *TileRequest) (Matrix, error) {
	var empty Matrix
	layer, ok := c.Layers[r.Layer]
	if !ok {
		return empty, owscore.Fail(owscore.LayerNotDefined, "LAYER", "Layer is not available")
	}
	if _, ok := layer.Styles[r.Style]; !ok {
		return empty, owscore.Fail(owscore.StyleNotDefined, "STYLE", "Style is not available")
	}
	if r.Operation == owscore.GetTile && !slices.Contains(layer.Formats, r.Format) {
		return empty, owscore.Fail(owscore.InvalidFormat, "FORMAT", "Tile format is not available")
	}
	matrix, bounds, err := c.matrixFor(layer, r.TileMatrixSet, r.TileMatrix)
	if err != nil {
		return empty, err
	}
	if err := validateTilePosition(bounds, r.TileRow, r.TileCol); err != nil {
		return empty, err
	}
	r.Dimensions = map[string]string{}
	for name, d := range layer.Dimensions {
		value, present, err := r.Parameters.One(name)
		if err != nil {
			return empty, err
		}
		if value == "default" {
			present = false
		}
		value, err = d.Resolve(ctx, name, value, present)
		if err != nil {
			return empty, err
		}
		r.Dimensions[name] = value
	}
	return matrix, nil
}

// ValidateInfo validates a non-nil parsed query, its format and tile pixel bounds.
// It fills the embedded request Dimensions and does not fetch feature data.
func (c Catalog) ValidateInfo(ctx context.Context, r *FeatureInfoRequest) error {
	matrix, err := c.ValidateTile(ctx, &r.TileRequest)
	if err != nil {
		return err
	}
	if !slices.Contains(c.Layers[r.Layer].InfoFormats, r.InfoFormat) {
		return owscore.Fail(owscore.InvalidFormat, "INFOFORMAT", "Information format is not available")
	}
	return validatePixelPosition(matrix, r.PixelColumn, r.PixelRow)
}

// matrixFor is shared by request validation and REST capability filtering.
func (c Catalog) matrixFor(layer Layer, setID, matrixID string) (Matrix, MatrixLimits, error) {
	link, ok := layer.MatrixSets[setID]
	if !ok {
		return Matrix{}, MatrixLimits{}, owscore.Invalid("TILEMATRIXSET")
	}
	matrix, ok := c.MatrixSets[setID].Matrices[matrixID]
	if !ok {
		return Matrix{}, MatrixLimits{}, owscore.Invalid("TILEMATRIX")
	}
	bounds := MatrixLimits{MaxTileRow: matrix.MatrixHeight - 1, MaxTileCol: matrix.MatrixWidth - 1}
	if len(link.Limits) > 0 {
		bounds, ok = link.Limits[matrixID]
		if !ok {
			return Matrix{}, MatrixLimits{}, owscore.Invalid("TILEMATRIX")
		}
	}
	return matrix, bounds, nil
}

func validateTilePosition(bounds MatrixLimits, row, col uint64) error {
	if row < bounds.MinTileRow || row > bounds.MaxTileRow {
		return owscore.Fail(owscore.TileOutOfRange, "TILEROW", "Tile row is outside the available range")
	}
	if col < bounds.MinTileCol || col > bounds.MaxTileCol {
		return owscore.Fail(owscore.TileOutOfRange, "TILECOL", "Tile column is outside the available range")
	}
	return nil
}

func validatePixelPosition(matrix Matrix, col, row uint64) error {
	if col >= matrix.TileWidth {
		return owscore.Fail(owscore.PointOutOfRange, "I", "Pixel column is outside the tile")
	}
	if row >= matrix.TileHeight {
		return owscore.Fail(owscore.PointOutOfRange, "J", "Pixel row is outside the tile")
	}
	return nil
}

// Names are ASCII-folded by the caller. KVP catalogs and REST use the same set.
func reservedParameter(key string) bool {
	switch key {
	case "SERVICE", "VERSION", "REQUEST", "LAYER", "STYLE", "FORMAT", "INFOFORMAT",
		"TILEMATRIXSET", "TILEMATRIX", "TILEROW", "TILECOL", "I", "J",
		"ACCEPTVERSIONS", "SECTIONS", "ACCEPTFORMATS", "UPDATESEQUENCE":
		return true
	default:
		return false
	}
}
