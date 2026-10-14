package owscore_test

import (
	"testing"

	"github.com/mapseekai/ogc-api/owscore"
)

func TestTypedNilPublicErrorIsInternalFailure(t *testing.T) {
	var cause *owscore.Error
	result := owscore.PublicError(cause)
	if result == nil || result.Kind != owscore.Internal {
		t.Fatalf("invalid public failure: %v", result)
	}
}
