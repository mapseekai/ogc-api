package maps_test

import (
	"context"
	"testing"

	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wms"
)

func TestWMSRendererBridgePreservesAxesAndLayers(t *testing.T) {
	var got maps.Request
	renderer := maps.RendererFunc(func(_ context.Context, request maps.Request) (maps.Result, error) {
		got = request
		return maps.Result{Width: request.Width, Height: request.Height, Content: response.Bytes(request.Format, []byte("map"))}, nil
	})
	request := &wms.MapRequest{Layers: []string{"roads", "water"}, Styles: []string{"named", ""}, CRS: "EPSG:4326", WireBBox: [4]float64{39, 116, 40, 117}, Bounds: [4]float64{116, 39, 117, 40}, Normalized: true, Width: 512, Height: 256, Format: "image/png"}
	content, err := maps.WMS(renderer, owscore.Limits{})(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := content.Close(); err != nil {
		t.Fatal(err)
	}
	if !got.BBoxXY || got.BBox[0] != 116 || len(got.Layers) != 2 || got.Layers[0].Style != "named" || got.Width != 512 {
		t.Fatal(got)
	}
}
