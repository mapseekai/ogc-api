// Package legacy maps backend-neutral errors into the existing WMS/WMTS error contract.
package legacy

import (
	"errors"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/owscore"
)

// Error preserves OWS errors and translates neutral provider categories without leaking causes.
func Error(err error) error {
	if err == nil {
		return nil
	}
	var original *owscore.Error
	if errors.As(err, &original) {
		if original == nil {
			return owscore.PublicError(err)
		}
		return err
	}
	public := common.PublicError(err)
	kind := owscore.Internal
	switch public.Status {
	case 400:
		kind = owscore.InvalidParameter
	case 403:
		kind = owscore.Forbidden
	case 404:
		kind = owscore.NotFound
	case 406:
		kind = owscore.InvalidFormat
	case 413, 414:
		kind = owscore.TooLarge
	case 501:
		kind = owscore.UnsupportedOperation
	case 503:
		kind = owscore.Unavailable
	}
	return &owscore.Error{Kind: kind, Message: public.Description, Cause: err}
}
