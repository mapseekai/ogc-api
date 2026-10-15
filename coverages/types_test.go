package coverages_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/response"
)

func TestMetadataPreparation(t *testing.T) {
	service := coverages.Service{Formats: []string{"application/json"}, Reader: coverages.ReaderFunc(func(context.Context, coverages.Request) (response.Payload, error) { return response.Payload{}, nil })}
	for _, tc := range []struct {
		domain, fields string
		valid          bool
	}{
		{"", "", true}, {`{"type":"DomainSet"}`, `{"type":"DataRecord"}`, true},
		{`{"type":"DomainSet"}`, "", false}, {"", `{"type":"DataRecord"}`, false},
		{`null`, `{}`, false}, {`[]`, `{}`, false}, {`{}`, `{}`, false}, {`broken`, `{}`, false},
	} {
		service.DomainSet, service.RangeType = json.RawMessage(tc.domain), json.RawMessage(tc.fields)
		prepared, err := service.Prepare()
		if (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
		if tc.valid && len(service.DomainSet) > 0 {
			service.DomainSet[0], service.RangeType[0] = 'X', 'X'
			if !json.Valid(prepared.DomainSet) || !json.Valid(prepared.RangeType) {
				t.Fatal("publication shares metadata storage")
			}
		}
	}
}
