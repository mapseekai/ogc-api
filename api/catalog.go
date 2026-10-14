package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/internal/jsonutil"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/processes"
	"github.com/mapseekai/ogc-api/tiles"
)

// Collection combines a public description with independently optional external capabilities.
// A nil capability publishes metadata only. Providers are not copied and must be concurrency-safe.
type Collection struct {
	Metadata common.Collection
	Features *features.Service
	Map      *maps.Service
	Coverage *coverages.Service
	Tiles    []tiles.Service
}

// Catalog is mutable publication input. Build a new Snapshot before publishing any changes.
// MetadataBudget limits the total pre-encoded discovery documents; zero uses 64 MiB.
// Title and BaseURL are trusted public values and must not contain credentials.
type Catalog struct {
	// MetadataCacheControl defaults to private, no-cache to protect caller-specific catalogs.
	MetadataCacheControl string
	Processes            []processes.Service
	Jobs                 *processes.Jobs
	Title                string
	Description          string
	BaseURL              string
	Collections          []Collection
	MatrixSets           []tiles.MatrixSet
	MetadataBudget       int
}

// Snapshot owns immutable metadata, indexes and backend bindings. Only NewSnapshot constructs
// a usable value. The host must not close replaced providers until their in-flight requests finish.
type Snapshot struct {
	metadataCacheControl string
	processes            map[string]processes.Service
	jobs                 *processes.Jobs
	baseURL              string
	basePath             string
	collections          map[string]collectionBinding
	grids                map[string]*tiles.Grid
	documents            map[string]document
}

// NewSnapshot validates, copies and pre-encodes resource metadata outside the request path.
// The input must not change during this call. Publication never invokes a provider.
func NewSnapshot(catalog Catalog) (*Snapshot, error) {
	endpoint, err := url.Parse(catalog.BaseURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || strings.ContainsAny(catalog.BaseURL, "\r\n\x00") {
		return nil, fmt.Errorf("API base URL must be a trusted absolute HTTP(S) URL without query, credentials or fragment")
	}
	if catalog.Title == "" {
		return nil, fmt.Errorf("API title is required")
	}
	budget := catalog.MetadataBudget
	if budget == 0 {
		budget = 64 << 20
	}
	if budget < 1 {
		return nil, fmt.Errorf("invalid metadata budget")
	}
	if len(catalog.Collections) > budget || len(catalog.MatrixSets) > budget || len(catalog.Processes) > budget {
		return nil, jsonutil.ErrTooLarge
	}
	if err := jsonutil.CheckSize([]string{catalog.Title, catalog.Description, catalog.BaseURL}, budget); err != nil {
		return nil, err
	}
	snapshot := &Snapshot{baseURL: strings.TrimSuffix(endpoint.String(), "/"), basePath: strings.TrimSuffix(endpoint.EscapedPath(), "/"),
		collections: make(map[string]collectionBinding, len(catalog.Collections)), grids: make(map[string]*tiles.Grid, len(catalog.MatrixSets)), documents: make(map[string]document)}
	snapshot.metadataCacheControl = catalog.MetadataCacheControl
	if snapshot.metadataCacheControl == "" {
		snapshot.metadataCacheControl = "private, no-cache"
	}
	for _, char := range snapshot.metadataCacheControl {
		if char < 32 || char == 127 {
			return nil, fmt.Errorf("invalid metadata cache control")
		}
	}
	add := func(path string, value any) error {
		data, err := jsonutil.Marshal(value, budget)
		if err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}
		budget -= len(data)
		hash := sha256.Sum256(data)
		snapshot.documents[path] = document{data: data, media: common.JSON, etag: `"` + hex.EncodeToString(hash[:]) + `"`}
		return nil
	}
	if err := snapshot.prepareGrids(catalog.MatrixSets, &budget, add); err != nil {
		return nil, err
	}
	descriptions, hasFeatures, hasTiles, err := snapshot.prepareCollections(catalog.Collections, &budget, add)
	if err != nil {
		return nil, err
	}
	if err := snapshot.prepareProcesses(catalog.Processes, catalog.Jobs, &budget, add); err != nil {
		return nil, err
	}
	if err := snapshot.encodeDiscovery(catalog.Title, catalog.Description, descriptions, hasFeatures, hasTiles, add); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// Static returns the same publication for every scope. It does not authorize requests.
func Static(snapshot *Snapshot) CatalogProvider {
	return func(context.Context, string) (*Snapshot, error) {
		if snapshot == nil || snapshot.baseURL == "" {
			return nil, fmt.Errorf("nil or uninitialized API snapshot")
		}
		return snapshot, nil
	}
}

// AtomicCatalog atomically publishes metadata and backend bindings as one unit.
// Its zero value contains no publication. Do not copy after first use.
// This is a single visibility domain; hosts with per-user visibility supply a CatalogProvider.
type AtomicCatalog struct{ current atomic.Pointer[Snapshot] }

// Publish replaces the current publication without locking readers. Existing requests
// retain their original snapshot; it never closes or reconfigures a backend.
func (c *AtomicCatalog) Publish(snapshot *Snapshot) error {
	if snapshot == nil || snapshot.baseURL == "" {
		return fmt.Errorf("nil or uninitialized API snapshot")
	}
	c.current.Store(snapshot)
	return nil
}

// Load implements CatalogProvider and returns the current immutable publication.
func (c *AtomicCatalog) Load(ctx context.Context, _ string) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot := c.current.Load()
	if snapshot == nil {
		return nil, common.ErrUnavailable
	}
	return snapshot, nil
}

