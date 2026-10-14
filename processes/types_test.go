package processes_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/response"
)

func TestResultCloseRetainsAllOrdinaryErrors(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	counts := [2]int{}
	results := processes.Results{
		"a": {Content: response.Stream("text/plain", &closeAction{Reader: strings.NewReader(""), calls: &counts[0], failure: first}, 0)},
		"b": {Content: response.Stream("text/plain", &closeAction{Reader: strings.NewReader(""), calls: &counts[1], failure: second}, 0)},
	}
	if err := results.Close(); !errors.Is(err, first) || !errors.Is(err, second) || counts != [2]int{1, 1} {
		t.Fatalf("lost close errors or leaked body: %v %v", err, counts)
	}
}

type closeAction struct {
	io.Reader
	calls      *int
	failure    error
	panicValue any
}

func (b *closeAction) Close() error {
	*b.calls++
	if b.panicValue != nil {
		panic(b.panicValue)
	}
	return b.failure
}
