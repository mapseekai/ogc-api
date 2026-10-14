package server

import (
	"context"
	"errors"

	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

// Snapshot owns private copies of its maps. Publish a replacement snapshot instead
// of mutating an existing one; one request only calls CatalogProvider once.
type Snapshot struct {
	wms  *wms.Catalog
	wmts *wmts.Catalog
}

// NewSnapshot checks and deep-copies one or both protocol catalogs.
// The source must not be mutated during construction; later source changes cannot affect the snapshot.
// Application validator functions remain shared and must be concurrency-safe.
func NewSnapshot(wmsCatalog *wms.Catalog, wmtsCatalog *wmts.Catalog) (*Snapshot, error) {
	if wmsCatalog == nil && wmtsCatalog == nil {
		return nil, errors.New("at least one protocol catalog is required")
	}
	s := &Snapshot{}
	if wmsCatalog != nil {
		copy := wmsCatalog.Clone()
		if err := copy.Check(); err != nil {
			return nil, err
		}
		s.wms = &copy
	}
	if wmtsCatalog != nil {
		copy := wmtsCatalog.Clone()
		if err := copy.Check(); err != nil {
			return nil, err
		}
		s.wmts = &copy
	}
	return s, nil
}

// Static returns the same immutable snapshot for every request and scope.
// It performs no authorization; use a filtered dynamic provider when visibility differs by caller.
// A nil snapshot produces an error when requested.
func Static(snapshot *Snapshot) CatalogProvider {
	return func(context.Context, string) (*Snapshot, error) {
		if snapshot == nil {
			return nil, errors.New("nil catalog snapshot")
		}
		return snapshot, nil
	}
}

// CatalogProvider must return a catalog already filtered for the caller's access.
// Scope is chosen by the host, never taken from a SERVICE or tenant query value.
type CatalogProvider func(ctx context.Context, scope string) (*Snapshot, error)
