package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/tiles"
)

const sampleDomainSet = `{"type":"DomainSet","generalGrid":{"type":"GeneralGridCoverage","srsName":"http://www.opengis.net/def/crs/OGC/1.3/CRS84","axisLabels":["x","y"],"axis":[{"type":"RegularAxis","axisLabel":"x","lowerBound":0,"upperBound":0,"uomLabel":"deg","resolution":1},{"type":"RegularAxis","axisLabel":"y","lowerBound":0,"upperBound":0,"uomLabel":"deg","resolution":1}],"gridLimits":{"type":"GridLimits","srsName":"http://www.opengis.net/def/crs/OGC/0/Index2D","axisLabels":["i","j"],"axis":[{"type":"IndexAxis","axisLabel":"i","lowerBound":0,"upperBound":0},{"type":"IndexAxis","axisLabel":"j","lowerBound":0,"upperBound":0}]}}}`

const sampleRangeType = `{"type":"DataRecord","field":[{"type":"Quantity","name":"temperature","label":"Temperature","description":"Temperature of the sample cell","definition":"http://vocab.nerc.ac.uk/standard_name/air_temperature/","uom":{"type":"UnitReference","code":"Cel"}}]}`

var _ features.Reader = pointReader{}

var _ features.Iterator = (*pointCursor)(nil)

func publications(baseURL string) (*api.Snapshot, server.Config, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	legacy, err := demo.Config(baseURL)
	if err != nil {
		return nil, server.Config{}, err
	}
	_, catalog := demo.Catalogs()
	matrix, err := tiles.FromWMTS("local", catalog.MatrixSets["local"], 1)
	if err != nil {
		return nil, server.Config{}, err
	}
	cache := make(map[string][]byte, len(matrix.Matrices))
	for _, grid := range matrix.Matrices {
		data, err := makeImage(grid.TileWidth, grid.TileHeight, 0xDDEEFF, false)
		if err != nil {
			return nil, server.Config{}, err
		}
		cache[grid.ID] = data
	}
	tileReader := tiles.ReaderFunc(func(ctx context.Context, request tiles.Request) (response.Payload, error) {
		if err := ctx.Err(); err != nil {
			return response.Payload{}, err
		}
		data, ok := cache[request.MatrixID]
		if !ok {
			return response.Payload{}, common.ErrNotFound
		}
		return response.Bytes("image/png", data), nil
	})
	renderer := maps.RendererFunc(renderMap)
	legacy.Handlers.GetTile = tiles.WMTS(tileReader)
	legacy.Handlers.GetMap = maps.WMS(renderer, legacy.Limits)
	mapService := &maps.Service{Renderer: renderer, Formats: []string{"image/png", "image/jpeg", "image/tiff"}, DefaultWidth: 256, DefaultHeight: 128}
	collection := api.Collection{
		Metadata: common.Collection{ID: "world", Title: "In-memory provider example", Extent: &common.Extent{Spatial: &common.SpatialExtent{BBox: [][]float64{{-180, -90, 180, 90}}, CRS: common.CRS84}}},
		Features: &features.Service{Reader: pointReader{}, Options: features.Options{FilterProperties: []string{"name"}}},
		Tiles: []tiles.Service{{MatrixSetID: "local", DataType: "map", Formats: []string{"image/png"}, Reader: tileReader,
			Limits: []tiles.Limit{{MatrixID: "L00", MinRow: 0, MaxRow: 1, MinCol: 0, MaxCol: 2}, {MatrixID: "10m", MinRow: 1, MaxRow: 2, MinCol: 1, MaxCol: 3}}}},
		Map: mapService,
		Coverage: &coverages.Service{Reader: coverages.ReaderFunc(readCoverage), Formats: []string{"application/prs.coverage+json", common.JSON}, Axes: []string{"x", "y"}, Properties: []string{"temperature"}, Schema: json.RawMessage(`{"type":"object","properties":{"temperature":{"type":"number"}}}`),
			DomainSet: json.RawMessage(sampleDomainSet), RangeType: json.RawMessage(sampleRangeType)},
	}
	snapshot, err := api.NewSnapshot(api.Catalog{Title: "ogc-api replaceable providers", Description: "Typed in-memory fixtures for protocol integration", BaseURL: baseURL + "/ogc", Map: mapService, Collections: []api.Collection{collection}, MatrixSets: []tiles.MatrixSet{matrix}, Processes: []processes.Service{echoService()}, ConformsTo: []string{"https://www.opengis.net/spec/ogcapi-maps-1/1.0/conf/core", "http://www.opengis.net/spec/ogcapi-coverages-1/1.0/conf/core"}})
	return snapshot, legacy, err
}

