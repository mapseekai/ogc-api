package features

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
)

// Filter is an exclusively owned, normalized predicate and optional source metadata.
// Expression is ready for a backend visitor; the backend never needs to reparse URL
// text or JSON. CRS is the explicit filter-crs URI, or empty when per-literal CRS84 /
// CRS84h defaults apply; each geometry already carries its effective CRS and
// native-axis definition. Additional filter systems require their own capability.
type Filter struct {
	Expression cql2.Expression
	Language   cql2.Language
	CRS        string
	// CRSDefinition describes an explicit filter-crs; zero means per-literal defaults.
	CRSDefinition common.CRS
}

// FilterOptions enables Part 3 parsing for one collection. Queryables is a closed
// allowlist, independent of feature output properties and the database schema.
// Operators declares only extensions the external reader can actually execute;
// Basic CQL2 is always parsed. Languages nil supports both standard encodings.
// No evaluation/server conformance is inferred from enabling this policy.
type FilterOptions struct {
	Queryables          map[string]Queryable
	Operators           []cql2.Operator
	Languages           []cql2.Language
	PropertyComparisons bool
	Limits              cql2.Limits
	// AdditionalCRS explicitly enables non-default filter literal coordinate systems.
	// Output CRS support alone never enables these operations. In Features Options
	// each entry must also match the collection's Part 2 supported CRS declaration.
	AdditionalCRS []common.CRS
}

// Queryable declares the type and optional discovery annotations of one field.
// Geometry uses Type cql2.GeometryType and a geometry-* Format (empty means
// geometry-any); temporal types use cql2.Date or cql2.Timestamp. Role is optional
// JSON Schema x-ogc-role metadata, not a query execution instruction.
type Queryable struct {
	Type        cql2.Type
	Title       string
	Description string
	Format      string
	Role        string
}

// QueryablesSchema is the Part 3 JSON Schema resource. AdditionalProperties is
// deliberately false: undeclared references fail before any Reader invocation.
// Extensions expose the parser/reader contract without asserting OGC conformance.
type QueryablesSchema struct {
	Schema               string                     `json:"$schema"`
	ID                   string                     `json:"$id"`
	Type                 string                     `json:"type"`
	Title                string                     `json:"title,omitempty"`
	Description          string                     `json:"description,omitempty"`
	Properties           map[string]QueryableSchema `json:"properties"`
	AdditionalProperties bool                       `json:"additionalProperties"`
	Languages            []cql2.Language            `json:"x-ogc-filter-languages"`
	Operators            []cql2.Operator            `json:"x-ogc-filter-operators"`
	PropertyComparisons  bool                       `json:"x-ogc-property-comparisons"`
	Limits               cql2.Limits                `json:"x-ogc-filter-limits"`
	FilterCRS            []string                   `json:"x-ogc-filter-crs"`
}

