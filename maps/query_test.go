package maps_test

import (
	"context"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wms"
)

func service(t testing.TB) maps.Service {
	t.Helper()
	result, err := (maps.Service{Renderer: maps.RendererFunc(func(context.Context, maps.Request) (maps.Result, error) { return maps.Result{}, nil }), Formats: []string{"image/png"}}).Prepare()
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMapQueryLimitsAndDefaults(t *testing.T) {
	s := service(t)
	tests := []struct {
		name          string
		query         common.Query
		valid         bool
		width, height uint64
	}{
		{"default", common.Query{}, true, 1024, 512},
		{"width only", common.Query{"width": "200"}, true, 200, 0},
		{"height only", common.Query{"height": "200"}, true, 0, 200},
		{"antimeridian", common.Query{"bbox": "170,-10,-170,10"}, true, 1024, 512},
		{"too many pixels", common.Query{"width": "8192", "height": "8192"}, false, 0, 0},
		{"zero width", common.Query{"width": "0"}, false, 0, 0},
		{"overflow", common.Query{"width": "18446744073709551616"}, false, 0, 0},
		{"unsupported CRS", common.Query{"crs": "EPSG:3857"}, false, 0, 0},
		{"invalid color", common.Query{"bgcolor": "0xZZZZZZ"}, false, 0, 0},
		{"invalid transparency", common.Query{"transparent": "1"}, false, 0, 0},
		{"unknown option", common.Query{"scale-denominator": "1000"}, false, 0, 0},
		{"zero extent", common.Query{"bbox": "0,0,0,1"}, false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := maps.ParseQuery(tt.query, s)
			if (err == nil) != tt.valid {
				t.Fatalf("query returned %v", err)
			}
			if tt.valid && (request.Width != tt.width || request.Height != tt.height) {
				t.Fatal(request)
			}
		})
	}
}

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

func FuzzParse(f *testing.F) {
	f.Add("width=512&height=256&bbox=-1,-1,1,1")
	f.Fuzz(func(t *testing.T, raw string) {
		values, err := common.ParseQuery(raw, 2048, 32)
		if err == nil {
			_, _ = maps.ParseQuery(values, service(t))
		}
	})
}
