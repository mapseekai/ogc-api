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
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wms"
)

type rasterProvider map[string]rasterLayer

type polygon [][][]float64

type rasterLayer struct {
	picture  image.Image
	world    [6]float64
	features [][]polygon
}

func loadRasters(directory string) (rasterProvider, error) {
	provider := make(rasterProvider)
	for _, name := range []string{"BasicPolygons", "Bridges", "Buildings", "DividedRoutes", "Forests", "Lakes", "MapNeatline", "NamedPlaces", "Ponds", "RoadSegments", "Streams"} {
		data, err := os.ReadFile(filepath.Join(directory, name+".png"))
		if err != nil {
			return nil, err
		}
		picture, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		layer := rasterLayer{picture: picture}
		data, err = os.ReadFile(filepath.Join(directory, name+".pgw"))
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(string(data))
		if len(fields) != len(layer.world) {
			return nil, fmt.Errorf("invalid worldfile for %s", name)
		}
		for i, field := range fields {
			layer.world[i], err = strconv.ParseFloat(field, 64)
			if err != nil || math.IsNaN(layer.world[i]) || math.IsInf(layer.world[i], 0) {
				return nil, fmt.Errorf("invalid worldfile coordinate for %s", name)
			}
		}
		if layer.world[0] <= 0 || layer.world[3] >= 0 || layer.world[1] != 0 || layer.world[2] != 0 {
			return nil, fmt.Errorf("fixture must use an unrotated north-up worldfile: %s", name)
		}
		data, err = os.ReadFile(filepath.Join(directory, name+".json"))
		if err == nil {
			var collection struct {
				Features []struct {
					Geometry struct {
						Type        string
						Coordinates json.RawMessage
					}
				}
			}
			if err := json.Unmarshal(data, &collection); err != nil {
				return nil, err
			}
			for _, feature := range collection.Features {
				var parts []polygon
				switch feature.Geometry.Type {
				case "Polygon":
					var part polygon
					err = json.Unmarshal(feature.Geometry.Coordinates, &part)
					parts = []polygon{part}
				case "MultiPolygon":
					err = json.Unmarshal(feature.Geometry.Coordinates, &parts)
				default:
					return nil, fmt.Errorf("CITE query fixture must contain polygons: %s", name)
				}
				if err != nil {
					return nil, err
				}
				layer.features = append(layer.features, parts)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		provider[name] = layer
	}
	return provider, nil
}

func (p rasterProvider) RenderMap(ctx context.Context, request maps.Request) (maps.Result, error) {
	limits := request.Limits.Defaults()
	if request.Width == 0 || request.Height == 0 || request.Width > limits.MaxImageWidth || request.Height > limits.MaxImageHeight || request.Height > limits.MaxImagePixels/request.Width {
		return maps.Result{}, fmt.Errorf("fixture render dimensions exceed limits")
	}
	background, err := strconv.ParseUint(strings.TrimPrefix(request.Background, "0x"), 16, 24)
	if err != nil {
		return maps.Result{}, err
	}
	picture := image.NewNRGBA(image.Rect(0, 0, int(request.Width), int(request.Height)))
	alpha := uint8(255)
	if request.Transparent {
		alpha = 0
	}
	draw.Draw(picture, picture.Bounds(), image.NewUniform(color.NRGBA{R: uint8(background >> 16), G: uint8(background >> 8), B: uint8(background), A: alpha}), image.Point{}, draw.Src)
	for _, selected := range request.Layers {
		layer, ok := p[selected.ID]
		if !ok {
			return maps.Result{}, fmt.Errorf("unknown CITE raster %s", selected.ID)
		}
		for row := 0; row < picture.Bounds().Dy(); row++ {
			if err := ctx.Err(); err != nil {
				return maps.Result{}, err
			}
			y := request.BBox[3] - (float64(row)+0.5)*(request.BBox[3]-request.BBox[1])/float64(request.Height)
			for column := 0; column < picture.Bounds().Dx(); column++ {
				x := request.BBox[0] + (float64(column)+0.5)*(request.BBox[2]-request.BBox[0])/float64(request.Width)
				pixel := layer.sample(x, y)
				if pixel.A != 0 {
					draw.Draw(picture, image.Rect(column, row, column+1, row+1), image.NewUniform(pixel), image.Point{}, draw.Over)
				}
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		return maps.Result{}, err
	}
	return maps.Result{Content: response.Bytes("image/png", encoded.Bytes()), Width: request.Width, Height: request.Height, CRS: request.CRS, BBox: request.BBox}, nil
}

func (p rasterProvider) featureInfo(ctx context.Context, request *wms.FeatureInfoRequest) (response.Payload, error) {
	if err := ctx.Err(); err != nil {
		return response.Payload{}, err
	}
	x := request.Bounds[0] + (float64(request.PixelColumn)+0.5)*(request.Bounds[2]-request.Bounds[0])/float64(request.Width)
	y := request.Bounds[3] - (float64(request.PixelRow)+0.5)*(request.Bounds[3]-request.Bounds[1])/float64(request.Height)
	items := []string{}
	for _, name := range request.QueryLayers {
		for i, parts := range p[name].features {
			for _, part := range parts {
				if uint64(len(items)) < request.FeatureCount && containsPolygon(part, x, y) {
					items = append(items, fmt.Sprintf("%s:%d", name, i))
					break
				}
			}
		}
	}
	data, err := json.Marshal(items)
	return response.Bytes("application/json", data), err
}

func (l rasterLayer) bounds() [4]float64 {
	b := l.picture.Bounds()
	return [4]float64{l.world[4] - l.world[0]/2, l.world[5] + (float64(b.Dy())-0.5)*l.world[3], l.world[4] + (float64(b.Dx())-0.5)*l.world[0], l.world[5] - l.world[3]/2}
}

func (l rasterLayer) sample(x, y float64) color.NRGBA {
	column := int(math.Floor((x-l.world[4])/l.world[0] + 0.5))
	row := int(math.Floor((y-l.world[5])/l.world[3] + 0.5))
	if !image.Pt(column, row).In(l.picture.Bounds()) {
		return color.NRGBA{}
	}
	pixel := color.NRGBAModel.Convert(l.picture.At(column, row)).(color.NRGBA)
	// Official raster fixtures use white for empty space, independently of requested background.
	if pixel.R == 255 && pixel.G == 255 && pixel.B == 255 {
		pixel.A = 0
	}
	return pixel
}

func containsPolygon(rings [][][]float64, x, y float64) bool {
	inside := false
	for _, ring := range rings {
		if len(ring) < 4 {
			continue
		}
		for i, a := range ring {
			b := ring[(i+1)%len(ring)]
			if len(a) < 2 || len(b) < 2 {
				return false
			}
			if (a[1] > y) != (b[1] > y) && x < (b[0]-a[0])*(y-a[1])/(b[1]-a[1])+a[0] {
				inside = !inside
			}
		}
	}
	return inside
}
