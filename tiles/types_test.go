package tiles_test

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/tiles"
)

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
		t.Run(fmt.Sprintf("%s_%d_%d", test.matrix, test.row, test.col), func(t *testing.T) {
			err := grid.Validate(test.matrix, test.row, test.col, limits)
			if (err == nil) != test.valid {
				t.Fatal(test, err)
			}
			if err != nil && !errors.Is(err, common.ErrNotFound) {
				t.Fatal(err)
			}

		})
	}
	if _, err := grid.CheckLimits([]tiles.Limit{{MatrixID: "10m", MaxRow: 3}}); err == nil {
		t.Fatal("invalid limits accepted")
	}
}

func matrixSet() tiles.MatrixSet {
	return tiles.MatrixSet{ID: "local", CRS: common.CRS84, Matrices: []tiles.Matrix{{ID: "10m", ScaleDenominator: 100, CellSize: 10, TileWidth: 512, TileHeight: 128, MatrixWidth: 5, MatrixHeight: 3}}}
}
