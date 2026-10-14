package processes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

type countedBody struct {
	io.Reader
	reads, closes int
}

func (b *countedBody) Read(data []byte) (int, error) { b.reads++; return b.Reader.Read(data) }
func (b *countedBody) Close() error                  { b.closes++; return nil }

func request(mode string, selection map[string]string) processes.Execute {
	outputs := make(map[string]processes.Output, len(selection))
	for name, transmission := range selection {
		outputs[name] = processes.Output{TransmissionMode: transmission}
	}
	return processes.Execute{Response: mode, Outputs: outputs}
}

func TestRawAndDocumentResults(t *testing.T) {
	raw := json.RawMessage(`{"value":42}`)
	for _, mode := range []string{"raw", "document"} {
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

func TestDecodeRejectsAmbiguityAndTruncation(t *testing.T) {
	for _, value := range []string{
		`{"response":"raw","response":"document"}`,
		`{"inputs":{"a":1,"a":2}}`,
		`{"mode":"async"}`, `{"Response":"document"}`, `{"response":null}`,
		`{"outputs":{"a":{"TransmissionMode":"value"}}}`, `{"subscriber":{"successUri":"https://example.test"}}`,
		`{} {}`, `null`, `[]`, `{"inputs":`,
		strings.Repeat(`{"a":`, 70) + `1` + strings.Repeat(`}`, 70),
	} {
		if _, err := processes.Decode(context.Background(), strings.NewReader(value), 4096); err == nil {
			t.Fatalf("accepted ambiguous request %s", value)
		}
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
