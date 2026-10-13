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
