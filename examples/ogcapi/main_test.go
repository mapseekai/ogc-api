package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/server"
)

func TestAdvertisedRESTBindingsAreMounted(t *testing.T) {
	host := exampleServer(t)
	status, data, _ := fetch(t, host, "GET", "/wmts?SERVICE=WMTS&REQUEST=GetCapabilities", "")
	if status != 200 || !strings.Contains(string(data), "ResourceURL") {
		t.Fatal("missing fixture metadata")
	}
	for _, path := range []string{"/tiles/world/default/local/L00/0/0.png", "/wmts/1.0.0/WMTSCapabilities.xml"} {
		t.Run(path, func(t *testing.T) {
			status, data, _ := fetch(t, host, "GET", path, "")
			if status != 200 {
				t.Errorf("advertised binding %s returned %d: %s", path, status, data)
			}

		})
	}
}

func TestOpenAPIAllowsActualRawJSONResult(t *testing.T) {
	host := exampleServer(t)
	_, data, _ := fetch(t, host, "GET", "/ogc/api", "")
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	schema := document["paths"].(map[string]any)["/processes/{processId}/execution"].(map[string]any)["post"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	status, actual, _ := fetch(t, host, "POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"ready"}}`)
	var result any
	if err := json.Unmarshal(actual, &result); err != nil {
		t.Fatal(err)
	}
	if _, object := result.(map[string]any); status == 200 && schema["type"] == "object" && !object {
		t.Errorf("OpenAPI declares object but actual successful raw result=%s", actual)
	}
}

func TestPooledHTTPConcurrency(t *testing.T) {
	const requests, workers = 2000, 64
	host := exampleServer(t)
	path := "/ogc/collections/world/tiles/local/10m/1/1"
	status, expected, _ := fetch(t, host, "GET", path, "")
	if status != http.StatusOK {
		t.Fatal("warm-up failed")
	}
	transport := &http.Transport{MaxConnsPerHost: workers, MaxIdleConnsPerHost: workers, MaxIdleConns: workers, DisableCompression: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	tasks := make(chan int, requests)
	for i := 0; i < requests; i++ {
		tasks <- i
	}
	close(tasks)
	type observation struct {
		elapsed time.Duration
		err     error
	}
	measurements := make(chan observation, requests)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range tasks {
				began := time.Now()
				reply, err := client.Get(host.URL + path)
				if err != nil {
					measurements <- observation{time.Since(began), err}
					continue
				}
				data, readErr := io.ReadAll(reply.Body)
				closeErr := reply.Body.Close()
				if readErr != nil {
					err = readErr
				} else if closeErr != nil {
					err = closeErr
				} else if reply.StatusCode != http.StatusOK || !bytes.Equal(data, expected) {
					err = fmt.Errorf("invalid tile response: status=%d bytes=%d", reply.StatusCode, len(data))
				}
				measurements <- observation{time.Since(began), err}
			}
		}()
	}
	began := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(began)
	close(measurements)
	durations := make([]int64, 0, requests)
	failures := 0
	for measurement := range measurements {
		durations = append(durations, measurement.elapsed.Nanoseconds())
		if measurement.err != nil {
			failures++
			if failures <= 3 {
				t.Error(measurement.err)
			}
		}
	}
	slices.Sort(durations)
	percentile := func(p int) float64 { return float64(durations[(len(durations)-1)*p/100]) / 1e6 }
	report := struct {
		Date              string  `json:"date"`
		Go                string  `json:"go"`
		Platform          string  `json:"platform"`
		Requests          int     `json:"requests"`
		Workers           int     `json:"workers"`
		Failures          int     `json:"failures"`
		BodyBytes         int     `json:"response_body_bytes"`
		ElapsedMS         float64 `json:"elapsed_ms"`
		RequestsPerSecond float64 `json:"requests_per_second"`
		P50MS             float64 `json:"p50_ms"`
		P95MS             float64 `json:"p95_ms"`
		P99MS             float64 `json:"p99_ms"`
		Scope             string  `json:"scope"`
	}{time.Now().UTC().Format(time.RFC3339), runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH, requests, workers, failures, len(expected), float64(elapsed.Nanoseconds()) / 1e6, float64(requests) / elapsed.Seconds(), percentile(50), percentile(95), percentile(99), "loopback HTTP/1.1; 64 pooled clients; fixed in-memory PNG provider; not a GIS workload or 2000 open connections"}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(encoded))
	if destination := os.Getenv("OGC_HTTP_REPORT"); destination != "" {
		if err := os.WriteFile(destination, append(encoded, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if len(durations) != requests || failures != 0 {
		t.Fatalf("requests=%d failures=%d", len(durations), failures)
	}
}

func TestCombinedExampleRealTCP(t *testing.T) {
	host := exampleServer(t)
	status, data, _ := fetch(t, host, "GET", "/ogc/", "")
	if status != 200 || !json.Valid(data) {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "GET", "/ogc/collections/world/items?bbox=-1,-1,1,1", "")
	if status != 200 || !strings.Contains(string(data), `"numberReturned":1`) {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "GET", "/ogc/collections/world/items?bbox=10,10,20,20", "")
	if status != 200 || !strings.Contains(string(data), `"numberReturned":0`) {
		t.Fatal(status, string(data))
	}
	status, tile, _ := fetch(t, host, "GET", "/ogc/collections/world/tiles/local/10m/1/1", "")
	if status != 200 {
		t.Fatal(status, string(tile))
	}
	dimensions, err := png.DecodeConfig(bytes.NewReader(tile))
	if err != nil {
		t.Fatal(err)
	}
	if dimensions.Width != 512 || dimensions.Height != 128 {
		t.Fatal(dimensions)
	}
	status, legacy, _ := fetch(t, host, "GET", "/wmts?SERVICE=WMTS&VERSION=1.0.0&REQUEST=GetTile&LAYER=world&STYLE=default&FORMAT=image/png&TILEMATRIXSET=local&TILEMATRIX=10m&TILEROW=1&TILECOL=1", "")
	if status != 200 || !bytes.Equal(tile, legacy) {
		t.Fatal("WMTS and OGC API Tiles did not share the provider", status)
	}
	status, data, headers := fetch(t, host, "GET", "/ogc/collections/world/map?bbox=-10,-5,10,5&width=100&height=50", "")
	if status != 200 || headers.Get("Content-Bbox") != "-10,-5,10,5" {
		t.Fatal(status, headers, string(data))
	}
	dimensions, err = png.DecodeConfig(bytes.NewReader(data))
	if err != nil || dimensions.Width != 100 || dimensions.Height != 50 {
		t.Fatal(dimensions, err)
	}
	status, data, _ = fetch(t, host, "GET", "/ogc/collections/world/coverage?subset=x(0:1)", "")
	if status != 200 || !json.Valid(data) {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"ready"},"response":"document"}`)
	if status != 200 || string(data) != `{"result":"ready"}` {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "HEAD", "/ogc/collections/world/items", "")
	if status != 200 || len(data) != 0 {
		t.Fatal(status, string(data))
	}
	status, data, _ = fetch(t, host, "GET", "/ogc/api", "")
	if status != 200 {
		t.Fatal(status, string(data))
	}
	var description struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(data, &description); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/collections/{collectionId}/items", "/collections/{collectionId}/map", "/collections/{collectionId}/coverage", "/processes/{processId}/execution"} {
		if _, ok := description.Paths[path]; !ok {
			t.Fatal("missing OpenAPI path", path)
		}
	}
}

func TestPublicExampleCORS(t *testing.T) {
	host := exampleServer(t)
	for _, method := range []string{"GET", "OPTIONS"} {
		request, err := http.NewRequest(method, host.URL+"/ogc/map", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("Access-Control-Request-Method", "GET")
		reply, err := host.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if reply.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatal(reply.Header)
		}
		if method == "OPTIONS" && (reply.StatusCode != 204 || reply.Header.Get("Access-Control-Allow-Methods") == "") {
			t.Fatal(reply.StatusCode, reply.Header)
		}
		if err := reply.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func exampleServer(t *testing.T) *httptest.Server {
	t.Helper()
	host := httptest.NewUnstartedServer(nil)
	snapshot, config, err := publications("http://" + host.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	apiEngine, err := api.New(api.Config{Catalog: api.Static(snapshot)})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := server.New(config)
	if err != nil {
		t.Fatal(err)
	}
	host.Config.Handler = combinedRouter(apiEngine, legacy)
	host.Start()
	t.Cleanup(host.Close)
	return host
}

func fetch(t *testing.T, host *httptest.Server, method, path, body string) (int, []byte, http.Header) {
	t.Helper()
	request, err := http.NewRequest(method, host.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", common.JSON)
	}
	reply, err := host.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reply.Body)
	closeErr := reply.Body.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return reply.StatusCode, data, reply.Header
}
