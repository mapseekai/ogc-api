package exception_test

import (
	"io"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/exception"
	"github.com/mapseekai/ogc-api/owscore"
)

func TestWMTSExceptionLocators(t *testing.T) {
	for _, tc := range []struct {
		kind    owscore.Kind
		locator string
		status  int
		omit    bool
	}{
		{owscore.VersionMismatch, "ACCEPTVERSIONS", 400, true},
		{owscore.InvalidUpdateSequence, "UPDATESEQUENCE", 400, true},
		{owscore.Internal, "private", 500, true},
		{owscore.NotAcceptable, "Accept", 406, true},
		{owscore.PointOutOfRange, "J", 400, false},
		{owscore.UnsupportedOperation, "GetFeatureInfo", 501, false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			plan, err := exception.Encode(owscore.Meta{Protocol: owscore.WMTS, Binding: owscore.KVP, Operation: owscore.GetCapabilities}, owscore.Fail(tc.kind, tc.locator, "Test"))
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Finish(0, nil)
			data, err := io.ReadAll(plan.Body)
			if err != nil || plan.Status != tc.status || strings.Contains(string(data), "locator=") == tc.omit {
				t.Fatalf("%d: %s %v", plan.Status, data, err)
			}
		})
	}
}
