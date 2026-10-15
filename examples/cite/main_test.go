package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCiteBindingsAndPixelInfo(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	picture.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	host := httptest.NewUnstartedServer(nil)
	provider := rasterProvider{"Lakes": {picture: picture, world: [6]float64{1, 0, 0, -0.5, -127.5, 63.75}}}
	handler, err := citeHandler("http://"+host.Listener.Addr().String(), provider, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	host.Config.Handler = handler
	host.Start()
	t.Cleanup(host.Close)
	for _, path := range []string{
		"/wmts/1.0.0/WMTSCapabilities.xml",
		"/tiles/TransparentNodata/Default_style/TransparentNodata/0/0/0.png",
		"/info/TransparentNodata/Default_style/TransparentNodata/0/0/0/0/0.json",
		"/samples/default/2020-01-01/LimitedGrid/0/0/0.png",
		"/sampleinfo/default/current/LimitedGrid/0/0/0/0/0.json",
		"/processapi/input",
		"/wms11?SERVICE=WMS&REQUEST=GetCapabilities&VERSION=1.1.1",
		"/wms?SERVICE=WMS&REQUEST=GetCapabilities&VERSION=1.3.0&UPDATESEQUENCE=1",
	} {
		t.Run(path, func(t *testing.T) {
			reply, err := host.Client().Get(host.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(reply.Body)
			if closeErr := reply.Body.Close(); err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
			if reply.StatusCode != http.StatusOK {
				t.Fatal(reply.StatusCode, string(body))
			}
			if strings.HasSuffix(path, ".json") && string(body) != "[65535,0,0,65535]" {
				t.Fatal("pixel information did not describe actual fixture", string(body))
			}
			if strings.Contains(path, "WMTSCapabilities") && !strings.Contains(string(body), "ServiceMetadataURL") {
				t.Fatal("REST capabilities binding was not advertised")
			}
			if strings.Contains(path, "UPDATESEQUENCE") && !strings.Contains(string(body), "CurrentUpdateSequence") {
				t.Fatal("fixture revision comparator was not applied", string(body))
			}
			if path == "/processapi/input" && string(body) != `"reference sample"` {
				t.Fatal("reference resource differs from resolved value", string(body))
			}
		})
	}
	for _, tc := range []struct {
		path, contains string
		status         int
	}{
		{"/samples/default/2020-01-01/LimitedGrid/0/1/0.png", "TileOutOfRange", 404},
		{"/samples/default/invalid/LimitedGrid/0/0/0.png", "InvalidParameterValue", 404},
		{"/wmts?SERVICE=WMTS&REQUEST=GetFeatureInfo&VERSION=1.0.0&LAYER=NoInfo&STYLE=default&FORMAT=image/png&TILEMATRIXSET=TransparentNodata&TILEMATRIX=0&TILEROW=0&TILECOL=0&I=0&J=0&INFOFORMAT=application/json", "OperationNotSupported", 501},
		{"/wmts?SERVICE=WMTS&REQUEST=GetCapabilities&SECTIONS=Contents", "<Contents>", 200},
	} {
		reply, err := host.Client().Get(host.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reply.Body)
		reply.Body.Close()
		if err != nil || reply.StatusCode != tc.status || !strings.Contains(string(body), tc.contains) {
			t.Fatalf("%s: %d %s %v", tc.path, reply.StatusCode, body, err)
		}
		if strings.Contains(tc.path, "SECTIONS=") && strings.Contains(string(body), "ServiceMetadataURL") {
			t.Fatal("unrequested section returned")
		}
	}
	reply, err := host.Client().Post(host.URL+"/processapi/processes/echo/execution", "application/json", strings.NewReader(`{"inputs":{"message":{"href":"`+host.URL+`/processapi/input"}},"response":"document"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer reply.Body.Close()
	body, err := io.ReadAll(reply.Body)
	if err != nil || reply.StatusCode != http.StatusOK || !strings.Contains(string(body), `"message":"reference sample"`) {
		t.Fatalf("reference execution: %d %s %v", reply.StatusCode, body, err)
	}
}
