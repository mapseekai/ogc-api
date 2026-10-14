package legacy_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/internal/legacy"
	"github.com/mapseekai/ogc-api/owscore"
)

func TestProviderErrorMapping(t *testing.T) {
	for _, test := range []struct {
		input error
		kind  owscore.Kind
	}{
		{common.ErrNotFound, owscore.NotFound}, {common.ErrForbidden, owscore.Forbidden},
		{common.ErrUnavailable, owscore.Unavailable}, {common.Invalid("bbox"), owscore.InvalidParameter},
		{fmt.Errorf("password=private"), owscore.Internal},
	} {
		mapped := legacy.Error(test.input)
		var public *owscore.Error
		if !errors.As(mapped, &public) || public.Kind != test.kind || !errors.Is(mapped, test.input) {
			t.Fatal(mapped)
		}
		if test.kind == owscore.Internal && public.Message == test.input.Error() {
			t.Fatal("private error leaked")
		}
	}
}
