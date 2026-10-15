package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func TestMapImageFormats(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 7, 5))
	picture.SetNRGBA(3, 2, color.NRGBA{R: 255, A: 255})
	for _, media := range []string{"image/png", "image/jpeg", "image/tiff"} {
		data, err := encodeMapImage(picture, media)
		if err != nil {
			t.Fatal(media, err)
		}
		if media == "image/tiff" {
			if !bytes.Equal(data[:8], []byte{'I', 'I', 42, 0, 8, 0, 0, 0}) {
				t.Fatal("invalid TIFF header")
			}
			continue
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != 7 || config.Height != 5 {
			t.Fatal(media, config, err)
		}
	}
	if _, err := encodeMapImage(picture, "image/gif"); err == nil {
		t.Fatal("unknown format accepted")
	}
	// Subimages retain their parent stride and origin; the strip must contain only this view.
	view := picture.SubImage(image.Rect(2, 1, 5, 4)).(*image.NRGBA)
	data, err := mapTIFF(view)
	if err != nil {
		t.Fatal(err)
	}
	count := int(binary.LittleEndian.Uint16(data[8:10]))
	pixels := 8 + 2 + count*12 + 4 + 8
	if len(data)-pixels != 3*3*4 || !bytes.Equal(data[pixels+16:pixels+20], []byte{255, 0, 0, 255}) {
		t.Fatal("TIFF strip included parent padding")
	}
	if _, err := mapTIFF(image.NewNRGBA(image.Rectangle{})); err == nil {
		t.Fatal("empty TIFF accepted")
	}
}
