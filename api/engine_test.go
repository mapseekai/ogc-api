package api_test

import (
	"context"
	"errors"
	"fmt"
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

func TestWildcardWithoutETag(t *testing.T) {
	e := engine(t, catalog(&memoryFeatures{}, tileReader("tile")), 1)
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			plan, err := e.Handle(context.Background(), api.Input{Method: method, EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0", IfNoneMatch: "*"})
			if err != nil {
				t.Fatal(err)
			}
			_ = plan.Finish(0, nil)
			if plan.Status != 304 {
				t.Errorf("%s existing representation without ETag returned %d, want 304", method, plan.Status)
			}

		})
	}
}

func TestWildcardConditionalPreservesAuthorizationAndCleanup(t *testing.T) {
	snapshot, err := api.NewSnapshot(catalog(&memoryFeatures{}, tileReader("tile")))
	if err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{MaxConcurrent: 1, Catalog: func(_ context.Context, scope string) (*api.Snapshot, error) {
		if scope != "allowed" {
			return nil, common.ErrForbidden
		}
		return snapshot, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		scope, path, accept string
		status              int
	}{
		{"denied", "/ogc/collections/roads/tiles/local/L00/0/0", "", 403},
		{"allowed", "/ogc/collections/missing/items", "", 404},
		{"allowed", "/ogc/collections/roads/items", "text/html", 406},
		{"allowed", "/ogc/collections/roads/tiles/local/L00/0/0", "", 304},
	} {
		t.Run(fmt.Sprintf("%s_%d", test.scope, test.status), func(t *testing.T) {
			status, data, _ := read(t, e, api.Input{Method: "GET", Scope: test.scope, EscapedPath: test.path, Accept: test.accept, IfNoneMatch: "*"})
			if status != test.status {
				t.Fatalf("conditional changed result: %d want %d: %s", status, test.status, data)
			}

		})
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", Scope: "allowed", EscapedPath: "/ogc/"})
	if status != 200 {
		t.Fatal("conditional response leaked quota")
	}
}

func TestOpaqueETagMatching(t *testing.T) {
	for _, test := range []struct {
		request, etag string
		match         bool
	}{
		{`W/"a,b"`, `"a,b"`, true}, {`"other", W/"a,b"`, `"a,b"`, true},
		{`"other"`, `"a,b"`, false}, {`"a,b"garbage`, `"a,b"`, false}, {`*`, `"a,b"`, true},
	} {
		t.Run(test.request, func(t *testing.T) {
			reader := tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
				payload := response.Bytes("application/vnd.mapbox-vector-tile", []byte("tile"))
				payload.Cache.ETag = test.etag
				return payload, nil
			})
			e := engine(t, catalog(&memoryFeatures{}, reader), 1)
			status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0", IfNoneMatch: test.request})
			want := 200
			if test.match {
				want = 304
			}
			if status != want || headers["ETag"] != test.etag {
				t.Fatalf("status=%d ETag=%q, want status=%d ETag=%q", status, headers["ETag"], want, test.etag)
			}
			if test.match && len(data) != 0 || !test.match && string(data) != "tile" {
				t.Fatalf("unexpected conditional body %q", data)
			}
		})
	}
}

func TestReadContracts(t *testing.T) {
	p := &memoryFeatures{}
	e := engine(t, catalog(p, tileReader("tile")), 8)
	tests := []struct {
		path, query, accept string
		status              int
	}{
		{"/ogc/", "", "", 200}, {"/ogc/conformance", "", "", 200}, {"/ogc/api", "", "application/json", 200},
		{"/ogc/collections", "", "", 200}, {"/ogc/collections/roads", "", "", 200},
		{"/ogc/collections/roads/items", "bbox=170,-10,-170,10&limit=2", "application/geo+json", 200},
		{"/ogc/collections/roads/items/one", "", "", 200},
		{"/ogc/tileMatrixSets", "", "", 200}, {"/ogc/tileMatrixSets/local", "", "", 200},
		{"/ogc/collections/roads/tiles", "", "", 200}, {"/ogc/collections/roads/tiles/local", "", "", 200},
		{"/ogc/collections/roads/tiles/local/L00/0/1", "", "", 200},
		{"/ogc/collections/roads/tiles/local/L00/1/0", "", "", 404},
		{"/ogc/collections/roads/tiles/local/0/0/0", "", "", 404},
		{"/ogc/collections/roads/items", "limit=1&limit=2", "", 400},
		{"/ogc/collections/roads/items", "filter=ignored", "", 400},
		{"/ogc/collections/roads/items", "limit=1001", "", 200},
		{"/ogc/collections/roads/items", "", "text/html", 406},
		{"/ogc/collections/roads/items", "f=", "", 400},
		{"/ogc/collections/missing/items", "", "", 404},
	}
	for _, tt := range tests {
		t.Run(tt.path+"?"+tt.query, func(t *testing.T) {
			status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: tt.path, RawQuery: tt.query, Accept: tt.accept})
			if status != tt.status {
				t.Fatalf("got %d %s, want %d", status, data, tt.status)
			}
		})
	}
	if p.closed.Load() != 2 {
		t.Fatalf("cursor close count %d", p.closed.Load())
	}
}

func TestStreamAdmissionAndCancellation(t *testing.T) {
	p := &memoryFeatures{}
	e := engine(t, catalog(p, tileReader("tile")), 1)
	ctx, cancel := context.WithCancel(context.Background())
	plan, err := e.Handle(ctx, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items"})
	if err != nil {
		t.Fatal(err)
	}
	status, _, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if status != 503 || headers["Retry-After"] != "1" {
		t.Fatal(status, headers)
	}
	cancel()
	if _, err := io.ReadAll(plan.Body); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := plan.Finish(0, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := plan.Finish(0, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if p.closed.Load() != 1 {
		t.Fatal("cursor not closed exactly once")
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if status != 200 {
		t.Fatal("admission slot leaked")
	}
}

func TestObserverPanicReleasesSlot(t *testing.T) {
	p := &memoryFeatures{}
	snapshot, err := api.NewSnapshot(catalog(p, tileReader("tile")))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	e, err := api.New(api.Config{Catalog: api.Static(snapshot), MaxConcurrent: 1, Observer: func(event api.Event) {
		if event.Stage == "handoff" && calls.Add(1) == 1 {
			panic("observer")
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic")
			}
		}()
		_, _ = e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items"})
	}()
	if p.closed.Load() != 1 {
		t.Fatal("panic leaked cursor")
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if status != 200 {
		t.Fatal("panic leaked slot")
	}
}

func TestTwoThousandCallersRespectAdmission(t *testing.T) {
	stressAdmission(t, 64)
}

func TestTwoThousandSuccessfulConcurrentCallers(t *testing.T) {
	stressAdmission(t, 2048)
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

func BenchmarkMetadataParallel(b *testing.B) {
	e := engine(b, catalog(&memoryFeatures{}, tileReader("tile")), 1024)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads"})
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

func engine(t testing.TB, c api.Catalog, max int) *api.Engine {
	t.Helper()
	snapshot, err := api.NewSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{Catalog: api.Static(snapshot), MaxConcurrent: max})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func read(t testing.TB, e *api.Engine, input api.Input) (int, []byte, map[string]string) {
	t.Helper()
	plan, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(plan.Body)
	finish := plan.Finish(int64(len(data)), err)
	if err != nil || finish != nil {
		t.Fatal(err, finish)
	}
	return plan.Status, data, plan.Headers
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
