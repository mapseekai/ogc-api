package coverages

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/common"
)

// ParseQuery parses the draft retrieval/subsetting/field-selection profile.
// Unknown parameters and undeclared axes/fields are rejected, never silently ignored.
func ParseQuery(values common.Query, service Service) (Request, error) {
	if err := values.Only("f", "bbox", "bbox-crs", "datetime", "subset", "subset-crs", "properties", "crs"); err != nil {
		return Request{}, err
	}
	request := Request{CRS: service.DefaultCRS, BBoxCRS: common.CRS84, SubsetCRS: service.DefaultCRS}
	for _, parameter := range []struct {
		name   string
		target *string
	}{
		{"crs", &request.CRS}, {"bbox-crs", &request.BBoxCRS}, {"subset-crs", &request.SubsetCRS},
	} {
		if value, ok := values[parameter.name]; ok {
			if !slices.Contains(service.CRS, value) {
				return Request{}, common.Invalid(parameter.name)
			}
			*parameter.target = value
		}
	}
	if value, ok := values["bbox"]; ok {
		bbox, err := common.ParseBBox(value, request.BBoxCRS == common.CRS84)
		if err != nil {
			return Request{}, err
		}
		request.BBox = bbox
	}
	if value, ok := values["datetime"]; ok {
		interval, err := common.ParseDatetime(value)
		if err != nil {
			return Request{}, err
		}
		request.Datetime = &interval
	}
	if value, ok := values["properties"]; ok {
		request.Properties = strings.Split(value, ",")
		seen := make(map[string]bool, len(request.Properties))
		for _, name := range request.Properties {
			if !slices.Contains(service.Properties, name) || seen[name] {
				return Request{}, common.Invalid("properties")
			}
			seen[name] = true
		}
	}
	if value, ok := values["subset"]; ok {
		subsets, err := ParseSubsets(value, service.Axes)
		if err != nil {
			return Request{}, err
		}
		request.Subsets = subsets
	}
	return request, nil
}

// ParseSubsets parses axis(value) and axis(lower:upper), respecting quoted timestamps.
// It rejects duplicate axes, non-finite numbers and undeclared dimension names.
func ParseSubsets(value string, axes []string) ([]Subset, error) {
	var result []Subset
	seen := make(map[string]bool)
	for value != "" {
		axis, rest, found := strings.Cut(value, "(")
		if !found || !slices.Contains(axes, axis) || seen[axis] {
			return nil, common.Invalid("subset")
		}
		seen[axis] = true
		quoted, escaped, end, colon := false, false, -1, -1
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if escaped {
				escaped = false
				continue
			}
			if quoted && c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = !quoted
				continue
			}
			if quoted {
				continue
			}
			if c == ')' {
				end = i
				break
			}
			if c == ':' {
				if colon >= 0 {
					return nil, common.Invalid("subset")
				}
				colon = i
			}
		}
		if end < 0 {
			return nil, common.Invalid("subset")
		}
		selection := Subset{Axis: axis, Lower: rest[:end], Slice: colon < 0}
		if colon >= 0 {
			selection.Lower, selection.Upper = rest[:colon], rest[colon+1:end]
		}
		if !subsetBound(selection.Lower) || !selection.Slice && !subsetBound(selection.Upper) {
			return nil, common.Invalid("subset")
		}
		if !selection.Slice {
			low, lowErr := strconv.ParseFloat(selection.Lower, 64)
			high, highErr := strconv.ParseFloat(selection.Upper, 64)
			if lowErr == nil && highErr == nil && low > high {
				return nil, common.Invalid("subset")
			}
		}
		result = append(result, selection)
		value = rest[end+1:]
		if value != "" {
			if !strings.HasPrefix(value, ",") || len(value) == 1 {
				return nil, common.Invalid("subset")
			}
			value = value[1:]
		}
	}
	if len(result) == 0 {
		return nil, common.Invalid("subset")
	}
	return result, nil
}

func subsetBound(value string) bool {
	if value == "*" {
		return true
	}
	if strings.HasPrefix(value, `"`) {
		_, err := strconv.Unquote(value)
		return err == nil
	}
	number, err := strconv.ParseFloat(value, 64)
	return err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
}
