package common

import (
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Query holds case-sensitive OGC API parameters. Each parameter occurs once.
// It is exclusively owned by one request and may be passed to an external backend.
type Query map[string]string

// ParseQuery bounds allocation before decoding and rejects duplicates and invalid escapes.
// Non-positive limits are programming errors, not requests for unbounded parsing.
func ParseQuery(raw string, maxBytes, maxParameters int) (Query, error) {
	if maxBytes <= 0 || maxParameters <= 0 {
		return nil, Invalid("query limits")
	}
	if len(raw) > maxBytes {
		return nil, &Error{Status: 414, Code: "RequestTooLarge", Description: "Query exceeds the configured limit"}
	}
	if raw == "" {
		return Query{}, nil
	}
	if strings.Count(raw, "&")+1 > maxParameters {
		return nil, Invalid("query parameter count")
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, Invalid("query")
	}
	query := make(Query, len(values))
	for key, values := range values {
		if key == "" || len(values) != 1 || !utf8.ValidString(key) || !utf8.ValidString(values[0]) {
			return nil, Invalid("query")
		}
		query[key] = values[0]
	}
	return query, nil
}

// Only rejects unrecognized parameters instead of silently ignoring filters.
func (q Query) Only(names ...string) error {
	for name := range q {
		if !slices.Contains(names, name) {
			return Invalid("query")
		}
	}
	return nil
}

// Integer reads an unsigned decimal parameter in the inclusive permitted range.
func (q Query) Integer(name string, fallback, min, max uint64) (uint64, error) {
	value, present := q[name]
	if !present {
		return fallback, nil
	}
	if value == "" {
		return 0, Invalid(name)
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, Invalid(name)
		}
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil || number < min || number > max {
		return 0, Invalid(name)
	}
	return number, nil
}

// ParseBBox reads a four- or six-coordinate bounding box. CRS84 longitude may
// cross the antimeridian; other axes must remain ordered. Values must be finite.
func ParseBBox(value string, geographic bool) ([]float64, error) {
	size := strings.Count(value, ",") + 1
	if size != 4 && size != 6 {
		return nil, Invalid("bbox")
	}
	parts := strings.Split(value, ",")
	bbox := make([]float64, size)
	for i, part := range parts {
		number, err := strconv.ParseFloat(part, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, Invalid("bbox")
		}
		bbox[i] = number
	}
	if err := ValidateBBox(bbox, geographic); err != nil {
		return nil, err
	}
	return bbox, nil
}

// ValidateBBox checks dimensions, finite values, axis order and optional CRS84 ranges.
func ValidateBBox(bbox []float64, geographic bool) error {
	if len(bbox) != 4 && len(bbox) != 6 {
		return Invalid("bbox")
	}
	half := len(bbox) / 2
	for i, n := range bbox {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return Invalid("bbox")
		}
		if i < half && !(geographic && i == 0) && n > bbox[i+half] {
			return Invalid("bbox")
		}
	}
	if geographic && (bbox[0] < -180 || bbox[0] > 180 || bbox[half] < -180 || bbox[half] > 180 ||
		bbox[1] < -90 || bbox[1] > 90 || bbox[half+1] < -90 || bbox[half+1] > 90) {
		return Invalid("bbox")
	}
	return nil
}

// Interval records a datetime instant or an inclusive interval. Nil endpoints are open.
type Interval struct {
	Start   *time.Time
	End     *time.Time
	Instant bool
}

// ParseDatetime accepts RFC3339 instants and open/closed intervals from OGC API Features.
func ParseDatetime(value string) (Interval, error) {
	var interval Interval
	start, end, bounded := strings.Cut(value, "/")
	if !bounded {
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return interval, Invalid("datetime")
		}
		return Interval{Start: &t, End: &t, Instant: true}, nil
	}
	parse := func(value string) (*time.Time, error) {
		if value == ".." || value == "" {
			return nil, nil
		}
		t, err := time.Parse(time.RFC3339Nano, value)
		return &t, err
	}
	var err error
	if interval.Start, err = parse(start); err != nil {
		return Interval{}, Invalid("datetime")
	}
	if interval.End, err = parse(end); err != nil {
		return Interval{}, Invalid("datetime")
	}
	if interval.Start == nil && interval.End == nil {
		return Interval{}, Invalid("datetime")
	}
	if interval.Start != nil && interval.End != nil && interval.Start.After(*interval.End) {
		return Interval{}, Invalid("datetime")
	}
	return interval, nil
}
