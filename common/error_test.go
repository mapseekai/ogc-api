package common_test

import (
	"testing"

	"github.com/mapseekai/ogc-api/common"
)

func TestTypedNilPublicErrorIsInternalFailure(t *testing.T) {
	var cause *common.Error
	result := common.PublicError(cause)
	if result == nil || result.Status != 500 {
		t.Fatalf("invalid public failure: %v", result)
	}
	if result.Error() == "" {
		t.Fatal("empty failure")
	}
}
