// Package tiles defines backend-neutral OGC API Tiles and tile matrix contracts.
package tiles

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/response"
)

// Request is a normalized tile read shared by OGC API Tiles and the WMTS bridge.
// Style and Dimensions are populated by WMTS; the OGC API read profile uses the default style.
type Request struct {
	CollectionID string
	MatrixSetID  string
	MatrixID     string
	Row          uint64
	Col          uint64
	Format       string
	Style        string
	Dimensions   map[string]string
}

// TileSet is the standard resource metadata for a tileset in one matrix set.
type TileSet struct {
	Title        string        `json:"title,omitempty"`
	DataType     string        `json:"dataType"`
	CRS          string        `json:"crs"`
	MatrixSetURI string        `json:"tileMatrixSetURI,omitempty"`
	Limits       []Limit       `json:"tileMatrixSetLimits,omitempty"`
	Links        []common.Link `json:"links"`
}

// MatrixSet describes the OGC Two Dimensional Tile Matrix Set 2.0 regular-grid model.
// Coordinates follow CRS axis order. No network lookup or spatial computation is performed.
type MatrixSet struct {
	ID                string   `json:"id"`
	Title             string   `json:"title,omitempty"`
	URI               string   `json:"uri,omitempty"`
	CRS               string   `json:"crs"`
	OrderedAxes       []string `json:"orderedAxes,omitempty"`
	WellKnownScaleSet string   `json:"wellKnownScaleSet,omitempty"`
	Matrices          []Matrix `json:"tileMatrices"`
}

// Matrix describes a regular two-dimensional grid. IDs are strings, never implicit zooms.
type Matrix struct {
	ID               string     `json:"id"`
	ScaleDenominator float64    `json:"scaleDenominator"`
	CellSize         float64    `json:"cellSize"`
	CornerOfOrigin   string     `json:"cornerOfOrigin,omitempty"`
	PointOfOrigin    [2]float64 `json:"pointOfOrigin"`
	TileWidth        uint64     `json:"tileWidth"`
	TileHeight       uint64     `json:"tileHeight"`
	MatrixWidth      uint64     `json:"matrixWidth"`
	MatrixHeight     uint64     `json:"matrixHeight"`
}

// Limit restricts a tileset to an inclusive matrix rectangle.
type Limit struct {
	MatrixID string `json:"tileMatrix"`
	MinRow   uint64 `json:"minTileRow"`
	MaxRow   uint64 `json:"maxTileRow"`
	MinCol   uint64 `json:"minTileCol"`
	MaxCol   uint64 `json:"maxTileCol"`
}

// Service publishes one collection tileset. Formats are media types in preference order.
// Empty Limits exposes the complete matrix set; otherwise only named rectangles are available.
type Service struct {
	MatrixSetID string
	DataType    string
	Formats     []string
	Limits      []Limit
	Reader      Reader
}

// Reader retrieves or produces one tile. Payload ownership transfers even alongside errors.
// Implementations must be concurrency-safe and cooperate with context cancellation.
type Reader interface {
	ReadTile(context.Context, Request) (response.Payload, error)
}

// ReaderFunc adapts a function to the tile capability without a plugin registry.
type ReaderFunc func(context.Context, Request) (response.Payload, error)

// ReadTile calls f with an exclusively owned request.
func (f ReaderFunc) ReadTile(ctx context.Context, r Request) (response.Payload, error) {
	return f(ctx, r)
}

// Grid owns immutable matrix metadata and an O(1) matrix lookup index.
type Grid struct {
	definition MatrixSet
	matrices   map[string]Matrix
}

// NewGrid validates and copies a regular tile grid. Later source edits cannot change it.
func NewGrid(set MatrixSet) (*Grid, error) {
	if !common.ValidID(set.ID) || set.CRS == "" || len(set.Matrices) == 0 {
		return nil, fmt.Errorf("invalid tile matrix set")
	}
	if err := common.ValidateCRS(set.CRS); err != nil {
		return nil, err
	}
	set.OrderedAxes = slices.Clone(set.OrderedAxes)
	set.Matrices = slices.Clone(set.Matrices)
	grid := &Grid{definition: set, matrices: make(map[string]Matrix, len(set.Matrices))}
	for _, m := range set.Matrices {
		if !common.ValidID(m.ID) || m.TileWidth == 0 || m.TileHeight == 0 || m.MatrixWidth == 0 || m.MatrixHeight == 0 {
			return nil, fmt.Errorf("invalid tile matrix")
		}
		if _, ok := grid.matrices[m.ID]; ok {
			return nil, fmt.Errorf("duplicate tile matrix")
		}
		for _, n := range []float64{m.ScaleDenominator, m.CellSize} {
			if !(n > 0) || math.IsInf(n, 0) {
				return nil, fmt.Errorf("invalid tile resolution")
			}
		}
		for _, n := range m.PointOfOrigin {
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, fmt.Errorf("invalid tile origin")
			}
		}
		if m.CornerOfOrigin != "" && m.CornerOfOrigin != "topLeft" && m.CornerOfOrigin != "bottomLeft" {
			return nil, fmt.Errorf("invalid corner of origin")
		}
		grid.matrices[m.ID] = m
	}
	return grid, nil
}

// Definition returns a detached metadata copy for discovery or encoding.
func (g *Grid) Definition() MatrixSet {
	set := g.definition
	set.OrderedAxes = slices.Clone(set.OrderedAxes)
	set.Matrices = slices.Clone(set.Matrices)
	return set
}

// CheckLimits validates and indexes resource-specific matrix rectangles.
func (g *Grid) CheckLimits(limits []Limit) (map[string]Limit, error) {
	index := make(map[string]Limit, len(limits))
	for _, limit := range limits {
		m, ok := g.Matrix(limit.MatrixID)
		if !ok || limit.MinRow > limit.MaxRow || limit.MinCol > limit.MaxCol || limit.MaxRow >= m.MatrixHeight || limit.MaxCol >= m.MatrixWidth {
			return nil, fmt.Errorf("invalid tileset limits")
		}
		if _, ok := index[limit.MatrixID]; ok {
			return nil, fmt.Errorf("duplicate tileset limits")
		}
		index[limit.MatrixID] = limit
	}
	return index, nil
}

// Validate checks matrix existence and inclusive resource bounds without allocating.
// limits must be immutable and previously returned by CheckLimits for this Grid.
func (g *Grid) Validate(id string, row, col uint64, limits map[string]Limit) error {
	m, ok := g.Matrix(id)
	if !ok || row >= m.MatrixHeight || col >= m.MatrixWidth {
		return common.ErrNotFound
	}
	if len(limits) > 0 {
		limit, ok := limits[id]
		if !ok || row < limit.MinRow || row > limit.MaxRow || col < limit.MinCol || col > limit.MaxCol {
			return common.ErrNotFound
		}
	}
	return nil
}

// Matrix returns a value copy of one matrix; it contains no mutable references.
func (g *Grid) Matrix(id string) (Matrix, bool) { m, ok := g.matrices[id]; return m, ok }
