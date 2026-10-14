package processes_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

type closeAction struct {
	io.Reader
	calls      *int
	failure    error
	panicValue any
}

func (b *closeAction) Close() error {
	*b.calls++
	if b.panicValue != nil {
		panic(b.panicValue)
	}
	return b.failure
}

func TestAllOutputsCloseAcrossFailurePaths(t *testing.T) {
	for _, mode := range []string{"raw", "document", "invalid"} {
		for panicIndex := 0; panicIndex < 3; panicIndex++ {
			counts := [3]int{}
			marker := errors.New("close interrupted")
			results := processes.Results{}
			selection := map[string]string{}
			for i, name := range []string{"a", "b", "c"} {
				body := &closeAction{Reader: strings.NewReader("x"), calls: &counts[i]}
				if i == panicIndex {
					body.panicValue = marker
				}
				results[name] = processes.Value{Content: response.Stream("text/plain", body, 1)}
				selection[name] = "value"
			}
			func() {
				defer func() {
					if got := recover(); got != marker {
						t.Errorf("mode=%s panicIndex=%d: changed panic %v", mode, panicIndex, got)
					}
				}()
				plan, err := processes.EncodeResults(context.Background(), request(mode, selection), results, 4096)
				if plan != nil {
					_ = plan.Finish(0, err)
				}
			}()
			if counts != [3]int{1, 1, 1} {
				t.Fatalf("mode=%s panicIndex=%d: incomplete cleanup %v", mode, panicIndex, counts)
			}
			if err := results.Close(); err != nil {
				t.Fatal(err)
			}
			if counts != [3]int{1, 1, 1} {
				t.Fatal("duplicate cleanup")
			}
		}
	}
}

func TestResultCloseRetainsAllOrdinaryErrors(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	counts := [2]int{}
	results := processes.Results{
		"a": {Content: response.Stream("text/plain", &closeAction{Reader: strings.NewReader(""), calls: &counts[0], failure: first}, 0)},
		"b": {Content: response.Stream("text/plain", &closeAction{Reader: strings.NewReader(""), calls: &counts[1], failure: second}, 0)},
	}
	if err := results.Close(); !errors.Is(err, first) || !errors.Is(err, second) || counts != [2]int{1, 1} {
		t.Fatalf("lost close errors or leaked body: %v %v", err, counts)
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
		if _, err := processes.Decode(context.Background(), strings.NewReader(body), 4096); err == nil {
			t.Fatalf("invalid protocol option accepted: %s", body)
		}
	}
	for _, body := range []string{`{}`, `{"inputs":{"nullable":null}}`, `{"outputs":{"a":{"format":{"schema":{}}}}}`, `{"outputs":{"a":{"format":{"schema":"https://example.test/schema"}}}}`} {
		if _, err := processes.Decode(context.Background(), strings.NewReader(body), 4096); err != nil {
			t.Fatalf("valid protocol document rejected: %s: %v", body, err)
		}
	}
}

func TestDocumentBudgetIncludesBase64Expansion(t *testing.T) {
	data := []byte("binary result")
	selection := request("document", map[string]string{"a": "value"})
	makeResults := func() processes.Results {
		return processes.Results{"a": {Content: response.Bytes("application/octet-stream", data)}}
	}
	plan, err := processes.EncodeResults(context.Background(), selection, makeResults(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(plan.Body)
	if err := plan.Finish(int64(len(encoded)), err); err != nil {
		t.Fatal(err)
	}
	var document map[string]struct{ Value, Encoding string }
	if err := json.Unmarshal(encoded, &document); err != nil || document["a"].Value != base64.StdEncoding.EncodeToString(data) || document["a"].Encoding != "base64" {
		t.Fatalf("incorrect binary document: %s %v", encoded, err)
	}
	for _, limit := range []int{len(encoded) - 1, len(encoded)} {
		plan, err := processes.EncodeResults(context.Background(), selection, makeResults(), limit)
		if plan != nil {
			_ = plan.Finish(0, err)
		}
		if (err == nil) != (limit == len(encoded)) {
			t.Fatalf("incorrect encoded document budget %d: %v", limit, err)
		}
	}
}

func TestResultEnvelopeIsBoundedAndClosed(t *testing.T) {
	body := &countedBody{Reader: strings.NewReader("x")}
	results := processes.Results{"a": {Content: response.Stream("text/plain", body, 1)}, "b": {Reference: &processes.Reference{Href: "https://example.test/" + strings.Repeat("x", 1<<20)}}}
	plan, err := processes.EncodeResults(context.Background(), request("raw", map[string]string{"a": "value", "b": "reference"}), results, 1024)
	if plan != nil {
		_ = plan.Finish(0, err)
	}
	if err == nil || body.closes != 1 || body.reads != 0 {
		t.Fatalf("envelope limit failed: %v closes=%d reads=%d", err, body.closes, body.reads)
	}
}

func TestInlineJSONDoesNotIgnoreRequestedEncoding(t *testing.T) {
	selection := request("raw", map[string]string{"a": "value"})
	selection.Outputs["a"] = processes.Output{TransmissionMode: "value", Format: &processes.Format{MediaType: "application/json", Encoding: "gzip"}}
	plan, err := processes.EncodeResults(context.Background(), selection, processes.Results{"a": {JSON: json.RawMessage(`42`)}}, 1024)
	if plan != nil {
		_ = plan.Finish(0, err)
	}
	if err == nil {
		t.Fatal("requested encoding was silently ignored")
	}
}
