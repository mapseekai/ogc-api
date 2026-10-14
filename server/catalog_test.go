package server_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/server"
)

func TestCatalogSnapshotScopeAndIsolation(t *testing.T) {
	maps, tiles := demo.Catalogs()
	snapshot, err := server.NewSnapshot(&maps, &tiles)
	if err != nil {
		t.Fatal(err)
	}
	delete(maps.Layers, "world")
	delete(tiles.Layers, "world")
	config := makeConfig(t)
	var calls atomic.Int32
	config.Catalog = func(_ context.Context, scope string) (*server.Snapshot, error) {
		calls.Add(1)
		if scope != "trusted" {
			return nil, fmt.Errorf("untrusted scope reached catalog")
		}
		return snapshot, nil
	}
	e := engine(t, config)
	status, data := call(t, e, server.Input{Method: "GET", Protocol: owscore.WMTS, Scope: "trusted", RawQuery: adaptertest.TileQuery + "&SCOPE=admin&TENANT=other"})
	if status != 200 || calls.Load() != 1 || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatal(status, calls.Load(), string(data))
	}
}

func TestConcurrentCatalogReplacement(t *testing.T) {
	config := makeConfig(t)
	var current atomic.Pointer[server.Snapshot]
	var snapshots []*server.Snapshot
	for _, title := range []string{"snapshot A", "snapshot B"} {
		maps, _ := demo.Catalogs()
		maps.Service.Title = title
		s, err := server.NewSnapshot(&maps, nil)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, s)
	}
	current.Store(snapshots[0])
	config.Catalog = func(context.Context, string) (*server.Snapshot, error) { return current.Load(), nil }
	e := engine(t, config)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for j := 0; j < 15; j++ {
				current.Store(snapshots[(index+j)%2])
				plan, err := e.Handle(context.Background(), capabilities(owscore.WMS, "1.3.0"))
				if err != nil {
					t.Error(err)
					return
				}
				data, err := io.ReadAll(plan.Body)
				if closeErr := plan.Finish(int64(len(data)), err); closeErr != nil {
					t.Error(closeErr)
					return
				}
				a := bytes.Contains(data, []byte("snapshot A"))
				b := bytes.Contains(data, []byte("snapshot B"))
				if a == b {
					t.Error("request mixed catalog snapshots")
				}
			}
		}(i)
	}
	wg.Wait()
}
