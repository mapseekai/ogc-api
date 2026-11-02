package demo

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func TestPNGFixture(t *testing.T) {
	// Original project-owned geometry, not an upstream map, photograph or screenshot.
	// Exact bytes were generated with Go 1.27.3; PNG chunking can vary by toolchain.
	canvas := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := range 256 {
		for x := range 256 {
			pixel := color.NRGBA{R: 235, G: 244, B: 241, A: 254}
			if x >= 96 && x < 160 && y >= 96 && y < 160 {
				pixel = color.NRGBA{R: 36, G: 109, B: 128, A: 255}
			}
			canvas.SetNRGBA(x, y, pixel)
		}
	}
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&encoded, canvas); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_DEMO_FIXTURE") == "1" {
		if err := os.WriteFile("fixture.png", encoded.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if fmt.Sprintf("%x", sha256.Sum256(pngData)) != "e9c8a771e83d70cf2d11d337c9f018aaf3807565d618d542e54293bf4e1bf661" {
		t.Fatal("embedded PNG differs from the reviewed fixture bytes")
	}
	decoded, err := png.Decode(bytes.NewReader(pngData))
	if err != nil || decoded.Bounds() != canvas.Bounds() {
		t.Fatal("invalid embedded 256x256 PNG", err)
	}
	for y := range 256 {
		for x := range 256 {
			if color.NRGBAModel.Convert(decoded.At(x, y)) != canvas.NRGBAAt(x, y) {
				t.Fatalf("fixture pixel differs at %d,%d", x, y)
			}
		}
	}
	payload := PNG()
	defer payload.Close()
	if payload.ContentType != "image/png" || payload.Length != int64(len(pngData)) {
		t.Fatal("PNG payload contract changed")
	}
}
