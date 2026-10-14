package tiles_test

import (
	"context"
	"math"
	"testing"

	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
	"github.com/mapseekai/ogc-api/wmts"
)

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
