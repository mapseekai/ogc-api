package server_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/server"
)

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
