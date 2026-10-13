package response

import (
	"context"
	"errors"
	"io"
	"strconv"
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

	OnFinish func(bytesRead int64, err error)
	once     sync.Once
	err      error
}

// New takes ownership of a payload and creates structured response headers.
// It does not validate the payload; callers outside server.Engine must call Validate first.
// The returned plan must be finished even when its body is not sent.
func New(status int, payload Payload) *Plan {
	headers := map[string]string{"Content-Type": payload.ContentType}
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

// Finish closes Body and invokes OnFinish exactly once, retaining the first send/close result.
// Concurrent calls are serialized, but transports must finish reading before cleanup.
// Callbacks must not call Finish recursively; their panics propagate to the host.
func (p *Plan) Finish(bytesRead int64, sendErr error) error {
	p.once.Do(func() {
		p.err = sendErr
		if p.Body != nil {
			p.err = errors.Join(p.err, p.Body.Close())
		}
		if p.OnFinish != nil {
			p.OnFinish(bytesRead, p.err)
		}
	})
	return p.err
}

// BindLifetime chains completion callbacks and cancels after they finish, even on panic.
// Call it before handing off a plan, never concurrently with Finish. Cancel must be non-nil.
func (p *Plan) BindLifetime(cancel context.CancelFunc, finished func(int64, error)) {
	previous := p.OnFinish
	p.OnFinish = func(n int64, err error) {
		defer cancel()
		if previous != nil {
			previous(n, err)
		}
		if finished != nil {
			finished(n, err)
		}
	}
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
