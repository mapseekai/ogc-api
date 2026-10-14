package coverages_test

import (
	"context"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/response"
)

func TestCoverageSubsetGrammar(t *testing.T) {
	axes := []string{"x", "y", "time"}
	tests := []struct {
		value string
		valid bool
		count int
	}{
		{`x(1:2),y(3)`, true, 2},
		{`time("2026-10-14T00:00:00Z":"2026-10-15T00:00:00Z")`, true, 1},
		{`x(*:5)`, true, 1},
		{`x(3:1)`, false, 0},
		{`x(NaN)`, false, 0},
		{`x(1),x(2)`, false, 0},
		{`unknown(1)`, false, 0},
		{`x(1),`, false, 0},
		{`x(1:2:3)`, false, 0},
		{`x("unclosed)`, false, 0},
		{``, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			subsets, err := coverages.ParseSubsets(tt.value, axes)
			if (err == nil) != tt.valid || tt.valid && len(subsets) != tt.count {
				t.Fatal(subsets, err)
			}
		})
	}
}

func TestCoverageReaderPolicy(t *testing.T) {
	s, err := (coverages.Service{Reader: coverages.ReaderFunc(func(context.Context, coverages.Request) (response.Payload, error) { return response.Payload{}, nil }), Formats: []string{"image/tiff"}, Axes: []string{"x"}, Properties: []string{"temperature"}}).Prepare()
	if err != nil {
		t.Fatal(err)
	}
	request, err := coverages.ParseQuery(common.Query{"subset": "x(0:10)", "properties": "temperature"}, s)
	if err != nil || len(request.Subsets) != 1 || len(request.Properties) != 1 {
		t.Fatal(request, err)
	}
	for _, bad := range []common.Query{{"properties": "unknown"}, {"properties": "temperature,temperature"}, {"crs": "EPSG:3857"}, {"scale-factor": "2"}} {
		if _, err := coverages.ParseQuery(bad, s); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(`x(1:2),time("2026-10-14T00:00:00Z")`)
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) <= 4096 {
			_, _ = coverages.ParseSubsets(input, []string{"x", "y", "time"})
		}
	})
}
