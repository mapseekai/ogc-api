package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
)

func TestSharedRendererNotModifiedAcrossWMSAndAPIMaps(t *testing.T) {
	calls := 0
	renderer := maps.RendererFunc(func(_ context.Context, request maps.Request) (maps.Result, error) {
		calls++
		if request.Conditions.IfNoneMatch != `W/"v1"` || request.Conditions.AcceptEncoding != "gzip" {
			t.Fatal(request.Conditions)
		}
		return maps.Result{Content: response.Payload{State: response.NotModified, ContentEncoding: "gzip", Vary: "Authorization", Cache: response.Cache{ETag: `"v1"`, CacheControl: "private"}}}, nil
	})
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Collections[0].Map = &maps.Service{Renderer: renderer, Formats: []string{"image/png"}}
	c.Map = c.Collections[0].Map
	e := engine(t, c, 1)
	for _, path := range []string{"/ogc/map", "/ogc/collections/roads/map"} {
		status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: path, IfNoneMatch: `W/"v1"`, AcceptEncoding: "gzip"})
		if status != 304 || len(data) != 0 || headers["Content-Length"] != "" || headers["Content-Bbox"] != "" || headers["Content-Crs"] != "" || headers["Content-Encoding"] != "gzip" || headers["ETag"] != `"v1"` || !strings.Contains(headers["Vary"], "Authorization") || !strings.Contains(headers["Vary"], "Accept-Encoding") {
			t.Fatal(status, string(data), headers)
		}
	}
	config, err := demo.Config("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	config.Handlers.GetMap = maps.WMS(renderer, config.Limits)
	legacy, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := legacy.Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMS, RawQuery: adaptertest.MapQuery("1.3.0"), IfNoneMatch: `W/"v1"`, AcceptEncoding: "gzip"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(plan.Body)
	finish := plan.Finish(int64(len(data)), err)
	if plan.Status != 304 || err != nil || finish != nil || len(data) != 0 || calls != 3 {
		t.Fatal(plan.Status, err, finish, calls)
	}
}

func TestAPIMapsBodylessPolicyAndRevalidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		state     response.State
		condition string
		reject    bool
		status    int
	}{
		{"unconditional_not_modified", response.NotModified, "", false, 500},
		{"no_content_forbidden", response.NoContent, "*", false, 500},
		{"revoked_not_modified", response.NotModified, "*", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := catalog(&memoryFeatures{}, tileReader("tile"))
			c.Collections[0].Map = &maps.Service{Formats: []string{"image/png"}, Renderer: maps.RendererFunc(func(_ context.Context, request maps.Request) (maps.Result, error) {
				return maps.Result{Content: response.Payload{State: tc.state}, Width: request.Width, Height: request.Height, BBox: request.BBox, CRS: request.CRS}, nil
			})}
			snapshot, err := api.NewSnapshot(c)
			if err != nil {
				t.Fatal(err)
			}
			checks := 0
			e, err := api.New(api.Config{Catalog: api.Static(snapshot), BeforeResponse: func(context.Context, api.Input) error { checks++; return common.ErrForbidden }})
			if err != nil {
				t.Fatal(err)
			}
			status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/map", IfNoneMatch: tc.condition})
			if status != tc.status || (checks == 1) != tc.reject {
				t.Fatal(status, checks)
			}
		})
	}
}

