package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
)

// fixtureReader executes only the declared Basic CQL2 contract in synthetic memory.
// This evaluator exists solely in the test command, never in product packages.
type fixtureReader struct{ dataset fixtureDataset }

type fixtureCursor struct{ items []features.Feature }

func (r fixtureReader) Items(ctx context.Context, request features.Request) (features.Page, error) {
	if err := ctx.Err(); err != nil {
		return features.Page{}, err
	}
	selected := []features.Feature{}
	for index, point := range r.dataset.points {
		q := request.Query
		if b := q.BBox; len(b) > 0 {
			position, err := transformPosition(point, r.dataset.storage, q.BBoxCRS)
			if err != nil {
				return features.Page{}, err
			}
			if !insideBBox(position, b, q.BBoxCRS) {
				continue
			}
		}
		if interval := q.Datetime; interval != nil {
			stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			if interval.Start != nil && stamp.Before(*interval.Start) || interval.End != nil && stamp.After(*interval.End) {
				continue
			}
		}
		if name, ok := q.Properties["name"]; ok && name != fmt.Sprintf("sample-%d", index) {
			continue
		}
		if q.Filter != nil {
			value, err := evaluate(q.Filter.Expression, index)
			if err != nil {
				return features.Page{}, err
			}
			if value != true {
				continue
			}
		}
		feature, err := r.feature(index, q.OutputCRS)
		if err != nil {
			return features.Page{}, err
		}
		selected = append(selected, feature)
	}
	count := uint64(len(selected))
	start := uint64(0)
	if request.Query.Cursor != "" {
		var err error
		start, err = strconv.ParseUint(request.Query.Cursor, 10, 64)
		if err != nil || start > count {
			return features.Page{}, common.Invalid("cursor")
		}
	}
	end := start + min(request.Query.Limit, count-start)
	page := features.Page{CRS: request.Query.OutputCRS, Items: &fixtureCursor{items: selected[start:end]}, NumberMatched: &count}
	if end < count {
		page.NextCursor = strconv.FormatUint(end, 10)
	}
	return page, nil
}

func (r fixtureReader) Item(ctx context.Context, request features.Request) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	for index := range r.dataset.points {
		if request.ItemID == strconv.Itoa(index) {
			return r.feature(index, request.Query.OutputCRS)
		}
	}
	return features.Feature{}, common.ErrNotFound
}

func (c *fixtureCursor) Next(ctx context.Context) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if len(c.items) == 0 {
		return features.Feature{}, io.EOF
	}
	item := c.items[0]
	c.items = c.items[1:]
	return item, nil
}
func (c *fixtureCursor) Close() error { c.items = nil; return nil }

func (r fixtureReader) feature(index int, crs common.CRS) (features.Feature, error) {
	position, err := transformPosition(r.dataset.points[index], r.dataset.storage, crs)
	if err != nil {
		return features.Feature{}, err
	}
	geometry, err := json.Marshal(struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	}{"Point", position})
	if err != nil {
		return features.Feature{}, err
	}
	// The second geometry-valued property must use the selected output CRS too.
	properties, err := json.Marshal(map[string]any{"name": fmt.Sprintf("sample-%d", index), "population": 9007199254740993 + int64(index), "active": index%2 == 0,
		"observedAt": "2020-01-01T00:00:00Z", "location": json.RawMessage(geometry)})
	if err != nil {
		return features.Feature{}, err
	}
	return features.Feature{CRS: crs, ID: strconv.Itoa(index), Geometry: geometry, Properties: properties}, nil
}

func insideBBox(position, bbox []float64, crs common.CRS) bool {
	longitude := 0
	if crs.AxisOrder == common.YX {
		longitude = 1
	}
	for axis, value := range position {
		inside := value >= bbox[axis] && value <= bbox[axis+crs.Dimensions]
		if crs.Geographic && axis == longitude && bbox[axis] > bbox[axis+crs.Dimensions] {
			inside = value >= bbox[axis] || value <= bbox[axis+crs.Dimensions]
		}
		if !inside {
			return false
		}
	}
	return true
}

func evaluate(expression cql2.Expression, index int) (any, error) {
	switch expression.Kind {
	case cql2.Property:
		switch expression.Property {
		case "nullable":
			return nil, nil
		case "name":
			return fmt.Sprintf("sample-%d", index), nil
		case "population":
			return big.NewRat(9007199254740993+int64(index), 1), nil
		case "active":
			return index%2 == 0, nil
		case "observedAt":
			return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), nil
		case "geom":
			return nil, fmt.Errorf("spatial evaluation is not declared by this fixture")
		}
	case cql2.LiteralKind:
		value := expression.Literal
		switch value.Type {
		case cql2.String:
			return value.Text, nil
		case cql2.Number:
			n, ok := new(big.Rat).SetString(string(value.Number))
			if !ok {
				return nil, fmt.Errorf("invalid validated fixture number")
			}
			return n, nil
		case cql2.Boolean:
			return value.Boolean, nil
		case cql2.Timestamp:
			return time.Parse(time.RFC3339Nano, value.Text)
		case cql2.Date:
			return time.Parse(time.DateOnly, value.Text)
		case cql2.GeometryType:
			return value.Geometry, nil
		case cql2.IntervalType:
			return value.Interval, nil
		}
	case cql2.Operation:
		args := make([]any, len(expression.Args))
		for i, arg := range expression.Args {
			value, err := evaluate(arg, index)
			if err != nil {
				return nil, err
			}
			args[i] = value
		}
		switch expression.Operator {
		case cql2.And, cql2.Or:
			unknown := false
			for _, value := range args {
				if value == nil {
					unknown = true
					continue
				}
				b, ok := value.(bool)
				if !ok {
					return nil, fmt.Errorf("nonboolean fixture operand")
				}
				if expression.Operator == cql2.And && !b {
					return false, nil
				}
				if expression.Operator == cql2.Or && b {
					return true, nil
				}
			}
			if unknown {
				return nil, nil
			}
			return expression.Operator == cql2.And, nil
		case cql2.Not:
			if args[0] == nil {
				return nil, nil
			}
			return !args[0].(bool), nil
		case cql2.IsNull:
			return args[0] == nil, nil
		default:
			if len(args) != 2 {
				return nil, fmt.Errorf("unsupported fixture arity")
			}
			if args[0] == nil || args[1] == nil {
				return nil, nil
			}
			order, err := compare(args[0], args[1])
			if err != nil {
				return nil, err
			}
			switch expression.Operator {
			case cql2.Equal:
				return order == 0, nil
			case cql2.NotEqual:
				return order != 0, nil
			case cql2.Less:
				return order < 0, nil
			case cql2.LessEqual:
				return order <= 0, nil
			case cql2.Greater:
				return order > 0, nil
			case cql2.GreaterEqual:
				return order >= 0, nil
			}
		}
	}
	return nil, fmt.Errorf("fixture received undeclared expression")
}

func compare(a, b any) (int, error) {
	switch a := a.(type) {
	case string:
		other, ok := b.(string)
		if ok {
			return strings.Compare(a, other), nil
		}
	case *big.Rat:
		other, ok := b.(*big.Rat)
		if ok {
			return a.Cmp(other), nil
		}
	case bool:
		other, ok := b.(bool)
		if ok {
			if a == other {
				return 0, nil
			}
			if a {
				return 1, nil
			}
			return -1, nil
		}
	case time.Time:
		other, ok := b.(time.Time)
		if ok {
			return a.Compare(other), nil
		}
	}
	return 0, fmt.Errorf("incompatible fixture comparison types: %T %T", a, b)
}
