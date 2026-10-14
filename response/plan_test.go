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
	closes int
}

func (b *countedBody) Close() error { b.closes++; return nil }

type generatedReader struct{}

func (generatedReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
