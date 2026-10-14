package common_test

import (
	"testing"

	"github.com/mapseekai/ogc-api/common"
)

func TestContentNegotiation(t *testing.T) {
	tests := []struct {
		accept  string
		offered []string
		want    string
	}{
		{"", []string{common.JSON}, common.JSON},
		{"application/json;q=0,*/*;q=1", []string{common.JSON}, ""},
		{"application/json;q=0,*/*;q=1", []string{common.JSON, "image/png"}, "image/png"},
		{"image/*;q=0.5,application/json;q=0.8", []string{"image/png", common.JSON}, common.JSON},
		{"application/json;q=NaN", []string{common.JSON}, ""},
		{"text/html", []string{common.JSON}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			got, err := common.Negotiate(tt.accept, "", tt.offered)
			if got != tt.want || (err != nil) != (tt.want == "") {
				t.Fatalf("got %s %v, want %s", got, err, tt.want)
			}
		})
	}
	if got, err := common.Negotiate("text/html", "json", []string{common.GeoJSON}); err != nil || got != common.GeoJSON {
		t.Fatal(got, err)
	}
}
