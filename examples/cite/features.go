package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

// sampleFeatures supplies geographic boundary data, including both polar regions and the antimeridian.
type sampleFeatures struct{}

func (sampleFeatures) Items(ctx context.Context, request features.Request) (features.Page, error) {
	if err := ctx.Err(); err != nil {
		return features.Page{}, err
	}
	selected := []features.Feature{}
	for i, point := range sampleCoordinates() {
		q := request.Query
		if b := q.BBox; len(b) != 0 {
			half := len(b) / 2
			longitude := point[0] >= b[0] && point[0] <= b[half]
			if b[0] > b[half] {
				longitude = point[0] >= b[0] || point[0] <= b[half]
			}
			if !longitude || point[1] < b[1] || point[1] > b[half+1] || half == 3 && (point[2] < b[2] || point[2] > b[5]) {
				continue
			}
		}
		if interval := q.Datetime; interval != nil {
			stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			if interval.Start != nil && stamp.Before(*interval.Start) || interval.End != nil && stamp.After(*interval.End) {
				continue
			}
		}
		selected = append(selected, sampleFeature(i, point))
	}
	count := uint64(len(selected))
	start := uint64(0)
	if request.Query.Cursor != "" {
		var err error
		start, err = strconv.ParseUint(request.Query.Cursor, 10, 64)
		if err != nil || start > count {
			return features.Page{}, common.Invalid("cursor")
		}
	}
	end := start + min(request.Query.Limit, count-start)
	page := features.Page{Items: &sampleCursor{items: selected[start:end]}, NumberMatched: &count}
	if end < count {
		page.NextCursor = strconv.FormatUint(end, 10)
	}
	return page, nil
}

func (sampleFeatures) Item(ctx context.Context, request features.Request) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	for i, point := range sampleCoordinates() {
		if request.ItemID == strconv.Itoa(i) {
			return sampleFeature(i, point), nil
		}
	}
	return features.Feature{}, common.ErrNotFound
}

type sampleCursor struct{ items []features.Feature }

func (c *sampleCursor) Next(ctx context.Context) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if len(c.items) == 0 {
		return features.Feature{}, io.EOF
	}
	item := c.items[0]
	c.items = c.items[1:]
	return item, nil
}

func (c *sampleCursor) Close() error { c.items = nil; return nil }

func sampleCoordinates() [][3]float64 {
	return [][3]float64{{0, 51.5, 0}, {-75, 0, 0}, {179, 67, 0}, {-179, 67, 0}, {0, 87, 0}, {0, -87, 0}, {0, 0, 0}}
}

func sampleFeature(i int, point [3]float64) features.Feature {
	geometry := fmt.Sprintf(`{"type":"Point","coordinates":[%g,%g,%g]}`, point[0], point[1], point[2])
	return features.Feature{ID: strconv.Itoa(i), Geometry: json.RawMessage(geometry), Properties: json.RawMessage(`{"observedAt":"2020-01-01T00:00:00Z"}`)}
}
