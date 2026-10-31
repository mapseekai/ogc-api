package features

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mapseekai/ogc-api/common"
)

// Defaults returns independent query policy with finite pagination limits.
func (o Options) Defaults() Options {
	o = o.withDefaults()
	o.FilterProperties = slices.Clone(o.FilterProperties)
	if o.CRS != nil {
		policy := o.CRS.Defaults()
		o.CRS = &policy
	}
	if o.Filter != nil {
		policy := o.Filter.Defaults()
		o.Filter = &policy
	}
	return o
}

// Validate rejects ambiguous reserved filter names and invalid deployment limits.
func (o Options) Validate() error {
	o = o.withDefaults()
	if o.DefaultLimit > o.MaxLimit {
		return fmt.Errorf("default feature limit exceeds maximum")
	}
	if o.CRS != nil {
		if err := o.CRS.Validate(); err != nil {
			return err
		}
	}
	seen := make(map[string]bool, len(o.FilterProperties))
	for _, name := range o.FilterProperties {
		if name == "" || seen[name] || slices.Contains([]string{"f", "bbox", "datetime", "limit", "cursor", "offset", "crs", "bbox-crs", "filter", "filter-lang", "filter-crs"}, name) {
			return fmt.Errorf("invalid feature filter property %q", name)
		}
		seen[name] = true
	}
	if o.Filter != nil {
		if err := o.Filter.Validate(); err != nil {
			return err
		}
		for _, crs := range o.Filter.AdditionalCRS {
			if o.CRS == nil {
				return fmt.Errorf("additional filter CRS requires explicit Part 2 reader policy")
			}
			declared, ok := o.CRS.lookup(crs.URI)
			if !ok || declared != crs {
				return fmt.Errorf("additional filter CRS must match a supported collection CRS")
			}
		}
		for _, name := range o.FilterProperties {
			if field, ok := o.Filter.Queryables[name]; !ok || field.Type != "string" {
				return fmt.Errorf("legacy property filters must be declared string queryables")
			}
		}
	}
	return nil
}

// ParseQuery normalizes Part 1 predicates, optional Part 2/3 parameters, and cursor.
// Input and Options are not mutated; returned slices and maps belong to the request.
func ParseQuery(values common.Query, options Options) (Query, error) {
	if err := options.Validate(); err != nil {
		return Query{}, err
	}
	options = options.withDefaults()
	allowed := append([]string{"f", "bbox", "datetime", "limit", "cursor"}, options.FilterProperties...)
	if options.OffsetPagination {
		allowed = append(allowed, "offset")
	}
	if options.CRS != nil {
		allowed = append(allowed, "crs", "bbox-crs")
	}
	if options.Filter != nil {
		allowed = append(allowed, "filter", "filter-lang", "filter-crs")
	}
	if err := values.Only(allowed...); err != nil {
		return Query{}, err
	}
	limit, err := pageLimit(values, options.DefaultLimit, options.MaxLimit)
	if err != nil {
		return Query{}, err
	}
	query := Query{Limit: limit, Cursor: strings.Clone(values["cursor"])}
	if _, present := values["offset"]; present {
		if _, mixed := values["cursor"]; mixed {
			return Query{}, common.Invalid("offset/cursor")
		}
		offset, err := values.Integer("offset", 0, 0, math.MaxUint64)
		if err != nil {
			return Query{}, err
		}
		query.Offset = &offset
	}
	query.OutputCRS, err = options.outputCRS(values)
	if err != nil {
		return Query{}, err
	}
	boxCRS, err := options.bboxCRS(values)
	if err != nil {
		return Query{}, err
	}
	if options.Filter != nil {
		query.Filter, err = options.Filter.parse(values)
		if err != nil {
			return Query{}, err
		}
	}
	if raw, ok := values["bbox"]; ok {
		query.BBoxCRS = boxCRS
		query.BBox, err = boxCRS.ParseBBox(raw)
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
			query.Properties[strings.Clone(name)] = strings.Clone(value)
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