func echoService() processes.Service {
	return processes.Service{Description: processes.Description{ID: "echo", Version: "1.0.0", Title: "Echo an inline string", Inputs: map[string]processes.Input{"message": {Schema: json.RawMessage(`{"type":"string"}`)}}, Outputs: map[string]processes.OutputDescription{"result": {Schema: json.RawMessage(`{"type":"string"}`)}}},
		Validate: func(_ context.Context, request processes.Execute) error {
			var message *string
			if err := json.Unmarshal(request.Inputs["message"], &message); err != nil || message == nil {
				return common.Invalid("message")
			}
			for _, output := range request.Outputs {
				if output.Format != nil && (output.Format.MediaType != "" && output.Format.MediaType != common.JSON || output.Format.Encoding != "") {
					return common.Invalid("format")
				}
			}
			return nil
		}, Executor: processes.ExecutorFunc(func(ctx context.Context, request processes.Execute) (processes.Results, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result := make(processes.Results, len(request.Outputs))
			for name := range request.Outputs {
				result[name] = processes.Value{JSON: request.Inputs["message"]}
			}
			return result, nil
		})}
}

func renderMap(ctx context.Context, request maps.Request) (maps.Result, error) {
	if err := ctx.Err(); err != nil {
		return maps.Result{}, err
	}
	width, height := request.Width, request.Height
	xExtent := request.BBox[2] - request.BBox[0]
	if xExtent < 0 {
		xExtent += 360
	}
	yExtent := request.BBox[3] - request.BBox[1]
	if width == 0 {
		width = uint64(math.Max(1, math.Round(float64(height)*xExtent/yExtent)))
	}
	if height == 0 {
		height = uint64(math.Max(1, math.Round(float64(width)*yExtent/xExtent)))
	}
	limits := request.Limits.Defaults()
	if width == 0 || height == 0 || width > limits.MaxImageWidth || height > limits.MaxImageHeight || height > limits.MaxImagePixels/width {
		return maps.Result{}, common.Invalid("image dimensions")
	}
	background, err := strconv.ParseUint(strings.TrimPrefix(request.Background, "0x"), 16, 24)
	if err != nil {
		return maps.Result{}, common.Invalid("background")
	}
	alpha := uint8(255)
	if request.Transparent && request.Format != "image/jpeg" {
		alpha = 0
	}
	picture := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	draw.Draw(picture, picture.Bounds(), image.NewUniform(color.NRGBA{R: uint8(background >> 16), G: uint8(background >> 8), B: uint8(background), A: alpha}), image.Point{}, draw.Src)
	// Render the same Origin point exposed by the feature and coverage providers.
	xOffset := -request.BBox[0]
	if xOffset < 0 && request.BBox[0] > request.BBox[2] {
		xOffset += 360
	}
	if xOffset >= 0 && xOffset <= xExtent && request.BBox[1] <= 0 && request.BBox[3] >= 0 {
		x := min(int(width)-1, int(xOffset/xExtent*float64(width)))
		y := min(int(height)-1, int(request.BBox[3]/yExtent*float64(height)))
		symbol := image.NewUniform(color.NRGBA{R: 220, G: 30, B: 30, A: 192})
		for _, layer := range request.Layers {
			if layer.ID != "world" {
				return maps.Result{}, common.Invalid("collections")
			}
			for dy := -4; dy <= 4; dy++ {
				for dx := -4; dx <= 4; dx++ {
					if dx*dx+dy*dy <= 16 {
						draw.Draw(picture, image.Rect(x+dx, y+dy, x+dx+1, y+dy+1), symbol, image.Point{}, draw.Over)
					}
				}
			}
		}
	}
	data, err := encodeMapImage(picture, request.Format)
	return maps.Result{Content: response.Bytes(request.Format, data), Width: width, Height: height, BBox: request.BBox, CRS: request.CRS}, err
}

