package api_test

import (
	"context"
	"io"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
)

func BenchmarkTileParallel(b *testing.B) {
	e := engine(b, catalog(&memoryFeatures{}, tileReader("tile")), 1024)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0"})
			if err != nil {
				b.Error(err)
				return
			}
			n, err := io.Copy(io.Discard, plan.Body)
			if finish := plan.Finish(n, err); finish != nil {
				b.Error(finish)
				return
			}
		}
	})
}

func tileReader(value string) tiles.Reader {
	return tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
		return response.Bytes("application/vnd.mapbox-vector-tile", []byte(value)), nil
	})
}
