package api

import (
	"context"
	"errors"
	"net/url"

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
	request := features.Request{CollectionID: parts[1]}
	if len(parts) == 4 {
		operation = "features.item"
		if err := values.Only("f"); err != nil {
			return nil, operation, err
		}
		request.ItemID = parts[3]
		feature, err := collection.features.Reader.Item(ctx, request)
		if err != nil {
			return nil, operation, err
		}
		if len(feature.Links) > e.config.Limits.MaxFeatureBytes/len(`{"href":"","rel":""}`) {
			return nil, operation, jsonutil.ErrTooLarge
		}
		feature.Links = append(feature.Links, snapshot.link(canonicalPath(parts), "self", common.GeoJSON), snapshot.link(canonicalPath(parts[:2]), "collection", common.JSON))
		data, err := features.EncodeFeature(feature, e.config.Limits.MaxFeatureBytes)
		if err != nil {
			return nil, operation, err
		}
		return response.New(200, response.Bytes(common.GeoJSON, data)), operation, nil
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
	if len(page.NextCursor) > e.config.Limits.MaxQueryBytes/2 {
		return nil, operation, errors.New("provider cursor exceeds query limit")
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
	if page.NextCursor != "" {
		encoded.Set("cursor", page.NextCursor)
		query := encoded.Encode()
		if len(query) > e.config.Limits.MaxQueryBytes {
			return nil, operation, errors.New("provider next link exceeds query limit")
		}
		links = append(links, common.Link{Href: snapshot.baseURL + canonicalPath(parts) + "?" + query, Rel: "next", Type: common.GeoJSON})
	}
	payload, err := features.Stream(ctx, page, links, query.Limit, e.config.Limits.MaxFeatureBytes)
	if err != nil {
		return nil, operation, err
	}
	transferred = true
	plan, err = payloadPlan(payload, common.GeoJSON)
	return plan, operation, err
}
