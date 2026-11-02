package api_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
)

func TestTileConditionalResults(t *testing.T) {
	for _, tc := range []struct {
		name, condition string
		state           response.State
		allowEmpty      bool
		backendError    error
		status          int
	}{
		{name: "content", status: 200},
		{name: "mismatch", condition: `"other"`, status: 200},
		{name: "match", condition: `"v1"`, status: 304},
		{name: "weak", condition: `W/"v1"`, status: 304},
		{name: "wildcard", condition: "*", status: 304},
		{name: "backend_not_modified", condition: `"v1"`, state: response.NotModified, status: 304},
		{name: "unsolicited_not_modified", state: response.NotModified, status: 500},
		{name: "allowed_empty", state: response.NoContent, allowEmpty: true, status: 204},
		{name: "disallowed_empty", state: response.NoContent, status: 500},
		{name: "backend_failure", state: response.NoContent, allowEmpty: true, backendError: errors.New("backend failed"), status: 500},
		{name: "authorization_failure", condition: "*", backendError: common.ErrForbidden, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received tiles.Request
			closed := 0
			reader := tiles.ReaderFunc(func(_ context.Context, request tiles.Request) (response.Payload, error) {
				received = request
				p := response.Payload{State: tc.state}
				if tc.state == response.Content {
					p = response.Stream("application/vnd.mapbox-vector-tile", &closeCountingBody{Reader: strings.NewReader("encoded-tile"), closed: &closed}, 12)
				}
				p.ContentEncoding = "gzip"
				p.Cache = response.Cache{ETag: `"v1"`, CacheControl: "private, max-age=60", LastModified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
				p.Vary = "Authorization, X-API-Key"
				return p, tc.backendError
			})
			c := catalog(&memoryFeatures{}, reader)
			c.Collections[0].Tiles[0].AllowNoContent = tc.allowEmpty
			e := engine(t, c, 1)
			status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0", IfNoneMatch: tc.condition, AcceptEncoding: "gzip"})
			if status != tc.status || received.Conditions.IfNoneMatch != tc.condition || received.Conditions.AcceptEncoding != "gzip" {
				t.Fatal(status, string(data), received)
			}
			if tc.state == response.Content && closed != 1 {
				t.Fatal("body not closed exactly once", closed)
			}
			if status == 200 && (string(data) != "encoded-tile" || headers["Content-Encoding"] != "gzip") {
				t.Fatal("encoding or bytes changed", string(data), headers)
			}
			if status == 204 || status == 304 {
				if len(data) != 0 || headers["Content-Length"] != "" || headers["Content-Type"] != "" {
					t.Fatal("bodyless response leaked entity", string(data), headers)
				}
			}
			if status < 400 {
				for _, vary := range []string{"Authorization", "X-API-Key", "Accept", "Accept-Encoding"} {
					if !strings.Contains(headers["Vary"], vary) {
						t.Fatal("lost Vary", headers)
					}
				}
				if headers["ETag"] != `"v1"` || headers["Cache-Control"] != "private, max-age=60" || headers["Last-Modified"] == "" {
					t.Fatal("lost cache metadata", headers)
				}
			}
			status, _, _ = read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/"})
			if status != 200 {
				t.Fatal("result leaked admission", status)
			}
		})
	}
}

func TestTileInvalidCoordinateDoesNotBecomeEmpty(t *testing.T) {
	calls := 0
	c := catalog(&memoryFeatures{}, tiles.ReaderFunc(func(context.Context, tiles.Request) (response.Payload, error) {
		calls++
		return response.Payload{State: response.NoContent}, nil
	}))
	c.Collections[0].Tiles[0].AllowNoContent = true
	e := engine(t, c, 1)
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/tiles/local/L00/2/0", IfNoneMatch: "*"})
	if status != 404 || calls != 0 {
		t.Fatal(status, calls)
	}
}

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
