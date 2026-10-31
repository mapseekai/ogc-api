package features

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"github.com/mapseekai/ogc-api/common"
)

// Output validation checks representation structure, not topology or the
// correctness of a coordinate operation. Properties remain provider-defined;
// the reader must transform every geometry-valued property, not just geometry.
func validateOutputGeometry(raw json.RawMessage, crs common.CRS, depth int) error {
	if depth > 32 {
		return fmt.Errorf("provider geometry nesting exceeds limit")
	}
	var geometry struct {
		Type        string            `json:"type"`
		Coordinates json.RawMessage   `json:"coordinates"`
		Geometries  []json.RawMessage `json:"geometries"`
		BBox        []float64         `json:"bbox"`
	}
	if err := json.Unmarshal(raw, &geometry); err != nil {
		return fmt.Errorf("provider returned invalid geometry: %w", err)
	}
	if geometry.BBox != nil {
		if err := crs.ValidateBBox(geometry.BBox); err != nil {
			return fmt.Errorf("provider geometry bbox: %w", err)
		}
	}
	if geometry.Type == "GeometryCollection" {
		if geometry.Geometries == nil {
			return fmt.Errorf("provider geometry collection has no geometries array")
		}
		for _, member := range geometry.Geometries {
			if err := validateOutputGeometry(member, crs, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	levels := map[string]int{"Point": 0, "MultiPoint": 1, "LineString": 1, "MultiLineString": 2, "Polygon": 2, "MultiPolygon": 3}
	level, ok := levels[geometry.Type]
	if !ok {
		return fmt.Errorf("provider returned an unsupported geometry type")
	}
	return validateOutputCoordinates(geometry.Coordinates, level, crs, true)
}

func validateOutputCoordinates(raw json.RawMessage, level int, crs common.CRS, root bool) error {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return fmt.Errorf("provider geometry coordinates must be arrays")
	}
	// RFC 7946 permits an empty coordinates member, not an empty nested
	// position/ring. Each GeometryCollection member starts a new geometry root.
	if len(values) == 0 {
		if !root {
			return fmt.Errorf("provider geometry has empty nested coordinates")
		}
		return nil
	}
	if level > 0 {
		for _, value := range values {
			if err := validateOutputCoordinates(value, level-1, crs, false); err != nil {
				return err
			}
		}
		return nil
	}
	if len(values) != crs.Dimensions {
		return fmt.Errorf("provider geometry coordinate dimension differs from output CRS")
	}
	position := [3]float64{}
	for i, value := range values {
		number := bytes.TrimSpace(value)
		if len(number) == 0 || !(number[0] == '-' || number[0] >= '0' && number[0] <= '9') {
			return fmt.Errorf("provider geometry coordinate must be a finite number")
		}
		if err := json.Unmarshal(number, &position[i]); err != nil || math.IsNaN(position[i]) || math.IsInf(position[i], 0) {
			return fmt.Errorf("provider geometry coordinate must be a finite number")
		}
	}
	if crs.Geographic {
		longitude, latitude := 0, 1
		if crs.AxisOrder == common.YX {
			longitude, latitude = 1, 0
		}
		if position[longitude] < -180 || position[longitude] > 180 || position[latitude] < -90 || position[latitude] > 90 {
			return fmt.Errorf("provider geometry coordinates outside geographic CRS range")
		}
	}
	return nil
}