// QueryableSchema is a discoverable scalar, temporal or spatial field schema.
// Spatial schemas intentionally omit type and $ref as required by Part 3.
type QueryableSchema struct {
	Type        string `json:"type,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Format      string `json:"format,omitempty"`
	Role        string `json:"x-ogc-role,omitempty"`
}

// Defaults returns an independent filter policy, suitable for immutable publication.
func (o FilterOptions) Defaults() FilterOptions {
	o.Queryables = maps.Clone(o.Queryables)
	o.Operators = slices.Clone(o.Operators)
	o.Languages = slices.Clone(o.Languages)
	o.AdditionalCRS = slices.Clone(o.AdditionalCRS)
	if len(o.Languages) == 0 {
		o.Languages = []cql2.Language{cql2.Text, cql2.JSON}
	}
	o.Limits = o.Limits.Defaults()
	return o
}

// Validate rejects metadata that disagrees with supported parser types or capabilities.
func (o FilterOptions) Validate() error {
	if len(o.Queryables) == 0 || len(o.Queryables) > 4096 {
		return fmt.Errorf("feature queryables must contain 1 to 4096 fields")
	}
	if err := o.parserOptions(common.CRS{}).Validate(); err != nil {
		return err
	}
	if len(o.AdditionalCRS) > 256 {
		return fmt.Errorf("too many additional filter coordinate systems")
	}
	for i, crs := range o.AdditionalCRS {
		if err := crs.Validate(); err != nil {
			return err
		}
		if crs.URI == common.CRS84 || crs.URI == common.CRS84h {
			return fmt.Errorf("filter defaults are not additional coordinate systems")
		}
		for _, previous := range o.AdditionalCRS[:i] {
			if previous.URI == crs.URI {
				return fmt.Errorf("duplicate additional filter CRS")
			}
		}
	}
	for name, field := range o.Queryables {
		if !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
			return fmt.Errorf("invalid queryable name")
		}
		if _, err := field.schema(); err != nil {
			return err
		}
	}
	for i, language := range o.Languages {
		if (language != cql2.Text && language != cql2.JSON) || slices.Contains(o.Languages[:i], language) {
			return fmt.Errorf("invalid filter language declaration")
		}
	}
	// A text default is required by Part 3; JSON-only profiles must not silently
	// change that default when filter-lang is omitted.
	if len(o.Languages) > 0 && !slices.Contains(o.Languages, cql2.Text) {
		return fmt.Errorf("filter policy requires the default cql2-text language")
	}
	return nil
}

// Schema builds an independent JSON Schema for the specified queryables URI.
// The caller supplies trusted collection annotations and an absolute resource URI.
func (o FilterOptions) Schema(id, title, description string) (QueryablesSchema, error) {
	if err := o.Validate(); err != nil {
		return QueryablesSchema{}, err
	}
	o = o.Defaults()
	schema := QueryablesSchema{Schema: "https://json-schema.org/draft/2020-12/schema", ID: id, Type: "object", Title: title, Description: description,
		Properties: make(map[string]QueryableSchema, len(o.Queryables)), Languages: o.Languages, Operators: o.parserOptions(common.CRS{}).SupportedOperators(), PropertyComparisons: o.PropertyComparisons, Limits: o.Limits,
		FilterCRS: []string{common.CRS84, common.CRS84h}}
	for _, crs := range o.AdditionalCRS {
		schema.FilterCRS = append(schema.FilterCRS, crs.URI)
	}
	for name, field := range o.Queryables {
		value, err := field.schema()
		if err != nil {
			return QueryablesSchema{}, err
		}
		schema.Properties[name] = value
	}
	return schema, nil
}

func (o FilterOptions) parserOptions(crs common.CRS) cql2.Options {
	properties := make(map[string]cql2.Type, len(o.Queryables))
	for name, field := range o.Queryables {
		properties[name] = field.Type
	}
	return cql2.Options{Properties: properties, Operators: o.Operators, PropertyComparisons: o.PropertyComparisons, CRS: crs.URI, CRSDefinition: crs, Limits: o.Limits}
}

func (o FilterOptions) parse(values common.Query) (*Filter, error) {
	raw, present := values["filter"]
	language := cql2.Text
	if value, ok := values["filter-lang"]; ok {
		switch cql2.Language(value) {
		case cql2.Text:
			language = cql2.Text
		case cql2.JSON:
			language = cql2.JSON
		default:
			return nil, common.Invalid("filter-lang")
		}
	}
	languages := o.Languages
	if len(languages) == 0 {
		languages = []cql2.Language{cql2.Text, cql2.JSON}
	}
	if !slices.Contains(languages, language) {
		return nil, common.Invalid("filter-lang")
	}
	crs := common.CRS{}
	if value, ok := values["filter-crs"]; ok {
		switch value {
		case common.CRS84:
			crs, _ = common.CRSByURI(common.CRS84)
		case common.CRS84h:
			crs, _ = common.CRSByURI(common.CRS84h)
		default:
			for _, definition := range o.AdditionalCRS {
				if value == definition.URI {
					crs = definition
					crs.URI = strings.Clone(crs.URI)
					break
				}
			}
			if crs == (common.CRS{}) {
				return nil, common.Invalid("filter-crs")
			}
		}
	}
	if !present {
		if _, ok := values["filter-lang"]; ok {
			return nil, common.Invalid("filter-lang")
		}
		if _, ok := values["filter-crs"]; ok {
			return nil, common.Invalid("filter-crs")
		}
		return nil, nil
	}
	expression, err := cql2.Parse(raw, language, o.parserOptions(crs))
	if err != nil {
		failure := common.Invalid("filter")
		failure.Cause = err
		return nil, failure
	}
	return &Filter{Expression: expression, Language: language, CRS: crs.URI, CRSDefinition: crs}, nil
}

func (q Queryable) schema() (QueryableSchema, error) {
	s := QueryableSchema{Title: q.Title, Description: q.Description, Role: q.Role, Format: q.Format}
	switch q.Type {
	case cql2.String, cql2.Number, cql2.Integer, cql2.Boolean:
		s.Type = string(q.Type)
		if q.Format != "" && q.Type != cql2.String {
			return QueryableSchema{}, fmt.Errorf("format requires string or spatial queryable")
		}
		if q.Type == cql2.String && !slices.Contains([]string{"", "uri", "uri-reference", "uri-template", "uuid"}, q.Format) {
			return QueryableSchema{}, fmt.Errorf("unsupported queryable string format")
		}
	case cql2.Date, cql2.Timestamp:
		s.Type, s.Format = "string", "date"
		if q.Type == cql2.Timestamp {
			s.Format = "date-time"
		}
		if q.Format != "" && q.Format != s.Format {
			return QueryableSchema{}, fmt.Errorf("inconsistent temporal queryable format")
		}
	case cql2.GeometryType:
		if q.Format == "" {
			s.Format = "geometry-any"
		}
		if !slices.Contains([]string{"geometry-any", "geometry-point", "geometry-multipoint", "geometry-linestring", "geometry-multilinestring", "geometry-polygon", "geometry-multipolygon", "geometry-geometrycollection", "geometry-point-or-multipoint", "geometry-linestring-or-multilinestring", "geometry-polygon-or-multipolygon"}, s.Format) {
			return QueryableSchema{}, fmt.Errorf("unsupported spatial queryable format")
		}
	default:
		return QueryableSchema{}, fmt.Errorf("unsupported queryable type")
	}
	return s, nil
}