type collectionBinding struct {
	metadata  common.Collection
	features  *features.Service
	portrayal *maps.Service
	coverage  *coverages.Service
	tiles     map[string]tileBinding
}

type tileBinding struct {
	service tiles.Service
	grid    *tiles.Grid
	limits  map[string]tiles.Limit
}

type document struct {
	data  []byte
	media string
	etag  string
}

type matrixSetItem struct {
	ID    string        `json:"id"`
	Links []common.Link `json:"links"`
}

func (s *Snapshot) prepareGrids(sets []tiles.MatrixSet, budget *int, add func(string, any) error) error {
	for _, set := range sets {
		if !common.ValidID(set.ID) {
			return fmt.Errorf("invalid matrix set id")
		}
		if _, ok := s.grids[set.ID]; ok {
			return fmt.Errorf("duplicate matrix set")
		}
		if err := jsonutil.CheckSize(set, *budget); err != nil {
			return err
		}
		grid, err := tiles.NewGrid(set)
		if err != nil {
			return err
		}
		s.grids[set.ID] = grid
		if err := add("/tileMatrixSets/"+url.PathEscape(set.ID), grid.Definition()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Snapshot) prepareCollections(resources []Collection, budget *int, add func(string, any) error) ([]common.Collection, bool, bool, error) {
	descriptions := make([]common.Collection, 0, len(resources))
	hasFeatures, hasTiles := false, false
	for _, resource := range resources {
		binding, err := s.prepareCollection(resource, *budget, add)
		if err != nil {
			return nil, false, false, err
		}
		s.collections[binding.metadata.ID] = binding
		descriptions = append(descriptions, binding.metadata)
		hasFeatures = hasFeatures || binding.features != nil
		hasTiles = hasTiles || len(binding.tiles) > 0
	}
	slices.SortFunc(descriptions, func(a, b common.Collection) int { return strings.Compare(a.ID, b.ID) })
	return descriptions, hasFeatures, hasTiles, nil
}

func (s *Snapshot) prepareCollection(resource Collection, budget int, add func(string, any) error) (collectionBinding, error) {
	if err := checkCollection(resource.Metadata); err != nil {
		return collectionBinding{}, err
	}
	metadata, err := copyMetadata(resource.Metadata, budget)
	if err != nil {
		return collectionBinding{}, err
	}
	if _, ok := s.collections[metadata.ID]; ok {
		return collectionBinding{}, fmt.Errorf("duplicate collection id")
	}
	path := "/collections/" + url.PathEscape(metadata.ID)
	metadata.Links = append(metadata.Links, s.link(path, "self", common.JSON), s.link("/collections", "parent", common.JSON))
	binding := collectionBinding{}
	if resource.Features != nil {
		service := *resource.Features
		if service.Reader == nil {
			return collectionBinding{}, fmt.Errorf("feature reader is required")
		}
		if err := service.Options.Validate(); err != nil {
			return collectionBinding{}, err
		}
		service.Options = service.Options.Defaults()
		binding.features = &service
		if len(metadata.CRS) > 0 && (len(metadata.CRS) != 1 || metadata.CRS[0] != common.CRS84) {
			return collectionBinding{}, fmt.Errorf("feature core responses must declare CRS84")
		}
		metadata.ItemType = "feature"
		metadata.Links = append(metadata.Links, s.link(path+"/items", "items", common.GeoJSON))
	}
	if resource.Map != nil {
		service, err := resource.Map.Prepare()
		if err != nil {
			return collectionBinding{}, err
		}
		binding.portrayal = &service
		for _, format := range service.Formats {
			metadata.Links = append(metadata.Links, s.link(path+"/map?f="+url.QueryEscape(format), "http://www.opengis.net/def/rel/ogc/1.0/map", format))
		}
	}
	if resource.Coverage != nil {
		service, err := resource.Coverage.Prepare()
		if err != nil {
			return collectionBinding{}, err
		}
		binding.coverage = &service
		for _, format := range service.Formats {
			metadata.Links = append(metadata.Links, s.link(path+"/coverage?f="+url.QueryEscape(format), "http://www.opengis.net/def/rel/ogc/1.0/coverage", format))
		}
		if len(service.Schema) > 0 {
			if err := add(path+"/schema", service.Schema); err != nil {
				return collectionBinding{}, err
			}
			doc := s.documents[path+"/schema"]
			doc.media = "application/schema+json"
			s.documents[path+"/schema"] = doc
			metadata.Links = append(metadata.Links, s.link(path+"/schema", "http://www.opengis.net/def/rel/ogc/1.0/schema", doc.media))
		}
	}
	binding.tiles, err = s.prepareTiles(resource.Tiles, path, &metadata, add)
	if err != nil {
		return collectionBinding{}, err
	}
	binding.metadata = metadata
	if err := add(path, metadata); err != nil {
		return collectionBinding{}, err
	}
	return binding, nil
}

func (s *Snapshot) prepareTiles(services []tiles.Service, path string, metadata *common.Collection, add func(string, any) error) (map[string]tileBinding, error) {
	bindings := make(map[string]tileBinding, len(services))
	tileDescriptions := make([]tiles.TileSet, 0, len(services))
	for _, service := range services {
		grid, ok := s.grids[service.MatrixSetID]
		if !ok || service.Reader == nil || len(service.Formats) == 0 {
			return nil, fmt.Errorf("invalid tile service")
		}
		if _, ok := bindings[service.MatrixSetID]; ok {
			return nil, fmt.Errorf("duplicate collection tileset")
		}
		if !slices.Contains([]string{"vector", "map", "coverage"}, service.DataType) {
			return nil, fmt.Errorf("invalid tileset dataType")
		}
		if err := owscore.ValidateFormats(service.Formats); err != nil {
			return nil, err
		}
		service.Formats = slices.Clone(service.Formats)
		service.Limits = slices.Clone(service.Limits)
		limits, err := grid.CheckLimits(service.Limits)
		if err != nil {
			return nil, err
		}
		bindings[service.MatrixSetID] = tileBinding{service: service, grid: grid, limits: limits}
		tilePath := path + "/tiles/" + url.PathEscape(service.MatrixSetID)
		definition := grid.Definition()
		descriptor := tiles.TileSet{DataType: service.DataType, CRS: definition.CRS, MatrixSetURI: s.baseURL + "/tileMatrixSets/" + url.PathEscape(service.MatrixSetID), Limits: service.Limits,
			Links: []common.Link{s.link(tilePath, "self", common.JSON), s.link("/tileMatrixSets/"+url.PathEscape(service.MatrixSetID), "http://www.opengis.net/def/rel/ogc/1.0/tiling-scheme", common.JSON)}}
		for _, format := range service.Formats {
			link := s.link(tilePath+"/{tileMatrix}/{tileRow}/{tileCol}?f="+url.QueryEscape(format), "item", format)
			link.Templated = true
			descriptor.Links = append(descriptor.Links, link)
		}
		if err := add(tilePath, descriptor); err != nil {
			return nil, err
		}
		tileDescriptions = append(tileDescriptions, descriptor)
	}
	if len(services) > 0 {
		slices.SortFunc(tileDescriptions, func(a, b tiles.TileSet) int { return strings.Compare(a.MatrixSetURI, b.MatrixSetURI) })
		if err := add(path+"/tiles", struct {
			TileSets []tiles.TileSet `json:"tilesets"`
			Links    []common.Link   `json:"links"`
		}{tileDescriptions, []common.Link{s.link(path+"/tiles", "self", common.JSON)}}); err != nil {
			return nil, err
		}
		types := make(map[string]bool)
		for _, service := range services {
			types[service.DataType] = true
		}
		for _, dataType := range []string{"vector", "map", "coverage"} {
			if types[dataType] {
				metadata.Links = append(metadata.Links, s.link(path+"/tiles", "http://www.opengis.net/def/rel/ogc/1.0/tilesets-"+dataType, common.JSON))
			}
		}
	}
	return bindings, nil
}

func (s *Snapshot) encodeDiscovery(title, description string, descriptions []common.Collection, hasFeatures, hasTiles bool, add func(string, any) error) error {
	conformance := []string{
		"http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/landing-page",
		"http://www.opengis.net/spec/ogcapi-common-1/1.0/conf/json",
	}
	if hasFeatures {
		conformance = append(conformance, "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core", "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson")
	}
	if hasTiles {
		conformance = append(conformance, "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/core", "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tileset", "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tilesets-list", "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/geodata-tilesets")
	}
	// Full Processes conformance is a deployment assertion: the external validator,
	// executor and job service must satisfy the declared schemas and lifecycle semantics.
	// Route presence alone is not sufficient to advertise every conformance class.
	slices.Sort(conformance)
	root := common.LandingPage{Title: title, Description: description, Links: []common.Link{
		s.link("/", "self", common.JSON), s.link("/api", "service-desc", "application/vnd.oai.openapi+json;version=3.0"),
		s.link("/conformance", "conformance", common.JSON),
		s.link("/conformance", "http://www.opengis.net/def/rel/ogc/1.0/conformance", common.JSON), s.link("/collections", "data", common.JSON)}}
	if len(s.grids) > 0 {
		root.Links = append(root.Links, s.link("/tileMatrixSets", "http://www.opengis.net/def/rel/ogc/1.0/tiling-schemes", common.JSON))
	}
	if len(s.processes) > 0 {
		root.Links = append(root.Links, s.link("/processes", "http://www.opengis.net/def/rel/ogc/1.0/processes", common.JSON))
	}
	if err := add("/", root); err != nil {
		return err
	}
	if err := add("/conformance", common.Conformance{ConformsTo: conformance}); err != nil {
		return err
	}
	if err := add("/collections", common.Collections{Collections: descriptions, Links: []common.Link{s.link("/collections", "self", common.JSON)}}); err != nil {
		return err
	}
	if len(s.grids) > 0 {
		ids := make([]string, 0, len(s.grids))
		for id := range s.grids {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		items := make([]matrixSetItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, matrixSetItem{ID: id, Links: []common.Link{s.link("/tileMatrixSets/"+url.PathEscape(id), "self", common.JSON)}})
		}
		if err := add("/tileMatrixSets", struct {
			Sets []matrixSetItem `json:"tileMatrixSets"`
		}{items}); err != nil {
			return err
		}
	}
	if err := add("/api", s.openAPI(title, description)); err != nil {
		return err
	}
	doc := s.documents["/api"]
	doc.media = "application/vnd.oai.openapi+json;version=3.0"
	s.documents["/api"] = doc
	return nil
}

func checkCollection(info common.Collection) error {
	if !common.ValidID(info.ID) {
		return fmt.Errorf("invalid collection id")
	}
	if info.Extent != nil && info.Extent.Spatial != nil {
		extent := info.Extent.Spatial
		if len(extent.BBox) == 0 {
			return fmt.Errorf("empty spatial extent")
		}
		for _, bbox := range extent.BBox {
			if err := common.ValidateBBox(bbox, extent.CRS == "" || extent.CRS == common.CRS84); err != nil {
				return fmt.Errorf("collection extent: %w", err)
			}
		}
	}
	return nil
}

func copyMetadata[T any](input T, maxBytes int) (T, error) {
	var out T
	data, err := jsonutil.Marshal(input, maxBytes)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(data, &out)
	return out, err
}

func (s *Snapshot) link(path, rel, media string) common.Link {
	return common.Link{Href: s.baseURL + path, Rel: rel, Type: media}
}
