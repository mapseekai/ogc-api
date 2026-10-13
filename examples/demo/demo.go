// Package demo provides fixed, locally embedded content for integration examples.
// It is not a map renderer and is not a CITE conformance data provider.
package demo

import (
	"context"
	_ "embed"
	"os"
	"strings"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

//go:embed fixture.png
var pngData []byte

func PNG() response.Payload { return response.Bytes("image/png", pngData) }
func Info() response.Payload {
	return response.Bytes("application/json", []byte(`{"type":"FeatureCollection","features":[]}`))
}

func Catalogs() (wms.Catalog, wmts.Catalog) {
	service := owscore.Service{Title: "ogc-api demo & test", Abstract: "Static protocol fixtures, not generated maps"}
	mapCatalog := wms.Catalog{
		Service: service,
		Formats: []string{"image/png"}, InfoFormats: []string{"application/json"},
		Layers: map[string]wms.Layer{
			"world": {
				Title:            "World <demo>",
				CRS:              []string{"EPSG:4326", "CRS:84", "EPSG:3857"},
				GeographicBounds: [4]float64{-180, -90, 180, 90},
				Queryable:        true,
				Styles:           map[string]owscore.Style{"named": {Title: "Named style"}},
			},
		},
	}
	tileCatalog := wmts.Catalog{
		Service: service,
		Layers: map[string]wmts.Layer{
			"world": {
				Title: "World <demo>", DefaultStyle: "default", Styles: map[string]owscore.Style{"default": {Title: "Default"}},
				Formats: []string{"image/png"}, InfoFormats: []string{"application/json"},
				MatrixSets: map[string]wmts.MatrixLink{"local": {Limits: map[string]wmts.MatrixLimits{
					"L00": {MinTileRow: 0, MaxTileRow: 1, MinTileCol: 0, MaxTileCol: 2},
					"10m": {MinTileRow: 1, MaxTileRow: 2, MinTileCol: 1, MaxTileCol: 3},
				}}},
			},
		},
		MatrixSets: map[string]wmts.MatrixSet{
			"local": {Title: "Custom grid", CRS: "urn:ogc:def:crs:EPSG::3857", Matrices: map[string]wmts.Matrix{
				"L00": {ScaleDenominator: 1000, TopLeftCorner: [2]float64{0, 1000}, TileWidth: 256, TileHeight: 256, MatrixWidth: 3, MatrixHeight: 2},
				"10m": {ScaleDenominator: 100, TopLeftCorner: [2]float64{0, 1000}, TileWidth: 512, TileHeight: 128, MatrixWidth: 5, MatrixHeight: 3},
			}},
		},
	}
	return mapCatalog, tileCatalog
}

func Config(baseURL string) (server.Config, error) {
	mapCatalog, tileCatalog := Catalogs()
	snapshot, err := server.NewSnapshot(&mapCatalog, &tileCatalog)
	if err != nil {
		return server.Config{}, err
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	return server.Config{
		Catalog: server.Static(snapshot), AllowHEAD: true,
		WMSURL: baseURL + "/wms", WMTSURL: baseURL + "/wmts", RESTBaseURL: baseURL,
		RESTTemplates: []wmts.Template{
			{Pattern: "/wmts/1.0.0/WMTSCapabilities.xml", Operation: owscore.GetCapabilities},
			{
				Pattern:   "/tiles/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}.png",
				Operation: owscore.GetTile,
				Fixed:     map[string]string{"FORMAT": "image/png"},
			},
			{
				Pattern:   "/info/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}/{J}/{I}.json",
				Operation: owscore.GetFeatureInfo,
				Fixed:     map[string]string{"INFOFORMAT": "application/json"},
			},
		},
		Handlers: server.Handlers{
			GetMap:             func(context.Context, *wms.MapRequest) (response.Payload, error) { return PNG(), nil },
			GetTile:            func(context.Context, *wmts.TileRequest) (response.Payload, error) { return PNG(), nil },
			GetWMSFeatureInfo:  func(context.Context, *wms.FeatureInfoRequest) (response.Payload, error) { return Info(), nil },
			GetWMTSFeatureInfo: func(context.Context, *wmts.FeatureInfoRequest) (response.Payload, error) { return Info(), nil },
		},
	}, nil
}

// Address keeps demos on loopback by default and permits a host-selected address.
func Address() string {
	if address := os.Getenv("OGC_ADDR"); address != "" {
		return address
	}
	return "127.0.0.1:8080"
}
