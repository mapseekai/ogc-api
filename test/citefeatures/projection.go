package main

import (
	"fmt"
	"math"

	"github.com/mapseekai/ogc-api/common"
)

const (
	epsg4326 = "http://www.opengis.net/def/crs/EPSG/0/4326"
	epsg3857 = "http://www.opengis.net/def/crs/EPSG/0/3857"
	epsg4979 = "http://www.opengis.net/def/crs/EPSG/0/4979"
	radius   = 6378137.0
)

type fixtureDataset struct {
	id, title  string
	storage    common.CRS
	points     [][]float64
	supported  []common.CRS
	defaultCRS string
	extentCRS  string
	extent     []float64
}

// These operations are only the fixture backend. The protocol packages never
// import this command or contain projection formulas/PROJ dependencies.
// Spherical Mercator formula/reference: https://proj.org/operations/projections/webmerc.html
func transformPosition(position []float64, source, target common.CRS) ([]float64, error) {
	if len(position) != source.Dimensions {
		return nil, fmt.Errorf("fixture storage coordinate dimension mismatch")
	}
	longitude, latitude := position[0], position[1]
	if source.URI == epsg3857 {
		longitude = position[0] / radius * 180 / math.Pi
		latitude = (math.Pi/2 - 2*math.Atan(math.Exp(-position[1]/radius))) * 180 / math.Pi
	} else if source.Geographic {
		if source.AxisOrder == common.YX {
			longitude, latitude = latitude, longitude
		}
	} else {
		return nil, fmt.Errorf("fixture does not implement this source CRS")
	}
	var result []float64
	if target.URI == epsg3857 {
		if math.Abs(latitude) >= 90 {
			return nil, common.Invalid("crs")
		}
		result = []float64{radius * longitude * math.Pi / 180, radius * math.Log(math.Tan(math.Pi/4+latitude*math.Pi/360))}
	} else if target.Geographic {
		result = []float64{longitude, latitude}
		if target.AxisOrder == common.YX {
			result[0], result[1] = result[1], result[0]
		}
	} else {
		return nil, fmt.Errorf("fixture does not implement this target CRS")
	}
	if target.Dimensions == 3 {
		if source.Dimensions != 3 {
			// Missing height is unknown, not zero. A 2D fixture never promises
			// 3D output and cannot execute a bounded-height query without data.
			return nil, common.Invalid("bbox")
		}
		result = append(result, position[2])
	}
	return result, nil
}

func twoDimensionalDatasets() ([]fixtureDataset, error) {
	geographic := fixtureCRS(common.CRS84)
	projected := fixtureCRS(epsg3857)
	regional := [][]float64{{2, 49}, {7, 50}, {7.1, 50.1}, {8, 51}}
	stored := make([][]float64, len(regional))
	for i, point := range regional {
		var err error
		stored[i], err = transformPosition(point, geographic, projected)
		if err != nil {
			return nil, err
		}
	}
	return []fixtureDataset{
		{id: "world", title: "Global 2D boundary samples", storage: geographic, points: [][]float64{{0, 51.5}, {-75, 0}, {179, 67}, {-179, 67}, {0, 87}, {0, -87}, {0, 0}},
			supported: []common.CRS{geographic, fixtureCRS(epsg4326)}, defaultCRS: common.CRS84, extentCRS: common.CRS84, extent: []float64{-180, -90, 180, 90}},
		{id: "regional", title: "Regional data genuinely stored in EPSG3857", storage: projected, points: stored,
			supported: []common.CRS{projected, geographic, fixtureCRS(epsg4326)}, defaultCRS: common.CRS84, extentCRS: common.CRS84, extent: []float64{1, 48, 9, 52}},
	}, nil
}

func threeDimensionalDatasets() []fixtureDataset {
	return []fixtureDataset{{id: "elevated", title: "Native latitude/longitude/ellipsoidal-height samples", storage: fixtureCRS(epsg4979),
		points:     [][]float64{{50, 7, 75}, {50.1, 7.1, 125}, {51, 8, 225}},
		supported:  []common.CRS{fixtureCRS(common.CRS84h), fixtureCRS(epsg4979), fixtureCRS(common.CRS84), fixtureCRS(epsg4326), fixtureCRS(epsg3857)},
		defaultCRS: common.CRS84h, extentCRS: common.CRS84h, extent: []float64{6, 49, 0, 9, 52, 300}}}
}

func fixtureCRS(uri string) common.CRS {
	crs, _ := common.CRSByURI(uri)
	return crs
}
