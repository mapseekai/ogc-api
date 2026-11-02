// Package adaptertest runs the same real-HTTP contracts against every adapter.
package adaptertest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

const TileQuery = "SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=world&STYLE=default&FORMAT=image/png&TILEMATRIXSET=local&TILEMATRIX=L00&TILEROW=0&TILECOL=0"

// Factory starts a real loopback listener and registers its cleanup with t.
type Factory func(t *testing.T, engine *server.Engine) string

func Run(t *testing.T, start Factory) {
	t.Helper()
	config, err := demo.Config("https://public.example.test")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	base := start(t, engine)
	client := &http.Client{Timeout: 5 * time.Second}
	for _, version := range wms.Versions() {
		t.Run("WMS/"+version, func(t *testing.T) {
			result, data := request(t, client, "GET", base+"/wms?"+MapQuery(version))
			if result.StatusCode != 200 || result.Header.Get("Content-Type") != "image/png" || !bytes.HasPrefix(data, []byte("\x89PNG")) {
				t.Fatalf("invalid map response: %d %s", result.StatusCode, data)
			}
			capQuery := "SERVICE=WMS&REQUEST=GetCapabilities&VERSION=" + version
			if version == "1.0.0" {
				capQuery = "REQUEST=capabilities&WMTVER=1.0.0"
			}
			result, data = request(t, client, "GET", base+"/wms?"+capQuery)
			root := "WMT_MS_Capabilities"
			if version == "1.3.0" {
				root = "WMS_Capabilities"
			}
			assertXML(t, data, root, version)
			if result.StatusCode != 200 || bytes.Contains(data, []byte("attacker.example")) {
				t.Fatal("bad capabilities response")
			}
			infoQuery := strings.ReplaceAll(MapQuery(version), "REQUEST=GetMap", "REQUEST=GetFeatureInfo") + "&QUERY_LAYERS=world"
			if version == "1.0.0" {
				infoQuery = strings.ReplaceAll(infoQuery, "REQUEST=map", "REQUEST=feature_info") + "&INFO_FORMAT=MIME&X=1&Y=1"
			} else if version == "1.3.0" {
				infoQuery += "&INFO_FORMAT=application/json&I=1&J=1"
			} else {
				infoQuery += "&INFO_FORMAT=application/json&X=1&Y=1"
			}
			result, data = request(t, client, "GET", base+"/wms?"+infoQuery)
			if result.StatusCode != 200 || !bytes.Contains(data, []byte("FeatureCollection")) {
				t.Fatalf("bad feature information: %d %s", result.StatusCode, data)
			}
			_, data = request(t, client, "GET", base+"/wms?"+strings.ReplaceAll(MapQuery(version), "LAYERS=world", "LAYERS=missing"))
			errorRoot := "ServiceExceptionReport"
			if version == "1.0.0" {
				errorRoot = "WMTException"
			}
			assertXML(t, data, errorRoot, version)
		})
	}
	cases := []struct {
		name, method, path string
		status             int
		contains           string
	}{
		{"tile", "GET", "/wmts?" + TileQuery, 200, "\x89PNG"},
		{"rest tile", "GET", "/tiles/world/default/local/L00/0/0.png", 200, "\x89PNG"},
		{"wmts metadata", "GET", "/wmts?SERVICE=WMTS&REQUEST=GetCapabilities", 200, "Capabilities"},
		{"rest metadata", "GET", "/wmts/1.0.0/WMTSCapabilities.xml", 200, "ResourceURL"},
		{"rest info", "GET", "/info/world/default/local/10m/1/1/127/511.json", 200, "FeatureCollection"},
		{"kvp info", "GET", "/wmts?" + strings.ReplaceAll(TileQuery, "REQUEST=GetTile", "REQUEST=GetFeatureInfo") + "&INFOFORMAT=application/json&I=255&J=255", 200, "FeatureCollection"},
		{"matrix limit", "GET", "/wmts?" + strings.ReplaceAll(TileQuery, "TILECOL=0", "TILECOL=3"), 400, "TileOutOfRange"},
		{"rest limit", "GET", "/tiles/world/default/local/L00/0/3.png", 404, "TileOutOfRange"},
		{"pixel limit", "GET", "/info/world/default/local/10m/1/1/128/0.json", 404, "PointIJOutOfRange"},
		{"path conflict", "GET", "/tiles/world/default/local/L00/0/0.png?TILEROW=1", 404, "InvalidParameterValue"},
		{"duplicate singleton", "GET", "/wmts?" + TileQuery + "&layer=other", 400, "InvalidParameterValue"},
		{"shared route", "GET", "/ows?" + TileQuery, 200, "\x89PNG"},
		{"ambiguous service", "GET", "/ows?VERSION=1.0.0&REQUEST=GetCapabilities", 400, "Required parameter is missing"},
		{"head", "HEAD", "/wmts?" + TileQuery, 200, ""},
		{"method", "POST", "/wmts?" + TileQuery, 405, "Method not allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, data := request(t, client, tc.method, base+tc.path)
			if result.StatusCode != tc.status || !bytes.Contains(data, []byte(tc.contains)) {
				t.Fatalf("status %d, body %q", result.StatusCode, data)
			}
			if tc.method == "HEAD" && (len(data) != 0 || result.ContentLength <= 0) {
				t.Fatal("HEAD lost metadata or returned a body")
			}
			if tc.status == 405 && result.Header.Get("Allow") != "GET, HEAD" {
				t.Fatal("missing Allow header")
			}
		})
	}
	t.Run("conditional results", func(t *testing.T) { testConditionalResults(t, start) })
	t.Run("owned escaped input", func(t *testing.T) { testOwnedInput(t, start) })
	t.Run("stream ownership", func(t *testing.T) { testStreams(t, start) })
	t.Run("business timeout", func(t *testing.T) { testBusinessTimeout(t, start) })
	t.Run("stream failures", func(t *testing.T) { RunStreamFailures(t, start) })
}

