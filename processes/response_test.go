package processes_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

func TestAllOutputClosersAttemptedOnPanic(t *testing.T) {
	closed := 0
	completed := 0
	values := processes.Results{
		"a": {Content: response.Stream("text/plain", &panickingCloser{Reader: strings.NewReader("a"), closed: &closed}, 1)},
		"b": {Content: response.Stream("text/plain", &panickingCloser{Reader: strings.NewReader("b"), closed: &closed}, 1)},
	}
	plan, err := processes.EncodeResults(context.Background(), request("raw", map[string]string{"a": "value", "b": "value"}), values, 1024)
	if err != nil {
		t.Fatal(err)
	}
	plan.BindLifetime(func() { completed++ }, nil)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected Close panic")
			}
		}()
		_ = plan.Finish(0, nil)
	}()
	if completed != 1 {
		t.Errorf("lifetime not released: %d", completed)
	}
	if closed != 2 {
		t.Errorf("only %d/2 transferred output closers attempted; lifetime cleanup=%d", closed, completed)
	}
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
		t.Run(fmt.Sprintf("budget_%d", limit), func(t *testing.T) {
			plan, err := processes.EncodeResults(context.Background(), selection, makeResults(), limit)
			if plan != nil {
				_ = plan.Finish(0, err)
			}
			if (err == nil) != (limit == len(encoded)) {
				t.Fatalf("incorrect encoded document budget %d: %v", limit, err)
			}

		})
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

func TestRawAndDocumentResults(t *testing.T) {
	raw := json.RawMessage(`{"value":42}`)
	for _, mode := range []string{"raw", "document"} {
		t.Run(mode, func(t *testing.T) {
			plan, err := processes.EncodeResults(context.Background(), request(mode, map[string]string{"answer": "value"}), processes.Results{"answer": {JSON: raw}}, 1024)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(plan.Body)
			if finish := plan.Finish(int64(len(data)), err); finish != nil {
				t.Fatal(finish)
			}
			if mode == "raw" && !bytes.Equal(data, raw) {
				t.Fatal(string(data))
			}
			if mode == "document" {
				var result map[string]struct {
					Value     json.RawMessage `json:"value"`
					MediaType string          `json:"mediaType"`
				}
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(result["answer"].Value, raw) || result["answer"].MediaType != common.JSON {
					t.Fatal(string(data))
				}
			}

		})
	}
}

func TestReferenceResult(t *testing.T) {
	plan, err := processes.EncodeResults(context.Background(), request("raw", map[string]string{"answer": "reference"}), processes.Results{"answer": {Reference: &processes.Reference{Href: "https://storage.test/results/a?signature=redacted", Type: "image/tiff"}}}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != 204 || !strings.Contains(plan.Headers["Link"], "storage.test/results") {
		t.Fatal(plan.Status, plan.Headers)
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMultipartResultsAreLazyAndOwned(t *testing.T) {
	body := &countedBody{Reader: strings.NewReader("raster")}
	outputs := processes.Results{
		"a": {Content: response.Stream("image/tiff", body, 6)},
		"b": {JSON: json.RawMessage(`42`)},
		"c": {Reference: &processes.Reference{Href: "https://storage.test/result", Type: "image/tiff"}},
	}
	plan, err := processes.EncodeResults(context.Background(), request("raw", map[string]string{"a": "value", "b": "value", "c": "reference"}), outputs, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if body.reads != 0 || body.closes != 0 {
		t.Fatal("multipart body consumed during construction")
	}
	media, parameters, err := mime.ParseMediaType(plan.Headers["Content-Type"])
	if err != nil || media != "multipart/related" {
		t.Fatal(media, err)
	}
	reader := multipart.NewReader(plan.Body, parameters["boundary"])
	for _, id := range []string{"a", "b", "c"} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		if part.Header.Get("Content-ID") != id {
			t.Fatal(part.Header)
		}
		if id == "a" && string(data) != "raster" || id == "b" && string(data) != "42" {
			t.Fatal(string(data))
		}
		if id == "c" && (len(data) != 0 || part.Header.Get("Content-Location") != "https://storage.test/result") {
			t.Fatal(part.Header, string(data))
		}
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatal(err)
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if body.closes != 1 {
		t.Fatal("multipart body not closed exactly once")
	}
}

func TestProcessResultErrorsReleaseOwnership(t *testing.T) {
	for _, malformed := range []string{"wrong output", "oversized document", "wrong format", "two representations"} {
		t.Run(malformed, func(t *testing.T) {
			body := &countedBody{Reader: strings.NewReader("abcdef")}
			result := processes.Results{"a": {Content: response.Stream("text/plain", body, 6)}}
			selection := request("document", map[string]string{"a": "value"})
			limit := 1024
			switch malformed {
			case "wrong output":
				selection.Outputs = map[string]processes.Output{"b": {TransmissionMode: "value"}}
			case "oversized document":
				limit = 2
			case "wrong format":
				selection.Outputs["a"] = processes.Output{TransmissionMode: "value", Format: &processes.Format{MediaType: "image/tiff"}}
			case "two representations":
				value := result["a"]
				value.JSON = json.RawMessage(`1`)
				result["a"] = value
			}
			plan, err := processes.EncodeResults(context.Background(), selection, result, limit)
			if err == nil {
				if plan != nil {
					_ = plan.Finish(0, nil)
				}
				t.Fatal("malformed result accepted")
			}
			if body.closes != 1 {
				t.Fatal("result content leaked")
			}
		})
	}
}

type panickingCloser struct {
	io.Reader
	closed *int
}

func (b *panickingCloser) Close() error { *b.closed++; panic("foreign close panic") }

type countedBody struct {
	io.Reader
	reads, closes int
}

func (b *countedBody) Read(data []byte) (int, error) { b.reads++; return b.Reader.Read(data) }

func (b *countedBody) Close() error { b.closes++; return nil }

func request(mode string, selection map[string]string) processes.Execute {
	outputs := make(map[string]processes.Output, len(selection))
	for name, transmission := range selection {
		outputs[name] = processes.Output{TransmissionMode: transmission}
	}
	return processes.Execute{Response: mode, Outputs: outputs}
}
