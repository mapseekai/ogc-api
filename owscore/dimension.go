package owscore

import (
	"context"
	"fmt"
	"maps"
	"mime"
	"slices"
	"strings"
)

// Dimension preserves expressions rather than expanding potentially unbounded ranges.
// Validate is required for intervals, nearest-value selection or other domain rules.
type Dimension struct {
	Units, UnitSymbol, Default            string
	Values                                []string
	Current, MultipleValues, NearestValue bool
	Validate                              func(context.Context, string) (string, error)
}

// CloneDimensions copies a dimension map and its value slices.
// Validator functions remain shared; their captured state must be concurrency-safe.
func CloneDimensions(source map[string]Dimension) map[string]Dimension {
	result := maps.Clone(source)
	for k, v := range result {
		v.Values = slices.Clone(v.Values)
		result[k] = v
	}
	return result
}

// Check validates declarations without invoking the application validator.
// Intervals and nearest-value selection require Validate to be configured.
func (d Dimension) Check() error {
	if len(d.Values) == 0 {
		return fmt.Errorf("dimension must declare values")
	}
	if d.NearestValue && d.Validate == nil {
		return fmt.Errorf("nearest-value dimension requires a validator")
	}
	for _, v := range d.Values {
		if v == "" {
			return fmt.Errorf("empty dimension value")
		}
		if strings.Contains(v, "/") && d.Validate == nil {
			return fmt.Errorf("interval dimension requires a validator")
		}
	}
	if d.Default != "" && d.Validate == nil && !(d.Current && d.Default == "current") && !slices.Contains(d.Values, d.Default) {
		return fmt.Errorf("dimension default is not declared")
	}
	return nil
}

// Resolve selects a default for an absent value, then validates the expression.
// It does not expand intervals or mutate the declaration. A configured Validate
// callback owns expression semantics and receives the supplied non-nil context.
func (d Dimension) Resolve(ctx context.Context, name, value string, present bool) (string, error) {
	if !present {
		value = d.Default
	}
	if value == "" {
		return "", Fail(MissingDimension, name, "A dimension value is required")
	}
	if d.Validate != nil {
		return d.Validate(ctx, value)
	}
	if d.Current && value == "current" {
		return value, nil
	}
	parts := []string{value}
	if d.MultipleValues {
		parts = strings.Split(value, ",")
	}
	for _, part := range parts {
		if !slices.Contains(d.Values, part) {
			return "", Fail(InvalidDimension, name, "Dimension value is not declared")
		}
	}
	return value, nil
}

// ValidateFormats rejects malformed MIME types and duplicate format strings.
// An empty slice is valid for an operation that is not advertised.
func ValidateFormats(formats []string) error {
	seen := map[string]bool{}
	for _, format := range formats {
		if strings.ContainsAny(format, "\r\n") {
			return fmt.Errorf("invalid media type")
		}
		media, _, err := mime.ParseMediaType(format)
		if err != nil || !strings.Contains(media, "/") || seen[format] {
			return fmt.Errorf("invalid or duplicate media type %q", format)
		}
		seen[format] = true
	}
	return nil
}
