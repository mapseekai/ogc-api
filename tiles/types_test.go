package tiles_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
	"github.com/mapseekai/ogc-api/wmts"
)

func matrixSet() tiles.MatrixSet {
	return tiles.MatrixSet{ID: "local", CRS: common.CRS84, Matrices: []tiles.Matrix{{ID: "10m", ScaleDenominator: 100, CellSize: 10, TileWidth: 512, TileHeight: 128, MatrixWidth: 5, MatrixHeight: 3}}}
}

func TestGridLimitsAndIsolation(t *testing.T) {
	definition := matrixSet()
	grid, err := tiles.NewGrid(definition)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := grid.CheckLimits([]tiles.Limit{{MatrixID: "10m", MinRow: 1, MaxRow: 2, MinCol: 1, MaxCol: 3}})
	if err != nil {
		t.Fatal(err)
	}
	definition.Matrices[0].MatrixWidth = 1000
	copy := grid.Definition()
	copy.Matrices[0].ID = "mutated"
	for _, test := range []struct {
		matrix   string
		row, col uint64
		valid    bool
	}{{"10m", 1, 1, true}, {"10m", 2, 3, true}, {"10m", 0, 1, false}, {"10m", 1, 4, false}, {"10m", math.MaxUint64, 1, false}, {"0", 1, 1, false}} {
		err := grid.Validate(test.matrix, test.row, test.col, limits)
		if (err == nil) != test.valid {
			t.Fatal(test, err)
		}
		if err != nil && !errors.Is(err, common.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if _, err := grid.CheckLimits([]tiles.Limit{{MatrixID: "10m", MaxRow: 3}}); err == nil {
		t.Fatal("invalid limits accepted")
	}
}

func TestWMTSReaderBridge(t *testing.T) {
	var got tiles.Request
	reader := tiles.ReaderFunc(func(_ context.Context, request tiles.Request) (response.Payload, error) {
		got = request
		return response.Bytes("image/png", nil), nil
	})
	content, err := tiles.WMTS(reader)(context.Background(), &wmts.TileRequest{Layer: "roads", Style: "default", TileMatrixSet: "local", TileMatrix: "10m", TileRow: 2, TileCol: 3, Format: "image/png", Dimensions: map[string]string{"TIME": "2026"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := content.Close(); err != nil {
		t.Fatal(err)
	}
	if got.MatrixID != "10m" || got.Col != 3 || got.Dimensions["TIME"] != "2026" {
		t.Fatal(got)
	}
}

func TestWMTSMatrixConversionRequiresUnits(t *testing.T) {
	source := wmts.MatrixSet{CRS: "EPSG:3857", Matrices: map[string]wmts.Matrix{"L00": {ScaleDenominator: 1000, TileWidth: 512, TileHeight: 128, MatrixWidth: 3, MatrixHeight: 2}}}
	converted, err := tiles.FromWMTS("local", source, 1)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Matrices[0].ID != "L00" || math.Abs(converted.Matrices[0].CellSize-0.28) > 1e-12 {
		t.Fatal(converted)
	}
	if _, err := tiles.FromWMTS("local", source, 0); err == nil {
		t.Fatal("CRS units guessed")
	}
}
