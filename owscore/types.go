// Package owscore contains framework-independent OGC protocol values.
package owscore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Protocol identifies the requested OGC service independently of its version.
type Protocol string

// Operation is the normalized operation name passed to business handlers.
type Operation string

// Binding identifies KVP or REST request encoding, not a protocol version.
type Binding string

const (
	WMS             Protocol  = "WMS"
	WMTS            Protocol  = "WMTS"
	KVP             Binding   = "KVP"
	REST            Binding   = "REST"
	GetCapabilities Operation = "GetCapabilities"
	GetMap          Operation = "GetMap"
	GetTile         Operation = "GetTile"
	GetFeatureInfo  Operation = "GetFeatureInfo"
)

// Meta records request identity and the effective version chosen by the codec.
// RequestedVersion preserves the client version when present; Version is effective.
type Meta struct {
	Protocol         Protocol
	Binding          Binding
	Operation        Operation
	RequestedVersion string
	Version          string
}

// Kind is a stable domain error category; codecs map it to version-specific wire codes.
type Kind string

const (
	MissingParameter      Kind = "MissingParameterValue"
	InvalidParameter      Kind = "InvalidParameterValue"
	VersionMismatch       Kind = "VersionNegotiationFailed"
	UnsupportedOperation  Kind = "OperationNotSupported"
	LayerNotDefined       Kind = "LayerNotDefined"
	StyleNotDefined       Kind = "StyleNotDefined"
	InvalidCRS            Kind = "InvalidCRS"
	InvalidFormat         Kind = "InvalidFormat"
	LayerNotQueryable     Kind = "LayerNotQueryable"
	InvalidDimension      Kind = "InvalidDimensionValue"
	MissingDimension      Kind = "MissingDimensionValue"
	TileOutOfRange        Kind = "TileOutOfRange"
	PointOutOfRange       Kind = "PointIJOutOfRange"
	CurrentUpdateSequence Kind = "CurrentUpdateSequence"
	InvalidUpdateSequence Kind = "InvalidUpdateSequence"
	NotFound              Kind = "NotFound"
	Forbidden             Kind = "Forbidden"
	TooLarge              Kind = "RequestTooLarge"
	Internal              Kind = "NoApplicableCode"
)

// Error separates a client-safe message from its private cause.
type Error struct {
	Kind    Kind
	Locator string
	Message string
	Cause   error
}

// Error returns the safe category and message, excluding the private cause.
func (e *Error) Error() string { return string(e.Kind) + ": " + e.Message }

// Unwrap exposes the private cause to errors.Is and errors.As, not to protocol encoders.
func (e *Error) Unwrap() error { return e.Cause }

// Fail constructs a domain error with a client-visible message.
// Do not pass private paths, queries or credentials as message or locator.
func Fail(kind Kind, locator, message string) *Error {
	return &Error{Kind: kind, Locator: locator, Message: message}
}

// Invalid constructs a client-safe invalid-parameter error for the given locator.
func Invalid(locator string) *Error {
	return Fail(InvalidParameter, locator, "Invalid parameter value")
}

// Missing constructs a client-safe missing-parameter error for the given locator.
func Missing(locator string) *Error {
	return Fail(MissingParameter, locator, "Required parameter is missing")
}

// PublicError retains a wrapped domain error or masks an arbitrary internal error.
// The supplied error must be non-nil; an internal cause is kept only for diagnostics.
func PublicError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Kind: Internal, Message: "The service could not complete the request", Cause: err}
}

// Version compares numeric components, not their textual representation.
type Version [3]uint32

// ParseVersion requires three unsigned decimal components that each fit in uint32.
// It checks syntax, not whether a particular service supports that version.
func ParseVersion(s string) (Version, error) {
	var v Version
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, Invalid("VERSION")
	}
	for i, p := range parts {
		if p == "" {
			return v, Invalid("VERSION")
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return v, Invalid("VERSION")
			}
		}
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return v, Invalid("VERSION")
		}
		v[i] = uint32(n)
	}
	return v, nil
}

// Compare returns -1, 0 or 1 by comparing version components numerically.
func (v Version) Compare(other Version) int {
	for i, n := range v {
		if n < other[i] {
			return -1
		}
		if n > other[i] {
			return 1
		}
	}
	return 0
}

// String renders the three numeric components in dotted decimal form.
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// Limits are deployment safeguards, not limits imposed by OGC.
type Limits struct {
	MaxQueryBytes        int
	MaxPathBytes         int
	MaxParameters        int
	MaxLayers            int
	MaxImageWidth        uint64
	MaxImageHeight       uint64
	MaxImagePixels       uint64
	MaxCapabilitiesBytes int
}

// Defaults returns a copy with zero-valued fields replaced by deployment defaults.
// It does not validate negative values; call Validate first for untrusted configuration.
func (l Limits) Defaults() Limits {
	if l.MaxQueryBytes == 0 {
		l.MaxQueryBytes = 64 << 10
	}
	if l.MaxPathBytes == 0 {
		l.MaxPathBytes = 16 << 10
	}
	if l.MaxParameters == 0 {
		l.MaxParameters = 256
	}
	if l.MaxLayers == 0 {
		l.MaxLayers = 64
	}
	if l.MaxImageWidth == 0 {
		l.MaxImageWidth = 8192
	}
	if l.MaxImageHeight == 0 {
		l.MaxImageHeight = 8192
	}
	if l.MaxImagePixels == 0 {
		l.MaxImagePixels = 16777216
	}
	if l.MaxCapabilitiesBytes == 0 {
		l.MaxCapabilitiesBytes = 8 << 20
	}
	return l
}

// Validate rejects negative signed limits. Zero means use the corresponding default.
func (l Limits) Validate() error {
	if l.MaxQueryBytes < 0 || l.MaxPathBytes < 0 || l.MaxParameters < 0 || l.MaxLayers < 0 || l.MaxCapabilitiesBytes < 0 {
		return fmt.Errorf("limits must not be negative")
	}
	return nil
}

// Service holds trusted, client-visible service metadata, not HTTP server settings.
type Service struct {
	Title             string
	Abstract          string
	Keywords          []string
	Fees              string
	AccessConstraints string
}

// Style describes a named rendering style; its identifier is the surrounding catalog map key.
type Style struct {
	Title    string
	Abstract string
}
