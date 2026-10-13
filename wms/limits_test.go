package wms_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/wms"
)

func TestOversizedListsStopBeforeFurtherMapWork(t *testing.T) {
	base := "SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS=a&STYLES=&CRS=CRS:84&BBOX=0,0,1,1&WIDTH=1&HEIGHT=1&FORMAT=image/png"
	for _, tc := range []struct{ field, original string }{{"LAYERS", "LAYERS=a"}, {"STYLES", "STYLES="}} {
		t.Run(tc.field, func(t *testing.T) {
			query := strings.Replace(base, tc.original, tc.field+"="+strings.Repeat("a,", 29999)+"a", 1)
			called := false
			req, err := wms.Parse(query, wms.Options{AxisResolver: func(string) wms.AxisOrder { called = true; return wms.XY }})
			var e *owscore.Error
			if !errors.As(err, &e) || e.Kind != owscore.TooLarge || e.Locator != tc.field {
				t.Fatalf("wrong limit error: %v", err)
			}
			if called || req.Map != nil {
				t.Fatal("continued map work after list limit failure")
			}
		})
	}
}
