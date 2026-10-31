// Package features defines OGC API Features query and streaming result contracts.
package features

import (
	"context"
	"encoding/json"

	"github.com/mapseekai/ogc-api/common"
)

// Query contains normalized feature predicates. All predicates are combined with AND.
// Cursor is an opaque provider-defined pagination token, never a database statement.
type Query struct {
	BBox      []float64
	BBoxCRS   common.CRS
	OutputCRS common.CRS
	Datetime  *common.Interval
	Limit     uint64
	Cursor    string
	// Offset selects the opt-in offset extension. Nil preserves cursor/default
	// pagination; a non-nil zero is an explicit first offset page.
	Offset     *uint64
	Properties map[string]string
	Filter     *Filter
}

// Request identifies a collection or one item. BBox retains its native BBoxCRS
// axes; OutputCRS independently selects all response geometry-valued properties.
// Filter geometry literals carry their own CRS, independent of both fields.
type Request struct {
	CollectionID string
	ItemID       string
	Query        Query
}

// Feature carries a GeoJSON feature. ID accepts string, json.Number or built-in
// integer/float types; custom marshalers and pointers are rejected.
// Geometry and Properties are complete JSON values; nil encodes as JSON null.
// Providers retain no mutable aliases after returning a feature.
type Feature struct {
	// CRS acknowledges the actual native coordinates of every geometry-valued
	// property, not an instruction to reproject. It is not a GeoJSON crs member.
	// Part 2 readers must set it for Item; legacy readers leave it zero.
	CRS        common.CRS      `json:"-"`
	Type       string          `json:"type"`
	ID         any             `json:"id,omitempty"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties json.RawMessage `json:"properties"`
	BBox       []float64       `json:"bbox,omitempty"`
	Links      []common.Link   `json:"links,omitempty"`
}

// Page transfers ownership of Items and metadata pointers even when returned
// alongside an error; providers retain no mutable aliases.
// NumberMatched is optional; NextCursor is non-empty only when a subsequent page exists.
type Page struct {
	// CRS acknowledges the actual output CRS of every returned geometry-valued
	// property. Part 2 readers must set it, even for an empty page.
	CRS           common.CRS
	Items         Iterator
	NumberMatched *uint64
	NextCursor    string
	// NextOffset is non-nil only for an offset request with a subsequent page.
	// It must advance beyond the requested offset. Nil means terminal, including
	// an exactly full final page; totals are not required to determine this.
	NextOffset *uint64
}

// Close releases page content when it cannot be handed to a response.
func (p Page) Close() error {
	if p.Items == nil {
		return nil
	}
	return p.Items.Close()
}

// Iterator yields one exclusively owned feature at a time; io.EOF marks completion.
// Next must honor cancellation and return no more than the requested Limit.
// Close releases database cursors or remote bodies and may be called before the first Next.
// Calls to Next and Close are serialized by the response owner.
type Iterator interface {
	Next(context.Context) (Feature, error)
	Close() error
}

// Reader is a read-only feature capability. Editors may be defined separately.
// Implementations are shared between requests and must protect their mutable state.
// Items transfers the iterator to its caller, including on error; Item returns no owned stream.
// Readers MUST apply Query.Filter AND BBox AND Datetime AND Properties. Evaluate
// CQL2 with three-valued logic: only TRUE includes an item; FALSE/NULL exclude it.
// BBox retains Part 1 semantics and is not shorthand for CQL2 S_INTERSECTS.
// BBox is interpreted in Query.BBoxCRS axes and dimensions, never OutputCRS or
// Filter.CRS. Readers perform all coordinate operations and must honor height
// bounds, not invent Z=0 for missing coordinates. Part 2 readers return the actual
// output CRS in Page.CRS / Feature.CRS; merely relabeling geometry is invalid.
// A reader/iterator may retain Request and its AST; the library never pools them.
type Reader interface {
	Items(context.Context, Request) (Page, error)
	Item(context.Context, Request) (Feature, error)
}

// Options declares pagination limits and explicitly supported property-filter names.
// Zero pagination values select 10 default features and a maximum of 1000.
type Options struct {
	DefaultLimit     uint64
	MaxLimit         uint64
	FilterProperties []string
	// OffsetPagination declares that the Reader applies offset after all
	// predicates/authorization. It never converts offset into a keyset cursor.
	OffsetPagination bool
	// Filter enables Part 3 only when the reader implements the declared capabilities.
	Filter *FilterOptions
	// CRS enables Part 2 only for explicitly declared backend coordinate systems.
	// Nil retains the Part 1-only parameter policy and legacy result contract.
	CRS *CRSOptions
}

// Service binds resource-specific query policy to an external feature reader.
type Service struct {
	Reader  Reader
	Options Options
}
