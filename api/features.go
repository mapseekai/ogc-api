package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
	"github.com/mapseekai/ogc-api/internal/jsonutil"
	"github.com/mapseekai/ogc-api/response"
)

func (e *Engine) featureResponse(ctx context.Context, input Input, snapshot *Snapshot, collection collectionBinding, parts []string, values common.Query) (plan *response.Plan, operation string, resultErr error) {
	operation = "features.items"
	if collection.features == nil || len(parts) < 3 || len(parts) > 4 {
		return nil, operation, common.ErrNotFound
	}
	if _, err := common.Negotiate(input.Accept, values["f"], []string{common.GeoJSON}); err != nil {
		return nil, operation, err
	}
	request := features.Request{CollectionID: strings.Clone(parts[1])}
	if len(parts) == 4 {
		operation = "features.item"
		query, err := features.ParseItemQuery(values, collection.features.Options)
		if err != nil {
			return nil, operation, err
		}
		request.Query = query
		request.ItemID = strings.Clone(parts[3])
		feature, err := collection.features.Reader.Item(ctx, request)
		if err != nil {
			return nil, operation, err
		}
		if collection.features.Options.CRS != nil && feature.CRS != query.OutputCRS {
			return nil, operation, errors.New("feature reader did not acknowledge the actual requested output CRS")
		}
		if len(feature.Links) > e.config.Limits.MaxFeatureBytes/len(`{"href":"","rel":""}`) {
			return nil, operation, jsonutil.ErrTooLarge
		}
		feature.Links = append(feature.Links, common.Link{Href: featureSelfURL(snapshot, parts, values), Rel: "self", Type: common.GeoJSON}, snapshot.link(canonicalPath(parts[:2]), "collection", common.JSON))
		data, err := features.EncodeFeature(feature, e.config.Limits.MaxFeatureBytes)
		if err != nil {
			return nil, operation, err
		}
		plan = response.New(200, response.Bytes(common.GeoJSON, data))
		if collection.features.Options.CRS != nil {
			plan.Headers["Content-Crs"] = "<" + feature.CRS.URI + ">"
		}
		return plan, operation, nil
	}
	query, err := features.ParseQuery(values, collection.features.Options)
	if err != nil {
		return nil, operation, err
	}
	request.Query = query
	page, err := collection.features.Reader.Items(ctx, request)
	transferred := false
	defer func() {
		if !transferred {
			resultErr = errors.Join(resultErr, page.Close())
		}
	}()
	if err != nil {
		return nil, operation, err
	}
	if collection.features.Options.CRS != nil && page.CRS != query.OutputCRS {
		return nil, operation, errors.New("feature reader did not acknowledge the actual requested output CRS")
	}
	if len(page.NextCursor) > e.config.Limits.MaxQueryBytes/2 {
		return nil, operation, errors.New("provider cursor exceeds query limit")
	}
	if query.Offset != nil {
		if page.NextCursor != "" || page.NextOffset != nil && *page.NextOffset <= *query.Offset {
			return nil, operation, errors.New("feature reader returned an invalid offset continuation")
		}
		if page.NextOffset != nil && page.NumberMatched != nil && *page.NextOffset >= *page.NumberMatched {
			return nil, operation, errors.New("feature reader offset continuation exceeds matched features")
		}
	} else if page.NextOffset != nil {
		return nil, operation, errors.New("feature reader returned offset continuation for a cursor request")
	}
	self := snapshot.baseURL + canonicalPath(parts)
	encoded := make(url.Values, len(values))
	for key, value := range values {
		encoded.Set(key, value)
	}
	if query := encoded.Encode(); query != "" {
		self += "?" + query
	}
	links := []common.Link{{Href: self, Rel: "self", Type: common.GeoJSON}, snapshot.link(canonicalPath(parts[:2]), "collection", common.JSON)}
	if collection.features.Options.Filter != nil {
		links = append(links, snapshot.link(canonicalPath(parts[:2])+"/queryables", "http://www.opengis.net/def/rel/ogc/1.0/queryables", "application/schema+json"))
	}
	if page.NextCursor != "" {
		encoded.Set("cursor", page.NextCursor)
		query := encoded.Encode()
		if len(query) > e.config.Limits.MaxQueryBytes {
			return nil, operation, errors.New("provider next link exceeds query limit")
		}
		links = append(links, common.Link{Href: snapshot.baseURL + canonicalPath(parts) + "?" + query, Rel: "next", Type: common.GeoJSON})
	}
	if query.Offset != nil {
		if *query.Offset > 0 {
			encoded.Set("offset", strconv.FormatUint(*query.Offset-min(*query.Offset, query.Limit), 10))
			links = append(links, common.Link{Href: snapshot.baseURL + canonicalPath(parts) + "?" + encoded.Encode(), Rel: "prev", Type: common.GeoJSON})
		}
		if page.NextOffset != nil {
			encoded.Set("offset", strconv.FormatUint(*page.NextOffset, 10))
			if len(encoded.Encode()) > e.config.Limits.MaxQueryBytes {
				return nil, operation, errors.New("provider next link exceeds query limit")
			}
			links = append(links, common.Link{Href: snapshot.baseURL + canonicalPath(parts) + "?" + encoded.Encode(), Rel: "next", Type: common.GeoJSON})
		}
	}
	payload, err := features.Stream(ctx, page, links, query.Limit, e.config.Limits.MaxFeatureBytes)
	if err != nil {
		return nil, operation, err
	}
	transferred = true
	plan, err = payloadPlan(payload, common.GeoJSON)
	if err == nil && collection.features.Options.CRS != nil {
		plan.Headers["Content-Crs"] = "<" + page.CRS.URI + ">"
	}
	if err == nil && collection.features.Options.Filter != nil {
		link := snapshot.link(canonicalPath(parts[:2])+"/queryables", "http://www.opengis.net/def/rel/ogc/1.0/queryables", "application/schema+json")
		plan.Headers["Link"] = "<" + link.Href + ">; rel=\"" + link.Rel + "\"; type=\"" + link.Type + "\""
	}
	return plan, operation, err
}

