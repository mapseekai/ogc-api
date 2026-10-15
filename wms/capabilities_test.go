package wms_test

import (
	"encoding/xml"
	"testing"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wms"
)

func TestCapabilitiesExtents(t *testing.T) {
	c := wms.Catalog{Service: owscore.Service{Title: "Survey"}, Formats: []string{"image/png"}, Layers: map[string]wms.Layer{
		"a": {Title: "A", CRS: []string{"EPSG:4326", "EPSG:3857"}, GeographicBounds: [4]float64{10, 20, 30, 40}, BoundingBoxes: map[string][4]float64{"EPSG:3857": {1, 2, 3, 4}}},
		"b": {Title: "B", CRS: []string{"EPSG:4326"}, GeographicBounds: [4]float64{-10, -20, 20, 30}},
	}}
	for _, version := range []string{"1.1.1", "1.3.0"} {
		t.Run(version, func(t *testing.T) {
			data, err := wms.EncodeCapabilities(c, wms.CapabilitiesOptions{Version: version, Endpoint: "https://example.invalid/wms"})
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Layer struct {
					SRS []string `xml:"SRS"`
					CRS []string `xml:"CRS"`
					Box struct {
						MinX float64 `xml:"minx,attr"`
						MinY float64 `xml:"miny,attr"`
						MaxX float64 `xml:"maxx,attr"`
						MaxY float64 `xml:"maxy,attr"`
					} `xml:"LatLonBoundingBox"`
					Children []struct {
						Boxes []struct {
							CRS  string  `xml:"CRS,attr"`
							SRS  string  `xml:"SRS,attr"`
							MinX float64 `xml:"minx,attr"`
							MinY float64 `xml:"miny,attr"`
							MaxX float64 `xml:"maxx,attr"`
							MaxY float64 `xml:"maxy,attr"`
						} `xml:"BoundingBox"`
					} `xml:"Layer"`
				} `xml:"Capability>Layer"`
			}
			if err := xml.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			root := doc.Layer
			box := root.Children[0].Boxes[0]
			if version == "1.3.0" {
				if len(root.CRS) != 1 || root.CRS[0] != "EPSG:4326" || box.CRS != "EPSG:4326" || box.MinX != 20 || box.MinY != 10 || box.MaxX != 40 || box.MaxY != 30 {
					t.Fatalf("axis order or inherited CRS: %s", data)
				}
			} else if len(root.SRS) != 1 || root.Box.MinX != -10 || root.Box.MinY != -20 || root.Box.MaxX != 30 || root.Box.MaxY != 40 || box.MinX != 10 || box.MinY != 20 {
				t.Fatalf("root union bounds or CRS: %s", data)
			}
			projected := root.Children[0].Boxes[1]
			if projected.MinX != 1 || projected.MinY != 2 || projected.MaxX != 3 || projected.MaxY != 4 {
				t.Fatalf("projected extent changed: %s", data)
			}
		})
	}
}
