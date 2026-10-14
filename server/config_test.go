package server_test

import (
	"bytes"
	"testing"

	"github.com/mapseekai/ogc-api/examples/demo"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/server"
)

func TestAdmissionValidation(t *testing.T) {
	config := makeConfig(t)
	config.MaxConcurrent = -1
	if _, err := server.New(config); err == nil {
		t.Fatal("accepted negative concurrency")
	}
}

func TestTrustedURLsAndPrefix(t *testing.T) {
	for _, target := range []string{"/wms", "https://user:password@example.test/wms", "https://example.test/wms?token=secret", "https://example.test/wms?SERVICE=WMS"} {
		t.Run(target, func(t *testing.T) {
			config := makeConfig(t)
			config.WMSURL = target
			if _, err := server.New(config); err == nil {
				t.Fatal("unsafe public URL accepted")
			}

		})
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

func makeConfig(t *testing.T) server.Config {
	t.Helper()
	config, err := demo.Config("https://public.example.test")
	if err != nil {
		t.Fatal(err)
	}
	return config
}