func featureSelfURL(snapshot *Snapshot, parts []string, values common.Query) string {
	self := snapshot.baseURL + canonicalPath(parts)
	encoded := make(url.Values, len(values))
	for name, value := range values {
		encoded.Set(name, value)
	}
	if query := encoded.Encode(); query != "" {
		self += "?" + query
	}
	return self
}

func prepareFeatureCRS(metadata *common.Collection, options features.Options) error {
	if options.CRS == nil {
		if len(metadata.CRS) > 0 && (len(metadata.CRS) != 1 || metadata.CRS[0] != common.CRS84) {
			return fmt.Errorf("additional feature CRS metadata requires explicit Part 2 policy")
		}
		return nil
	}
	supported := make([]string, len(options.CRS.Supported))
	for i, crs := range options.CRS.Supported {
		supported[i] = crs.URI
	}
	if len(metadata.CRS) > 0 {
		if len(metadata.CRS) != len(supported) {
			return fmt.Errorf("feature CRS metadata differs from the reader policy")
		}
		for i, uri := range metadata.CRS {
			if !slices.Contains(supported, uri) || slices.Contains(metadata.CRS[:i], uri) {
				return fmt.Errorf("feature CRS metadata differs from the reader policy")
			}
		}
	}
	// Use fully expanded per-collection lists. No JSON-pointer inheritance is
	// advertised, so collection and collections resources remain self-contained.
	metadata.CRS = supported
	if metadata.StorageCRS != "" && !slices.Contains(supported, metadata.StorageCRS) {
		return fmt.Errorf("feature storage CRS must be a supported collection CRS")
	}
	if epoch := metadata.StorageCRSCoordinateEpoch; epoch != nil {
		if metadata.StorageCRS == "" || math.IsNaN(*epoch) || math.IsInf(*epoch, 0) {
			return fmt.Errorf("feature storage coordinate epoch requires a storage CRS and finite decimal year")
		}
	}
	if metadata.Extent != nil && metadata.Extent.Spatial != nil {
		extent := metadata.Extent.Spatial
		for _, bbox := range extent.BBox {
			uri := extent.CRS
			if uri == "" {
				uri = common.CRS84
				if len(bbox) == 6 {
					uri = common.CRS84h
				}
			}
			if uri != common.CRS84 && uri != common.CRS84h {
				return fmt.Errorf("feature discovery extent must retain Part 1 CRS84/CRS84h")
			}
			crs, _ := common.CRSByURI(uri)
			if err := crs.ValidateBBox(bbox); err != nil {
				return fmt.Errorf("feature discovery extent: %w", err)
			}
		}
	}
	return nil
}
