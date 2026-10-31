package features

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/internal/jsonutil"
	"github.com/mapseekai/ogc-api/response"
)

// EncodeFeature validates and encodes one feature, with bounded raw geometry/property inputs.
// maxBytes limits the encoded feature, not a complete collection. Zero is not unlimited.
func EncodeFeature(feature Feature, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 || len(feature.Geometry) > maxBytes || len(feature.Properties) > maxBytes {
		return nil, fmt.Errorf("feature exceeds response limit")
	}
	if feature.Type == "" {
		feature.Type = "Feature"
	}
	if feature.Type != "Feature" {
		return nil, fmt.Errorf("provider returned a non-feature")
	}
	if feature.CRS != (common.CRS{}) {
		if err := feature.CRS.Validate(); err != nil {
			return nil, fmt.Errorf("provider feature CRS: %w", err)
		}
	}
	if len(feature.Geometry) > 0 && !bytes.Equal(bytes.TrimSpace(feature.Geometry), []byte("null")) {
		var geometry struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(feature.Geometry, &geometry) != nil || geometry.Type == "" {
			return nil, fmt.Errorf("provider returned invalid geometry")
		}
		if feature.CRS != (common.CRS{}) {
			if err := validateOutputGeometry(feature.Geometry, feature.CRS, 1); err != nil {
				return nil, err
			}
		}
	}
	if len(feature.Properties) > 0 {
		data := bytes.TrimSpace(feature.Properties)
		if !json.Valid(data) || !(data[0] == '{' || bytes.Equal(data, []byte("null"))) {
			return nil, fmt.Errorf("provider returned invalid feature properties")
		}
	}
	switch feature.ID.(type) {
	case nil, string, json.Number, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
	default:
		return nil, fmt.Errorf("provider returned invalid feature id type")
	}

	if feature.BBox != nil {
		var err error
		if feature.CRS == (common.CRS{}) {
			err = common.ValidateBBox(feature.BBox, true)
		} else {
			err = feature.CRS.ValidateBBox(feature.BBox)
		}
		if err != nil {
			return nil, fmt.Errorf("provider feature bbox: %w", err)
		}
	}
	if err := featureBudget(feature, maxBytes); err != nil {
		return nil, err
	}
	data, err := json.Marshal(feature)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, jsonutil.ErrTooLarge
	}
	return data, nil
}

// Stream transfers Page ownership into a pull-based GeoJSON response without goroutines.
// A read buffers at most one encoded feature. Later iterator/encoding failures abort
// the stream and must be recorded by the transport via Plan.Finish, never replaced with 200 JSON.
// The caller must close the original page if this constructor returns an error.
func Stream(ctx context.Context, page Page, links []common.Link, limit uint64, maxFeatureBytes int) (response.Payload, error) {
	if page.Items == nil || limit == 0 || maxFeatureBytes <= 0 {
		return response.Payload{}, fmt.Errorf("invalid feature stream contract")
	}
	if page.CRS != (common.CRS{}) {
		if err := page.CRS.Validate(); err != nil {
			return response.Payload{}, fmt.Errorf("provider page CRS: %w", err)
		}
	}
	stream := &collectionStream{ctx: ctx, page: page, links: links, limit: limit, maxBytes: maxFeatureBytes,
		buffer: []byte(`{"type":"FeatureCollection","features":[`), timestamp: time.Now().UTC()}
	return response.Stream(common.GeoJSON, stream, response.UnknownLength), nil
}

type collectionStream struct {
	ctx       context.Context
	page      Page
	links     []common.Link
	limit     uint64
	maxBytes  int
	comma     bool
	buffer    []byte
	offset    int
	count     uint64
	done      bool
	closed    bool
	timestamp time.Time
	failure   error
}

