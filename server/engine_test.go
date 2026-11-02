package server_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/exception"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestConditionalResults(t *testing.T) {
	inputs := map[string]server.Input{
		"WMS":       {Method: "GET", Protocol: owscore.WMS, RawQuery: adaptertest.MapQuery("1.3.0")},
		"WMTS KVP":  {Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery},
		"WMTS REST": {Method: "GET", Protocol: owscore.WMTS, Binding: owscore.REST, EscapedPath: "/tiles/world/default/local/10m/2/3.png"},
	}
	for name, input := range inputs {
		for _, tc := range []struct {
			name, condition string
			state           response.State
			allow           bool
			want            int
		}{
			{"unconditional", "", response.Content, false, 200},
			{"miss", `"other"`, response.Content, false, 200},
			{"match", `"v1"`, response.Content, false, 304},
			{"weak", `W/"v1"`, response.Content, false, 304},
			{"wildcard", "*", response.Content, false, 304},
			{"backend not modified", `"v1"`, response.NotModified, false, 304},
			{"empty enabled", "", response.NoContent, true, 204},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				config := makeConfig(t)
				config.MaxConcurrent = 1
				config.AllowNoContent = tc.allow
				var calls, checks int
				body := &trackedBody{Reader: strings.NewReader("data")}
				backend := func(conditions response.Conditions) (response.Payload, error) {
					calls++
					if conditions.IfNoneMatch != tc.condition || conditions.AcceptEncoding != "gzip" {
						t.Fatal(conditions)
					}
					p := response.Payload{State: tc.state}
					if tc.state == response.Content {
						p = response.Stream("image/png", body, 4)
					}
					p.Cache = response.Cache{ETag: `"v1"`, CacheControl: "private", LastModified: time.Unix(100, 0)}
					p.ContentEncoding = "gzip"
					p.Vary = "Authorization, X-API-Key"
					return p, nil
				}
				config.Handlers.GetMap = func(_ context.Context, r *wms.MapRequest) (response.Payload, error) { return backend(r.Conditions) }
				config.Handlers.GetTile = func(_ context.Context, r *wmts.TileRequest) (response.Payload, error) { return backend(r.Conditions) }
				config.BeforeResponse = func(_ context.Context, in server.Input) error {
					if strings.Contains(in.RawQuery, "GetCapabilities") {
						return nil
					}
					checks++
					if body.closes != 0 {
						t.Fatal("closed before host recheck")
					}
					return nil
				}
				input.IfNoneMatch, input.AcceptEncoding = tc.condition, "gzip"
				e := engine(t, config)
				plan, err := e.Handle(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				if plan.Status != tc.want || calls != 1 || checks != 1 {
					t.Fatal(plan.Status, calls, checks)
				}
				if plan.Headers["ETag"] != `"v1"` || plan.Headers["Cache-Control"] != "private" || plan.Headers["Last-Modified"] == "" {
					t.Fatal(plan.Headers)
				}
				if !strings.Contains(plan.Headers["Vary"], "Authorization") || !strings.Contains(plan.Headers["Vary"], "Accept-Encoding") {
					t.Fatal(plan.Headers)
				}
				if tc.want != 200 && plan.Headers["Content-Length"] != "" {
					t.Fatal(plan.Headers)
				}
				if tc.want != 204 && plan.Headers["Content-Encoding"] != "gzip" {
					t.Fatal(plan.Headers)
				}
				if tc.want == 200 && body.closes != 0 {
					t.Fatal("lazy body closed")
				}
				data := readPlan(t, plan)
				if tc.want == 200 && string(data) != "data" || tc.want != 200 && len(data) != 0 {
					t.Fatal(string(data))
				}
				_ = plan.Finish(0, nil)
				if tc.state == response.Content && body.closes != 1 {
					t.Fatal(body.closes)
				}
				// A completed bodyless result must release the same admission slot.
				status, _ := call(t, e, capabilities(owscore.WMTS, "1.0.0"))
				if status != 200 {
					t.Fatal("slot leaked", status)
				}
			})
		}
	}
}

