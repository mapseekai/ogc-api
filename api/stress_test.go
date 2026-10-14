package api_test

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
)

func TestTwoThousandCallersRespectAdmission(t *testing.T) {
	stressAdmission(t, 64)
}

func TestTwoThousandSuccessfulConcurrentCallers(t *testing.T) {
	stressAdmission(t, 2048)
}

func stressAdmission(t *testing.T, capacity int) {
	t.Helper()
	const callers = 2000
	var backendCalls atomic.Int64
	backend := tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
		backendCalls.Add(1)
		return response.Bytes("application/vnd.mapbox-vector-tile", []byte("tile")), nil
	})
	e := engine(t, catalog(&memoryFeatures{}, backend), capacity)
	release := make(chan struct{})
	start := make(chan struct{})
	plans := make(chan *response.Plan, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0"})
			if err != nil {
				t.Error(err)
				plans <- nil
				return
			}
			plans <- plan
			<-release
			n, err := io.Copy(io.Discard, plan.Body)
			if err := plan.Finish(n, err); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	accepted, rejected := 0, 0
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for i := 0; i < callers; i++ {
		select {
		case plan := <-plans:
			if plan == nil {
				continue
			}
			switch plan.Status {
			case 200:
				accepted++
			case 503:
				rejected++
			default:
				t.Errorf("unexpected response %d", plan.Status)
			}
		case <-timer.C:
			close(release)
			wg.Wait()
			t.Fatal("admission queued callers instead of rejecting overload")
		}
	}
	close(release)
	wg.Wait()
	if accepted != min(capacity, callers) || rejected != callers-min(capacity, callers) || backendCalls.Load() != int64(min(capacity, callers)) {
		t.Fatalf("accepted=%d rejected=%d backendCalls=%d", accepted, rejected, backendCalls.Load())
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if status != 200 {
		t.Fatal("response slots were not returned")
	}
	t.Logf("%d simultaneous callers: %d retained responses, %d immediate overload rejections; slots recovered", callers, accepted, rejected)
}

func TestScopedCatalogCannotCrossAuthorizationBoundary(t *testing.T) {
	snapshot, err := api.NewSnapshot(catalog(&memoryFeatures{}, tileReader("private")))
	if err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{Catalog: func(ctx context.Context, scope string) (*api.Snapshot, error) {
		if scope != "authorized" {
			return nil, common.ErrForbidden
		}
		return api.Static(snapshot)(ctx, scope)
	}})
	if err != nil {
		t.Fatal(err)
	}
	status, _, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads", Scope: "authorized"})
	if status != 200 || headers["Cache-Control"] != "private, no-cache" {
		t.Fatal(status, headers)
	}
	status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads", Scope: "other", IfNoneMatch: headers["ETag"]})
	if status != 403 {
		t.Fatal(status, string(data))
	}
}

func TestInFlightResponseKeepsOriginalBackend(t *testing.T) {
	first, err := api.NewSnapshot(catalog(&memoryFeatures{}, tileReader("old")))
	if err != nil {
		t.Fatal(err)
	}
	second, err := api.NewSnapshot(catalog(&memoryFeatures{}, tileReader("new")))
	if err != nil {
		t.Fatal(err)
	}
	var publication api.AtomicCatalog
	if err := publication.Publish(first); err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{Catalog: publication.Load})
	if err != nil {
		t.Fatal(err)
	}
	input := api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0"}
	retained, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := publication.Publish(second); err != nil {
		t.Fatal(err)
	}
	status, data, _ := read(t, e, input)
	if status != 200 || string(data) != "new" {
		t.Fatal(status, string(data))
	}
	old, err := io.ReadAll(retained.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := retained.Finish(int64(len(old)), nil); err != nil {
		t.Fatal(err)
	}
	if string(old) != "old" {
		t.Fatal("in-flight response switched backends")
	}
}
