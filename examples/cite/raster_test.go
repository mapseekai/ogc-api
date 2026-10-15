package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"github.com/mapseekai/ogc-api/maps"
)

func TestRasterPixelCentersAndRender(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	picture.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	picture.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	layer := rasterLayer{picture: picture, world: [6]float64{1, 0, 0, -1, 0.5, 0.5}}
	if layer.bounds() != [4]float64{0, 0, 2, 1} || layer.sample(-1, 0.5).A != 0 || layer.sample(1.5, 0.5).A != 0 {
		t.Fatal("incorrect worldfile boundaries or empty pixels")
	}
	provider := rasterProvider{"test": layer}
	request := maps.Request{Layers: []maps.Layer{{ID: "test"}}, BBox: []float64{0, 0, 2, 1}, Width: 2, Height: 1, Background: "0x0000FF"}
	result, err := provider.RenderMap(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(result.Content.Body)
	if closeErr := result.Content.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	first := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	if first.R != 128 || first.B != 127 || first.A != 255 || color.NRGBAModel.Convert(decoded.At(1, 0)).(color.NRGBA).B != 255 {
		t.Fatal("render did not preserve data and requested background")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.RenderMap(ctx, request); err == nil {
		t.Fatal("cancelled render succeeded")
	}
}

func TestPolygonHoles(t *testing.T) {
	rings := [][][]float64{{{0, 0}, {4, 0}, {4, 4}, {0, 4}, {0, 0}}, {{1, 1}, {2, 1}, {2, 2}, {1, 2}, {1, 1}}}
	if !containsPolygon(rings, 3, 3) || containsPolygon(rings, 1.5, 1.5) || containsPolygon(rings, 5, 5) {
		t.Fatal("incorrect polygon or hole membership")
	}
}
