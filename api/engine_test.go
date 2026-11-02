package api_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
)

func TestReadRevalidationDoesNotRunAfterProcessExecution(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Processes = []processes.Service{echoProcess()}
	snapshot, err := api.NewSnapshot(c)
	if err != nil {
		t.Fatal(err)
	}
	var checks int
	e, err := api.New(api.Config{Catalog: api.Static(snapshot), BeforeResponse: func(context.Context, api.Input) error { checks++; return common.ErrForbidden }})
	if err != nil {
		t.Fatal(err)
	}
	status, data, _ := read(t, e, api.Input{Method: "POST", EscapedPath: "/ogc/processes/echo/execution", ContentType: common.JSON, Body: strings.NewReader(`{"inputs":{"message":"hello"}}`)})
	if status != 200 || checks != 0 || !strings.Contains(string(data), "hello") {
		t.Fatal(status, checks, string(data))
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/processes/echo"})
	if status != 403 || checks != 1 {
		t.Fatal(status, checks)
	}
}

func TestDirectoryPercentAliasesCannotBypassLazyMetadata(t *testing.T) {
	var calls atomic.Int64
	var denied atomic.Bool
	provider := directoryProvider{
		list: func(context.Context, string, api.CollectionQuery) (api.CollectionPage, error) {
			calls.Add(1)
			return api.CollectionPage{Collections: []common.Collection{{ID: "a", Title: "lazy-current"}}}, nil
		},
		get: func(context.Context, string, string) (common.Collection, error) {
			calls.Add(1)
			if denied.Load() {
				return common.Collection{}, common.ErrForbidden
			}
			return common.Collection{ID: "a", Title: "lazy-current"}, nil
		},
	}
	c := directoryCatalog(provider, "a", "b")
	c.MetadataCacheControl = "public, max-age=3600"
	c.Collections[0].Metadata.Title = "STATIC-PLACEHOLDER"
	e := engine(t, c, 1)
	for _, path := range []string{"/ogc/collections", "/ogc/%63ollections", "/ogc/collect%69ons"} {
		before := calls.Load()
		status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: path, RawQuery: "limit=1&offset=0"})
		if status != 200 || calls.Load() != before+1 || !strings.Contains(string(data), "lazy-current") || strings.Contains(string(data), "STATIC-PLACEHOLDER") || headers["Cache-Control"] != "private, no-cache" {
			t.Fatal("directory alias missed page provider", path, status, string(data), headers)
		}
	}
	for _, path := range []string{"/ogc/collections/a", "/ogc/%63ollections/a", "/ogc/collect%69ons/%61"} {
		denied.Store(false)
		input := api.Input{Method: "GET", EscapedPath: path}
		before := calls.Load()
		status, data, headers := read(t, e, input)
		if status != 200 || calls.Load() != before+1 || !strings.Contains(string(data), "lazy-current") || strings.Contains(string(data), "STATIC-PLACEHOLDER") || headers["Cache-Control"] != "private, no-cache" {
			t.Fatal("description alias missed lazy provider", path, status, string(data), headers)
		}
		input.IfNoneMatch = headers["ETag"]
		status, data, _ = read(t, e, input)
		if status != 304 || len(data) != 0 || calls.Load() != before+2 {
			t.Fatal("alias conditional skipped visibility lookup", path, status)
		}
		denied.Store(true)
		status, data, _ = read(t, e, input)
		if status != 403 || calls.Load() != before+3 || strings.Contains(string(data), "STATIC-PLACEHOLDER") {
			t.Fatal("alias conditional bypassed provider rejection", path, status, string(data))
		}
	}
	before := calls.Load()
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/%63ollections/private"})
	if status != 404 || calls.Load() != before {
		t.Fatal("unknown alias reached provider", status, calls.Load())
	}
}

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

