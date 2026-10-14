package processes_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

type auditPanickingCloser struct {
	io.Reader
	closed *int
}

func (b *auditPanickingCloser) Close() error { *b.closed++; panic("foreign close panic") }
func TestAuditAllOutputClosersAttemptedOnPanic(t *testing.T) {
	closed := 0
	completed := 0
	values := processes.Results{
		"a": {Content: response.Stream("text/plain", &auditPanickingCloser{Reader: strings.NewReader("a"), closed: &closed}, 1)},
		"b": {Content: response.Stream("text/plain", &auditPanickingCloser{Reader: strings.NewReader("b"), closed: &closed}, 1)},
	}
	plan, err := processes.EncodeResults(context.Background(), request("raw", map[string]string{"a": "value", "b": "value"}), values, 1024)
	if err != nil {
		t.Fatal(err)
	}
	plan.BindLifetime(func() { completed++ }, nil)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected Close panic")
			}
		}()
		_ = plan.Finish(0, nil)
	}()
	if completed != 1 {
		t.Errorf("lifetime not released: %d", completed)
	}
	if closed != 2 {
		t.Errorf("only %d/2 transferred output closers attempted; lifetime cleanup=%d", closed, completed)
	}
}
func TestAuditNullProtocolOptionsRejected(t *testing.T) {
	for _, raw := range []string{`{"outputs":{"a":{"transmissionMode":null}}}`, `{"outputs":{"a":{"format":{"mediaType":null}}}}`, `{"outputs":{"a":{"format":{"encoding":null}}}}`} {
		_, err := processes.Decode(context.Background(), strings.NewReader(raw), 4096)
		if err == nil {
			t.Errorf("explicit null silently became an absent string option: %s", raw)
		}
	}
}