func readCoverage(ctx context.Context, request coverages.Request) (response.Payload, error) {
	if err := ctx.Err(); err != nil {
		return response.Payload{}, err
	}
	if len(request.BBox) > 0 {
		bbox := request.BBox
		half := len(bbox) / 2
		longitude := bbox[0] <= 0 && bbox[half] >= 0
		if bbox[0] > bbox[half] {
			longitude = bbox[0] <= 0 || bbox[half] >= 0
		}
		if !longitude || bbox[1] > 0 || bbox[half+1] < 0 || half == 3 && (bbox[2] > 0 || bbox[5] < 0) {
			return response.Payload{}, common.ErrNotFound
		}
	}
	// This static coverage has no temporal geometry, so every datetime predicate matches it.
	// The sample has one cell at (0,0); reject requests that exclude that cell.
	for _, subset := range request.Subsets {
		if subset.Slice {
			coordinate, err := strconv.ParseFloat(subset.Lower, 64)
			if err != nil {
				return response.Payload{}, common.Invalid("subset")
			}
			if coordinate != 0 {
				return response.Payload{}, common.ErrNotFound
			}
		}
		if !subset.Slice {
			if subset.Lower != "*" {
				lower, err := strconv.ParseFloat(subset.Lower, 64)
				if err != nil {
					return response.Payload{}, common.Invalid("subset")
				}
				if lower > 0 {
					return response.Payload{}, common.ErrNotFound
				}
			}
			if subset.Upper != "*" {
				upper, err := strconv.ParseFloat(subset.Upper, 64)
				if err != nil {
					return response.Payload{}, common.Invalid("subset")
				}
				if upper < 0 {
					return response.Payload{}, common.ErrNotFound
				}
			}
		}
	}
	if request.Format == common.JSON {
		data := []byte(`{"type":"CoverageByDomainAndRange","domainSet":` + sampleDomainSet + `,"rangeType":` + sampleRangeType + `,"rangeSet":{"type":"RangeSet","dataBlock":{"type":"VDataBlock","values":[20]}}}`)
		return response.Bytes(common.JSON, data), nil
	}
	data := []byte(`{"type":"Coverage","domain":{"type":"Domain","domainType":"Grid","axes":{"x":{"values":[0]},"y":{"values":[0]}},"referencing":[{"coordinates":["x","y"],"system":{"type":"GeographicCRS","id":"http://www.opengis.net/def/crs/OGC/1.3/CRS84"}}]},"parameters":{"temperature":{"type":"Parameter","observedProperty":{"label":{"en":"Temperature"}}}},"ranges":{"temperature":{"type":"NdArray","dataType":"float","axisNames":["y","x"],"shape":[1,1],"values":[20]}}}`)
	return response.Bytes(request.Format, data), nil
}

func makeImage(width, height uint64, background uint32, transparent bool) ([]byte, error) {
	if width == 0 || height == 0 || width > 8192 || height > 8192 || height > 16_777_216/width {
		return nil, fmt.Errorf("example image size exceeds limit")
	}
	alpha := uint8(255)
	if transparent {
		alpha = 0
	}
	picture := image.NewPaletted(image.Rect(0, 0, int(width), int(height)), color.Palette{color.NRGBA{R: uint8(background >> 16), G: uint8(background >> 8), B: uint8(background), A: alpha}})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

type pointReader struct{}

func (pointReader) Items(ctx context.Context, request features.Request) (features.Page, error) {
	if err := ctx.Err(); err != nil {
		return features.Page{}, err
	}
	if request.Query.Cursor != "" {
		return features.Page{}, common.Invalid("cursor")
	}
	selected := true
	if len(request.Query.BBox) > 0 {
		bbox := request.Query.BBox
		half := len(bbox) / 2
		longitude := bbox[0] <= 0 && bbox[half] >= 0
		if bbox[0] > bbox[half] {
			longitude = 0 >= bbox[0] || 0 <= bbox[half]
		}
		selected = longitude && bbox[1] <= 0 && bbox[half+1] >= 0
		if half == 3 {
			selected = selected && bbox[2] <= 0 && bbox[5] >= 0
		}
	}
	if request.Query.Datetime != nil {
		timestamp := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
		interval := request.Query.Datetime
		selected = selected && (interval.Start == nil || !timestamp.Before(*interval.Start)) && (interval.End == nil || !timestamp.After(*interval.End))
	}
	if name, ok := request.Query.Properties["name"]; ok {
		selected = selected && name == "Origin"
	}
	count := uint64(0)
	if selected {
		count = 1
	}
	return features.Page{Items: &pointCursor{pending: selected}, NumberMatched: &count}, nil
}

func (pointReader) Item(ctx context.Context, request features.Request) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if request.ItemID != "origin" {
		return features.Feature{}, common.ErrNotFound
	}
	return pointFeature(), nil
}

type pointCursor struct{ pending bool }

func (c *pointCursor) Next(ctx context.Context) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if !c.pending {
		return features.Feature{}, io.EOF
	}
	c.pending = false
	return pointFeature(), nil
}

func (c *pointCursor) Close() error { c.pending = false; return nil }

func pointFeature() features.Feature {
	return features.Feature{ID: "origin", Geometry: json.RawMessage(`{"type":"Point","coordinates":[0,0,0]}`), Properties: json.RawMessage(`{"name":"Origin","observedAt":"2026-10-14T00:00:00Z"}`)}
}
