package response

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Plan is owned by its transport after Engine.Handle returns successfully.
// The transport must call Finish on every path, including HEAD and send errors.
// Do not copy a Plan after first use; it contains a synchronization primitive.
type Plan struct {
	// ProtocolError is diagnostic only, separate from a transport failure.
	ProtocolError error
	Status        int
	Headers       map[string]string
	Body          io.ReadCloser
	Length        int64

	// OnFinish is an optional host callback; replacing it before Finish does not remove engine cleanup.
	OnFinish func(bytesRead int64, err error)
	lifetime func(int64, error)
	once     sync.Once
	err      error
}

// New takes ownership of a payload and creates structured response headers.
// It does not validate the payload; callers outside server.Engine must call Validate first.
// The returned plan must be finished even when its body is not sent.
func New(status int, payload Payload) *Plan {
	headers := map[string]string{"Content-Type": payload.ContentType}
	if payload.Vary != "" {
		headers["Vary"] = MergeVary(payload.Vary)
	}
	if payload.Length >= 0 {
		headers["Content-Length"] = strconv.FormatInt(payload.Length, 10)
	}
	if payload.ContentEncoding != "" {
		headers["Content-Encoding"] = payload.ContentEncoding
	}
	if payload.Cache.ETag != "" {
		headers["ETag"] = payload.Cache.ETag
	}
	if !payload.Cache.LastModified.IsZero() {
		headers["Last-Modified"] = payload.Cache.LastModified.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	}
	if payload.Cache.CacheControl != "" {
		headers["Cache-Control"] = payload.Cache.CacheControl
	}
	return &Plan{Status: status, Headers: headers, Body: payload.Body, Length: payload.Length}
}

// NewResult constructs a successful read response after authorization and host
// resource/version revalidation. It does not authorize requests. Ownership passes
// on success; on error the caller must close payload. A conditional hit closes the
// discarded content without reading it. NoContent requires an explicit host policy.
func NewResult(payload Payload, conditions Conditions, allowNoContent bool) (*Plan, error) {
	if err := payload.ValidateResult(""); err != nil {
		return nil, err
	}
	status := 200
	switch payload.State {
	case NotModified:
		if strings.TrimSpace(conditions.IfNoneMatch) == "" {
			return nil, errors.New("not-modified result without a conditional request")
		}
		status = 304
	case NoContent:
		if !allowNoContent {
			return nil, errors.New("no-content result is not allowed by this profile")
		}
		status = 204
	}
	plan := New(status, payload)
	if err := ApplyConditions(plan, conditions); err != nil {
		return nil, err
	}
	if status == 204 || status == 304 {
		if err := payload.Close(); err != nil {
			return nil, err
		}
		plan.Body = io.NopCloser(strings.NewReader(""))
		plan.Length = 0
		delete(plan.Headers, "Content-Length")
		delete(plan.Headers, "Content-Type")
		if status == 204 {
			delete(plan.Headers, "Content-Encoding")
		}
	}
	return plan, nil
}

// ApplyConditions evaluates a successful representation only after the caller has
// completed authorization and resource revalidation. On a hit it closes the
// discarded body and installs an empty reader without reading content. On error,
// the caller retains the plan and must finish it. Other statuses are unchanged.
func ApplyConditions(plan *Plan, conditions Conditions) error {
	if plan.Status != 200 || !MatchesETag(conditions.IfNoneMatch, plan.Headers["ETag"]) {
		return nil
	}
	if plan.Body != nil {
		if err := plan.Body.Close(); err != nil {
			return err
		}
	}
	plan.Body = io.NopCloser(strings.NewReader(""))
	plan.Length = 0
	plan.Status = 304
	delete(plan.Headers, "Content-Length")
	delete(plan.Headers, "Content-Type")
	return nil
}

