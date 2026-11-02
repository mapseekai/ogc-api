package response_test

import (
	"testing"

	"github.com/mapseekai/ogc-api/response"
)

func TestContractValidation(t *testing.T) {
	for _, media := range []string{"image/png\r\nX-Bad: yes", "broken", "text/plain"} {
		t.Run(media, func(t *testing.T) {
			p := response.Bytes(media, nil)
			if p.Validate("image/png") == nil {
				t.Fatal("bad content type accepted", media)
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}

		})
	}
}

func TestResultValidation(t *testing.T) {
	for _, state := range []response.State{response.NotModified, response.NoContent} {
		p := response.Payload{State: state}
		if err := p.ValidateResult("image/png"); err != nil {
			t.Fatal(err)
		}
		if p.Validate("image/png") == nil {
			t.Fatal("legacy validation accepted bodyless state")
		}
		p.ContentType = "text/plain"
		if p.ValidateResult("image/png") == nil {
			t.Fatal("wrong result media accepted")
		}
		p.ContentType = "image/png"
		if err := p.ValidateResult("image/png"); err != nil {
			t.Fatal(err)
		}
		p.Length = 1
		if p.ValidateResult("image/png") == nil {
			t.Fatal("bodyless length accepted")
		}
	}
	for _, p := range []response.Payload{
		{}, {State: response.State(3)},
		{State: response.NoContent, Vary: "Accept\r\nX: yes"},
		{State: response.NotModified, Vary: "invalid field"},
		{State: response.NoContent, ContentEncoding: "gzip;"},
		{State: response.NotModified, Cache: response.Cache{ETag: "bad\n"}},
	} {
		if p.ValidateResult("") == nil {
			t.Fatalf("invalid result accepted: %#v", p)
		}
	}
	p := response.Bytes("image/png", nil)
	p.State = response.NoContent
	defer p.Close()
	if p.ValidateResult("") == nil {
		t.Fatal("body on bodyless result accepted")
	}
}
