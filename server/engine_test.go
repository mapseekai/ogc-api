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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mapseekai/ogc-api/adaptertest"
	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/exception"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

func makeConfig(t *testing.T) server.Config {
	t.Helper()
	config, err := demo.Config("https://public.example.test")
	if err != nil {
		t.Fatal(err)
	}
	return config
}
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

func TestCapabilitiesAndExceptionsGolden(t *testing.T) {
	e := engine(t, makeConfig(t))
	for _, version := range wms.Versions() {
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

func TestOptionalOperationsNotAdvertised(t *testing.T) {
	config := makeConfig(t)
	config.Handlers.GetWMSFeatureInfo = nil
	config.Handlers.GetWMTSFeatureInfo = nil
	e := engine(t, config)
	for _, protocol := range []owscore.Protocol{owscore.WMS, owscore.WMTS} {
		version := "1.3.0"
		if protocol == owscore.WMTS {
			version = "1.0.0"
		}
		_, data := call(t, e, capabilities(protocol, version))
		if bytes.Contains(data, []byte("GetFeatureInfo")) || bytes.Contains(data, []byte("InfoFormat")) || bytes.Contains(data, []byte(`queryable="1"`)) || bytes.Contains(data, []byte(`resourceType="FeatureInfo"`)) {
			t.Fatal("unregistered operation was advertised", string(data))
		}
	}
}

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
		input := capabilities(tc.protocol, tc.version)
		input.RawQuery += "&UPDATESEQUENCE=" + tc.sequence
		_, data := call(t, e, input)
		if !bytes.Contains(data, []byte(tc.expected)) {
			t.Fatal(string(data))
		}
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

func TestEncodedCapabilitiesValidation(t *testing.T) {
	base := makeConfig(t)
	_, good := call(t, engine(t, base), capabilities(owscore.WMS, "1.3.0"))
	for _, tc := range []struct {
		name  string
		data  []byte
		limit int
		want  int
	}{
		{"valid", good, 0, 200},
		{"wrong root", []byte(`<Capabilities version="1.3.0"/>`), 0, 500},
		{"wrong version", bytes.ReplaceAll(good, []byte(`version="1.3.0"`), []byte(`version="1.1.1"`)), 0, 500},
		{"trailing document", append(bytes.Clone(good), []byte(`<extra/>`)...), 0, 500},
		{"entity", []byte(`<!DOCTYPE x [<!ENTITY secret SYSTEM "file:///private">]><x/>`), 0, 500},
		{"too large", good, 100, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := makeConfig(t)
			config.CapabilitiesMode = server.Encoded
			config.Limits.MaxCapabilitiesBytes = tc.limit
			config.EncodedCapabilities = func(_ context.Context, r *server.CapabilitiesRequest) (response.Payload, error) {
				if r.WMS == nil || r.Version != "1.3.0" {
					t.Error("missing typed capabilities request")
				}
				return response.Bytes("text/xml", tc.data), nil
			}
			status, data := call(t, engine(t, config), capabilities(owscore.WMS, "1.3.0"))
			if status != tc.want {
				t.Fatal(status, string(data))
			}
		})
	}
}

func TestTrustedURLsAndPrefix(t *testing.T) {
	for _, target := range []string{"/wms", "https://user:password@example.test/wms", "https://example.test/wms?token=secret", "https://example.test/wms?SERVICE=WMS"} {
		config := makeConfig(t)
		config.WMSURL = target
		if _, err := server.New(config); err == nil {
			t.Fatal("unsafe public URL accepted")
		}
	}
	config, err := demo.Config("https://public.example.test/deploy/prefix")
	if err != nil {
		t.Fatal(err)
	}
	e := engine(t, config)
	status, data := call(t, e, server.Input{Method: "GET", Protocol: owscore.WMTS, Binding: owscore.REST, EscapedPath: "/deploy/prefix/tiles/world/default/local/L00/0/0.png"})
	if status != 200 {
		t.Fatal(status, string(data))
	}
	_, data = call(t, e, capabilities(owscore.WMTS, "1.0.0"))
	if !bytes.Contains(data, []byte("https://public.example.test/deploy/prefix/tiles/")) {
		t.Fatal("deployment prefix was lost")
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
		call(t, e, server.Input{Method: "GET", Protocol: owscore.WMS, RawQuery: adaptertest.MapQuery(version)})
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
		status, data := call(t, e, capabilities(owscore.WMS, version))
		if status != 200 {
			t.Fatal(status, string(data))
		}
		golden(t, "wms_"+version+"_dimensions.xml", data)
	}
	status, data := call(t, e, capabilities(owscore.WMTS, "1.0.0"))
	if status != 200 {
		t.Fatal(status, string(data))
	}
	golden(t, "wmts_1.0.0_dimensions.xml", data)
}