func TestConditionalResultsRejectUnsafeSuccess(t *testing.T) {
	for _, mode := range []string{"empty disabled", "backend failure", "revoked", "stopped", "version changed", "invalid tile", "unsolicited 304"} {
		t.Run(mode, func(t *testing.T) {
			config := makeConfig(t)
			var calls, checks int
			config.AllowNoContent = mode != "empty disabled"
			config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
				calls++
				if mode == "backend failure" {
					return response.Payload{State: response.NoContent}, errors.New("backend broke")
				}
				if mode == "empty disabled" {
					return response.Payload{State: response.NoContent}, nil
				}
				return response.Payload{State: response.NotModified, Cache: response.Cache{ETag: `"v1"`}}, nil
			}
			config.BeforeResponse = func(context.Context, server.Input) error {
				checks++
				if mode == "revoked" || mode == "stopped" || mode == "version changed" {
					return owscore.Fail(owscore.Unavailable, "", "Resource changed")
				}
				return nil
			}
			input := server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery, IfNoneMatch: `"v1"`}
			if mode == "invalid tile" {
				input.RawQuery = strings.Replace(input.RawQuery, "TILEROW=0", "TILEROW=999999", 1)
			}
			if mode == "unsolicited 304" {
				input.IfNoneMatch = ""
			}
			plan, err := engine(t, config).Handle(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Finish(0, nil)
			if plan.Status == 204 || plan.Status == 304 || plan.ProtocolError == nil {
				t.Fatal(plan.Status, plan.ProtocolError)
			}
			if mode == "invalid tile" && (calls != 0 || checks != 0) {
				t.Fatal("validation bypass", calls, checks)
			}
			if mode == "backend failure" && checks != 0 {
				t.Fatal("checked failed backend")
			}
		})
	}
}

func TestConditionalHeaderBudgetAndEmptyInfoPolicy(t *testing.T) {
	config := makeConfig(t)
	config.MaxHeaderBytes = 4
	var calls int
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		calls++
		return response.Bytes("image/png", nil), nil
	}
	status, _ := call(t, engine(t, config), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery, AcceptEncoding: "gzip", IfNoneMatch: "*"})
	if status == 200 || calls != 0 {
		t.Fatal(status, calls)
	}
	config = makeConfig(t)
	config.AllowNoContent = true
	config.Handlers.GetWMTSFeatureInfo = func(context.Context, *wmts.FeatureInfoRequest) (response.Payload, error) {
		return response.Payload{State: response.NoContent}, nil
	}
	query := strings.Replace(adaptertest.TileQuery, "REQUEST=GetTile", "REQUEST=GetFeatureInfo", 1) + "&I=0&J=0&INFOFORMAT=application/json"
	plan, err := engine(t, config).Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: query})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Finish(0, nil)
	if plan.Status == 204 || plan.ProtocolError == nil {
		t.Fatal("empty policy escaped tile/map profile", plan.Status)
	}
}

func TestBeforeResponsePanicReleasesContent(t *testing.T) {
	config := makeConfig(t)
	config.MaxConcurrent = 1
	body := &trackedBody{Reader: strings.NewReader("tile")}
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		return response.Stream("image/png", body, 4), nil
	}
	config.BeforeResponse = func(_ context.Context, input server.Input) error {
		if input.IfNoneMatch != "" {
			panic("host state failure")
		}
		return nil
	}
	e := engine(t, config)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("host panic swallowed")
			}
		}()
		_, _ = e.Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery, IfNoneMatch: "*"})
	}()
	if body.closes != 1 {
		t.Fatal(body.closes)
	}
	status, _ := call(t, e, capabilities(owscore.WMTS, "1.0.0"))
	if status != 200 {
		t.Fatal("slot leaked", status)
	}
}

