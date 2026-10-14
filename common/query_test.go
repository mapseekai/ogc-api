package common_test

import (
	"errors"
	"testing"

	"github.com/mapseekai/ogc-api/common"
)

func TestQueryValidation(t *testing.T) {
	for _, raw := range []string{"limit=1&limit=2", "%6cimit=1&limit=2", "bbox=%zz", "a=%FF", "=x", "a=1;b=2"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := common.ParseQuery(raw, 1024, 10); err == nil {
				t.Fatalf("accepted %q", raw)
			}
		})
	}
	for _, raw := range []string{"170,-10,-170,10", "1,2,1,2", "0,0,0,1,1,10"} {
		if _, err := common.ParseBBox(raw, true); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{"NaN,0,1,2", "0,0,Inf,2", "0,5,1,2", "0,0,1", "0,0,1,2,3", "181,0,1,1"} {
		if _, err := common.ParseBBox(raw, true); err == nil {
			t.Fatalf("accepted bbox %s", raw)
		}
	}
	for _, raw := range []string{"2026-10-14T00:00:00Z", "../2026-10-14T00:00:00Z", "2026-10-14T00:00:00Z/.."} {
		if _, err := common.ParseDatetime(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"", "2026-10-14", "../..", "2026-10-15T00:00:00Z/2026-10-14T00:00:00Z"} {
		if _, err := common.ParseDatetime(raw); err == nil {
			t.Fatal("accepted datetime", raw)
		}
	}
	q, err := common.ParseQuery("Limit=2", 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Only("limit"); err == nil {
		t.Fatal("API query names must be case-sensitive")
	}
}

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

func TestPrivateErrorIsMasked(t *testing.T) {
	err := errors.New("password=very-secret")
	public := common.PublicError(err)
	if public.Status != 500 || public.Description == err.Error() {
		t.Fatal(public)
	}
	if !errors.Is(public, err) {
		t.Fatal("lost diagnostic cause")
	}
}

func FuzzParse(f *testing.F) {
	for _, raw := range []string{"bbox=170,-10,-170,10", "limit=10", "a=%ff", "datetime=../2026-01-01T00:00:00Z"} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		q, err := common.ParseQuery(raw, 1024, 32)
		if err == nil {
			if value, ok := q["bbox"]; ok {
				_, _ = common.ParseBBox(value, true)
			}
			if value, ok := q["datetime"]; ok {
				_, _ = common.ParseDatetime(value)
			}
		}
	})
}
