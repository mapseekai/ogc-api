package features

import (
	"fmt"
	"slices"

	"github.com/mapseekai/ogc-api/common"
)

// Defaults returns independent query policy with finite pagination limits.
func (o Options) Defaults() Options {
	o = o.withDefaults()
	o.FilterProperties = slices.Clone(o.FilterProperties)
	return o
}

// Validate rejects ambiguous reserved filter names and invalid deployment limits.
func (o Options) Validate() error {
	o = o.withDefaults()
	if o.DefaultLimit > o.MaxLimit {
		return fmt.Errorf("default feature limit exceeds maximum")
	}
	seen := make(map[string]bool, len(o.FilterProperties))
	for _, name := range o.FilterProperties {
		if name == "" || seen[name] || slices.Contains([]string{"f", "bbox", "datetime", "limit", "cursor", "crs", "bbox-crs", "filter", "filter-lang"}, name) {
			return fmt.Errorf("invalid feature filter property %q", name)
		}
		seen[name] = true
	}
	return nil
}

// ParseQuery normalizes the Features Part 1 predicates and the explicit cursor extension.
// Input and Options are not mutated; returned slices and maps belong to the request.
func ParseQuery(values common.Query, options Options) (Query, error) {
	options = options.withDefaults()
	allowed := append([]string{"f", "bbox", "datetime", "limit", "cursor"}, options.FilterProperties...)
	if err := values.Only(allowed...); err != nil {
		return Query{}, err
	}
	limit, err := pageLimit(values, options.DefaultLimit, options.MaxLimit)
	if err != nil {
		return Query{}, err
	}
	query := Query{Limit: limit, Cursor: values["cursor"]}
	if raw, ok := values["bbox"]; ok {
		query.BBox, err = common.ParseBBox(raw, true)
		if err != nil {
			return Query{}, err
		}
	}
	if raw, ok := values["datetime"]; ok {
		interval, err := common.ParseDatetime(raw)
		if err != nil {
			return Query{}, err
		}
		query.Datetime = &interval
	}
	for _, name := range options.FilterProperties {
		if value, ok := values[name]; ok {
			if query.Properties == nil {
				query.Properties = make(map[string]string)
			}
			query.Properties[name] = value
		}
	}
	return query, nil
}

func (o Options) withDefaults() Options {
	if o.DefaultLimit == 0 {
		o.DefaultLimit = 10
	}
	if o.MaxLimit == 0 {
		o.MaxLimit = 1000
	}
	return o
}

// OGC API Features 1.0.1 /req/core/fc-limit-response-1 requires clamping,
// including arbitrarily large positive decimal integers, rather than rejecting them.
func pageLimit(values common.Query, fallback, maximum uint64) (uint64, error) {
	raw, present := values["limit"]
	if !present {
		return fallback, nil
	}
	if raw == "" {
		return 0, common.Invalid("limit")
	}
	var value uint64
	for _, character := range raw {
		if character < '0' || character > '9' {
			return 0, common.Invalid("limit")
		}
		digit := uint64(character - '0')
		if value > maximum/10 || value == maximum/10 && digit > maximum%10 {
			value = maximum
		} else {
			value = value*10 + digit
		}
	}
	if value == 0 {
		return 0, common.Invalid("limit")
	}
	return value, nil
}