func MapQuery(version string) string {
	query := "SERVICE=WMS&VERSION=" + version + "&REQUEST=GetMap&LAYERS=world&STYLES=&SRS=EPSG:4326&BBOX=100%2C20%2C110%2C30&WIDTH=256&HEIGHT=256&FORMAT=image/png"
	if version == "1.3.0" {
		query = strings.ReplaceAll(query, "SRS=", "CRS=")
		query = strings.ReplaceAll(query, "BBOX=100%2C20%2C110%2C30", "BBOX=20%2C100%2C30%2C110")
	}
	if version == "1.0.0" {
		query = strings.ReplaceAll(query, "VERSION=", "WMTVER=")
		query = strings.ReplaceAll(query, "REQUEST=GetMap", "REQUEST=map")
		query = strings.ReplaceAll(query, "image/png", "PNG")
		query += "&EXCEPTIONS=WMS_XML"
	}
	return query
}

func testStreams(t *testing.T, start Factory) {
	t.Helper()
	for _, tc := range []struct {
		name, method             string
		length                   int64
		providerError, wrongType bool
	}{
		{"unknown", "GET", -1, false, false}, {"known", "GET", 13, false, false}, {"head", "HEAD", 13, false, false}, {"unknown head", "HEAD", -1, false, false},
		{"payload with error", "GET", -1, true, false}, {"wrong content type", "GET", -1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := demo.Config("https://public.example.test")
			if err != nil {
				t.Fatal(err)
			}
			var closes atomic.Int32
			done := make(chan server.Event, 1)
			config.Observer = func(event server.Event) {
				if event.Stage == "finish" {
					done <- event
				}
			}
			config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
				body := &streamBody{reader: strings.NewReader("streamed tile"), ctx: ctx, closed: &closes}
				media := "image/png"
				if tc.wrongType {
					media = "text/plain"
				}
				payload := response.Stream(media, body, tc.length)
				if tc.providerError {
					return payload, fmt.Errorf("private database path must not leak")
				}
				return payload, nil
			}
			engine, err := server.New(config)
			if err != nil {
				t.Fatal(err)
			}
			base := start(t, engine)
			client := &http.Client{Timeout: 5 * time.Second}
			result, data := request(t, client, tc.method, base+"/wmts?"+TileQuery)
			if tc.method == "HEAD" && tc.length < 0 && result.Header.Get("Content-Length") != "" {
				t.Error("unknown length was falsely advertised as known for HEAD")
			}
			if tc.providerError || tc.wrongType {
				if result.StatusCode != 500 || bytes.Contains(data, []byte("private database")) {
					t.Fatal("unsafe provider error response")
				}
			} else if tc.method != "HEAD" && string(data) != "streamed tile" {
				t.Fatal(string(data))
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("transport completion was not reported")
			}
			if closes.Load() != 1 {
				t.Fatalf("body closed %d times", closes.Load())
			}
		})
	}
}

