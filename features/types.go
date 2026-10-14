// Package features defines OGC API Features query and streaming result contracts.
package features

import (
	"context"
	"encoding/json"

	"github.com/mapseekai/ogc-api/common"
)

// Feature carries a GeoJSON feature. ID accepts string, json.Number or built-in
// integer/float types; custom marshalers and pointers are rejected.
// Geometry and Properties are complete JSON values; nil encodes as JSON null.
// Providers retain no mutable aliases after returning a feature.
type Feature struct {
	Type       string          `json:"type"`
	ID         any             `json:"id,omitempty"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties json.RawMessage `json:"properties"`
	BBox       []float64       `json:"bbox,omitempty"`
	Links      []common.Link   `json:"links,omitempty"`
}

// Query contains normalized feature predicates. All predicates are combined with AND.
// Cursor is an opaque provider-defined pagination token, never a database statement.
type Query struct {
	BBox       []float64
	Datetime   *common.Interval
	Limit      uint64
	Cursor     string
	Properties map[string]string
}

// Request identifies a collection or one item. All coordinates use CRS84 in this profile.
type Request struct {
	CollectionID string
	ItemID       string
	Query        Query
}

// Iterator yields one exclusively owned feature at a time; io.EOF marks completion.
// Next must honor cancellation and return no more than the requested Limit.
// Close releases database cursors or remote bodies and may be called before the first Next.
// Calls to Next and Close are serialized by the response owner.
type Iterator interface {
	Next(context.Context) (Feature, error)
	Close() error
}

// Page transfers ownership of Items even when returned alongside an error.
// NumberMatched is optional; NextCursor is non-empty only when a subsequent page exists.
type Page struct {
	Items         Iterator
	NumberMatched *uint64
	NextCursor    string
}

// Close releases page content when it cannot be handed to a response.
func (p Page) Close() error {
	if p.Items == nil {
		return nil
	}
	return p.Items.Close()
}

// Reader is a read-only feature capability. Editors may be defined separately.
// Implementations are shared between requests and must protect their mutable state.
// Items transfers the iterator to its caller, including on error; Item returns no owned stream.
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
}

// Service binds resource-specific query policy to an external feature reader.
type Service struct {
	Reader  Reader
	Options Options
}
