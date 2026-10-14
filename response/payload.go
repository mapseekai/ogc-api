// Package response owns content returned by application callbacks.
package response

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"sync"
	"time"
)

const UnknownLength int64 = -1

// Payload transfers ownership to the engine on return from a callback.
// Use Bytes or Stream so copied payload values share the same close guard.
type Payload struct {
	Body            io.ReadCloser
	ContentType     string
	ContentEncoding string
	Length          int64
	Cache           Cache
}

// Bytes wraps a slice without copying it and records its exact length.
// After transfer, the slice must remain immutable until the response finishes.
// Read-only static slices may be shared between responses.
func Bytes(contentType string, data []byte) Payload {
	return Stream(contentType, io.NopCloser(bytes.NewReader(data)), int64(len(data)))
}

// Stream never buffers the reader. Length is -1 for unknown, and 0 for empty.
// The caller must not read, close or mutate the content after transferring it.
func Stream(contentType string, body io.ReadCloser, length int64) Payload {
	if body != nil {
		body = &onceCloser{ReadCloser: body}
	}
	return Payload{Body: body, ContentType: contentType, Length: length}
}

// Validate rejects contract errors before HTTP headers are committed.
func (p Payload) Validate(expected string) error {
	if p.Body == nil || p.Length < UnknownLength {
		return errors.New("invalid response body or length")
	}
	for _, value := range []string{p.ContentType, p.ContentEncoding, p.Cache.ETag, p.Cache.CacheControl} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("invalid response header value")
		}
	}
	media, params, err := mime.ParseMediaType(p.ContentType)
	if err != nil || !strings.Contains(media, "/") {
		return errors.New("invalid response content type")
	}
	if expected != "" {
		wanted, requestedParams, err := mime.ParseMediaType(expected)
		if err != nil || media != wanted {
			return fmt.Errorf("provider content type does not match requested format")
		}
		for k, v := range requestedParams {
			if params[k] != v {
				return fmt.Errorf("provider content-type parameter %s does not match", k)
			}
		}
	}
	for _, encoding := range strings.Split(p.ContentEncoding, ",") {
		encoding = strings.TrimSpace(encoding)
		for _, c := range encoding {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
				return errors.New("invalid content encoding")
			}
		}
	}
	return nil
}

// Close releases the body if present. Bytes and Stream make this idempotent across payload copies.
// Closing does not make arbitrary underlying readers safe for concurrent use.
func (p Payload) Close() error {
	if p.Body == nil {
		return nil
	}
	return p.Body.Close()
}

// Cache holds optional HTTP cache metadata, not cached content or conditional-request logic.
type Cache struct {
	ETag         string
	LastModified time.Time
	CacheControl string
}

type onceCloser struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (c *onceCloser) Close() error {
	c.once.Do(func() { c.err = c.ReadCloser.Close() })
	return c.err
}