func TestMapAndCoverageEndToEnd(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	var rendered maps.Request
	c.Collections[0].Map = &maps.Service{Formats: []string{"image/png"}, Renderer: maps.RendererFunc(func(_ context.Context, request maps.Request) (maps.Result, error) {
		rendered = request
		return maps.Result{Content: response.Bytes("image/png", []byte("map")), Width: request.Width, Height: request.Height, BBox: request.BBox, CRS: request.CRS}, nil
	})}
	var covered coverages.Request
	c.Collections[0].Coverage = &coverages.Service{Formats: []string{"image/tiff"}, Axes: []string{"x"}, Properties: []string{"temperature"}, Schema: json.RawMessage(`{"type":"object"}`), Reader: coverages.ReaderFunc(func(_ context.Context, request coverages.Request) (response.Payload, error) {
		covered = request
		return response.Bytes("image/tiff", []byte("coverage")), nil
	})}
	e := engine(t, c, 8)
	status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/map", RawQuery: "bbox=0,0,10,10&width=100&height=100", IfNoneMatch: `"other"`, AcceptEncoding: "gzip"})
	if status != 200 || string(data) != "map" || rendered.Layers[0].ID != "roads" || headers["Content-Bbox"] != "0,0,10,10" || rendered.Conditions.IfNoneMatch != `"other"` || rendered.Conditions.AcceptEncoding != "gzip" || !strings.Contains(headers["Vary"], "Accept-Encoding") {
		t.Fatal(status, string(data), headers)
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/coverage", RawQuery: "subset=x(1:2)&properties=temperature"})
	if status != 200 || string(data) != "coverage" || covered.Subsets[0].Axis != "x" || covered.CollectionID != "roads" {
		t.Fatal(status, string(data), covered)
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/schema"})
	if status != 200 || !json.Valid(data) {
		t.Fatal(status, string(data))
	}
	status, data, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/conformance"})
	if status != 200 || strings.Contains(string(data), "coverages") || strings.Contains(string(data), "maps-1") {
		t.Fatal("profile routes must not claim unimplemented conformance", string(data))
	}
}

func TestDatasetMapSelection(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	var rendered maps.Request
	service := &maps.Service{Formats: []string{"image/png"}, Renderer: maps.RendererFunc(func(_ context.Context, request maps.Request) (maps.Result, error) {
		rendered = request
		return maps.Result{Content: response.Bytes(request.Format, []byte("map")), Width: request.Width, Height: request.Height, BBox: request.BBox, CRS: request.CRS}, nil
	})}
	c.Map = service
	c.Collections[0].Map = service
	c.Collections = append(c.Collections, api.Collection{Metadata: common.Collection{ID: "buildings"}, Map: service})
	e := engine(t, c, 1)
	for _, tc := range []struct {
		query  string
		ids    []string
		status int
	}{
		{"", []string{"buildings", "roads"}, 200},
		{"collections=roads,buildings", []string{"roads", "buildings"}, 200},
		{"collections=" + url.QueryEscape("https://example.test/ogc/collections/roads"), []string{"roads"}, 200},
		{"collections=missing", nil, 400}, {"collections=", nil, 400},
		{"collections=roads,roads", nil, 400},
		{"collections=" + url.QueryEscape("https://other.test/collections/roads"), nil, 400},
		{"unknown=1", nil, 400},
	} {
		status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/map", RawQuery: tc.query})
		if status != tc.status {
			t.Fatal(tc.query, status, string(data))
		}
		if status == 200 {
			var ids []string
			for _, layer := range rendered.Layers {
				ids = append(ids, layer.ID)
			}
			if !reflect.DeepEqual(ids, tc.ids) || headers["Content-Bbox"] != "-180,-90,180,90" || headers["Content-Crs"] != "" {
				t.Fatal(tc.query, ids, headers)
			}
		} else if !strings.Contains(string(data), "InvalidParameterValue") {
			t.Fatal(string(data))
		}
	}
	status, _, headers := read(t, e, api.Input{Method: "POST", EscapedPath: "/ogc/map"})
	if status != 405 || headers["Allow"] != "GET, HEAD" {
		t.Fatal(status, headers)
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/m%61p"})
	if status != 200 {
		t.Fatal("encoded dataset map path did not use canonical routing", status)
	}
	status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/map", RawQuery: "collections=buildings"})
	if status != 400 {
		t.Fatal("collection map accepted dataset selection", status)
	}
	for _, path := range []string{"/ogc/", "/ogc/collections/roads"} {
		status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: path})
		var metadata struct {
			CRS    []string
			Extent *common.Extent
			Links  []common.Link
		}
		if err := json.Unmarshal(data, &metadata); err != nil || status != 200 || len(metadata.CRS) != 1 || metadata.CRS[0] != common.CRS84 {
			t.Fatal(path, string(data), err)
		}
		found := false
		for _, link := range metadata.Links {
			if link.Rel == "http://www.opengis.net/def/rel/ogc/1.0/map" {
				found = true
				if strings.Contains(link.Href, "?") {
					t.Fatal("map discovery must permit Accept negotiation", link)
				}
			}
		}
		if !found || path == "/ogc/" && metadata.Extent == nil {
			t.Fatal(path, metadata)
		}
	}
}

func TestCoverageMetadataResources(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	service := &coverages.Service{Formats: []string{common.JSON}, Reader: coverages.ReaderFunc(func(context.Context, coverages.Request) (response.Payload, error) {
		return response.Bytes(common.JSON, []byte(`{}`)), nil
	}), DomainSet: json.RawMessage(`{"type":"DomainSet","generalGrid":{"axisLabels":["x","y"]}}`), RangeType: json.RawMessage(`{"type":"DataRecord","field":[{"name":"temperature"}]}`)}
	c.Collections[0].Coverage = service
	e := engine(t, c, 1)
	service.DomainSet[0], service.RangeType[0] = 'X', 'X'
	for _, name := range []string{"domainset", "rangetype"} {
		path := "/ogc/collections/roads/coverage/" + name
		status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: path})
		if status != 200 || !json.Valid(data) || headers["Content-Type"] != common.JSON || headers["ETag"] == "" {
			t.Fatal(status, string(data), headers)
		}
		for _, input := range []api.Input{{Method: "GET", EscapedPath: path, RawQuery: "subset=x(0)"}, {Method: "GET", EscapedPath: path, Accept: "image/png"}} {
			status, _, _ := read(t, e, input)
			if status != 400 && status != 406 {
				t.Fatal(status)
			}
		}
	}
	_, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads"})
	var metadata common.Collection
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"domainset", "rangetype"} {
		found := false
		for _, link := range metadata.Links {
			if link.Rel == "http://www.opengis.net/def/rel/ogc/1.0/coverage-"+name {
				found = link.Href == "https://example.test/ogc/collections/roads/coverage/"+name && link.Type == common.JSON
			}
		}
		if !found {
			t.Fatal("missing usable coverage metadata link", name, metadata.Links)
		}
	}
}

func TestMapCRSHeaderDefaults(t *testing.T) {
	for _, crs := range []string{common.CRS84, "https://www.opengis.net/def/crs/OGC/1.3/CRS84", "http://www.opengis.net/def/crs/EPSG/0/3857"} {
		c := catalog(&memoryFeatures{}, tileReader("tile"))
		c.Collections[0].Map = &maps.Service{Formats: []string{"image/png"}, CRS: []string{common.CRS84, crs}, DefaultCRS: crs, Renderer: maps.RendererFunc(func(_ context.Context, r maps.Request) (maps.Result, error) {
			return maps.Result{Content: response.Bytes(r.Format, []byte("map")), Width: r.Width, Height: r.Height, BBox: r.BBox, CRS: r.CRS}, nil
		})}
		e := engine(t, c, 1)
		status, _, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/map"})
		want := ""
		if strings.Contains(crs, "3857") {
			want = "<" + crs + ">"
		}
		if status != 200 || headers["Content-Crs"] != want {
			t.Fatal(crs, status, headers)
		}
	}
}
