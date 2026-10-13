package wms_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wms"
)

func mapQuery(version string) string {
	p := "SERVICE=WMS&VERSION=" + version + "&REQUEST=GetMap&LAYERS=a,b&STYLES=,&SRS=EPSG:4326&BBOX=100,20,110,30&WIDTH=256&HEIGHT=128&FORMAT=image/png"
	if version == "1.3.0" {
		p = strings.ReplaceAll(p, "SRS=", "CRS=")
		p = strings.ReplaceAll(p, "BBOX=100,20,110,30", "BBOX=20,100,30,110")
	}
	if version == "1.0.0" {
		p = strings.ReplaceAll(p, "VERSION=", "WMTVER=")
		p = strings.ReplaceAll(p, "REQUEST=GetMap", "REQUEST=map")
		p = strings.ReplaceAll(p, "image/png", "PNG")
	}
	return p
}
func TestAllVersionsNormalize(t *testing.T) {
	for _, v := range wms.Versions() {
		t.Run(v, func(t *testing.T) {
			r, e := wms.Parse(mapQuery(v), wms.Options{})
			if e != nil {
				t.Fatal(e)
			}
			if r.Map.Bounds != [4]float64{100, 20, 110, 30} || r.Map.Format != "image/png" || !r.Map.Normalized {
				t.Fatalf("%+v", r.Map)
			}
			if !reflect.DeepEqual(r.Map.Styles, []string{"", ""}) {
				t.Fatal(r.Map.Styles)
			}
		})
	}
}
func TestCapabilitiesNegotiation(t *testing.T) {
	for _, tt := range []struct{ v, w string }{{"", "1.3.0"}, {"0.9.0", "1.0.0"}, {"1.1.5", "1.1.1"}, {"9.0.0", "1.3.0"}} {
		q := "SERVICE=WMS&REQUEST=GetCapabilities"
		if tt.v != "" {
			q += "&VERSION=" + tt.v
		}
		r, e := wms.Parse(q, wms.Options{})
		if e != nil || r.Version != tt.w {
			t.Fatal(r, e)
		}
	}
	if _, e := wms.Parse(strings.ReplaceAll(mapQuery("1.1.1"), "VERSION=1.1.1", "VERSION=1.2.0"), wms.Options{}); e == nil {
		t.Fatal("map was silently downgraded")
	}
}
func TestMapRejections(t *testing.T) {
	q := mapQuery("1.3.0")
	tests := []string{
		strings.ReplaceAll(q, "STYLES=,&", ""), q + "&layers=c",
		strings.ReplaceAll(q, "BBOX=20,100,30,110", "BBOX=NaN,0,1,2"),
		strings.ReplaceAll(q, "WIDTH=256", "WIDTH=18446744073709551616"),
		strings.ReplaceAll(q, "HEIGHT=128", "HEIGHT=0"),
		strings.ReplaceAll(q, "SERVICE=WMS", "SERVICE=WMTS"),
		strings.ReplaceAll(q, "REQUEST=GetMap", "REQUEST=getmap"),
	}
	for _, s := range tests {
		if _, e := wms.Parse(s, wms.Options{}); e == nil {
			t.Errorf("accepted %s", s)
		}
	}
}
func TestUnknownAxisAndInfo(t *testing.T) {
	r, e := wms.Parse(strings.ReplaceAll(mapQuery("1.3.0"), "EPSG:4326", "CUSTOM:1"), wms.Options{})
	if e != nil || r.Map.Normalized {
		t.Fatal(r, e)
	}
	q := strings.ReplaceAll(mapQuery("1.3.0"), "REQUEST=GetMap", "REQUEST=GetFeatureInfo") + "&QUERY_LAYERS=b&I=255&J=127&INFO_FORMAT=application/json&FEATURE_COUNT=invalid"
	r, e = wms.Parse(q, wms.Options{})
	if e != nil || r.FeatureInfo.FeatureCount != 1 {
		t.Fatal(r, e)
	}
	if _, e = wms.Parse(strings.ReplaceAll(q, "I=255", "I=256"), wms.Options{}); e == nil {
		t.Fatal("pixel boundary accepted")
	}
}
func FuzzParse(f *testing.F) {
	f.Add(mapQuery("1.3.0"))
	f.Add(mapQuery("1.0.0"))
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = wms.Parse(s, wms.Options{Limits: owscore.Limits{MaxQueryBytes: 4096}})
	})
}

func BenchmarkParse(b *testing.B) {
	query := mapQuery("1.3.0")
	for b.Loop() {
		if _, err := wms.Parse(query, wms.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}
