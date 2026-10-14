package jsonutil_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mapseekai/ogc-api/internal/jsonutil"
)

type pointerEncoder struct{ Called *bool }

func (e *pointerEncoder) MarshalJSON() ([]byte, error) {
	*e.Called = true
	return []byte(`"unexpected"`), nil
}

func TestMarshalBudgetMatchesNativeJSON(t *testing.T) {
	timestamp := time.Date(2026, 10, 14, 0, 0, 0, 123, time.UTC)
	values := []any{
		nil, true, false, 0, int64(-123), uint64(math.MaxUint64), 0.125, 1e-9,
		"", "\"\\\b\f\n\r\t<>&中文\u2028\u2029", json.Number("123.25"),
		[]byte{0, 1, 2, 3}, []byte{}, []byte(nil), []string{}, []string(nil),
		[2]string{"a", "b"}, map[string]any{"a": "b", "c": []any{42, nil}},
		map[string]any(nil), timestamp, &timestamp,
		json.RawMessage(`{"html":"<>&","n":1}`), json.RawMessage(`null`),
		struct {
			Name string `json:"name"`
			Skip string `json:"skip,omitempty"`
			Data []byte `json:"data,omitempty"`
		}{Name: "test"},
	}
	for _, value := range values {
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		got, err := jsonutil.Marshal(value, len(want))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%T: exact budget returned %s, %v; want %s", value, got, err, want)
		}
		if _, err := jsonutil.Marshal(value, len(want)-1); !errors.Is(err, jsonutil.ErrTooLarge) {
			t.Errorf("%T: undersized budget returned %v", value, err)
		}
	}
}

func TestBoundedEncodingDoesNotInvokeCustomMarshalers(t *testing.T) {
	called := false
	encoder := &pointerEncoder{Called: &called}
	for _, value := range []any{encoder, map[string]any{"encoder": encoder}, &struct{ Encoder pointerEncoder }{*encoder}} {
		if _, err := jsonutil.Marshal(value, 1024); err == nil || called {
			t.Fatalf("custom marshaler executed: %T, called=%t error=%v", value, called, err)
		}
	}
}

func TestBoundedEncodingRejectsOversizedGraphs(t *testing.T) {
	large := strings.Repeat("x", 4<<20)
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, value := range []any{large, []string{large}, map[string]string{large: "x"}, json.RawMessage(large), cycle} {
		if _, err := jsonutil.Marshal(value, 1024); err == nil {
			t.Fatalf("accepted oversized/cyclic value %T", value)
		}
	}
	for _, value := range []any{math.NaN(), math.Inf(1), make(chan int), map[int]string{1: "a"}} {
		if _, err := jsonutil.Marshal(value, 1024); err == nil {
			t.Fatalf("accepted unsupported value %T", value)
		}
	}
}

func FuzzStringBudget(f *testing.F) {
	for _, value := range []string{"", "normal", "<>&\u2028", "\xff", "\"\\\n"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 4096 {
			return
		}
		if !utf8.ValidString(value) {
			budget := jsonutil.Budget(1 << 20)
			if err := budget.String(value); err == nil {
				t.Fatal("invalid UTF-8 accepted")
			}
			return
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		budget := jsonutil.Budget(len(encoded))
		if err := budget.String(value); err != nil || budget != 0 {
			t.Fatalf("incorrect string budget: remaining=%d error=%v", budget, err)
		}
	})
}
