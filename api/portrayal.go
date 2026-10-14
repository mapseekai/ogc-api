package api

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/maps"
	"github.com/mapseekai/ogc-api/response"
)

func (e *Engine) mapResponse(ctx context.Context, input Input, collection collectionBinding, parts []string, values common.Query) (*response.Plan, string, error) {
	const operation = "maps.map"
	if len(parts) != 3 || collection.portrayal == nil {
		return nil, operation, common.ErrNotFound
	}
	service := *collection.portrayal
	request, err := maps.ParseQuery(values, service)
	if err != nil {
		return nil, operation, err
	}
	request.Format, err = common.Negotiate(input.Accept, values["f"], service.Formats)
	if err != nil {
		return nil, operation, err
	}
	request.Layers = []maps.Layer{{ID: parts[1]}}
	result, err := service.Renderer.RenderMap(ctx, request)
	if err == nil {
		err = maps.ValidateResult(request, result)
	}
	if err != nil {
		return nil, operation, errors.Join(err, result.Content.Close())
	}
	plan := response.New(200, result.Content)
	plan.Headers["Content-Crs"] = "<" + result.CRS + ">"
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
