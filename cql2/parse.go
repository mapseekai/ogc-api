package cql2

import (
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/mapseekai/ogc-api/common"
)

const (
	maxBytes = 64 << 10
	maxDepth = 64
	maxNodes = 4096
	crs84    = common.CRS84
	crs84h   = common.CRS84h
)

// ErrInvalid identifies invalid, unsupported, or over-budget filter input.
// Errors never echo input values; callers may expose a generic parameter error.
var ErrInvalid = errors.New("invalid CQL2 expression")

// Parse decodes, validates and normalizes either encoding into the same tree.
// It is concurrency-safe when the caller does not mutate Options during a call.
// All extension operators are opt-in; Parse never evaluates a predicate.
func Parse(input string, language Language, options Options) (Expression, error) {
	limits, err := options.Limits.normalized()
	if err != nil {
		return Expression{}, err
	}
	if len(input) == 0 || len(input) > limits.MaxBytes || !utf8.ValidString(input) {
		return Expression{}, invalid("input size or encoding")
	}
	if err := options.Validate(); err != nil {
		return Expression{}, err
	}
	var expression Expression
	switch language {
	case Text:
		expression, err = parseText(input, limits)
	case JSON:
		expression, err = parseJSON(input, limits)
	default:
		err = invalid("filter language")
	}
	if err != nil {
		return Expression{}, err
	}
	state := validation{options: options, limits: limits}
	typ, err := state.expression(&expression, 1)
	if err != nil {
		return Expression{}, err
	}
	if typ != Boolean || expression.Kind == Property {
		return Expression{}, invalid("filter must be a predicate")
	}
	return expression, nil
}

// Validate rejects unsupported property types, extension operators and CRS policy.
func (o Options) Validate() error {
	if _, err := o.Limits.normalized(); err != nil {
		return err
	}
	if o.CRSDefinition != (common.CRS{}) {
		if o.CRSDefinition.Validate() != nil || o.CRSDefinition.URI != o.CRS {
			return invalid("filter CRS declaration")
		}
	} else if o.CRS != "" && o.CRS != crs84 && o.CRS != crs84h {
		return invalid("filter CRS")
	}
	for name, typ := range o.Properties {
		if name == "" || len(name) > maxBytes || !utf8.ValidString(name) || !validType(typ) {
			return invalid("queryable declaration")
		}
	}
	for i, op := range o.Operators {
		if !knownOperator(op) || slices.Contains(o.Operators[:i], op) {
			return invalid("operator declaration")
		}
	}
	return nil
}

// SupportedOperators returns an independent ordered list including Basic CQL2.
func (o Options) SupportedOperators() []Operator {
	ops := []Operator{And, Or, Not, Equal, NotEqual, Less, LessEqual, Greater, GreaterEqual, IsNull}
	for _, op := range o.Operators {
		if !slices.Contains(ops, op) {
			ops = append(ops, op)
		}
	}
	return ops
}

// Defaults returns the finite effective parser limits without changing its receiver.
func (l Limits) Defaults() Limits {
	if l.MaxBytes == 0 {
		l.MaxBytes = maxBytes
	}
	if l.MaxDepth == 0 {
		l.MaxDepth = maxDepth
	}
	if l.MaxNodes == 0 {
		l.MaxNodes = maxNodes
	}
	return l
}

type validation struct {
	options Options
	limits  Limits
	nodes   int
}

func (v *validation) expression(e *Expression, depth int) (Type, error) {
	v.nodes++
	if v.nodes > v.limits.MaxNodes || depth > v.limits.MaxDepth {
		return "", invalid("expression complexity")
	}
	switch e.Kind {
	case Property:
		typ, ok := v.options.Properties[e.Property]
		if !ok {
			return "", invalid("unknown property")
		}
		e.PropertyType = typ
		return typ, nil
	case LiteralKind:
		if e.Literal == nil {
			return "", invalid("literal")
		}
		if e.Literal.Geometry != nil {
			definition := v.options.CRSDefinition
			if definition == (common.CRS{}) && v.options.CRS != "" {
				definition, _ = common.CRSByURI(v.options.CRS)
			}
			if err := validateGeometry(e.Literal.Geometry, definition, &v.nodes, v.limits, depth); err != nil {
				return "", err
			}
		}
		return e.Literal.Type, nil
	case Operation:
		if !v.options.supports(e.Operator) {
			return "", invalid("unsupported operator")
		}
	default:
		return "", invalid("node kind")
	}
	types := make([]Type, len(e.Args))
	for i := range e.Args {
		typ, err := v.expression(&e.Args[i], depth+1)
		if err != nil {
			return "", err
		}
		types[i] = typ
	}
	op := e.Operator
	switch {
	case op == And || op == Or:
		if len(types) < 2 {
			return "", invalid("logical arity")
		}
		for i, typ := range types {
			if typ != Boolean || e.Args[i].Kind == Property {
				return "", invalid("logical operand")
			}
		}
	case op == Not:
		if len(types) != 1 || types[0] != Boolean || e.Args[0].Kind == Property {
			return "", invalid("not operand")
		}
	case op == IsNull:
		if len(types) != 1 {
			return "", invalid("null arity")
		}
	case spatialOperator(op):
		if len(types) != 2 || types[0] != GeometryType || types[1] != GeometryType {
			return "", invalid("spatial operand")
		}
	case temporalOperator(op):
		if len(types) != 2 || !temporal(types[0]) || !temporal(types[1]) {
			return "", invalid("temporal operand")
		}
		// CQL2 7.8.2 Table 6 forbids instant operands for these Allen
		// interval relations, even when a timestamp has equal start/end values.
		if intervalOnlyTemporal(e.Operator) && (types[0] != IntervalType || types[1] != IntervalType) {
			return "", invalid("interval operands required")
		}
		if !compatibleTemporal(e.Args[0], types[0], e.Args[1], types[1]) {
			return "", invalid("temporal granularity")
		}
	case op == Like:
		if len(types) != 2 || types[0] != String || types[1] != String || e.Args[1].Kind != LiteralKind {
			return "", invalid("like operand")
		}
		if err := v.propertyOperands(e.Args); err != nil {
			return "", err
		}
	case op == Between:
		if len(types) != 3 {
			return "", invalid("between arity")
		}
		for _, typ := range types {
			if !numeric(typ) {
				return "", invalid("between operand")
			}
		}
		if err := v.propertyOperands(e.Args); err != nil {
			return "", err
		}
	case op == In:
		if len(types) < 2 {
			return "", invalid("empty in list")
		}
		for i := 1; i < len(types); i++ {
			if !comparable(types[0], types[i]) {
				return "", invalid("in operand")
			}
		}
		if err := v.propertyOperands(e.Args); err != nil {
			return "", err
		}
	default:
		if len(types) != 2 || !comparable(types[0], types[1]) {
			return "", invalid("comparison operand")
		}
		if err := v.propertyOperands(e.Args); err != nil {
			return "", err
		}
	}
	return Boolean, nil
}

