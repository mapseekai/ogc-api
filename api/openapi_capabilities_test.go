package api_test

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

func TestProcessOpenAPIResponseKindsMatchExecution(t *testing.T) {
	for _, kind := range []string{"string", "array", "object", "number", "boolean", "null", "binary", "multipart", "reference"} {
		t.Run(kind, func(t *testing.T) {
			service := processes.Service{Description: processes.Description{ID: "output", Version: "1.0.0", OutputTransmission: []string{"value", "reference"}, Outputs: map[string]processes.OutputDescription{"result": {Schema: json.RawMessage(`{}`)}}}, Validate: func(context.Context, processes.Execute) error { return nil }}
			if kind == "multipart" {
				service.Description.Outputs["other"] = processes.OutputDescription{Schema: json.RawMessage(`{}`)}
			}
			service.Executor = processes.ExecutorFunc(func(context.Context, processes.Execute) (processes.Results, error) {
				switch kind {
				case "binary":
					return processes.Results{"result": {Content: response.Bytes("image/tiff", []byte("raster"))}}, nil
				case "multipart":
					return processes.Results{"result": {Content: response.Bytes("image/tiff", []byte("raster"))}, "other": {JSON: json.RawMessage(`42`)}}, nil
				case "reference":
					return processes.Results{"result": {Reference: &processes.Reference{Href: "https://storage.example.test/result", Type: common.JSON}}}, nil
				default:
					values := map[string]string{"string": `"ready"`, "array": `[1,2]`, "object": `{"a":1}`, "number": `42`, "boolean": `true`, "null": `null`}
					return processes.Results{"result": {JSON: json.RawMessage(values[kind])}}, nil
				}
			})
			c := catalog(&memoryFeatures{}, tileReader("tile"))
			c.Processes = []processes.Service{service}
			c.Jobs = &processes.Jobs{Reader: &jobService{}}
			e := engine(t, c, 8)
			_, encoded, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/api"})
			var document map[string]any
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			paths := document["paths"].(map[string]any)
			for _, path := range []string{"/processes/{processId}/execution", "/jobs/{jobId}/results"} {
				method := "get"
				if strings.HasPrefix(path, "/processes") {
					method = "post"
				}
				responses := paths[path].(map[string]any)[method].(map[string]any)["responses"].(map[string]any)
				content := responses["200"].(map[string]any)["content"].(map[string]any)
				if schema := content[common.JSON].(map[string]any)["schema"].(map[string]any); len(schema) != 0 {
					t.Fatal("raw JSON was constrained to one kind", schema)
				}
				for _, media := range []string{"application/octet-stream", "multipart/related", "*/*"} {
					schema := content[media].(map[string]any)["schema"].(map[string]any)
					if schema["type"] != "string" || schema["format"] != "binary" {
						t.Fatal("invalid binary schema", media, schema)
					}
				}
				noContent, ok := responses["204"].(map[string]any)
				if !ok || noContent["content"] != nil || noContent["headers"] == nil {
					t.Fatal("invalid 204 contract", responses)
				}
			}
			body := `{}`
			if kind == "reference" {
				body = `{"outputs":{"result":{"transmissionMode":"reference"}}}`
			}
			status, actual, headers := read(t, e, api.Input{Method: "POST", EscapedPath: "/ogc/processes/output/execution", ContentType: common.JSON, Body: strings.NewReader(body)})
			switch kind {
			case "reference":
				if status != 204 || len(actual) != 0 || headers["Link"] == "" {
					t.Fatal(status, string(actual), headers)
				}
			case "binary":
				if status != 200 || headers["Content-Type"] != "image/tiff" || string(actual) != "raster" {
					t.Fatal(status, string(actual), headers)
				}
			case "multipart":
				media, params, err := mime.ParseMediaType(headers["Content-Type"])
				if err != nil || media != "multipart/related" {
					t.Fatal(media, err)
				}
				reader := multipart.NewReader(strings.NewReader(string(actual)), params["boundary"])
				count := 0
				for {
					part, err := reader.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if _, err := io.Copy(io.Discard, part); err != nil {
						t.Fatal(err)
					}
					count++
				}
				if status != 200 || count != 2 {
					t.Fatal(status, count)
				}
			default:
				if status != 200 || !json.Valid(actual) {
					t.Fatal(status, string(actual))
				}
			}
		})
	}
}
