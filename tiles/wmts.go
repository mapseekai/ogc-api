package tiles

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/mapseekai/ogc-api/internal/legacy"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wmts"
)

// WMTS adapts one tile reader to server.Handlers.GetTile. The original WMTS engine
// still performs protocol-specific validation; this bridge never reparses HTTP inputs.
func WMTS(reader Reader) func(context.Context, *wmts.TileRequest) (response.Payload, error) {
	return func(ctx context.Context, r *wmts.TileRequest) (response.Payload, error) {
		payload, err := reader.ReadTile(ctx, Request{CollectionID: r.Layer, MatrixSetID: r.TileMatrixSet, MatrixID: r.TileMatrix,
			Row: r.TileRow, Col: r.TileCol, Format: r.Format, Style: r.Style, Dimensions: r.Dimensions})
		return payload, legacy.Error(err)
	}
}

// FromWMTS converts a checked WMTS matrix description. metersPerUnit must describe
// the CRS units explicitly; the library never guesses units from an EPSG identifier.
func FromWMTS(id string, set wmts.MatrixSet, metersPerUnit float64) (MatrixSet, error) {
	if !(metersPerUnit > 0) || math.IsInf(metersPerUnit, 0) {
		return MatrixSet{}, fmt.Errorf("invalid CRS meters per unit")
	}
	result := MatrixSet{ID: id, Title: set.Title, CRS: set.CRS, WellKnownScaleSet: set.WellKnownScaleSet}
	names := make([]string, 0, len(set.Matrices))
	for name := range set.Matrices {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		m := set.Matrices[name]
		result.Matrices = append(result.Matrices, Matrix{ID: name, ScaleDenominator: m.ScaleDenominator, CellSize: m.ScaleDenominator * 0.00028 / metersPerUnit,
			PointOfOrigin: m.TopLeftCorner, TileWidth: m.TileWidth, TileHeight: m.TileHeight, MatrixWidth: m.MatrixWidth, MatrixHeight: m.MatrixHeight})
	}
	if _, err := NewGrid(result); err != nil {
		return MatrixSet{}, err
	}
	return result, nil
}