func TestConditionalDiscardCancellationIsNotSuccess(t *testing.T) {
	config := makeConfig(t)
	config.MaxConcurrent = 1
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &cancelCloseBody{cancel: cancel}
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		p := response.Stream("image/png", body, 4)
		p.Cache.ETag = `"v1"`
		return p, nil
	}
	e := engine(t, config)
	plan, err := e.Handle(parent, server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery, IfNoneMatch: `"v1"`})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status == 304 || plan.ProtocolError == nil {
		t.Fatal("canceled discard became a successful response", plan.Status)
	}
	_ = plan.Finish(0, nil)
	_ = plan.Finish(0, nil)
	if body.reads != 0 || body.closes != 1 {
		t.Fatal(body.reads, body.closes)
	}
	status, _ := call(t, e, capabilities(owscore.WMTS, "1.0.0"))
	if status != 200 {
		t.Fatal("slot leaked", status)
	}
}

func TestAdmissionHeldUntilFinish(t *testing.T) {
	config := makeConfig(t)
	config.MaxConcurrent = 1
	e := engine(t, config)
	input := server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery}
	first, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != 503 || second.Headers["Retry-After"] != "1" {
		t.Fatal(second.Status, second.Headers)
	}
	readPlan(t, second)
	if err := first.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if err := first.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	status, _ := call(t, e, input)
	if status != 200 {
		t.Fatal("admission slot not released")
	}
}