func (v *validation) propertyOperands(args []Expression) error {
	for _, arg := range args {
		if arg.Kind == Operation {
			return invalid("scalar operand required")
		}
	}
	if v.options.PropertyComparisons {
		return nil
	}
	if len(args) == 0 || args[0].Kind != Property {
		return invalid("property must be first operand")
	}
	for _, arg := range args[1:] {
		if arg.Kind != LiteralKind {
			return invalid("literal operand required")
		}
	}
	return nil
}

func (o Options) supports(op Operator) bool {
	if slices.Contains([]Operator{And, Or, Not, Equal, NotEqual, Less, LessEqual, Greater, GreaterEqual, IsNull}, op) {
		return true
	}
	return slices.Contains(o.Operators, op)
}

func (l Limits) normalized() (Limits, error) {
	l = l.Defaults()
	if l.MaxBytes < 1 || l.MaxBytes > maxBytes || l.MaxDepth < 1 || l.MaxDepth > maxDepth || l.MaxNodes < 1 || l.MaxNodes > maxNodes {
		return Limits{}, invalid("parser limits")
	}
	return l, nil
}

func validType(t Type) bool {
	return slices.Contains([]Type{String, Number, Integer, Boolean, Date, Timestamp, GeometryType, IntervalType}, t)
}
func numeric(t Type) bool  { return t == Number || t == Integer }
func temporal(t Type) bool { return t == Date || t == Timestamp || t == IntervalType }
func comparable(a, b Type) bool {
	return (a == b && a != GeometryType && a != IntervalType) || (numeric(a) && numeric(b))
}
func compatibleTemporal(a Expression, at Type, b Expression, bt Type) bool {
	if at == IntervalType && a.Literal != nil {
		at = intervalGranularity(a.Literal.Interval)
	}
	if bt == IntervalType && b.Literal != nil {
		bt = intervalGranularity(b.Literal.Interval)
	}
	return at == bt || at == IntervalType || bt == IntervalType || at == "" || bt == ""
}
func intervalGranularity(i *Interval) Type {
	if i == nil {
		return ""
	}
	if i.Start != nil {
		return i.Start.Type
	}
	if i.End != nil {
		return i.End.Type
	}
	return ""
}
func spatialOperator(op Operator) bool {
	return slices.Contains([]Operator{SIntersects, SEquals, SDisjoint, STouches, SWithin, SOverlaps, SCrosses, SContains}, op)
}
func temporalOperator(op Operator) bool {
	return slices.Contains([]Operator{TAfter, TBefore, TContains, TDisjoint, TDuring, TEquals, TFinishedBy, TFinishes, TIntersects, TMeets, TMetBy, TOverlappedBy, TOverlaps, TStartedBy, TStarts}, op)
}
func intervalOnlyTemporal(op Operator) bool {
	return slices.Contains([]Operator{TContains, TDuring, TFinishedBy, TFinishes, TMeets, TMetBy, TOverlappedBy, TOverlaps, TStartedBy, TStarts}, op)
}
func knownOperator(op Operator) bool {
	return spatialOperator(op) || temporalOperator(op) || slices.Contains([]Operator{And, Or, Not, Equal, NotEqual, Less, LessEqual, Greater, GreaterEqual, IsNull, Like, Between, In}, op)
}
func literal(value Literal) Expression { return Expression{Kind: LiteralKind, Literal: &value} }
func operation(op Operator, args ...Expression) Expression {
	return Expression{Kind: Operation, Operator: op, Args: args}
}
func invalid(reason string) error { return fmt.Errorf("%w: %s", ErrInvalid, reason) }
