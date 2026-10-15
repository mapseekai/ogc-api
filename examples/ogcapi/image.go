package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
)

// mapImage retains an alpha channel even when this view has only opaque pixels.
type mapImage struct{ *image.NRGBA }

func (mapImage) Opaque() bool { return false }

func encodeMapImage(picture *image.NRGBA, format string) ([]byte, error) {
	var buffer bytes.Buffer
	switch format {
	case "image/png":
		if err := png.Encode(&buffer, mapImage{picture}); err != nil {
			return nil, err
		}
	case "image/jpeg":
		if err := jpeg.Encode(&buffer, picture, &jpeg.Options{Quality: 90}); err != nil {
			return nil, err
		}
	case "image/tiff":
		return mapTIFF(picture)
	default:
		return nil, fmt.Errorf("unsupported example map format")
	}
	return buffer.Bytes(), nil
}

// shortcut: this example writes only single-strip, uncompressed 8-bit RGBA TIFF;
// use a raster codec in a production provider needing compression or GeoTIFF tags.
func mapTIFF(picture *image.NRGBA) ([]byte, error) {
	width, height := picture.Bounds().Dx(), picture.Bounds().Dy()
	if width < 1 || height < 1 || width > 8192 || height > 8192 || height > 16_777_216/width {
		return nil, fmt.Errorf("example TIFF size exceeds limit")
	}
	const tags = 12
	const bitsOffset = 8 + 2 + tags*12 + 4
	const pixelsOffset = bitsOffset + 8
	fields := [][4]uint32{
		{256, 4, 1, uint32(width)}, {257, 4, 1, uint32(height)},
		{258, 3, 4, bitsOffset}, {259, 3, 1, 1}, {262, 3, 1, 2},
		{273, 4, 1, pixelsOffset}, {274, 3, 1, 1}, {277, 3, 1, 4},
		{278, 4, 1, uint32(height)}, {279, 4, 1, uint32(width * height * 4)},
		{284, 3, 1, 1}, {338, 3, 1, 2},
	}
	data := make([]byte, 0, pixelsOffset+width*height*4)
	data = append(data, 'I', 'I', 42, 0, 8, 0, 0, 0)
	data = binary.LittleEndian.AppendUint16(data, tags)
	for _, field := range fields {
		data = binary.LittleEndian.AppendUint16(data, uint16(field[0]))
		data = binary.LittleEndian.AppendUint16(data, uint16(field[1]))
		data = binary.LittleEndian.AppendUint32(data, field[2])
		data = binary.LittleEndian.AppendUint32(data, field[3])
	}
	data = binary.LittleEndian.AppendUint32(data, 0)
	for range 4 {
		data = binary.LittleEndian.AppendUint16(data, 8)
	}
	for y := picture.Rect.Min.Y; y < picture.Rect.Max.Y; y++ {
		start := picture.PixOffset(picture.Rect.Min.X, y)
		data = append(data, picture.Pix[start:start+width*4]...)
	}
	return data, nil
}
