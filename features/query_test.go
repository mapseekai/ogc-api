package features_test

import (
	"fmt"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func TestFeatureQueries(t *testing.T) {
	options := features.Options{DefaultLimit: 2, MaxLimit: 5, FilterProperties: []string{"name"}}
	query, err := features.ParseQuery(common.Query{"name": "a", "limit": "3", "bbox": "170,-10,-170,10"}, options)
	if err != nil || query.Properties["name"] != "a" || query.Limit != 3 {
		t.Fatal(query, err)
	}
	for _, bad := range []common.Query{{"limit": "0"}, {"limit": "-1"}, {"crs": common.CRS84}, {"datetime": "invalid"}} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			if _, err := features.ParseQuery(bad, options); err == nil {
				t.Fatal("accepted invalid query", bad)
			}

		})
	}
	for _, raw := range []string{"6", "18446744073709551616000"} {
		t.Run("clamp_"+raw, func(t *testing.T) {
			query, err := features.ParseQuery(common.Query{"limit": raw}, options)
			if err != nil || query.Limit != 5 {
				t.Fatal("feature limit must be clamped", query, err)
			}

		})
	}
	if err := (features.Options{FilterProperties: []string{"limit"}}).Validate(); err == nil {
		t.Fatal("reserved filter accepted")
	}
}
