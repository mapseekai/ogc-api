package processes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/internal/jsonutil"
	"github.com/mapseekai/ogc-api/response"
)

// EncodeResults takes ownership of every result and returns a raw, document or multipart
// response plan. On error it closes all content. Documents are bounded by maxDocumentBytes;
// raw output streams and multipart bodies are never fully buffered or read in a goroutine.
// Request must have been normalized against the process description before execution.
func EncodeResults(ctx context.Context, request Execute, results Results, maxDocumentBytes int) (plan *response.Plan, err error) {
	transferred := false
	defer func() {
		if !transferred {
			err = errors.Join(err, results.Close())
		}
	}()
	if maxDocumentBytes <= 0 {
		return nil, fmt.Errorf("invalid process document limit")
	}
	if err := checkResultMetadata(results, maxDocumentBytes); err != nil {
		return nil, err
	}
	if err := checkResults(request, results); err != nil {
		return nil, err
	}
	if request.Response == "document" {
		document, err := resultDocument(ctx, results, maxDocumentBytes)
		if err != nil {
			return nil, err
		}
		return response.New(200, response.Bytes(common.JSON, document)), nil
	}
	names := make([]string, 0, len(results))
	allReferences := true
	for name, value := range results {
		names = append(names, name)
		allReferences = allReferences && value.Reference != nil
	}
	slices.Sort(names)
	if allReferences {
		plan := response.New(204, response.Bytes(common.JSON, nil))
		delete(plan.Headers, "Content-Type")
		delete(plan.Headers, "Content-Length")
		links := make([]string, 0, len(names))
		for _, name := range names {
			reference := results[name].Reference
			link := "<" + reference.Href + ">; rel=\"item\"; title=" + strconv.Quote(name)
			if reference.Type != "" {
				link += "; type=" + strconv.Quote(reference.Type)
			}
			links = append(links, link)
		}
		if len(links) > 0 {
			plan.Headers["Link"] = strings.Join(links, ", ")
		}
		return plan, nil
	}
	if len(names) == 1 {
		value := results[names[0]]
		if value.Content.Body != nil {
			transferred = true
			return response.New(200, value.Content), nil
		}
		return response.New(200, response.Bytes(common.JSON, value.JSON)), nil
	}
	content, err := multipartResults(ctx, results, names)
	if err != nil {
		return nil, err
	}
	transferred = true
	return response.New(200, content), nil
}

func checkResults(request Execute, results Results) error {
	if request.Response != "raw" && request.Response != "document" {
		return fmt.Errorf("unnormalized execution response mode")
	}
	if len(results) != len(request.Outputs) {
		return fmt.Errorf("process returned an inconsistent output set")
	}
	for name, value := range results {
		output, ok := request.Outputs[name]
		if !ok || !safeID(name) {
			return fmt.Errorf("process returned an undeclared output")
		}
		choices := 0
		if value.JSON != nil {
			choices++
		}
		if value.Content.Body != nil {
			choices++
		}
		if value.Reference != nil {
			choices++
		}
		if choices != 1 {
			return fmt.Errorf("process output must contain exactly one representation")
		}
		if output.TransmissionMode == "reference" && value.Reference == nil || output.TransmissionMode == "value" && value.Reference != nil {
			return fmt.Errorf("process output transmission differs from request")
		}
		expected := ""
		if output.Format != nil {
			expected = output.Format.MediaType
		}
		if value.Content.Body != nil {
			if err := value.Content.Validate(expected); err != nil {
				return err
			}
			if output.Format != nil && output.Format.Encoding != "" && output.Format.Encoding != value.Content.ContentEncoding {
				return fmt.Errorf("process output encoding differs from request")
			}
		}
		if value.JSON != nil {
			if output.Format != nil && output.Format.Encoding != "" {
				return fmt.Errorf("inline JSON cannot satisfy an encoded output request")
			}
			if !json.Valid(value.JSON) {
				return fmt.Errorf("process returned invalid JSON")
			}
			if expected != "" {
				media, _, err := mime.ParseMediaType(expected)
				if err != nil || media != common.JSON {
					return fmt.Errorf("JSON process output differs from requested media type")
				}
			}
		}
		if value.Reference != nil {
			if err := checkReference(*value.Reference); err != nil {
				return err
			}
			if expected != "" && value.Reference.Type != expected {
				return fmt.Errorf("process reference type differs from request")
			}
		}
	}
	return nil
}

func checkReference(reference Reference) error {
	parsed, err := url.Parse(reference.Href)
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.User != nil || strings.ContainsAny(reference.Href, "\r\n\x00<>") {
		return fmt.Errorf("process returned an invalid public result reference")
	}
	if reference.Type != "" {
		if _, _, err := mime.ParseMediaType(reference.Type); err != nil || strings.ContainsAny(reference.Type, "\r\n\x00") {
			return fmt.Errorf("process returned an invalid reference media type")
		}
	}
	return nil
}

