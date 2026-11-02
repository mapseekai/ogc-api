package response_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/response"
)

func TestResultPlans(t *testing.T) {
	for _, tt := range []struct {
		name      string
		state     response.State
		condition string
		allow     bool
		status    int
	}{
		{"unconditional", response.Content, "", false, 200},
		{"mismatch", response.Content, `"other"`, false, 200},
		{"strong", response.Content, `"tag"`, false, 304},
		{"weak", response.Content, `W/"tag"`, false, 304},
		{"wildcard", response.Content, "*", false, 304},
		{"backend", response.NotModified, `"tag"`, false, 304},
		{"unsolicited", response.NotModified, "", false, 0},
		{"empty disabled", response.NoContent, "", false, 0},
		{"empty allowed", response.NoContent, "", true, 204},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &countedBody{Reader: strings.NewReader("tile")}
			payload := response.Payload{State: tt.state}
			if tt.state == response.Content {
				payload = response.Stream("image/png", body, 4)
			}
			payload.ContentEncoding = "gzip"
			payload.Cache = response.Cache{ETag: `"tag"`, CacheControl: "private"}
			payload.Vary = "Authorization, Accept-Encoding"
			plan, err := response.NewResult(payload, response.Conditions{IfNoneMatch: tt.condition}, tt.allow)
			if tt.status == 0 {
				if err == nil {
					t.Fatal("invalid result accepted")
				}
				_ = payload.Close()
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plan.Status != tt.status || plan.Headers["ETag"] != `"tag"` || plan.Headers["Cache-Control"] != "private" || plan.Headers["Vary"] != payload.Vary {
				t.Fatalf("plan: %#v", plan)
			}
			if tt.status != 200 {
				if _, ok := plan.Headers["Content-Length"]; ok {
					t.Fatal("bodyless content length")
				}
				if _, ok := plan.Headers["Content-Type"]; ok {
					t.Fatal("bodyless content type")
				}
				data, err := io.ReadAll(plan.Body)
				if err != nil || len(data) != 0 || plan.Length != 0 {
					t.Fatalf("body %q, %v", data, err)
				}
			} else if body.closes != 0 {
				t.Fatal("stream closed before delivery")
			}
			if tt.status != 204 && plan.Headers["Content-Encoding"] != "gzip" {
				t.Fatal("coding lost")
			}
			_ = plan.Finish(0, nil)
			_ = plan.Finish(0, nil)
			_ = payload.Close()
			if tt.state == response.Content && body.closes != 1 {
				t.Fatalf("closes %d", body.closes)
			}
		})
	}
}

func TestSharedConditionsAndVary(t *testing.T) {
	for _, tt := range []struct {
		raw, tag string
		match    bool
	}{
		{"", `"tag"`, false}, {"*", "", true},
		{`"other", W/"a,b"`, `"a,b"`, true},
		{`"tag"`, `W/"tag"`, true},
		{`tag`, `"tag"`, false}, {`"tag"junk`, `"tag"`, false},
	} {
		if response.MatchesETag(tt.raw, tt.tag) != tt.match {
			t.Fatalf("match %q %q", tt.raw, tt.tag)
		}
	}
	if got := response.MergeVary("Authorization, X-API-Key", "Accept-Encoding, authorization", "Accept"); got != "Authorization, X-API-Key, Accept-Encoding, Accept" {
		t.Fatal(got)
	}
	if got := response.MergeVary("Accept", "*, Authorization"); got != "*" {
		t.Fatal(got)
	}
}

func TestConditionalCloseFailureRetainsCleanup(t *testing.T) {
	failure := errors.New("close failure")
	body := &countedBody{Reader: strings.NewReader("tile"), closeErr: failure}
	payload := response.Stream("image/png", body, 4)
	plan, err := response.NewResult(payload, response.Conditions{IfNoneMatch: "*"}, false)
	if plan != nil || !errors.Is(err, failure) {
		t.Fatalf("plan %v, error %v", plan, err)
	}
	if err := payload.Close(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if body.closes != 1 {
		t.Fatalf("closes %d", body.closes)
	}
}

func TestHostCallbackCannotReplaceEngineLifetime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	plan := response.New(200, response.Bytes("application/json", []byte(`{}`)))
	engineFinished, hostFinished := 0, 0
	plan.BindLifetime(cancel, func(int64, error) { engineFinished++ })
	plan.OnFinish = func(int64, error) { hostFinished++ }
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if engineFinished != 1 || hostFinished != 1 || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("engine=%d host=%d context=%v", engineFinished, hostFinished, ctx.Err())
	}
}

func TestHostCallbackPanicStillFinishesEngineLifetime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	plan := response.New(200, response.Bytes("application/json", nil))
	engineFinished := 0
	plan.BindLifetime(cancel, func(int64, error) { engineFinished++ })
	plan.OnFinish = func(int64, error) { panic("host callback") }
	func() {
		defer func() {
			if recover() == nil {
				t.Error("host panic swallowed")
			}
		}()
		_ = plan.Finish(0, nil)
	}()
	if engineFinished != 1 || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("engine lifetime was skipped")
	}
}

func TestOwnershipAndLength(t *testing.T) {
	body := &countedBody{Reader: strings.NewReader("tile")}
	payload := response.Stream("image/png", body, 4)
	plan := response.New(200, payload)
	if err := payload.Validate("image/png"); err != nil {
		t.Fatal(err)
	}
	if err := plan.Finish(4, nil); err != nil {
		t.Fatal(err)
	}
	if err := plan.Finish(4, nil); err != nil {
		t.Fatal(err)
	}
	if err := payload.Close(); err != nil {
		t.Fatal(err)
	}
	if body.closes != 1 {
		t.Fatal(body.closes)
	}
	for _, length := range []int64{-1, 0, 4} {
		t.Run(fmt.Sprintf("length_%d", length), func(t *testing.T) {
			p := response.New(200, response.Stream("image/png", io.NopCloser(strings.NewReader("")), length))
			_, exists := p.Headers["Content-Length"]
			if exists != (length >= 0) {
				t.Fatal("unknown and zero length were confused")
			}
			if err := p.Finish(0, nil); err != nil {
				t.Fatal(err)
			}

		})
	}
}

func TestShortStreamAndCancellation(t *testing.T) {
	r := response.Reader{Context: context.Background(), Source: strings.NewReader("a"), Length: 2}
	if _, err := io.Copy(io.Discard, &r); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = response.Reader{Context: ctx, Source: strings.NewReader("a"), Length: -1}
	if _, err := io.Copy(io.Discard, &r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func BenchmarkStreamMemory(b *testing.B) {
	for _, size := range []int64{1 << 20, 64 << 20} {
		b.Run(strconv.FormatInt(size, 10), func(b *testing.B) {
			b.SetBytes(size)
			for b.Loop() {
				payload := response.Stream("image/png", io.NopCloser(io.LimitReader(generatedReader{}, size)), size)
				plan := response.New(200, payload)
				n, err := io.Copy(io.Discard, plan.Body)
				if finishErr := plan.Finish(n, err); finishErr != nil {
					b.Fatal(finishErr)
				}
			}
		})
	}
}

type countedBody struct {
	io.Reader
	closes   int
	closeErr error
}

func (b *countedBody) Close() error { b.closes++; return b.closeErr }

type generatedReader struct{}

func (generatedReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