func TestCiteProtocolExceptionsAndNegotiation(t *testing.T) {
	e := engine(t, makeConfig(t))
	for _, tc := range []struct {
		name     string
		input    server.Input
		status   int
		contains string
	}{
		{"WMS capabilities ignores unsupported format", server.Input{Method: "GET", Protocol: owscore.WMS, RawQuery: "SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities&FORMAT=invalid"}, 200, "WMS_Capabilities"},
		{"WMS unknown query layer", server.Input{Method: "GET", Protocol: owscore.WMS, RawQuery: "SERVICE=WMS&VERSION=1.3.0&REQUEST=GetFeatureInfo&LAYERS=world&STYLES=&CRS=CRS:84&BBOX=-180,-90,180,90&WIDTH=256&HEIGHT=256&FORMAT=image/png&QUERY_LAYERS=unknown&I=0&J=0&INFO_FORMAT=application/json"}, 200, `code="LayerNotDefined"`},
		{"WMTS unknown operation", server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: "SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetBOGUS"}, 400, `exceptionCode="InvalidParameterValue"`},
		{"WMTS missing tile format", server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: strings.ReplaceAll(strings.ReplaceAll(adaptertest.TileQuery, "REQUEST=GetTile", "REQUEST=GetFeatureInfo"), "FORMAT=image/png&", "") + "&I=0&J=0&INFOFORMAT=application/json"}, 400, `locator="FORMAT"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, data := call(t, e, tc.input)
			if status != tc.status || !strings.Contains(string(data), tc.contains) {
				t.Fatalf("status %d: %s", status, data)
			}
		})
	}
	for _, tc := range []struct {
		accept string
		status int
	}{{"", 200}, {"image/*", 200}, {"image/png;q=0,*/*;q=1", 406}, {"text/plain", 406}} {
		t.Run("Accept "+tc.accept, func(t *testing.T) {
			plan, err := e.Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, Binding: owscore.REST, EscapedPath: "/tiles/world/default/local/10m/2/3.png", Accept: tc.accept})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Finish(0, nil)
			if plan.Status != tc.status {
				t.Fatalf("status %d: %s", plan.Status, readPlan(t, plan))
			}
		})
	}
	config := makeConfig(t)
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		return response.Bytes("image/png;profile=survey", []byte("tile")), nil
	}
	qualified := engine(t, config)
	status, _ := call(t, qualified, server.Input{Method: "GET", Protocol: owscore.WMTS, Binding: owscore.REST, EscapedPath: "/tiles/world/default/local/10m/2/3.png", Accept: "image/png;profile=survey"})
	if status != 200 {
		t.Fatalf("actual representation parameters were ignored: %d", status)
	}
}

func TestClosePanicReleasesAdmission(t *testing.T) {
	config := makeConfig(t)
	config.MaxConcurrent = 1
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		return response.Stream("image/png", &panicCloseBody{Reader: strings.NewReader("tile")}, 4), nil
	}
	e := engine(t, config)
	plan, err := e.Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("close panic was swallowed")
			}
		}()
		_ = plan.Finish(0, nil)
	}()
	next, err := e.Handle(context.Background(), capabilities(owscore.WMTS, "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != 200 {
		t.Fatal("close panic leaked admission slot")
	}
	readPlan(t, next)
}

func TestCapabilitiesAndExceptionsGolden(t *testing.T) {
	e := engine(t, makeConfig(t))
	for _, version := range wms.Versions() {
		t.Run(version, func(t *testing.T) {
			status, data := call(t, e, capabilities(owscore.WMS, version))
			if status != 200 {
				t.Fatalf("capabilities %s: %s", version, data)
			}
			golden(t, "wms_"+version+"_capabilities.xml", data)
			plan, err := exception.Encode(owscore.Meta{Protocol: owscore.WMS, Version: version, Binding: owscore.KVP}, owscore.Fail(owscore.InvalidCRS, "CRS", "Unknown CRS < & >"))
			if err != nil {
				t.Fatal(err)
			}
			golden(t, "wms_"+version+"_exception.xml", readPlan(t, plan))

		})
	}
	status, data := call(t, e, capabilities(owscore.WMTS, "1.0.0"))
	if status != 200 {
		t.Fatal(status, string(data))
	}
	golden(t, "wmts_1.0.0_capabilities.xml", data)
	plan, err := exception.Encode(owscore.Meta{Protocol: owscore.WMTS, Version: "1.0.0", Binding: owscore.KVP}, owscore.Fail(owscore.TileOutOfRange, "TILECOL", "Tile is outside the declared matrix"))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "wmts_1.0.0_exception.xml", readPlan(t, plan))
}

func TestOptionalOperationsNotAdvertised(t *testing.T) {
	config := makeConfig(t)
	config.Handlers.GetWMSFeatureInfo = nil
	config.Handlers.GetWMTSFeatureInfo = nil
	e := engine(t, config)
	for _, protocol := range []owscore.Protocol{owscore.WMS, owscore.WMTS} {
		t.Run(string(protocol), func(t *testing.T) {
			version := "1.3.0"
			if protocol == owscore.WMTS {
				version = "1.0.0"
			}
			_, data := call(t, e, capabilities(protocol, version))
			if bytes.Contains(data, []byte("GetFeatureInfo")) || bytes.Contains(data, []byte("InfoFormat")) || bytes.Contains(data, []byte(`queryable="1"`)) || bytes.Contains(data, []byte(`resourceType="FeatureInfo"`)) {
				t.Fatal("unregistered operation was advertised", string(data))
			}

		})
	}
}

func TestUpdateSequenceAndSections(t *testing.T) {
	maps, tiles := demo.Catalogs()
	maps.UpdateSequence = "release-b"
	tiles.UpdateSequence = "release-b"
	s, err := server.NewSnapshot(&maps, &tiles)
	if err != nil {
		t.Fatal(err)
	}
	config := makeConfig(t)
	config.Catalog = server.Static(s)
	config.CompareUpdateSequence = func(a, b string) (int, error) { return strings.Compare(a, b), nil }
	e := engine(t, config)
	for _, tc := range []struct {
		protocol                    owscore.Protocol
		version, sequence, expected string
	}{
		{owscore.WMS, "1.3.0", "release-b", "CurrentUpdateSequence"},
		{owscore.WMS, "1.3.0", "release-c", "InvalidUpdateSequence"},
		{owscore.WMS, "1.3.0", "release-a", "WMS_Capabilities"},
		{owscore.WMTS, "1.0.0", "release-c", "InvalidUpdateSequence"},
	} {
		t.Run(fmt.Sprintf("%s_%s", tc.protocol, tc.sequence), func(t *testing.T) {
			input := capabilities(tc.protocol, tc.version)
			input.RawQuery += "&UPDATESEQUENCE=" + tc.sequence
			_, data := call(t, e, input)
			if !bytes.Contains(data, []byte(tc.expected)) {
				t.Fatal(string(data))
			}

		})
	}
	input := capabilities(owscore.WMTS, "1.0.0")
	input.RawQuery += "&UPDATESEQUENCE=release-b"
	_, data := call(t, e, input)
	if !bytes.Contains(data, []byte("Capabilities")) || bytes.Contains(data, []byte("Contents")) {
		t.Fatal(string(data))
	}
	input = capabilities(owscore.WMTS, "1.0.0")
	input.RawQuery += "&SECTIONS=Contents"
	_, data = call(t, e, input)
	if !bytes.Contains(data, []byte("Contents")) || bytes.Contains(data, []byte("ServiceIdentification")) {
		t.Fatal(string(data))
	}
}

func TestProviderReceivesNormalizedModel(t *testing.T) {
	config := makeConfig(t)
	config.Handlers.GetMap = func(_ context.Context, r *wms.MapRequest) (response.Payload, error) {
		if r.Bounds != [4]float64{100, 20, 110, 30} || !r.Normalized {
			t.Errorf("incorrect normalized model: %+v", r)
		}
		return demo.PNG(), nil
	}
	config.Handlers.GetTile = func(_ context.Context, r *wmts.TileRequest) (response.Payload, error) {
		if r.TileMatrix != "L00" || r.TileMatrixSet != "local" {
			t.Error("matrix identifier was converted to zoom")
		}
		return demo.PNG(), nil
	}
	e := engine(t, config)
	for _, version := range wms.Versions() {
		t.Run(version, func(t *testing.T) {
			call(t, e, server.Input{Method: "GET", Protocol: owscore.WMS, RawQuery: adaptertest.MapQuery(version)})

		})
	}
	call(t, e, server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
	call(t, e, server.Input{Method: "GET", Protocol: owscore.WMTS, Binding: owscore.REST, EscapedPath: "/tiles/world/default/local/L00/0/0.png"})
}

func TestWMSHTTP200ErrorRemainsObservable(t *testing.T) {
	config := makeConfig(t)
	var observed error
	config.Observer = func(event server.Event) {
		if event.Stage == "finish" {
			observed = event.ProtocolError
		}
	}
	status, _ := call(t, engine(t, config), server.Input{
		Method: "GET", Protocol: owscore.WMS,
		RawQuery: strings.ReplaceAll(adaptertest.MapQuery("1.3.0"), "LAYERS=world", "LAYERS=missing"),
	})
	var protocolError *owscore.Error
	if status != 200 || !errors.As(observed, &protocolError) || protocolError.Kind != owscore.LayerNotDefined {
		t.Fatalf("lost protocol error: status %d, error %v", status, observed)
	}
}

func TestDimensionCapabilitiesGolden(t *testing.T) {
	maps, tiles := demo.Catalogs()
	mapLayer := maps.Layers["world"]
	mapLayer.Dimensions = map[string]owscore.Dimension{"time": {
		Units: "ISO8601", Default: "2026-01-01", Values: []string{"2026-01-01", "2026-01-02"}, Current: true, MultipleValues: true,
	}}
	maps.Layers["world"] = mapLayer
	tileLayer := tiles.Layers["world"]
	tileLayer.Dimensions = map[string]owscore.Dimension{"Time": {
		Units: "ISO8601", UnitSymbol: "date", Default: "2026-01-01", Values: []string{"2026-01-01", "2026-01-02"}, Current: true,
	}}
	tiles.Layers["world"] = tileLayer
	snapshot, err := server.NewSnapshot(&maps, &tiles)
	if err != nil {
		t.Fatal(err)
	}
	config := makeConfig(t)
	config.Catalog = server.Static(snapshot)
	config.WMS.Versions = []string{"1.1.0", "1.1.1", "1.3.0"}
	e := engine(t, config)
	for _, version := range config.WMS.Versions {
		t.Run(version, func(t *testing.T) {
			status, data := call(t, e, capabilities(owscore.WMS, version))
			if status != 200 {
				t.Fatal(status, string(data))
			}
			golden(t, "wms_"+version+"_dimensions.xml", data)

		})
	}
	status, data := call(t, e, capabilities(owscore.WMTS, "1.0.0"))
	if status != 200 {
		t.Fatal(status, string(data))
	}
	golden(t, "wmts_1.0.0_dimensions.xml", data)
}

func TestErrorResponseHonorsParentCancellation(t *testing.T) {
	config := makeConfig(t)
	config.Timeout = time.Millisecond
	body := &trackedBody{Reader: strings.NewReader("discarded")}
	config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
		<-ctx.Done()
		return response.Stream("image/png", body, -1), ctx.Err()
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan, err := engine(t, config).Handle(parent, server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	data, readErr := io.ReadAll(plan.Body)
	finishErr := plan.Finish(int64(len(data)), readErr)
	if len(data) != 0 || !errors.Is(readErr, context.Canceled) || !errors.Is(finishErr, context.Canceled) || body.closes != 1 {
		t.Fatalf("parent cancellation ignored: bytes=%d read=%v finish=%v closes=%d", len(data), readErr, finishErr, body.closes)
	}
}

func TestFinishObserverPanicStillCancelsAndCloses(t *testing.T) {
	config := makeConfig(t)
	body := &trackedBody{Reader: strings.NewReader("tile")}
	var callbackCtx context.Context
	panicValue := errors.New("finish observer panic")
	config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
		callbackCtx = ctx
		return response.Stream("image/png", body, 4), nil
	}
	config.Observer = func(e server.Event) {
		if e.Stage == "finish" {
			panic(panicValue)
		}
	}
	plan, err := engine(t, config).Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if got := recover(); got != panicValue {
				t.Errorf("panic changed: %v", got)
			}
		}()
		_ = plan.Finish(0, nil)
	}()
	if body.closes != 1 || !errors.Is(callbackCtx.Err(), context.Canceled) {
		t.Fatalf("closes=%d context=%v", body.closes, callbackCtx.Err())
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if body.closes != 1 {
		t.Fatal("duplicate close")
	}
}

func TestStartObserverPanicDoesNotInvokeProvider(t *testing.T) {
	config := makeConfig(t)
	called := false
	config.Handlers.GetTile = func(context.Context, *wmts.TileRequest) (response.Payload, error) {
		called = true
		return response.Payload{}, nil
	}
	panicValue := errors.New("start observer panic")
	config.Observer = func(e server.Event) {
		if e.Stage == "start" {
			panic(panicValue)
		}
	}
	e := engine(t, config)
	defer func() {
		if got := recover(); got != panicValue {
			t.Errorf("panic changed: %v", got)
		}
		if called {
			t.Error("provider invoked after start panic")
		}
	}()
	_, _ = e.Handle(context.Background(), server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
}

func TestErrorDocumentHasBoundedIndependentContext(t *testing.T) {
	snapshot, err := server.NewSnapshot(nil, &wmts.Catalog{Service: owscore.Service{Title: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := server.New(server.Config{
		Catalog: server.Static(snapshot), WMTSURL: "https://example.test/wmts", Timeout: time.Nanosecond,
		Handlers: server.Handlers{GetTile: func(context.Context, *wmts.TileRequest) (response.Payload, error) {
			t.Fatal("invalid request reached provider")
			return response.Payload{}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"finish", "parent cancellation", "send deadline"} {
		t.Run(scenario, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			plan, err := e.Handle(parent, server.Input{Method: "GET", Protocol: owscore.WMTS})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Finish(0, nil)
			if plan.ProtocolError == nil {
				t.Fatal("expected an error response")
			}
			// Exercise the public Reader contract rather than asserting its private wrapper type.
			prefix := make([]byte, 1)
			if n, err := plan.Body.Read(prefix); n != 1 || err != nil {
				t.Fatalf("error response lost its independent send window: n=%d error=%v", n, err)
			}
			want := context.Canceled
			switch scenario {
			case "finish":
				if err := plan.Finish(0, nil); err != nil {
					t.Fatal(err)
				}
			case "parent cancellation":
				cancel()
			case "send deadline":
				// Allow the context timer goroutine to run after its one-second deadline.
				timer := time.NewTimer(time.Second + 50*time.Millisecond)
				defer timer.Stop()
				<-timer.C
				want = context.DeadlineExceeded
			}
			if _, err := plan.Body.Read(prefix); !errors.Is(err, want) {
				t.Fatalf("%s: read returned %v, want %v", scenario, err, want)
			}
		})
	}
}

func TestTimeoutExceptionRemainsReadable(t *testing.T) {
	config := makeConfig(t)
	config.Timeout = time.Millisecond
	config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
		<-ctx.Done()
		return response.Payload{}, ctx.Err()
	}
	plan, err := engine(t, config).Handle(context.Background(), server.Input{
		Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(plan.Body)
	_ = plan.Finish(int64(len(data)), readErr)
	if readErr != nil || !bytes.Contains(data, []byte("ExceptionReport")) {
		t.Fatalf("generated error plan cannot be sent: status=%d declared-length=%d bytes=%d read-error=%v", plan.Status, plan.Length, len(data), readErr)
	}
}

func TestObserverPanicReleasesTransferredBody(t *testing.T) {
	for _, stage := range []string{"business", "handoff"} {
		t.Run(stage, func(t *testing.T) {
			config := makeConfig(t)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := &trackedBody{Reader: strings.NewReader("tile")}
			var callbackCtx context.Context
			config.Handlers.GetTile = func(ctx context.Context, _ *wmts.TileRequest) (response.Payload, error) {
				callbackCtx = ctx
				return response.Stream("image/png", body, 4), nil
			}
			config.Observer = func(event server.Event) {
				if event.Stage == stage {
					panic("observer fixture panic")
				}
			}
			e := engine(t, config)
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				plan, err := e.Handle(parent, server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery})
				if plan != nil {
					_ = plan.Finish(0, err)
				}
			}()
			if recovered == nil {
				t.Fatal("expected observer panic to reach the host")
			}
			closed := body.closes
			ctxErr := callbackCtx.Err()
			if body.closes == 0 {
				_ = body.Close()
			}
			if closed != 1 || ctxErr == nil {
				t.Fatalf("resource ownership lost after %s observer panic: closes=%d context-error=%v", stage, closed, ctxErr)
			}
		})
	}
}

func BenchmarkEngineGetTile(b *testing.B) {
	config, err := demo.Config("https://example.test")
	if err != nil {
		b.Fatal(err)
	}
	e, err := server.New(config)
	if err != nil {
		b.Fatal(err)
	}
	input := server.Input{Method: "GET", Protocol: owscore.WMTS, RawQuery: adaptertest.TileQuery}
	b.ReportAllocs()
	for b.Loop() {
		p, err := e.Handle(context.Background(), input)
		if err != nil {
			b.Fatal(err)
		}
		n, err := io.Copy(io.Discard, p.Body)
		if err := p.Finish(n, err); err != nil {
			b.Fatal(err)
		}
	}
}

type panicCloseBody struct{ io.Reader }

func (b *panicCloseBody) Close() error { panic("foreign close panic") }

func engine(t *testing.T, config server.Config) *server.Engine {
	t.Helper()
	e, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func readPlan(t *testing.T, plan *response.Plan) []byte {
	t.Helper()
	data, err := io.ReadAll(plan.Body)
	finishErr := plan.Finish(int64(len(data)), err)
	if finishErr != nil {
		t.Fatal(finishErr)
	}
	return data
}

func call(t *testing.T, e *server.Engine, input server.Input) (int, []byte) {
	t.Helper()
	plan, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return plan.Status, readPlan(t, plan)
}

func capabilities(protocol owscore.Protocol, version string) server.Input {
	return server.Input{Method: "GET", Protocol: protocol, RawQuery: "SERVICE=" + string(protocol) + "&REQUEST=GetCapabilities&VERSION=" + version}
}

func golden(t *testing.T, name string, data []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, expected) {
		t.Fatalf("golden mismatch: %s; regenerate only after reviewing the protocol change", path)
	}
}

type trackedBody struct {
	io.Reader
	closes int
}

func (b *trackedBody) Close() error { b.closes++; return nil }

type cancelCloseBody struct {
	cancel        context.CancelFunc
	reads, closes int
}

func (b *cancelCloseBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }

func (b *cancelCloseBody) Close() error { b.closes++; b.cancel(); return nil }