func resultDocument(ctx context.Context, results Results, maxBytes int) ([]byte, error) {
	document := make(map[string]any, len(results))
	remaining := int64(maxBytes)
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		value := results[name]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if value.Reference != nil {
			remaining -= int64(len(name) + len(value.Reference.Href) + len(value.Reference.Type) + len(value.Reference.Title))
			if remaining < 0 {
				return nil, fmt.Errorf("process document exceeds limit")
			}
			document[name] = value.Reference
			continue
		}
		data := value.JSON
		media := common.JSON
		encoding := ""
		if value.Content.Body != nil {
			media, encoding = value.Content.ContentType, value.Content.ContentEncoding
			if value.Content.Length > remaining {
				return nil, fmt.Errorf("process document exceeds limit")
			}
			reader := &response.Reader{Context: ctx, Source: value.Content.Body, Length: value.Content.Length}
			var err error
			data, err = io.ReadAll(io.LimitReader(reader, remaining+1))
			if err != nil {
				return nil, err
			}
		}
		remaining -= int64(len(data) + len(name))
		if remaining < 0 {
			return nil, fmt.Errorf("process document exceeds limit")
		}
		mediaType, _, err := mime.ParseMediaType(media)
		if err != nil {
			return nil, err
		}
		if encoding == "" && (mediaType == common.JSON || strings.HasSuffix(mediaType, "+json")) {
			if !json.Valid(data) {
				return nil, fmt.Errorf("process output is not valid JSON")
			}
			raw := json.RawMessage(data)
			trimmed := bytes.TrimSpace(data)
			if len(trimmed) > 0 && trimmed[0] == '{' {
				document[name] = map[string]any{"value": raw, "mediaType": media}
			} else {
				document[name] = raw
			}
		} else {
			// Binary document output is explicitly qualified, never converted using an unsafe string cast.
			if encoding != "" {
				return nil, fmt.Errorf("encoded binary output requires raw response or an external reference")
			}
			document[name] = map[string]any{"value": []byte(data), "mediaType": media, "encoding": "base64"}
		}
	}
	data, err := jsonutil.Marshal(document, maxBytes)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func multipartResults(ctx context.Context, results Results, names []string) (response.Payload, error) {
	var header bytes.Buffer
	writer := multipart.NewWriter(&header)
	readers := make([]io.Reader, 0, 2*len(names)+1)
	firstType := ""
	for _, name := range names {
		value := results[name]
		media := common.JSON
		fields := make(textproto.MIMEHeader)
		fields.Set("Content-ID", name)
		switch {
		case value.Reference != nil:
			if value.Reference.Type != "" {
				media = value.Reference.Type
			}
			fields.Set("Content-Location", value.Reference.Href)
		case value.Content.Body != nil:
			media = value.Content.ContentType
			if value.Content.ContentEncoding != "" {
				fields.Set("Content-Encoding", value.Content.ContentEncoding)
			}
		}
		if firstType == "" {
			firstType = media
		}
		fields.Set("Content-Type", media)
		if _, err := writer.CreatePart(fields); err != nil {
			return response.Payload{}, err
		}
		readers = append(readers, bytes.NewReader(bytes.Clone(header.Bytes())))
		header.Reset()
		switch {
		case value.Content.Body != nil:
			// Preserve per-part length checking; the outer multipart length remains unknown.
			readers = append(readers, &response.Reader{Context: ctx, Source: value.Content.Body, Length: value.Content.Length})
		case value.JSON != nil:
			readers = append(readers, bytes.NewReader(value.JSON))
		}
	}
	if err := writer.Close(); err != nil {
		return response.Payload{}, err
	}
	readers = append(readers, bytes.NewReader(bytes.Clone(header.Bytes())))
	media := mime.FormatMediaType("multipart/related", map[string]string{"boundary": writer.Boundary(), "type": firstType})
	content := &resultBody{Reader: io.MultiReader(readers...), results: results}
	return response.Stream(media, content, response.UnknownLength), nil
}

type resultBody struct {
	io.Reader
	results Results
}

func (b *resultBody) Close() error { return b.results.Close() }

// Bound variable-sized response metadata before parsing URLs or building headers.
// Raw bodies remain streaming and are not charged to this metadata budget.
func checkResultMetadata(results Results, maximum int) error {
	if len(results) > 256 {
		return fmt.Errorf("too many process outputs")
	}
	budget := jsonutil.Budget(maximum)
	for name, value := range results {
		if err := budget.String(name); err != nil {
			return err
		}
		if value.Reference != nil {
			for _, text := range []string{value.Reference.Href, value.Reference.Type, value.Reference.Title} {
				if err := budget.String(text); err != nil {
					return err
				}
			}
		}
		if value.Content.Body != nil {
			for _, text := range []string{value.Content.ContentType, value.Content.ContentEncoding, value.Content.Cache.ETag, value.Content.Cache.CacheControl} {
				if err := budget.String(text); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
