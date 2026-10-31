package main

import (
	"math"
	"testing"

	"github.com/mapseekai/ogc-api/common"
)

func TestProjectionIndependentKnownCoordinates(t *testing.T) {
	// PROJ's official webmerc example independently gives (2,49) as
	// (222638.98,6274861.39) metres; constants below retain more precision.
	// https://proj.org/en/stable/operations/projections/webmerc.html
	for _, tc := range []struct {
		lon, lat, x, y float64
	}{
		{2, 49, 222638.98158654713, 6274861.394006576},
		{7, 50, 779236.435552915, 6446275.841017161},
		{120, 45, 13358338.895192828, 5621521.486192066},
		{-75, 0, -8348961.809495518, 0},
	} {
		actual, err := transformPosition([]float64{tc.lon, tc.lat}, fixtureCRS(common.CRS84), fixtureCRS(epsg3857))
		if err != nil || math.Abs(actual[0]-tc.x) > 1e-7 || math.Abs(actual[1]-tc.y) > 1e-7 {
			t.Fatal("forward projection differs from independent reference", actual, tc, err)
		}
		inverse, err := transformPosition([]float64{tc.x, tc.y}, fixtureCRS(epsg3857), fixtureCRS(common.CRS84))
		if err != nil || math.Abs(inverse[0]-tc.lon) > 1e-10 || math.Abs(inverse[1]-tc.lat) > 1e-10 {
			t.Fatal("inverse projection differs from independent reference", inverse, tc, err)
		}
	}
}

func TestProjectionNativeAxesAndHeight(t *testing.T) {
	position := []float64{50, 7, 123}
	toGeographic, err := transformPosition(position, fixtureCRS(epsg4979), fixtureCRS(common.CRS84h))
	if err != nil || len(toGeographic) != 3 || toGeographic[0] != 7 || toGeographic[1] != 50 || toGeographic[2] != 123 {
		t.Fatal("native latitude/longitude/height was changed", toGeographic, err)
	}
	projected, err := transformPosition(position, fixtureCRS(epsg4979), fixtureCRS(epsg3857))
	if err != nil || len(projected) != 2 || math.Abs(projected[0]-779236.435552915) > 1e-7 || math.Abs(projected[1]-6446275.841017161) > 1e-7 {
		t.Fatal(projected, err)
	}
	if _, err := transformPosition(projected, fixtureCRS(epsg3857), fixtureCRS(epsg4979)); err == nil {
		t.Fatal("invented a zero height after a 2D operation")
	}
	for _, latitude := range []float64{-90, 90} {
		if _, err := transformPosition([]float64{0, latitude}, fixtureCRS(common.CRS84), fixtureCRS(epsg3857)); err == nil {
			t.Fatal("silently clamped singular coordinates")
		}
	}
	if position[0] != 50 || position[1] != 7 || position[2] != 123 {
		t.Fatal("operation mutated storage coordinates")
	}
}
