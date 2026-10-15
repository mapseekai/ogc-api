package api

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/response"
)

func (e *Engine) mapResponse(ctx context.Context, input Input, snapshot *Snapshot, parts []string, values common.Query) (*response.Plan, string, error) {
	const operation = "maps.map"
	service := snapshot.portrayal
	layers := []maps.Layer{}
	query := make(common.Query, len(values))
	for name, value := range values {
		query[name] = value
	}
	if len(parts) == 3 {
		service = snapshot.collections[parts[1]].portrayal
		layers = append(layers, maps.Layer{ID: parts[1]})
	} else if service != nil {
		if selection, ok := query["collections"]; ok {
			delete(query, "collections")
			if selection == "" || strings.Count(selection, ",") >= service.Limits.MaxLayers {
				return nil, operation, common.Invalid("collections")
			}
			seen := make(map[string]bool)
			for _, id := range strings.Split(selection, ",") {
				if prefix := snapshot.baseURL + "/collections/"; strings.HasPrefix(id, prefix) {
					var err error
					id, err = url.PathUnescape(strings.TrimPrefix(id, prefix))
					if err != nil {
						return nil, operation, common.Invalid("collections")
					}
				}
				if snapshot.collections[id].portrayal == nil || seen[id] {
					return nil, operation, common.Invalid("collections")
				}
				seen[id] = true
				layers = append(layers, maps.Layer{ID: id})
			}
		} else {
			ids := []string{}
			for id, collection := range snapshot.collections {
				if collection.portrayal != nil {
					ids = append(ids, id)
				}
			}
			slices.Sort(ids)
			for _, id := range ids {
				layers = append(layers, maps.Layer{ID: id})
			}
		}
	}
	if service == nil {
		return nil, operation, common.ErrNotFound
	}
	request, err := maps.ParseQuery(query, *service)
	if err != nil {
		return nil, operation, err
	}
	request.Format, err = common.Negotiate(input.Accept, values["f"], service.Formats)
	if err != nil {
		return nil, operation, err
	}
	request.Layers = layers
	result, err := service.Renderer.RenderMap(ctx, request)
	if err == nil {
		err = maps.ValidateResult(request, result)
	}
	if err != nil {
		return nil, operation, errors.Join(err, result.Content.Close())
	}
	plan := response.New(200, result.Content)
	if result.CRS != common.CRS84 && result.CRS != "https://www.opengis.net/def/crs/OGC/1.3/CRS84" {
		plan.Headers["Content-Crs"] = "<" + result.CRS + ">"
	}
	coordinates := make([]string, len(result.BBox))
	for i, number := range result.BBox {
		coordinates[i] = strconv.FormatFloat(number, 'g', -1, 64)
	}
	plan.Headers["Content-Bbox"] = strings.Join(coordinates, ",")
	return plan, operation, nil
}

func (e *Engine) coverageResponse(ctx context.Context, input Input, collection collectionBinding, parts []string, values common.Query) (*response.Plan, string, error) {
	const operation = "coverages.coverage"
	if len(parts) != 3 || collection.coverage == nil {
		return nil, operation, common.ErrNotFound
	}
	service := *collection.coverage
	request, err := coverages.ParseQuery(values, service)
	if err != nil {
		return nil, operation, err
	}
	request.CollectionID = parts[1]
	request.Format, err = common.Negotiate(input.Accept, values["f"], service.Formats)
	if err != nil {
		return nil, operation, err
	}
	content, err := service.Reader.ReadCoverage(ctx, request)
	if err != nil {
		return nil, operation, errors.Join(err, content.Close())
	}
	plan, err := payloadPlan(content, request.Format)
	if err != nil {
		return nil, operation, err
	}
	plan.Headers["Content-Crs"] = "<" + request.CRS + ">"
	return plan, operation, nil
}
