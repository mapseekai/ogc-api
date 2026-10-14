package api

import (
	"context"
	"errors"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/tiles"
)

func (e *Engine) tileResponse(ctx context.Context, input Input, collection collectionBinding, parts []string, values common.Query) (*response.Plan, string, error) {
	const operation = "tiles.tile"
	if len(parts) != 7 {
		return nil, operation, common.ErrNotFound
	}
	binding, ok := collection.tiles[parts[3]]
	if !ok {
		return nil, operation, common.ErrNotFound
	}
	if err := values.Only("f"); err != nil {
		return nil, operation, err
	}
	media, err := common.Negotiate(input.Accept, values["f"], binding.service.Formats)
	if err != nil {
		return nil, operation, err
	}
	indices := common.Query{"row": parts[5], "col": parts[6]}
	row, err := indices.Integer("row", 0, 0, ^uint64(0))
	if err != nil {
		return nil, operation, common.ErrNotFound
	}
	col, err := indices.Integer("col", 0, 0, ^uint64(0))
	if err != nil {
		return nil, operation, common.ErrNotFound
	}
	if err := binding.grid.Validate(parts[4], row, col, binding.limits); err != nil {
		return nil, operation, err
	}
	payload, err := binding.service.Reader.ReadTile(ctx, tiles.Request{CollectionID: parts[1], MatrixSetID: parts[3], MatrixID: parts[4], Row: row, Col: col, Format: media})
	if err != nil {
		return nil, operation, errors.Join(err, payload.Close())
	}
	plan, err := payloadPlan(payload, media)
	return plan, operation, err
}
