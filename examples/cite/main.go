// Command cite serves official CITE fixtures through the repository's protocol engines.
package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/examples/internal/nethttp"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/tiles"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

func main() {
	data, base := os.Getenv("OGC_CITE_DATA"), os.Getenv("OGC_PUBLIC_URL")
	if data == "" || base == "" {
		log.Fatal("OGC_CITE_DATA and OGC_PUBLIC_URL are required")
	}
	raster, err := loadRasters(data)
	if err != nil {
		log.Fatal(err)
	}
	tile, err := os.ReadFile(filepath.Join(data, "nodataSquare.png"))
	if err != nil {
		log.Fatal(err)
	}
	handler, err := citeHandler(base, raster, tile)
	if err != nil {
		log.Fatal(err)
	}
	address := os.Getenv("OGC_ADDR")
	if address == "" {
		address = "127.0.0.1:18088"
	}
	log.Printf("CITE fixtures: %s", base)
	log.Fatal(http.ListenAndServe(address, handler))
}

func citeHandler(base string, raster rasterProvider, tile []byte) (http.Handler, error) {
	mapCatalog := wms.Catalog{Service: owscore.Service{Title: "Official Blue Lake fixtures"}, UpdateSequence: "1", Formats: []string{"image/png"}, InfoFormats: []string{"application/json"}, Layers: make(map[string]wms.Layer)}
	for name, layer := range raster {
		mapCatalog.Layers[name] = wms.Layer{Title: "cite:" + name, CRS: []string{"CRS:84", "EPSG:4326"}, GeographicBounds: layer.bounds(), Queryable: len(layer.features) > 0}
	}
	tileCatalog := wmts.Catalog{Service: owscore.Service{Title: "Official transparent tile fixture"}, UpdateSequence: "1",
		Layers:     map[string]wmts.Layer{"TransparentNodata": {Title: "TransparentNodata", DefaultStyle: "Default_style", Styles: map[string]owscore.Style{"Default_style": {Title: "Default_style"}}, Formats: []string{"image/png"}, InfoFormats: []string{"application/json"}, MatrixSets: map[string]wmts.MatrixLink{"TransparentNodata": {}}}},
		MatrixSets: map[string]wmts.MatrixSet{"TransparentNodata": {CRS: "urn:ogc:def:crs:OGC:1.3:CRS84", Matrices: map[string]wmts.Matrix{"0": {ScaleDenominator: 559082264.0287178, TopLeftCorner: [2]float64{-180, 90}, TileWidth: 256, TileHeight: 256, MatrixWidth: 1, MatrixHeight: 1}}}},
	}
	tileCatalog.MatrixSets["LimitedGrid"] = wmts.MatrixSet{CRS: "urn:ogc:def:crs:OGC:1.3:CRS84", Matrices: map[string]wmts.Matrix{
		"0": {ScaleDenominator: 279541132.0143589, TopLeftCorner: [2]float64{-180, 90}, TileWidth: 256, TileHeight: 256, MatrixWidth: 2, MatrixHeight: 2},
		"1": {ScaleDenominator: 139770566.00717945, TopLeftCorner: [2]float64{-180, 90}, TileWidth: 256, TileHeight: 256, MatrixWidth: 4, MatrixHeight: 4},
		"2": {ScaleDenominator: 69885283.00358973, TopLeftCorner: [2]float64{-180, 90}, TileWidth: 256, TileHeight: 256, MatrixWidth: 8, MatrixHeight: 8},
	}}
	tileCatalog.Layers["Samples"] = wmts.Layer{Title: "Repeated tile samples", DefaultStyle: "default", Styles: map[string]owscore.Style{"default": {Title: "Default"}}, Formats: []string{"image/png"}, InfoFormats: []string{"application/json"}, MatrixSets: map[string]wmts.MatrixLink{"LimitedGrid": {Limits: map[string]wmts.MatrixLimits{"0": {MinTileRow: 0, MaxTileRow: 0, MinTileCol: 0, MaxTileCol: 0}, "1": {MinTileRow: 1, MaxTileRow: 1, MinTileCol: 1, MaxTileCol: 1}, "2": {MinTileRow: 2, MaxTileRow: 2, MinTileCol: 2, MaxTileCol: 2}}}}, Dimensions: map[string]owscore.Dimension{"Time": {Default: "2020-01-01", Current: true, Values: []string{"2020-01-01", "2020-01-02"}}}}
	tileCatalog.Layers["NoInfo"] = wmts.Layer{Title: "Nonqueryable sample", DefaultStyle: "default", Styles: map[string]owscore.Style{"default": {Title: "Default"}}, Formats: []string{"image/png"}, MatrixSets: map[string]wmts.MatrixLink{"TransparentNodata": {}}}
	snapshot, err := server.NewSnapshot(&mapCatalog, &tileCatalog)
	if err != nil {
		return nil, err
	}
	reader := tiles.ReaderFunc(func(ctx context.Context, _ tiles.Request) (response.Payload, error) {
		if err := ctx.Err(); err != nil {
			return response.Payload{}, err
		}
		return response.Bytes("image/png", tile), nil
	})
	picture, err := png.Decode(bytes.NewReader(tile))
	if err != nil {
		return nil, err
	}
	limits := owscore.Limits{MaxImageWidth: 1024, MaxImageHeight: 768, MaxImagePixels: 1024 * 768}
	config := server.Config{Catalog: server.Static(snapshot), AllowHEAD: true, WMS: wms.Options{Versions: []string{"1.3.0"}}, Limits: limits, WMSURL: base + "/wms?", WMTSURL: base + "/wmts?", RESTBaseURL: base,
		CompareUpdateSequence: func(requested, current string) (int, error) {
			revision, err := strconv.ParseInt(requested, 10, 64)
			if err != nil {
				return 0, owscore.Fail(owscore.InvalidUpdateSequence, "UPDATESEQUENCE", "Invalid numeric fixture revision")
			}
			active, err := strconv.ParseInt(current, 10, 64)
			return cmp.Compare(revision, active), err
		},
		RESTTemplates: []wmts.Template{
			{Pattern: "/wmts/1.0.0/WMTSCapabilities.xml", Operation: owscore.GetCapabilities},
			{Pattern: "/tiles/TransparentNodata/{style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}.png", Operation: owscore.GetTile, Fixed: map[string]string{"LAYER": "TransparentNodata", "FORMAT": "image/png"}},
			{Pattern: "/tiles/NoInfo/{style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}.png", Operation: owscore.GetTile, Fixed: map[string]string{"LAYER": "NoInfo", "FORMAT": "image/png"}},
			{Pattern: "/info/TransparentNodata/{style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}/{J}/{I}.json", Operation: owscore.GetFeatureInfo, Fixed: map[string]string{"LAYER": "TransparentNodata", "INFOFORMAT": "application/json"}},
			{Pattern: "/samples/{style}/{Time}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}.png", Operation: owscore.GetTile, Fixed: map[string]string{"LAYER": "Samples", "FORMAT": "image/png"}, Dimensions: []string{"Time"}},
			{Pattern: "/sampleinfo/{style}/{Time}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}/{J}/{I}.json", Operation: owscore.GetFeatureInfo, Fixed: map[string]string{"LAYER": "Samples", "INFOFORMAT": "application/json"}, Dimensions: []string{"Time"}},
		},
		Handlers: server.Handlers{GetMap: maps.WMS(raster, limits), GetWMSFeatureInfo: raster.featureInfo, GetTile: tiles.WMTS(reader), GetWMTSFeatureInfo: func(ctx context.Context, r *wmts.FeatureInfoRequest) (response.Payload, error) {
			if err := ctx.Err(); err != nil {
				return response.Payload{}, err
			}
			red, green, blue, alpha := picture.At(int(r.PixelColumn), int(r.PixelRow)).RGBA()
			data, err := json.Marshal([]uint32{red, green, blue, alpha})
			return response.Bytes("application/json", data), err
		}},
	}
	legacy, err := server.New(config)
	if err != nil {
		return nil, err
	}
	config.WMS.Versions = []string{"1.1.1"}
	config.WMSURL = base + "/wms11?"
	legacy11, err := server.New(config)
	if err != nil {
		return nil, err
	}
	grid := tiles.MatrixSet{ID: "WebMercatorQuad", URI: "http://www.opengis.net/def/tilematrixset/OGC/1.0/WebMercatorQuad", CRS: "http://www.opengis.net/def/crs/EPSG/0/3857", OrderedAxes: []string{"E", "N"},
		Matrices: []tiles.Matrix{{ID: "0", ScaleDenominator: 559082264.0287178, CellSize: 156543.03392804097, PointOfOrigin: [2]float64{-20037508.342789244, 20037508.342789244}, TileWidth: 256, TileHeight: 256, MatrixWidth: 1, MatrixHeight: 1}}}
	catalog, err := api.NewSnapshot(api.Catalog{Title: "CITE collection fixtures", BaseURL: base + "/tilesapi", MatrixSets: []tiles.MatrixSet{grid}, Collections: []api.Collection{{Metadata: common.Collection{ID: "world", Title: "Synthetic geographic boundary samples", Extent: &common.Extent{Spatial: &common.SpatialExtent{CRS: common.CRS84, BBox: [][]float64{{-180, -90, 180, 90}}}}}, Features: &features.Service{Reader: sampleFeatures{}}, Tiles: []tiles.Service{{MatrixSetID: grid.ID, DataType: "map", Formats: []string{"image/png"}, Reader: reader}}}}})
	if err != nil {
		return nil, err
	}
	engine, err := api.New(api.Config{Catalog: api.Static(catalog)})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	jobs := &echoJobs{inputURL: base + "/processapi/input"}
	processCatalog, err := api.NewSnapshot(api.Catalog{Title: "CITE process fixtures", BaseURL: base + "/processapi", Processes: []processes.Service{jobs.service()}, Jobs: &processes.Jobs{Reader: jobs, Lister: jobs}, ConformsTo: []string{"http://www.opengis.net/spec/ogcapi-processes-1/1.0/conf/core", "http://www.opengis.net/spec/ogcapi-processes-1/1.0/conf/json", "http://www.opengis.net/spec/ogcapi-processes-1/1.0/conf/ogc-process-description"}})
	if err != nil {
		return nil, err
	}
	processEngine, err := api.New(api.Config{Catalog: api.Static(processCatalog)})
	if err != nil {
		return nil, err
	}
	mux.Handle("/processapi/", nethttp.APIHandler(processEngine, "cite"))
	mux.HandleFunc("/processapi/input", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method != "HEAD" {
			_, _ = w.Write([]byte(`"reference sample"`))
		}
	})
	mux.Handle("/wms", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMS}))
	mux.Handle("/wms11", nethttp.Handler(legacy11, server.Endpoint{Protocol: owscore.WMS}))
	mux.Handle("/wmts", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMTS}))
	mux.Handle("/wmts/", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	mux.Handle("/tilesapi/", nethttp.APIHandler(engine, "cite"))
	mux.Handle("/tiles/", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	mux.Handle("/info/", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	mux.Handle("/samples/", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	mux.Handle("/sampleinfo/", nethttp.Handler(legacy, server.Endpoint{Protocol: owscore.WMTS, Binding: owscore.REST}))
	return mux, nil
}
