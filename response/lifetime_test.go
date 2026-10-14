package response_test

import (
	"context"
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
	if engineFinished != 1 || hostFinished != 1 || ctx.Err() != context.Canceled {
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
	if engineFinished != 1 || ctx.Err() != context.Canceled {
		t.Fatal("engine lifetime was skipped")
	}
}