func (s *collectionStream) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	if s.failure != nil {
		return 0, s.failure
	}
	for s.offset == len(s.buffer) {
		if s.done {
			return 0, io.EOF
		}
		if err := s.advance(); err != nil {
			s.failure = err
			return 0, err
		}
	}
	if s.comma {
		s.comma = false
		dst[0] = ','
		return 1, nil
	}
	n := copy(dst, s.buffer[s.offset:])
	s.offset += n
	return n, nil
}

func (s *collectionStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.buffer = nil
	return s.page.Close()
}

func (s *collectionStream) advance() error {
	feature, err := s.page.Items.Next(s.ctx)
	if errors.Is(err, io.EOF) {
		if s.page.NumberMatched != nil && *s.page.NumberMatched < s.count {
			return fmt.Errorf("provider numberMatched is smaller than numberReturned")
		}
		tail := struct {
			Links          []common.Link `json:"links"`
			NumberReturned uint64        `json:"numberReturned"`
			NumberMatched  *uint64       `json:"numberMatched,omitempty"`
			Timestamp      time.Time     `json:"timeStamp"`
		}{s.links, s.count, s.page.NumberMatched, s.timestamp}
		data, err := json.Marshal(tail)
		if err != nil {
			return err
		}
		s.buffer = append([]byte("],"), data[1:]...)
		s.offset = 0
		s.done = true
		return nil
	}
	if err != nil {
		return err
	}
	if s.count >= s.limit {
		return fmt.Errorf("provider exceeded requested feature limit")
	}
	if s.page.CRS != (common.CRS{}) {
		if feature.CRS != (common.CRS{}) && feature.CRS != s.page.CRS {
			return fmt.Errorf("provider feature CRS differs from page CRS")
		}
		feature.CRS = s.page.CRS
	}
	data, err := EncodeFeature(feature, s.maxBytes)
	if err != nil {
		return err
	}
	s.comma = s.count > 0
	s.buffer = data
	s.offset = 0
	s.count++
	return nil
}

// Keep this typed hot path in step with Feature and common.Link JSON fields.
// The reference-encoding tests cover the budget at exact encoded boundaries.
func featureBudget(feature Feature, maximum int) error {
	budget := jsonutil.Budget(maximum)
	if err := budget.Take(len(`{"type":"Feature","geometry":,"properties":}`)); err != nil {
		return err
	}
	if err := budget.Raw(feature.Geometry); err != nil {
		return err
	}
	if err := budget.Raw(feature.Properties); err != nil {
		return err
	}
	if feature.ID != nil {
		if err := budget.Take(len(`,"id":`)); err != nil {
			return err
		}
		if err := budget.Value(feature.ID); err != nil {
			return err
		}
	}
	if len(feature.BBox) > 0 {
		if err := budget.Take(len(`,"bbox":`)); err != nil {
			return err
		}
		if err := budget.Value(feature.BBox); err != nil {
			return err
		}
	}
	if len(feature.Links) == 0 {
		return nil
	}
	if len(feature.Links) > maximum/len(`{"href":"","rel":""}`) {
		return jsonutil.ErrTooLarge
	}
	if err := budget.Take(len(`,"links":[]`)); err != nil {
		return err
	}
	for i, link := range feature.Links {
		if i > 0 {
			if err := budget.Take(1); err != nil {
				return err
			}
		}
		if err := budget.Take(len(`{"href":,"rel":}`)); err != nil {
			return err
		}
		if err := budget.String(link.Href); err != nil {
			return err
		}
		if err := budget.String(link.Rel); err != nil {
			return err
		}
		for _, field := range []struct{ prefix, value string }{{`,"type":`, link.Type}, {`,"title":`, link.Title}} {
			if field.value == "" {
				continue
			}
			if err := budget.Take(len(field.prefix)); err != nil {
				return err
			}
			if err := budget.String(field.value); err != nil {
				return err
			}
		}
		if link.Templated {
			if err := budget.Take(len(`,"templated":true`)); err != nil {
				return err
			}
		}
	}
	return nil
}
