package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

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
