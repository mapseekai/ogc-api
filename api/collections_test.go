package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/response"
)

func TestStaticCollectionPaginationAndLegacyPolicy(t *testing.T) {
	catalog := directoryCatalog(nil, "a", "b", "c", "d")
	e := engine(t, catalog, 8)
	for _, tc := range []struct {
		query, next, previous string
		ids                   []string
	}{
		{"", "2", "", []string{"a", "b"}},
		{"offset=0&limit=2&f=json", "2", "", []string{"a", "b"}},
		{"offset=2&limit=2", "", "0", []string{"c", "d"}},
		{"offset=4&limit=2", "", "2", []string{}},
		{"offset=18446744073709551615&limit=2", "", "18446744073709551613", []string{}},
	} {
		status, data, headers := read(t, e, api.Input{Method: "GET", Scope: "scope-a", EscapedPath: "/ogc/collections", RawQuery: tc.query})
		var page directoryDocument
		if status != 200 || json.Unmarshal(data, &page) != nil || !reflect.DeepEqual(directoryIDs(page), tc.ids) || page.NumberMatched == nil || *page.NumberMatched != 4 || page.NumberReturned != uint64(len(tc.ids)) {
			t.Fatal(tc.query, status, string(data))
		}
		if headers["Cache-Control"] != "private, no-cache" || headers["ETag"] == "" {
			t.Fatal("unscoped page cache", headers)
		}
		assertDirectoryLinks(t, page, tc.next, tc.previous)
	}
	for _, query := range []string{"offset=-1", "offset=+1", "offset=18446744073709551616", "offset=", "offset=1&offset=1", "limit=0", "limit=5", "limit=-1", "limit=18446744073709551616", "limit=1&limit=2", "cursor=x", "filter=true", "token=long-lived-credential"} {
		status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections", RawQuery: query})
		if status != 400 || strings.Contains(string(data), "long-lived-credential") {
			t.Fatal("invalid/credential parameter accepted or reflected", query, status, string(data))
		}
	}
	empty := engine(t, directoryCatalog(nil), 1)
	status, data, _ := read(t, empty, api.Input{Method: "GET", EscapedPath: "/ogc/collections"})
	if status != 200 || !strings.Contains(string(data), `"collections":[]`) || strings.Contains(string(data), `"rel":"next"`) {
		t.Fatal("empty directory has a false continuation", status, string(data))
	}
	catalog.CollectionPaging = nil
	legacy := engine(t, catalog, 1)
	for _, query := range []string{"offset=0", "limit=2"} {
		status, _, _ := read(t, legacy, api.Input{Method: "GET", EscapedPath: "/ogc/collections", RawQuery: query})
		if status != 400 {
			t.Fatal("directory pagination enabled implicitly", query, status)
		}
	}
	status, data, _ = read(t, legacy, api.Input{Method: "GET", EscapedPath: "/ogc/collections"})
	if status != 200 || strings.Contains(string(data), "numberReturned") {
		t.Fatal("legacy representation changed", status, string(data))
	}
}

