// Package maps defines map portrayal requests independently of a rendering engine.
package maps

import (
	"context"
	"fmt"
	"slices"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
)

// Request is a normalized map request. BBox follows BBoxCRS axes unless BBoxXY is true.
// A zero Width or Height asks the renderer to preserve pixel aspect ratio. The renderer
// must enforce Limits before allocating output; the API also checks returned dimensions.
type Request struct {
	Layers      []Layer
	BBox        []float64
	BBoxCRS     string
	BBoxXY      bool
	CRS         string
	Width       uint64
	Height      uint64
	Format      string
	Transparent bool
	Background  string
	Datetime    *common.Interval
	Limits      owscore.Limits
}

// Result transfers Content even alongside an error. BBox describes the actual map in CRS axes.
// Width and Height are the encoded image dimensions, not requested dimensions copied blindly.
type Result struct {
	Content response.Payload
	BBox    []float64
	CRS     string
	Width   uint64
	Height  uint64
}

// Layer retains ordered layer/style selection, including resolved WMS dimensions.
type Layer struct {
	ID         string
	Style      string
	Dimensions map[string]string
}

// Service declares a collection map and its permitted portrayal parameters.
// The bbox/scaling profile supports f, bbox, bbox-crs, crs, width, height, datetime,
// transparent and bgcolor. Unimplemented parameter classes are not advertised.
type Service struct {
	Renderer      Renderer
	Formats       []string
	CRS           []string
	DefaultCRS    string
	DefaultBBox   []float64
	DefaultWidth  uint64
	DefaultHeight uint64
	Limits        owscore.Limits
}

// Prepare returns an independently owned, validated service declaration.
func (s Service) Prepare() (Service, error) {
	s.Formats = slices.Clone(s.Formats)
	s.CRS = slices.Clone(s.CRS)
	s.DefaultBBox = slices.Clone(s.DefaultBBox)
	if s.Renderer == nil || len(s.Formats) == 0 {
		return Service{}, fmt.Errorf("map renderer and formats are required")
	}
	if err := owscore.ValidateFormats(s.Formats); err != nil {
		return Service{}, err
	}
	if len(s.CRS) == 0 {
		s.CRS = []string{common.CRS84}
	}
	for _, crs := range s.CRS {
		if err := common.ValidateCRS(crs); err != nil {
			return Service{}, err
		}
	}
	if s.DefaultCRS == "" {
		s.DefaultCRS = s.CRS[0]
	}
	if !slices.Contains(s.CRS, s.DefaultCRS) || !slices.Contains(s.CRS, common.CRS84) {
		return Service{}, fmt.Errorf("map CRS declaration must include default CRS and CRS84")
	}
	if len(s.DefaultBBox) == 0 {
		s.DefaultBBox = []float64{-180, -90, 180, 90}
	}
	if len(s.DefaultBBox) != 4 {
		return Service{}, fmt.Errorf("map default bbox must be two-dimensional")
	}
	if err := common.ValidateBBox(s.DefaultBBox, true); err != nil {
		return Service{}, err
	}
	if s.DefaultWidth == 0 {
		s.DefaultWidth = 1024
	}
	if s.DefaultHeight == 0 {
		s.DefaultHeight = 512
	}
	if err := s.Limits.Validate(); err != nil {
		return Service{}, err
	}
	s.Limits = s.Limits.Defaults()
	if err := checkSize(s.DefaultWidth, s.DefaultHeight, s.Limits); err != nil {
		return Service{}, err
	}
	return s, nil
}

// Renderer is implemented by an external rendering engine or remote map service.
// It must be concurrency-safe and cooperate with context cancellation.
type Renderer interface {
	RenderMap(context.Context, Request) (Result, error)
}

// RendererFunc adapts a function to the map capability.
type RendererFunc func(context.Context, Request) (Result, error)

// RenderMap invokes the external renderer.
func (f RendererFunc) RenderMap(ctx context.Context, request Request) (Result, error) {
	return f(ctx, request)
}

// ValidateResult enforces output dimensions, requested format and rendering metadata.
// It does not inspect or decode raster bytes, which remain the renderer's responsibility.
func ValidateResult(request Request, result Result) error {
	if result.Width == 0 || result.Height == 0 {
		return fmt.Errorf("renderer returned empty image dimensions")
	}
	if err := checkSize(result.Width, result.Height, request.Limits); err != nil {
		return fmt.Errorf("renderer exceeded image limits: %w", err)
	}
	if request.Width != 0 && result.Width != request.Width || request.Height != 0 && result.Height != request.Height {
		return fmt.Errorf("renderer dimensions differ from request")
	}
	if result.CRS != request.CRS || len(result.BBox) != 4 {
		return fmt.Errorf("renderer returned inconsistent CRS or bounding box")
	}
	if err := common.ValidateBBox(result.BBox, result.CRS == common.CRS84); err != nil {
		return fmt.Errorf("renderer bounding box: %w", err)
	}
	return result.Content.Validate(request.Format)
}

func checkSize(width, height uint64, limits owscore.Limits) error {
	if width > limits.MaxImageWidth || height > limits.MaxImageHeight {
		return common.Invalid("image dimensions")
	}
	if width != 0 && height > limits.MaxImagePixels/width {
		return common.Invalid("image pixels")
	}
	return nil
}
