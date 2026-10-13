package owscore_test

import (
	"context"
	"testing"

	"github.com/mapseekai/ogc-api/owscore"
)

func TestDimensionDoesNotExpandIntervals(t *testing.T) {
	expression := "1900-01-01/2999-12-31/P0.001S"
	called := false
	d := owscore.Dimension{Values: []string{expression}, Validate: func(_ context.Context, value string) (string, error) { called = true; return value, nil }}
	if err := d.Check(); err != nil {
		t.Fatal(err)
	}
	value, err := d.Resolve(context.Background(), "TIME", expression, true)
	if err != nil || value != expression || !called {
		t.Fatal(value, err)
	}
	d.Validate = nil
	if err := d.Check(); err == nil {
		t.Fatal("interval was accepted without a validator")
	}
}
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
		if _, err := owscore.ParseVersion(value); err == nil {
			t.Fatal(value)
		}
	}
}
