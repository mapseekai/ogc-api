package common_test

import (
	"errors"
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

func TestPrivateErrorIsMasked(t *testing.T) {
	err := errors.New("password=very-secret")
	public := common.PublicError(err)
	if public.Status != 500 || public.Description == err.Error() {
		t.Fatal(public)
	}
	if !errors.Is(public, err) {
		t.Fatal("lost diagnostic cause")
	}
}
