// Command citefeatures is a test-only HTTP host with real in-memory CRS operations.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
)

type fixtureIdentity struct {
	Nonce    string `json:"nonce"`
	PID      int    `json:"pid"`
	BaseURL  string `json:"base_url"`
	Revision string `json:"source_revision"`
	Modified bool   `json:"source_modified"`
}

func main() {
	address := os.Getenv("OGC_CITE_ADDR")
	if address == "" {
		address = "127.0.0.1:18090"
	}
	if err := runFixture(address, os.Getenv("OGC_CITE_NONCE"), os.Getenv("OGC_CITE_READY_FILE")); err != nil {
		log.Fatal(err)
	}
}

func runFixture(address, nonce, readyPath string) error {
	if (nonce == "") != (readyPath == "") || nonce != "" && (len(nonce) < 32 || len(nonce) > 128) {
		return fmt.Errorf("fixture readiness requires a path and a 32-128 byte per-run nonce")
	}
	identity := fixtureIdentity{Nonce: nonce, PID: os.Getpid()}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				identity.Revision = setting.Value
			}
			if setting.Key == "vcs.modified" {
				identity.Modified = setting.Value == "true"
			}
		}
	}
	server, listener, identity, err := newFixtureServer(address, identity)
	if err != nil {
		return err
	}
	defer listener.Close()
	if readyPath != "" {
		if err := writeFixtureIdentity(readyPath, identity); err != nil {
			return err
		}
	}
	log.Printf("local CITE fixture: %s and %s/three-dimensional; verified test backend asserts Part2 only", identity.BaseURL, identity.BaseURL)
	return server.Serve(listener)
}

func newFixtureServer(address string, identity fixtureIdentity) (*http.Server, net.Listener, fixtureIdentity, error) {
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	number, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || portErr != nil || number < 0 || number > 65535 {
		return nil, nil, identity, fmt.Errorf("fixture listener must use a literal loopback IP and numeric port")
	}
	// The listener is acquired before metadata/readiness is published. Port zero
	// asks the OS for an exclusively owned port, without a free-port TOCTOU gap.
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, identity, err
	}
	identity.BaseURL = "http://" + listener.Addr().String()
	handler, err := newHandler(identity.BaseURL)
	if err != nil {
		listener.Close()
		return nil, nil, identity, err
	}
	service := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ogc-cite-ready" && identity.Nonce != "" {
			if r.Method != http.MethodGet || r.Header.Get("X-OGC-CITE-Nonce") != identity.Nonce {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", common.JSON)
			if err := json.NewEncoder(w).Encode(identity); err != nil {
				log.Printf("readiness response: %v", err)
			}
			return
		}
		handler.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: service, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second}
	return server, listener, identity, nil
}

func writeFixtureIdentity(path string, identity fixtureIdentity) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".cite-ready-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encodeErr := json.NewEncoder(file).Encode(identity)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}

func newHandler(base string) (http.Handler, error) {
	datasets, err := twoDimensionalDatasets()
	if err != nil {
		return nil, err
	}
	engine, err := fixtureEngine(base, datasets)
	if err != nil {
		return nil, err
	}
	threeDimensional, err := fixtureEngine(base+"/three-dimensional", threeDimensionalDatasets())
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		selected := engine
		if r.URL.Path == "/three-dimensional" || strings.HasPrefix(r.URL.Path, "/three-dimensional/") {
			selected = threeDimensional
		}
		plan, err := selected.Handle(r.Context(), api.Input{Method: r.Method, EscapedPath: r.URL.EscapedPath(), RawQuery: r.URL.RawQuery, Accept: r.Header.Get("Accept"), IfNoneMatch: r.Header.Get("If-None-Match"), Scope: "official-ets-local-fixture"})
		if err != nil {
			http.Error(w, "fixture host failure", 500)
			log.Printf("host error: %v", err)
			return
		}
		var n int64
		var sent error
		defer func() {
			if err := plan.Finish(n, sent); err != nil {
				log.Printf("finish: %v", err)
			}
		}()
		for name, value := range plan.Headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(plan.Status)
		if r.Method != "HEAD" {
			n, sent = io.Copy(w, plan.Body)
		}
		log.Printf("HTTP %s %s status=%d bytes=%d elapsed=%s error=%v", r.Method, r.URL.RequestURI(), plan.Status, n, time.Since(start), sent)
	}), nil
}

func fixtureEngine(base string, datasets []fixtureDataset) (*api.Engine, error) {
	catalog := api.Catalog{Title: "Part 2 coordinate operations test fixture", Description: "Test backend only: native-axis WGS84 and genuine spherical Mercator conversions; no certification claim", BaseURL: base, ConformsTo: []string{features.CRSConformance}}
	for _, dataset := range datasets {
		catalog.Collections = append(catalog.Collections, api.Collection{
			Metadata: common.Collection{ID: dataset.id, Title: dataset.title, StorageCRS: dataset.storage.URI,
				Extent: &common.Extent{Spatial: &common.SpatialExtent{CRS: dataset.extentCRS, BBox: [][]float64{dataset.extent}}}},
			Features: &features.Service{Reader: fixtureReader{dataset: dataset}, Options: features.Options{DefaultLimit: 10, MaxLimit: 1000, FilterProperties: []string{"name"},
				CRS: &features.CRSOptions{Supported: dataset.supported, DefaultCRS: dataset.defaultCRS}, Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{
					"name": {Type: cql2.String}, "nullable": {Type: cql2.String}, "population": {Type: cql2.Integer}, "active": {Type: cql2.Boolean}, "observedAt": {Type: cql2.Timestamp},
				}}}},
		})
	}
	snapshot, err := api.NewSnapshot(catalog)
	if err != nil {
		return nil, err
	}
	return api.New(api.Config{Catalog: api.Static(snapshot), Timeout: 20 * time.Second})
}
