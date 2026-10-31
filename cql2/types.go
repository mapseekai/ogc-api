// Package cql2 parses a bounded, backend-independent CQL2 profile. It does not
// evaluate expressions, generate database queries, or perform reprojection.
package cql2

import (
	"encoding/json"

	"github.com/mapseekai/ogc-api/common"
)

// Standard CQL2 encodings supported by Parse.
const (
	Text Language = "cql2-text"
	JSON Language = "cql2-json"
)

// Scalar, spatial and temporal types understood by this parser.
const (
	String       Type = "string"
	Number       Type = "number"
	Integer      Type = "integer"
	Boolean      Type = "boolean"
	Date         Type = "date"
	Timestamp    Type = "timestamp"
	GeometryType Type = "geometry"
	IntervalType Type = "interval"
)

// Basic comparison and logical operators are always supported. Other operators
// must be explicitly enabled in Options.Operators by the external reader.
const (
	And           Operator = "and"
	Or            Operator = "or"
	Not           Operator = "not"
	Equal         Operator = "="
	NotEqual      Operator = "<>"
	Less          Operator = "<"
	LessEqual     Operator = "<="
	Greater       Operator = ">"
	GreaterEqual  Operator = ">="
	IsNull        Operator = "isNull"
	Like          Operator = "like"
	Between       Operator = "between"
	In            Operator = "in"
	SIntersects   Operator = "s_intersects"
	SEquals       Operator = "s_equals"
	SDisjoint     Operator = "s_disjoint"
	STouches      Operator = "s_touches"
	SWithin       Operator = "s_within"
	SOverlaps     Operator = "s_overlaps"
	SCrosses      Operator = "s_crosses"
	SContains     Operator = "s_contains"
	TAfter        Operator = "t_after"
	TBefore       Operator = "t_before"
	TContains     Operator = "t_contains"
	TDisjoint     Operator = "t_disjoint"
	TDuring       Operator = "t_during"
	TEquals       Operator = "t_equals"
	TFinishedBy   Operator = "t_finishedBy"
	TFinishes     Operator = "t_finishes"
	TIntersects   Operator = "t_intersects"
	TMeets        Operator = "t_meets"
	TMetBy        Operator = "t_metBy"
	TOverlappedBy Operator = "t_overlappedBy"
	TOverlaps     Operator = "t_overlaps"
	TStartedBy    Operator = "t_startedBy"
	TStarts       Operator = "t_starts"
)

// Expression node kinds.
const (
	Property    Kind = "property"
	LiteralKind Kind = "literal"
	Operation   Kind = "operation"
)

// Language identifies a standard CQL2 wire encoding.
type Language string

// Type distinguishes property and literal types; Integer describes a queryable.
type Type string

// Operator uses the canonical CQL2 JSON spelling, independent of source encoding.
type Operator string

// Kind makes property references, typed literals and operations unambiguous.
type Kind string

// Expression is an owned syntax tree, not an executable query. Exactly one of
// Property/PropertyType, Literal, or Operator/Args is populated according to Kind. IN stores
// its non-empty list as Args[1:]. NOT IN/LIKE/BETWEEN and IS NOT NULL use Not.
// Returned trees have no mutable aliases to input, policy, or another request.
// The owner may retain a tree indefinitely, including beyond an API Handle call.
type Expression struct {
	Kind         Kind
	Property     string
	PropertyType Type
	Literal      *Literal
	Operator     Operator
	Args         []Expression
}

// Literal holds a typed value. Text is used for string/date/timestamp, Number is
// an exact normalized decimal/scientific value (never a float64), and only the corresponding
// value field is populated. NULL is a runtime value tested with IsNull, not
// a CQL2 literal. Dates retain day granularity.
type Literal struct {
	Type     Type
	Text     string
	Number   json.Number
	Boolean  bool
	Geometry *Geometry
	Interval *Interval
}

// Geometry holds validated Simple Feature coordinate structure, not topology.
// Type is a GeoJSON geometry name, or "BBox". BBox is a spatial literal, NOT the
// Part 1 feature bbox predicate. CRS is normalized per literal (CRS84/CRS84h).
// GeometryCollection uses Geometries; BBox uses Bounds; others use Coordinates.
// CRSDefinition records native axes/dimension; coordinates are never rewritten.
type Geometry struct {
	Type string
	CRS  string
	// CRSDefinition retains declared native axes/dimensions, without transformation.
	CRSDefinition common.CRS
	Coordinates   Coordinates
	Bounds        []json.Number
	Envelope      []json.Number
	Geometries    []Geometry
}

// Coordinates represents GeoJSON nesting without interface{} or float rounding.
// A position uses Position; all higher coordinate levels use Children.
type Coordinates struct {
	Position []json.Number
	Children []Coordinates
}

// Interval stores literal temporal endpoints; nil means the open ".." bound.
// Non-nil endpoints are Date or Timestamp literals with matching granularity.
type Interval struct {
	Start *Literal
	End   *Literal
}

// Limits bounds input before allocation and recursion. Zero selects finite defaults.
// MaxNodes also bounds JSON values and coordinate containers, not only AST nodes.
// Nonzero values may lower, but never raise, the hard defaults.
type Limits struct {
	MaxBytes int `json:"maxBytes"`
	MaxDepth int `json:"maxDepth"`
	MaxNodes int `json:"maxNodes"`
}

// Options declares queryable types and reader-supported extensions. A missing
// property is rejected; no database schema is inferred. Operators nil enables
// only Basic CQL2; binary scalar comparisons default to property/literal.
// IsNull accepts supported values and boolean expressions in both encodings.
// CRS empty uses CRS84 for 2D literals and CRS84h for 3D literals. Explicit CRS
// must match dimension. Additional systems require matching CRSDefinition metadata.
// Options are borrowed only for Parse and never retained by a returned expression.
type Options struct {
	Properties          map[string]Type
	Operators           []Operator
	PropertyComparisons bool
	CRS                 string
	// CRSDefinition is required for a non-default CRS and must match CRS exactly.
	// Zero retains standard CRS84/CRS84h handling. It never enables an authority
	// alias or causes coordinate conversion; returned geometry owns the metadata.
	CRSDefinition common.CRS
	Limits        Limits
}
