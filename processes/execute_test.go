package processes_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/processes"
)

func TestMetadataEncodingCannotMutateProcessCardinality(t *testing.T) {
	service := processes.Service{
		Description: processes.Description{ID: "cardinality", Version: "1.0.0", Inputs: map[string]processes.Input{"message": {Schema: json.RawMessage(`{"type":"string"}`)}}},
		Validate:    func(context.Context, processes.Execute) error { return nil },
		Executor:    processes.ExecutorFunc(func(context.Context, processes.Execute) (processes.Results, error) { return nil, nil }),
	}
	input := service.Description.Inputs["message"]
	input.MaxOccurs = json.Number("18446744073709551615")
	service.Description.Inputs["message"] = input
	prepared, err := service.Prepare(4096)
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared.Description.Inputs["message"].MaxOccurs; got != json.Number("18446744073709551615") {
		t.Fatalf("integer lost precision: %v", got)
	}
}

func TestNullProtocolOptionsRejected(t *testing.T) {
	for _, raw := range []string{`{"outputs":{"a":{"transmissionMode":null}}}`, `{"outputs":{"a":{"format":{"mediaType":null}}}}`, `{"outputs":{"a":{"format":{"encoding":null}}}}`} {
		t.Run(raw, func(t *testing.T) {
			_, err := processes.Decode(context.Background(), strings.NewReader(raw), 4096)
			if err == nil {
				t.Errorf("explicit null silently became an absent string option: %s", raw)
			}

		})
	}
}

func TestProtocolOptionTypesAndBusinessNull(t *testing.T) {
	for _, body := range []string{
		`{"response":""}`, `{"response":null}`, `{"response":3}`,
		`{"outputs":{"a":{"transmissionMode":""}}}`, `{"outputs":{"a":{"transmissionMode":3}}}`,
		`{"outputs":{"a":{"format":{"mediaType":{}}}}}`, `{"outputs":{"a":{"format":{"encoding":[]}}}}`,
		`{"outputs":{"a":{"format":{"schema":null}}}}`, `{"outputs":{"a":{"format":{"schema":true}}}}`,
		`{"outputs":{"a":{"format":{"MediaType":"text/plain"}}}}`,
	} {
		t.Run("invalid_"+body, func(t *testing.T) {
			if _, err := processes.Decode(context.Background(), strings.NewReader(body), 4096); err == nil {
				t.Fatalf("invalid protocol option accepted: %s", body)
			}

		})
	}
	for _, body := range []string{`{}`, `{"inputs":{"nullable":null}}`, `{"outputs":{"a":{"format":{"schema":{}}}}}`, `{"outputs":{"a":{"format":{"schema":"https://example.test/schema"}}}}`} {
		t.Run("valid_"+body, func(t *testing.T) {
			if _, err := processes.Decode(context.Background(), strings.NewReader(body), 4096); err != nil {
				t.Fatalf("valid protocol document rejected: %s: %v", body, err)
			}

		})
	}
}

func TestDecodeRejectsAmbiguityAndTruncation(t *testing.T) {
	for _, value := range []string{
		`{"response":"raw","response":"document"}`,
		`{"inputs":{"a":1,"a":2}}`,
		`{"mode":"async"}`, `{"Response":"document"}`, `{"response":null}`,
		`{"outputs":{"a":{"TransmissionMode":"value"}}}`, `{"subscriber":{"successUri":"https://example.test"}}`,
		`{} {}`, `null`, `[]`, `{"inputs":`,
		strings.Repeat(`{"a":`, 70) + `1` + strings.Repeat(`}`, 70),
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := processes.Decode(context.Background(), strings.NewReader(value), 4096); err == nil {
				t.Fatalf("accepted ambiguous request %s", value)
			}

		})
	}
	if _, err := processes.Decode(context.Background(), strings.NewReader(`{"inputs":{"a":"12345"}}`), 10); err == nil {
		t.Fatal("body limit ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := processes.Decode(ctx, strings.NewReader(`{}`), 1024); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func FuzzDecode(f *testing.F) {
	for _, value := range []string{`{}`, `{"inputs":{"a":1},"response":"document"}`, `{"response":"raw","response":"document"}`} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		_, _ = processes.Decode(context.Background(), strings.NewReader(value), 4096)
	})
}
