// Package jsonutil decodes bounded, unambiguous JSON protocol documents.
package jsonutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/mapseekai/ogc-api/response"
)

// ErrTooLarge identifies a document that exceeds the configured body budget.
var ErrTooLarge = errors.New("JSON document exceeds limit")

// Decode reads one JSON object within limit and depth bounds. It rejects duplicate
// object keys, unknown struct fields, trailing documents and malformed UTF-8.
// The caller retains ownership of source and must provide transport read deadlines.
func Decode(ctx context.Context, source io.Reader, limit int64, value any) error {
	if source == nil || limit <= 0 || limit == int64(^uint64(0)>>1) {
		return fmt.Errorf("invalid JSON source or limit")
	}
	reader := &response.Reader{Context: ctx, Source: source, Length: response.UnknownLength}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return ErrTooLarge
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid JSON encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scan(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON document")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("JSON object required")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func scan(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]bool)
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || keys[key] {
				return fmt.Errorf("duplicate or invalid JSON object key")
			}
			keys[key] = true
			if err := scan(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scan(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