func testConditionalResults(t *testing.T, start Factory) {
	t.Helper()
	var encoded bytes.Buffer
	compressor := gzip.NewWriter(&encoded)
	if _, err := compressor.Write([]byte("encoded tile")); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ name, path string }{
		{"WMS", "/wms?" + MapQuery("1.3.0")},
		{"WMTS KVP", "/wmts?" + TileQuery},
		{"WMTS REST", "/tiles/world/default/local/L00/0/0.png"},
	} {
		t.Run(route.name, func(t *testing.T) {
			for _, test := range []struct {
				name, method, match      string
				state                    response.State
				allowEmpty, fail, revoke bool
				status                   int
			}{
				{name: "unconditional", method: "GET", status: 200},
				{name: "nonmatching", method: "GET", match: `"other"`, status: 200},
				{name: "matching", method: "GET", match: `"tile"`, status: 304},
				{name: "weak", method: "GET", match: `W/"tile"`, status: 304},
				{name: "wildcard", method: "GET", match: "*", status: 304},
				{name: "head", method: "HEAD", status: 200},
				{name: "head matching", method: "HEAD", match: "*", status: 304},
				{name: "backend not modified", method: "GET", match: `"tile"`, state: response.NotModified, status: 304},
				{name: "empty enabled", method: "GET", state: response.NoContent, allowEmpty: true, status: 204},
				{name: "empty disabled", method: "GET", state: response.NoContent, status: 500},
				{name: "empty backend error", method: "GET", state: response.NoContent, allowEmpty: true, fail: true, status: 500},
				{name: "revoked matching", method: "GET", match: "*", revoke: true, status: 403},
				{name: "revoked backend not modified", method: "GET", match: `"tile"`, state: response.NotModified, revoke: true, status: 403},
			} {
				t.Run(test.name, func(t *testing.T) {
					config, err := demo.Config("https://public.example.test")
					if err != nil {
						t.Fatal(err)
					}
					config.AllowNoContent = test.allowEmpty
					config.MaxConcurrent = 1
					var calls, closes atomic.Int32
					finished := make(chan server.Event, 2)
					config.Observer = func(event server.Event) {
						if event.Stage == "finish" {
							finished <- event
						}
					}
					config.BeforeResponse = func(_ context.Context, input server.Input) error {
						if input.IfNoneMatch != test.match || input.AcceptEncoding != "gzip" {
							return errors.New("adapter dropped conditional headers")
						}
						if test.revoke {
							return owscore.Fail(owscore.Forbidden, "", "Access revoked")
						}
						return nil
					}
					read := func(ctx context.Context, conditions response.Conditions) (response.Payload, error) {
						calls.Add(1)
						if conditions.IfNoneMatch != test.match || conditions.AcceptEncoding != "gzip" {
							return response.Payload{}, errors.New("backend lost conditional headers")
						}
						payload := response.Payload{State: test.state, ContentType: "image/png"}
						if test.state == response.Content {
							payload = response.Stream("image/png", &streamBody{reader: bytes.NewReader(encoded.Bytes()), ctx: ctx, closed: &closes}, int64(encoded.Len()))
						}
						payload.ContentEncoding = "gzip"
						payload.Cache = response.Cache{ETag: `"tile"`, CacheControl: "private, max-age=60", LastModified: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
						payload.Vary = "Authorization, Accept-Encoding"
						if test.fail {
							return payload, errors.New("backend failed")
						}
						return payload, nil
					}
					config.Handlers.GetMap = func(ctx context.Context, r *wms.MapRequest) (response.Payload, error) { return read(ctx, r.Conditions) }
					config.Handlers.GetTile = func(ctx context.Context, r *wmts.TileRequest) (response.Payload, error) {
						return read(ctx, r.Conditions)
					}
					engine, err := server.New(config)
					if err != nil {
						t.Fatal(err)
					}
					base := start(t, engine)
					client := &http.Client{Timeout: 5 * time.Second}
					req, err := http.NewRequest(test.method, base+route.path, nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("If-None-Match", test.match)
					req.Header.Set("Accept-Encoding", "gzip")
					reply, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					data, readErr := io.ReadAll(reply.Body)
					closeErr := reply.Body.Close()
					if readErr != nil || closeErr != nil {
						t.Fatal(readErr, closeErr)
					}
					if reply.StatusCode != test.status {
						t.Fatalf("status=%d want=%d body=%s", reply.StatusCode, test.status, data)
					}
					if calls.Load() != 1 {
						t.Fatal("conditional request bypassed backend authorization")
					}
					if test.status == 200 && test.method != "HEAD" && !bytes.Equal(data, encoded.Bytes()) {
						t.Fatal("encoded response was changed or compressed twice")
					}
					if (test.method == "HEAD" || test.status == 204 || test.status == 304) && len(data) != 0 {
						t.Fatal("bodyless response sent content")
					}
					if test.status == 204 || test.status == 304 {
						if reply.Header.Get("Content-Length") != "" || reply.Header.Get("Content-Type") != "" {
							t.Fatalf("bodyless headers: %v", reply.Header)
						}
					}
					if test.status < 400 {
						if reply.Header.Get("ETag") != `"tile"` || reply.Header.Get("Cache-Control") != "private, max-age=60" || reply.Header.Get("Last-Modified") == "" {
							t.Fatalf("cache metadata lost: %v", reply.Header)
						}
						if test.status != 204 && reply.Header.Get("Content-Encoding") != "gzip" {
							t.Fatal("content encoding lost")
						}
						for _, field := range []string{"Authorization", "X-API-Key", "X-Tenant", "Accept-Encoding"} {
							if !strings.Contains(reply.Header.Get("Vary"), field) {
								t.Fatalf("Vary lost %s: %v", field, reply.Header)
							}
						}
						if route.name == "WMTS REST" && !strings.Contains(reply.Header.Get("Vary"), "Accept") {
							t.Fatal("REST negotiation Vary lost")
						}
					}
					select {
					case <-finished:
					case <-time.After(5 * time.Second):
						t.Fatal("response did not finish")
					}
					wantCloses := int32(0)
					if test.state == response.Content {
						wantCloses = 1
					}
					if closes.Load() != wantCloses {
						t.Fatalf("closed=%d want=%d", closes.Load(), wantCloses)
					}
					// The one-slot engine must admit another request after every result.
					next, _ := request(t, client, "GET", base+"/wmts?SERVICE=WMTS&REQUEST=GetCapabilities")
					if next.StatusCode == 503 {
						t.Fatal("result leaked concurrency slot")
					}
				})
			}
		})
	}
}

func request(t *testing.T, client *http.Client, method, target string) (*http.Response, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Forwarded-Host", "attacker.example")
	result, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(result.Body)
	closeErr := result.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	return result, data
}

func assertXML(t *testing.T, data []byte, root, version string) {
	t.Helper()
	var document struct {
		XMLName xml.Name
		Version string `xml:"version,attr"`
	}
	if err := xml.Unmarshal(data, &document); err != nil {
		t.Fatalf("invalid XML: %s: %v", data, err)
	}
	if document.XMLName.Local != root || document.Version != version {
		t.Fatalf("unexpected root: %+v", document)
	}
}

type streamBody struct {
	reader io.Reader
	ctx    context.Context
	closed *atomic.Int32
}

func (b *streamBody) Read(p []byte) (int, error) {
	if b.closed.Load() != 0 {
		return 0, errors.New("stream was closed before consumption")
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.reader.Read(p)
}

func (b *streamBody) Close() error { b.closed.Add(1); return nil }
