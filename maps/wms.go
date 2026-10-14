package maps

import (
	"context"
	"fmt"

	"github.com/mapseekai/ogc-api/internal/legacy"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wms"
)

// WMS connects a map renderer to server.Handlers.GetMap after the WMS engine has
// validated versions, axes, layers and dimensions. Limits must match the hosting WMS engine.
// It preserves whether the WMS bounds were normalized to XY, avoiding silent axis changes.
func WMS(renderer Renderer, limits owscore.Limits) func(context.Context, *wms.MapRequest) (response.Payload, error) {
	return func(ctx context.Context, source *wms.MapRequest) (response.Payload, error) {
		bounds := source.WireBBox
		if source.Normalized {
			bounds = source.Bounds
		}
		request := Request{
			BBox: append([]float64(nil), bounds[:]...), BBoxCRS: source.CRS,
			BBoxXY: source.Normalized, CRS: source.CRS, Width: source.Width, Height: source.Height,
			Format: source.Format, Transparent: source.Transparent,
			Background: fmt.Sprintf("0x%06X", source.Background), Limits: limits.Defaults(),
			Layers: make([]Layer, len(source.Layers)),
		}
		for i, id := range source.Layers {
			request.Layers[i] = Layer{ID: id, Style: source.Styles[i], Dimensions: source.LayerDimensions[id]}
		}
		result, err := renderer.RenderMap(ctx, request)
		// Legacy WMS owns wire response metadata; validate its requested image dimensions here.
		if err == nil && (result.Width != request.Width || result.Height != request.Height) {
			err = fmt.Errorf("renderer dimensions differ from WMS request")
		}
		return result.Content, legacy.Error(err)
	}
}
