package owscore_test

import (
	"testing"

	"github.com/mapseekai/ogc-api/owscore"
)

func TestNumericVersions(t *testing.T) {
	a, err := owscore.ParseVersion("1.10.0")
	if err != nil {
		t.Fatal(err)
	}
	b, err := owscore.ParseVersion("1.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if a.Compare(b) <= 0 {
		t.Fatal("versions compared lexically")
	}
	for _, value := range []string{"1.0", "-1.0.0", "1.0.NaN", "4294967296.0.0"} {
		t.Run(value, func(t *testing.T) {
			if _, err := owscore.ParseVersion(value); err == nil {
				t.Fatal(value)
			}

		})
	}
}

func TestTypedNilPublicErrorIsInternalFailure(t *testing.T) {
	var cause *owscore.Error
	result := owscore.PublicError(cause)
	if result == nil || result.Kind != owscore.Internal {
		t.Fatalf("invalid public failure: %v", result)
	}
}
