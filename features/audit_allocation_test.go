package features_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func BenchmarkAuditRejectOversizedFeature(b *testing.B) {
	text := strings.Repeat("x", 4<<20)
	for _, which := range []string{"id", "link", "properties"} {
		b.Run(which, func(b *testing.B) {
			f := features.Feature{}
			switch which {
			case "id":
				f.ID = text
			case "link":
				f.Links = []common.Link{{Href: text, Rel: "related"}}
			case "properties":
				f.Properties = json.RawMessage(`{"x":"` + text + `"}`)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := features.EncodeFeature(f, 1024); err == nil {
					b.Fatal("budget should reject fixture")
				}
			}
		})
	}
}
