package kvp_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/mapseekai/ogc-api/kvp"
	"github.com/mapseekai/ogc-api/owscore"
)

func TestListLimitPreservesWireSemantics(t *testing.T) {
	for _, tc := range []struct {
		query   string
		maximum int
		want    []string
		present bool
		kind    owscore.Kind
	}{
		{"LAYERS=A%2CB,C", 2, []string{"A,B", "C"}, true, ""},
		{"LAYERS=%252C", 1, []string{"%2C"}, true, ""},
		{"LAYERS=,named,", 3, []string{"", "named", ""}, true, ""},
		{"LAYERS=", 1, []string{""}, true, ""},
		{"", 1, nil, false, ""},
		{"LAYERS=a,b", 1, nil, true, owscore.TooLarge},
		{"LAYERS=a&layers=b", 1, nil, true, owscore.InvalidParameter},
		{"LAYERS=a", -1, nil, false, owscore.InvalidParameter},
		{"LAYERS=a,b", 0, []string{"a", "b"}, true, ""},
	} {
		t.Run(fmt.Sprintf("%s/max=%d", tc.query, tc.maximum), func(t *testing.T) {
			p, err := kvp.Parse(tc.query, owscore.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			got, present, err := p.ListLimit("LAYERS", tc.maximum)
			if present != tc.present || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%v %t; want %v %t", got, present, tc.want, tc.present)
			}
			var protocolErr *owscore.Error
			if tc.kind == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.As(err, &protocolErr) || protocolErr.Kind != tc.kind {
				t.Fatalf("error %v; want %s", err, tc.kind)
			}
		})
	}
}

func TestParameters(t *testing.T) {
	p, err := kvp.Parse("service=WMS&STYLES=&LAYERS=a%2Cb,c&title=A%2BB+C&x=one&X=two", owscore.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok, e := p.One("SERVICE"); e != nil || !ok || v != "WMS" {
		t.Fatal(v, ok, e)
	}
	if v, ok, e := p.One("styles"); e != nil || !ok || v != "" {
		t.Fatal(v, ok, e)
	}
	if _, ok, _ := p.One("missing"); ok {
		t.Fatal("missing field became present")
	}
	if v, _, _ := p.One("title"); v != "A+B C" {
		t.Fatal(v)
	}
	if _, _, e := p.One("x"); e == nil {
		t.Fatal("duplicate accepted")
	}
	layers, _, e := p.List("layers")
	if e != nil || len(layers) != 2 || layers[0] != "a,b" {
		t.Fatal(layers, e)
	}
}

func TestMalformedAndLimits(t *testing.T) {
	for _, q := range []string{"x=%", "x=%zz", "=a", "a=%00", "a=%FF"} {
		t.Run(q, func(t *testing.T) {
			if _, e := kvp.Parse(q, owscore.Limits{}); e == nil {
				t.Errorf("accepted %q", q)
			}

		})
	}
	for _, l := range []owscore.Limits{{MaxQueryBytes: 2}, {MaxParameters: 1}} {
		t.Run(fmt.Sprintf("limits_%v", l), func(t *testing.T) {
			if _, e := kvp.Parse("a=1&b=2", l); e == nil {
				t.Fatal("limit not enforced")
			}

		})
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"SERVICE=WMS&STYLES=", "a=1&a=2", "a=%2C,%252F", "%", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, e := kvp.Parse(s, owscore.Limits{})
		if e == nil && p.RawQuery() != s {
			t.Fatal("raw query changed")
		}
	})
}

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		_, err := kvp.Parse("SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=world&STYLE=default&FORMAT=image%2Fpng&TILEMATRIXSET=local&TILEMATRIX=L00&TILEROW=0&TILECOL=0", owscore.Limits{})
		if err != nil {
			b.Fatal(err)
		}
	}
}
