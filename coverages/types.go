// Package coverages defines a versioned OGC API Coverages draft retrieval profile.
// It handles protocol queries only; raster decoding, subsetting and resampling are external.
package coverages

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
)

// Profile identifies the draft used for the retrieval contract, not an OGC certification.
const Profile = "OGC 19-087 / 1.0.0rc4 / retrieval profile"

// Request is a normalized coverage read with independently specified output and subset CRS.
type Request struct {
	CollectionID string
	Format       string
	CRS          string
	BBoxCRS      string
	SubsetCRS    string
	BBox         []float64
	Datetime     *common.Interval
	Subsets      []Subset
	Properties   []string
}

// Subset selects one coordinate or an inclusive interval on a named domain axis.
// Bounds retain their lexical numeric or quoted-string representation for the external engine.
type Subset struct {
	Axis  string
	Lower string
	Upper string
	Slice bool
}

// Service publishes formats, reference systems, fields and axes accepted by the backend.
// Schema is optional field metadata served at /collections/{id}/schema.
// This draft profile is listed in documentation, not automatically added to /conformance.
type Service struct {
	Reader     Reader
	Formats    []string
	CRS        []string
	DefaultCRS string
	Axes       []string
	Properties []string
	Schema     json.RawMessage
	// DomainSet and RangeType are optional CIS JSON metadata resources. When
	// supplied, both must be nonempty objects. Prepare detaches their storage;
	// the provider is responsible for their agreement with the coverage values.
	DomainSet json.RawMessage
	RangeType json.RawMessage
}

// Prepare validates and detaches publication metadata without accessing a data source.
func (s Service) Prepare() (Service, error) {
	if s.Reader == nil || len(s.Formats) == 0 {
		return Service{}, fmt.Errorf("coverage reader and formats are required")
	}
	if err := owscore.ValidateFormats(s.Formats); err != nil {
		return Service{}, err
	}
	s.Formats = slices.Clone(s.Formats)
	s.CRS = slices.Clone(s.CRS)
	s.Axes = slices.Clone(s.Axes)
	s.Properties = slices.Clone(s.Properties)
	s.Schema = slices.Clone(s.Schema)
	s.DomainSet = slices.Clone(s.DomainSet)
	s.RangeType = slices.Clone(s.RangeType)
	if (len(s.DomainSet) == 0) != (len(s.RangeType) == 0) {
		return Service{}, fmt.Errorf("coverage domain set and range type must be supplied together")
	}
	for _, data := range []json.RawMessage{s.DomainSet, s.RangeType} {
		if len(data) == 0 {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil || len(object) == 0 {
			return Service{}, fmt.Errorf("coverage metadata must be a nonempty JSON object")
		}
	}
	if len(s.CRS) == 0 {
		s.CRS = []string{common.CRS84}
	}
	for _, crs := range s.CRS {
		if err := common.ValidateCRS(crs); err != nil {
			return Service{}, err
		}
	}
	if s.DefaultCRS == "" {
		s.DefaultCRS = s.CRS[0]
	}
	if !slices.Contains(s.CRS, s.DefaultCRS) {
		return Service{}, fmt.Errorf("coverage default CRS is not declared")
	}
	if len(s.Schema) > 0 && !json.Valid(s.Schema) {
		return Service{}, fmt.Errorf("invalid coverage field schema")
	}
	return s, nil
}

// Reader obtains coverage content from an external engine or storage service.
// Payload ownership transfers on every return, including errors.
type Reader interface {
	ReadCoverage(context.Context, Request) (response.Payload, error)
}

// ReaderFunc adapts a function to the coverage capability.
type ReaderFunc func(context.Context, Request) (response.Payload, error)

// ReadCoverage calls the configured external implementation.
func (f ReaderFunc) ReadCoverage(ctx context.Context, request Request) (response.Payload, error) {
	return f(ctx, request)
}