func TestPagedMetadataProviderScopeRoutingAndEmptyProgress(t *testing.T) {
	var listCalls, getCalls, catalogCalls atomic.Int64
	provider := directoryProvider{
		list: func(ctx context.Context, scope string, query api.CollectionQuery) (api.CollectionPage, error) {
			listCalls.Add(1)
			if err := ctx.Err(); err != nil {
				return api.CollectionPage{}, err
			}
			// A public scan position, not a hidden-row total or credential. This
			// first permission-filtered page is empty but has a real continuation.
			next := query.Offset + 1
			if query.Offset == 0 {
				return api.CollectionPage{NextOffset: &next}, nil
			}
			id := "b"
			if scope == "a" {
				id = "a"
				if query.Offset > 1 {
					id = "c"
				}
			}
			page := api.CollectionPage{Collections: []common.Collection{{ID: id, Title: scope + "-" + id}}}
			if scope == "a" && query.Offset == 1 {
				page.NextOffset = &next
			}
			return page, nil
		},
		get: func(_ context.Context, scope, id string) (common.Collection, error) {
			getCalls.Add(1)
			return common.Collection{ID: id, Title: scope + "-" + id}, nil
		},
	}
	a, err := api.NewSnapshot(directoryCatalog(provider, "a", "c"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := api.NewSnapshot(directoryCatalog(provider, "b"))
	if err != nil {
		t.Fatal(err)
	}
	var revoked atomic.Bool
	e, err := api.New(api.Config{Catalog: func(_ context.Context, scope string) (*api.Snapshot, error) {
		catalogCalls.Add(1)
		if revoked.Load() {
			return nil, common.ErrForbidden
		}
		if scope == "a" {
			return a, nil
		}
		return b, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	position := "0"
	got := []string{}
	for requests := 0; ; requests++ {
		if requests >= 3 {
			t.Fatal("permission-filtered page failed to advance/terminate")
		}
		status, data, _ := read(t, e, api.Input{Method: "GET", Scope: "a", EscapedPath: "/ogc/collections", RawQuery: "limit=1&offset=" + position + "&f=json"})
		var page directoryDocument
		if status != 200 || json.Unmarshal(data, &page) != nil || page.NumberMatched != nil || strings.Contains(string(data), "b-b") {
			t.Fatal(status, string(data))
		}
		got = append(got, directoryIDs(page)...)
		next := ""
		for _, link := range page.Links {
			parsed, err := url.Parse(link.Href)
			if err != nil || parsed.Query().Get("f") != "json" || parsed.Query().Get("limit") != "1" || parsed.Query().Get("scope") != "" {
				t.Fatal("lost page selectors or exposed scope", link, err)
			}
			if link.Rel == "next" {
				next = parsed.Query().Get("offset")
				if next <= position {
					t.Fatal("non-advancing continuation", link)
				}
			}
		}
		if next == "" {
			break
		}
		position = next
	}
	if !reflect.DeepEqual(got, []string{"a", "c"}) || listCalls.Load() != 3 || getCalls.Load() != 0 || catalogCalls.Load() != 3 {
		t.Fatal("page request performed full/extra lookups or lost visibility", got, listCalls.Load(), getCalls.Load(), catalogCalls.Load())
	}
	// Routing membership is not the contents of the first (empty) discovery page.
	status, data, _ := read(t, e, api.Input{Method: "GET", Scope: "a", EscapedPath: "/ogc/collections/c"})
	if status != 200 || !strings.Contains(string(data), `"title":"a-c"`) || getCalls.Load() != 1 || listCalls.Load() != 3 {
		t.Fatal("off-page authorized collection required listing all metadata", status, string(data))
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", Scope: "a", EscapedPath: "/ogc/collections/c/items"})
	if status != 200 {
		t.Fatal("off-page feature route disappeared", status)
	}
	for _, input := range []api.Input{{Method: "GET", Scope: "a", EscapedPath: "/ogc/collections/b"}, {Method: "GET", Scope: "b", EscapedPath: "/ogc/collections/a"}, {Method: "GET", Scope: "b", EscapedPath: "/ogc/collections/a/items"}} {
		status, _, _ := read(t, e, input)
		if status != 404 || getCalls.Load() != 1 {
			t.Fatal("cross-scope route bypassed snapshot authorization", input, status, getCalls.Load())
		}
	}
	first := api.Input{Method: "GET", Scope: "a", EscapedPath: "/ogc/collections", RawQuery: "offset=0&limit=1"}
	_, _, headers := read(t, e, first)
	first.IfNoneMatch = headers["ETag"]
	before := listCalls.Load()
	status, data, _ = read(t, e, first)
	if status != 304 || len(data) != 0 || listCalls.Load() != before+1 {
		t.Fatal("conditional page skipped provider visibility", status, len(data), listCalls.Load())
	}
	first.Scope = "b" // The empty wire document is otherwise exactly the same.
	status, _, headersB := read(t, e, first)
	if status != 200 || headersB["ETag"] == headers["ETag"] {
		t.Fatal("scope reused another tenant's page ETag", status, headersB)
	}
	first.Scope = "a"
	first.RawQuery = "offset=00&limit=1" // Same body, distinct accepted query.
	status, _, headersB = read(t, e, first)
	if status != 200 || headersB["ETag"] == headers["ETag"] {
		t.Fatal("query conditions reused a page ETag", status, headersB)
	}
	first.RawQuery = "offset=0&limit=1"
	before = listCalls.Load()
	revoked.Store(true)
	status, _, _ = read(t, e, first)
	if status != 403 || listCalls.Load() != before {
		t.Fatal("If-None-Match bypassed current authorization", status)
	}
}

func TestDirectoryProviderContractErrorsAndCancellation(t *testing.T) {
	zero, one, two, four := uint64(0), uint64(1), uint64(2), uint64(4)
	for name, page := range map[string]api.CollectionPage{
		"nonadvancing": {NextOffset: &zero},
		"backwards":    {PreviousOffset: &zero},
		"too-many":     {Collections: []common.Collection{{ID: "a"}, {ID: "b"}, {ID: "c"}}},
		"duplicate":    {Collections: []common.Collection{{ID: "a"}, {ID: "a"}}},
		"unpublished":  {Collections: []common.Collection{{ID: "private-b", Title: "DO-NOT-DISCLOSE"}}},
		"bad-total":    {Collections: []common.Collection{{ID: "a"}}, NumberMatched: &zero},
		"hidden-total": {NumberMatched: &four},
	} {
		t.Run(name, func(t *testing.T) {
			provider := directoryProvider{list: func(context.Context, string, api.CollectionQuery) (api.CollectionPage, error) { return page, nil }}
			e := engine(t, directoryCatalog(provider, "a", "b", "c"), 1)
			status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections", RawQuery: "limit=2"})
			if status != 500 || strings.Contains(string(data), "DO-NOT-DISCLOSE") || strings.Contains(string(data), "private-b") {
				t.Fatal("invalid provider page escaped validation", status, string(data))
			}
		})
	}
	provider := directoryProvider{
		list: func(ctx context.Context, _ string, q api.CollectionQuery) (api.CollectionPage, error) {
			if err := ctx.Err(); err != nil {
				return api.CollectionPage{}, err
			}
			return api.CollectionPage{NextOffset: &two, PreviousOffset: &zero, NumberMatched: &one}, nil
		},
		get: func(context.Context, string, string) (common.Collection, error) {
			return common.Collection{ID: "b"}, nil
		},
	}
	e := engine(t, directoryCatalog(provider, "a"), 1)
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/a"})
	if status != 500 {
		t.Fatal("mismatched metadata lookup accepted", status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, err := e.Handle(ctx, api.Input{Method: "GET", EscapedPath: "/ogc/collections"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Finish(0, nil)
	if plan.Status != 408 || !errors.Is(plan.ProtocolError, context.Canceled) {
		t.Fatal("cancellation lost", plan.Status, plan.ProtocolError)
	}
}

func TestInvalidProviderExtentIsInternalNotClientError(t *testing.T) {
	metadata := common.Collection{ID: "a", Title: "PRIVATE-METADATA", Extent: &common.Extent{Spatial: &common.SpatialExtent{BBox: [][]float64{{0, 100, 1, 101}}}}}
	provider := directoryProvider{
		list: func(context.Context, string, api.CollectionQuery) (api.CollectionPage, error) {
			return api.CollectionPage{Collections: []common.Collection{metadata}}, nil
		},
		get: func(context.Context, string, string) (common.Collection, error) { return metadata, nil },
	}
	e := engine(t, directoryCatalog(provider, "a"), 1)
	for _, path := range []string{"/ogc/collections", "/ogc/collections/a", "/ogc/%63ollections", "/ogc/%63ollections/a"} {
		plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: path})
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(plan.Body)
		finish := plan.Finish(int64(len(data)), err)
		var body common.Problem
		if err != nil || finish != nil || json.Unmarshal(data, &body) != nil || plan.Status != 500 || body.Code != "NoApplicableCode" || body.Detail != "Internal Server Error" || strings.Contains(string(data), "bbox") || strings.Contains(string(data), "PRIVATE-METADATA") {
			t.Fatal("invalid provider extent blamed client or leaked metadata", path, plan.Status, string(data), err, finish)
		}
		outer, ok := plan.ProtocolError.(*common.Error)
		var cause *common.Error
		if !ok || outer.Status != 500 || !errors.As(outer.Cause, &cause) || cause.Status != 400 {
			t.Fatal("internal metadata diagnostic cause lost", plan.ProtocolError)
		}
	}
	// Intentional provider failures retain their normal safe protocol mapping;
	// only validation of successfully returned metadata is an internal error.
	provider.get = func(context.Context, string, string) (common.Collection, error) {
		return common.Collection{}, common.ErrUnavailable
	}
	e = engine(t, directoryCatalog(provider, "a"), 1)
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/%63ollections/a"})
	if status != 503 {
		t.Fatal("provider failure category changed", status)
	}
}

func TestLazyCollectionMetadataHeadersLinksAndBudgets(t *testing.T) {
	provider := directoryProvider{
		list: func(context.Context, string, api.CollectionQuery) (api.CollectionPage, error) {
			return api.CollectionPage{Collections: []common.Collection{{ID: "a"}}}, nil
		},
		get: func(context.Context, string, string) (common.Collection, error) {
			return common.Collection{ID: "a", Title: "lazy", Links: []common.Link{
				{Href: "https://other.invalid/private", Rel: "items"},
				{Href: "https://other.invalid/map", Rel: "http://www.opengis.net/def/rel/ogc/1.0/map"},
				{Href: "https://example.test/license", Rel: "license"},
			}}, nil
		},
	}
	e := engine(t, directoryCatalog(provider, "a"), 1)
	input := api.Input{Method: "GET", Scope: "a", EscapedPath: "/ogc/collections/a", RawQuery: "f=json"}
	status, data, headers := read(t, e, input)
	if status != 200 || strings.Contains(string(data), "other.invalid") || !strings.Contains(string(data), "https://example.test/license") || !strings.Contains(string(data), "/ogc/collections/a/items") || headers["Cache-Control"] != "private, no-cache" {
		t.Fatal("lazy metadata changed authorized capabilities or cache", status, string(data), headers)
	}
	input.Method = "HEAD"
	// The host suppresses HEAD content and must Finish without reading it; the
	// protocol plan retains the GET representation's headers and length.
	head, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	finish := head.Finish(0, nil)
	if head.Status != 200 || head.Headers["ETag"] != headers["ETag"] || head.Headers["Content-Length"] != headers["Content-Length"] || finish != nil {
		t.Fatal("lazy metadata HEAD differs from GET", head.Status, head.Headers, finish)
	}
	input.Method = "GET"
	input.IfNoneMatch = headers["ETag"]
	status, data, _ = read(t, e, input)
	if status != 304 || len(data) != 0 {
		t.Fatal("lazy description conditional failed", status, string(data))
	}
	provider.get = func(context.Context, string, string) (common.Collection, error) {
		return common.Collection{ID: "a", Title: strings.Repeat("x", 4096)}, nil
	}
	limited, err := api.New(api.Config{Catalog: api.Static(mustDirectorySnapshot(t, directoryCatalog(provider, "a"))), Limits: api.Limits{MaxDocumentBytes: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	status, data, _ = read(t, limited, api.Input{Method: "GET", EscapedPath: "/ogc/collections/a"})
	if status != 500 || strings.Contains(string(data), strings.Repeat("x", 50)) {
		t.Fatal("lazy metadata escaped document budget", status, string(data))
	}
}

func TestDirectoryLinkQueryBudgets(t *testing.T) {
	maximum := ^uint64(0)
	provider := directoryProvider{list: func(context.Context, string, api.CollectionQuery) (api.CollectionPage, error) {
		return api.CollectionPage{NextOffset: &maximum}, nil
	}}
	for _, tc := range []struct {
		budget int
		query  string
	}{{15, ""}, {20, "limit=1&offset=0"}} {
		snapshot := mustDirectorySnapshot(t, directoryCatalog(provider, "a"))
		e, err := api.New(api.Config{Catalog: api.Static(snapshot), Limits: api.Limits{MaxQueryBytes: tc.budget}})
		if err != nil {
			t.Fatal(err)
		}
		status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections", RawQuery: tc.query})
		if status != 500 || strings.Contains(string(data), `"rel":"next"`) {
			t.Fatal("unfollowable directory link returned", tc, status, string(data))
		}
	}
}

func TestDirectoryRequestRetainsPublishedViewAcrossReplacement(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	oldProvider := directoryProvider{list: func(ctx context.Context, _ string, _ api.CollectionQuery) (api.CollectionPage, error) {
		close(entered)
		select {
		case <-release:
			return api.CollectionPage{Collections: []common.Collection{{ID: "a", Title: "old-view"}}}, nil
		case <-ctx.Done():
			return api.CollectionPage{}, ctx.Err()
		}
	}}
	old, err := api.NewSnapshot(directoryCatalog(oldProvider, "a"))
	if err != nil {
		t.Fatal(err)
	}
	newProvider := directoryProvider{list: func(context.Context, string, api.CollectionQuery) (api.CollectionPage, error) {
		return api.CollectionPage{Collections: []common.Collection{{ID: "a", Title: "new-view"}}}, nil
	}}
	next, err := api.NewSnapshot(directoryCatalog(newProvider, "a"))
	if err != nil {
		t.Fatal(err)
	}
	var published api.AtomicCatalog
	if err := published.Publish(old); err != nil {
		t.Fatal(err)
	}
	e, err := api.New(api.Config{Catalog: published.Load})
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		plan *response.Plan
		err  error
	}
	result := make(chan outcome, 1)
	go func() {
		plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections"})
		result <- outcome{plan, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("provider did not start")
	}
	if err := published.Publish(next); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	select {
	case first := <-result:
		if first.err != nil {
			t.Fatal(first.err)
		}
		data, err := io.ReadAll(first.plan.Body)
		finish := first.plan.Finish(int64(len(data)), err)
		if err != nil || finish != nil || !strings.Contains(string(data), "old-view") || strings.Contains(string(data), "new-view") {
			t.Fatal("one request mixed publication views", string(data), err, finish)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not finish")
	}
	status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections"})
	if status != 200 || !strings.Contains(string(data), "new-view") {
		t.Fatal("new request did not use new publication", status, string(data))
	}
}

type directoryDocument struct {
	Collections    []common.Collection
	Links          []common.Link
	NumberMatched  *uint64
	NumberReturned uint64
}

type directoryProvider struct {
	list func(context.Context, string, api.CollectionQuery) (api.CollectionPage, error)
	get  func(context.Context, string, string) (common.Collection, error)
}

func (p directoryProvider) Collections(ctx context.Context, scope string, q api.CollectionQuery) (api.CollectionPage, error) {
	return p.list(ctx, scope, q)
}

func (p directoryProvider) Collection(ctx context.Context, scope, id string) (common.Collection, error) {
	if p.get == nil {
		return common.Collection{}, common.ErrNotFound
	}
	return p.get(ctx, scope, id)
}

func directoryCatalog(provider api.CollectionMetadataProvider, ids ...string) api.Catalog {
	catalog := api.Catalog{Title: "Paged directory", BaseURL: "https://example.test/ogc", CollectionPaging: &api.CollectionPaging{DefaultLimit: 2, MaxLimit: 4, Provider: provider}}
	for _, id := range ids {
		catalog.Collections = append(catalog.Collections, api.Collection{Metadata: common.Collection{ID: id}, Features: &features.Service{Reader: &memoryFeatures{}}})
	}
	return catalog
}

func mustDirectorySnapshot(t *testing.T, catalog api.Catalog) *api.Snapshot {
	t.Helper()
	snapshot, err := api.NewSnapshot(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func directoryIDs(page directoryDocument) []string {
	ids := make([]string, 0, len(page.Collections))
	for _, c := range page.Collections {
		ids = append(ids, c.ID)
	}
	return ids
}

func assertDirectoryLinks(t *testing.T, page directoryDocument, next, previous string) {
	t.Helper()
	positions := map[string]string{}
	for _, link := range page.Links {
		u, err := url.Parse(link.Href)
		if err != nil || u.User != nil || u.Query().Get("limit") != "2" {
			t.Fatal("invalid directory link", link, err)
		}
		positions[link.Rel] = u.Query().Get("offset")
	}
	if positions["next"] != next || positions["prev"] != previous {
		t.Fatal("incorrect continuations", positions, next, previous)
	}
	if _, ok := positions["self"]; !ok {
		t.Fatal("missing self link")
	}
}