// MergeVary combines case-insensitive field names without losing host dimensions.
// A wildcard supersedes all field names. Inputs must be valid Vary field values.
func MergeVary(values ...string) string {
	var fields []string
	seen := make(map[string]bool)
	for _, value := range values {
		for _, field := range strings.Split(value, ",") {
			field = strings.TrimSpace(field)
			if field == "*" {
				return "*"
			}
			key := strings.ToLower(field)
			if field != "" && !seen[key] {
				fields = append(fields, field)
				seen[key] = true
			}
		}
	}
	return strings.Join(fields, ", ")
}

// MatchesETag applies If-None-Match weak comparison to an existing representation.
// The wildcard matches existence even when the representation has no entity tag.
func MatchesETag(raw, etag string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "*" {
		return true
	}
	if raw == "" || etag == "" {
		return false
	}
	target := strings.TrimPrefix(etag, "W/")
	for raw != "" {
		raw = strings.TrimSpace(raw)
		raw = strings.TrimPrefix(raw, "W/")
		if len(raw) == 0 || raw[0] != '"' {
			return false
		}
		// Opaque tags may contain commas; backslashes do not escape quotation marks in ETags.
		end := strings.IndexByte(raw[1:], '"')
		if end < 0 {
			return false
		}
		end += 2
		candidate := raw[:end]
		tail := strings.TrimSpace(raw[end:])
		if tail != "" && tail[0] != ',' {
			return false
		}
		if candidate == target {
			return true
		}
		if tail == "" {
			return false
		}
		raw = tail[1:]
	}
	return false
}

// BindLifetime chains protected completion callbacks and cancels after they finish, even on panic.
// Host OnFinish callbacks cannot overwrite this resource lifetime.
// Call it before handing off a plan, never concurrently with Finish. Cancel must be non-nil.
func (p *Plan) BindLifetime(cancel context.CancelFunc, finished func(int64, error)) {
	previous := p.lifetime
	p.lifetime = func(n int64, err error) {
		defer cancel()
		if previous != nil {
			previous(n, err)
		}
		if finished != nil {
			finished(n, err)
		}
	}
}

// Finish closes Body and invokes OnFinish exactly once, retaining the first send/close result.
// Concurrent calls are serialized, but transports must finish reading before cleanup.
// Callbacks must not call Finish recursively; their panics propagate to the host.
func (p *Plan) Finish(bytesRead int64, sendErr error) error {
	p.once.Do(func() {
		p.err = sendErr
		// Lifetime cleanup also runs if a foreign body's Close implementation panics.
		defer func() {
			if p.lifetime != nil {
				defer p.lifetime(bytesRead, p.err)
			}
			if p.OnFinish != nil {
				p.OnFinish(bytesRead, p.err)
			}
		}()
		if p.Body != nil {
			p.err = errors.Join(p.err, p.Body.Close())
		}
	})
	return p.err
}

// Reader checks cancellation between reads and detects premature EOF. It cannot
// forcibly interrupt an arbitrary blocking Reader; the source must cooperate.
type Reader struct {
	Context   context.Context
	Source    io.Reader
	Length    int64
	ReadBytes int64
	Err       error
}

// Read checks Context before each read and detects premature EOF for a known length.
// It never reads beyond Length and cannot interrupt a source already blocked in Read.
// Calls must be serialized; Context and Source must be non-nil.
func (r *Reader) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	if err := r.Context.Err(); err != nil {
		r.Err = err
		return 0, err
	}
	if r.Length >= 0 {
		remaining := r.Length - r.ReadBytes
		if remaining == 0 {
			return 0, io.EOF
		}
		if int64(len(buf)) > remaining {
			buf = buf[:int(remaining)]
		}
	}
	n, err := r.Source.Read(buf)
	r.ReadBytes += int64(n)
	if err == io.EOF && r.Length >= 0 && r.ReadBytes < r.Length {
		err = io.ErrUnexpectedEOF
	}
	if err != nil && err != io.EOF {
		r.Err = err
	}
	return n, err
}

// Timeout uses the caller's cancellation when duration is zero.
func Timeout(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	if duration > 0 {
		return context.WithTimeout(parent, duration)
	}
	return context.WithCancel(parent)
}
