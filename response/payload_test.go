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