func TestBeforeResponseRejectsStaleSuccess(t *testing.T) {
	for _, state := range []response.State{response.Content, response.NotModified, response.NoContent} {
		for _, failure := range []struct {
			name   string
			err    error
			status int
		}{
			{"revoked", common.ErrForbidden, 403}, {"stopped", common.ErrUnavailable, 503}, {"version_changed", common.ErrConflict, 409},
		} {
			t.Run(fmt.Sprintf("%d_%s", state, failure.name), func(t *testing.T) {
				closed, calls, checks := 0, 0, 0
				c := catalog(&memoryFeatures{}, tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
					calls++
					p := response.Payload{State: state}
					if state == response.Content {
						p = response.Stream("application/vnd.mapbox-vector-tile", &closeCountingBody{Reader: strings.NewReader("tile"), closed: &closed}, 4)
					}
					p.Cache.ETag = `"old"`
					return p, nil
				}))
				c.Collections[0].Tiles[0].AllowNoContent = true
				snapshot, err := api.NewSnapshot(c)
				if err != nil {
					t.Fatal(err)
				}
				e, err := api.New(api.Config{Catalog: api.Static(snapshot), MaxConcurrent: 1, BeforeResponse: func(_ context.Context, input api.Input) error {
					checks++
					if calls == 0 || closed != 0 || input.Scope != "caller" {
						t.Fatal("revalidation ran out of order", calls, closed, input)
					}
					return failure.err
				}})
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0", IfNoneMatch: `"old"`, Scope: "caller"})
					if status != failure.status || checks != i+1 || calls != i+1 {
						t.Fatal("stale success escaped", status, checks, calls)
					}
					if state == response.Content && closed != 1 {
						t.Fatal("discarded body leaked", closed)
					}
					closed = 0
				}
			})
		}
	}
}

func TestBeforeResponsePanicAndCancellationCleanup(t *testing.T) {
	for _, mode := range []string{"panic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			closed := 0
			c := catalog(&memoryFeatures{}, tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
				return response.Stream("application/vnd.mapbox-vector-tile", &closeCountingBody{Reader: strings.NewReader("tile"), closed: &closed}, 4), nil
			}))
			snapshot, err := api.NewSnapshot(c)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := true
			e, err := api.New(api.Config{Catalog: api.Static(snapshot), MaxConcurrent: 1, BeforeResponse: func(context.Context, api.Input) error {
				if first {
					first = false
					if mode == "panic" {
						panic("recheck")
					}
					cancel()
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			func() {
				defer func() {
					if recovered := recover(); (mode == "panic") != (recovered != nil) {
						t.Error("unexpected panic result", recovered)
					}
				}()
				plan, err := e.Handle(ctx, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0", IfNoneMatch: "*"})
				if err != nil {
					t.Fatal(err)
				}
				if plan.Status == 304 || plan.Status == 200 {
					t.Error("cancelled success escaped", plan.Status)
				}
				_ = plan.Finish(0, nil)
			}()
			if closed != 1 {
				t.Fatal("recheck leaked stream", closed)
			}
			status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
			if status != 200 {
				t.Fatal("recheck leaked admission", status)
			}
		})
	}
}

func TestAcceptEncodingHeaderBudget(t *testing.T) {
	snapshot, err := api.NewSnapshot(catalog(&memoryFeatures{}, tileReader("tile")))
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	e, err := api.New(api.Config{Catalog: api.Static(snapshot), Limits: api.Limits{MaxHeaderBytes: 8}, BeforeResponse: func(context.Context, api.Input) error { checks++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0", AcceptEncoding: "gzip, deflate"})
	if status != 431 || checks != 0 {
		t.Fatal(status, checks)
	}
}

func TestConditionalDiscardFailureCannotReturnNotModified(t *testing.T) {
	for _, failure := range []string{"cancel", "close_error"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			closed := 0
			c := catalog(&memoryFeatures{}, tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
				return response.Stream("application/vnd.mapbox-vector-tile", &conditionalCloseBody{Reader: strings.NewReader("tile"), close: func() error {
					closed++
					if failure == "cancel" {
						cancel()
						return nil
					}
					return errors.New("private-storage-detail")
				}}, 4), nil
			}))
			e := engine(t, c, 1)
			plan, err := e.Handle(ctx, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0", IfNoneMatch: "*"})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Status < 400 {
				t.Fatal("discard failure escaped as success", plan.Status)
			}
			if failure == "close_error" {
				data, err := io.ReadAll(plan.Body)
				if err != nil || strings.Contains(string(data), "private-storage-detail") {
					t.Fatal(err, string(data))
				}
			}
			_ = plan.Finish(0, nil)
			_ = plan.Finish(0, nil)
			if closed != 1 {
				t.Fatal("discarded body not closed exactly once", closed)
			}
			status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
			if status != 200 {
				t.Fatal("discard failure leaked admission", status)
			}
		})
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

// conditionalCloseBody lets a conditional discard exercise backend close failures.
type conditionalCloseBody struct {
	io.Reader
	close func() error
}

func (b *conditionalCloseBody) Close() error { return b.close() }
