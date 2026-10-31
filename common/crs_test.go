package common_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/mapseekai/ogc-api/common"
)

func TestCRSNativeAxesAndDimensions(t *testing.T) {
	for _, tc := range []struct {
		uri, bbox string
		valid     bool
	}{
		{common.CRS84, "170,-10,-170,10", true},
		{common.CRS84, "1,91,2,92", false},
		{common.CRS84h, "170,-10,-100,-170,10,100", true},
		{common.CRS84h, "1,2,3,4", false},
		{"http://www.opengis.net/def/crs/EPSG/0/4326", "-10,170,10,-170", true},
		{"http://www.opengis.net/def/crs/EPSG/0/4326", "100,1,110,2", false},
		{"http://www.opengis.net/def/crs/EPSG/0/4326", "10,1,-10,2", false},
		{"http://www.opengis.net/def/crs/EPSG/0/3857", "-20000000,-30000000,20000000,30000000", true},
		{"http://www.opengis.net/def/crs/EPSG/0/3857", "2,0,1,1", false},
		{"http://www.opengis.net/def/crs/EPSG/0/4979", "-10,170,-1,10,-170,3", true},
	} {
		t.Run(tc.uri+tc.bbox, func(t *testing.T) {
			crs, ok := common.CRSByURI(tc.uri)
			if !ok {
				t.Fatal("missing standard metadata")
			}
			bbox, err := crs.ParseBBox(tc.bbox)
			if (err == nil) != tc.valid {
				t.Fatal(bbox, err)
			}
		})
	}
	projected := common.CRS{URI: "urn:example:crs:north-east", AxisOrder: common.YX, Dimensions: 3}
	want := []float64{-3000000, -4000000, -10, 3000000, 4000000, 100}
	got, err := projected.ParseBBox("-3000000,-4000000,-10,3000000,4000000,100")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("native coordinates were changed", got, err)
	}
	for _, bad := range [][]float64{{0, 0, math.Inf(1), 1, 1, 2}, {0, 0, 3, 1, 1, 2}, {0, 0, math.NaN(), 1, 1, 2}} {
		if projected.ValidateBBox(bad) == nil {
			t.Fatal("invalid coordinates accepted", bad)
		}
	}
}

func TestCRSDeclarationValidation(t *testing.T) {
	for _, uri := range []string{"", "relative", "http:", "http:///bad", "https://", "http://user:secret@example.test/x", "https://example.test/x#fragment", "urn:example:bad\r\nHeader: x", "urn:example:<bad>", "https://example.test/雪", "https://example.test/\\bad", "https://example.test/{}", "https://example.test/?q=%ZZ", "https://example.test/[invalid]", "https://example.test/?q=[]", "urn:", "urn:a:value", "urn:-bad:value", "urn:example:"} {
		if common.ValidateCRS(uri) == nil {
			t.Fatal("invalid URI", uri)
		}
	}
	for _, uri := range []string{"http://[::1]/crs", "https://example.test/a%20b?part=x/y?z", "urn:example:spatial:reference", "http://www.opengis.net/def/crs/EPSG/0/4326"} {
		if err := common.ValidateCRS(uri); err != nil {
			t.Fatal("valid URI rejected", uri, err)
		}
	}
	crs, _ := common.CRSByURI("http://www.opengis.net/def/crs/EPSG/0/4326")
	crs.AxisOrder = common.XY
	if crs.Validate() == nil {
		t.Fatal("EPSG4326 silently treated as longitude/latitude")
	}
	for _, crs := range []common.CRS{
		{URI: "urn:example:custom", Dimensions: 2},
		{URI: "urn:example:custom", Dimensions: 4, AxisOrder: common.XY},
	} {
		if crs.Validate() == nil {
			t.Fatal("ambiguous axes or dimensions accepted")
		}
	}
}
