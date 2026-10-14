package api

import (
	"slices"

	"github.com/mapseekai/ogc-api/common"
)

func (s *Snapshot) openAPI(title, description string) map[string]any {
	paths := map[string]any{
		"/":            map[string]any{"get": openOperation("landingPage", "API discovery", []string{common.JSON}, nil, "LandingPage")},
		"/conformance": map[string]any{"get": openOperation("conformance", "Declared conformance classes", []string{common.JSON}, nil, "Conformance")},
		"/api":         map[string]any{"get": openOperation("apiDefinition", "OpenAPI definition", []string{"application/vnd.oai.openapi+json;version=3.0", common.JSON}, nil, "")},
		"/collections": map[string]any{"get": openOperation("collections", "Available collections", []string{common.JSON}, nil, "Collections")},
	}
	ids, featureIDs, tileIDs := []string{}, []string{}, []string{}
	tileFormats := []string{}
	for id, c := range s.collections {
		ids = append(ids, id)
		if c.features != nil {
			featureIDs = append(featureIDs, id)
		}
		if len(c.tiles) > 0 {
			tileIDs = append(tileIDs, id)
			for _, set := range c.tiles {
				tileFormats = append(tileFormats, set.service.Formats...)
			}
		}
	}
	slices.Sort(ids)
	slices.Sort(featureIDs)
	slices.Sort(tileIDs)
	slices.Sort(tileFormats)
	tileFormats = slices.Compact(tileFormats)
	if len(ids) > 0 {
		paths["/collections/{collectionId}"] = map[string]any{"get": openOperation("collection", "Collection metadata", []string{common.JSON}, []any{pathParameter("collectionId", ids)}, "Collection")}
	}
	if len(featureIDs) > 0 {
		parameters := []any{pathParameter("collectionId", featureIDs), queryParameter("limit", "Maximum returned features; values above the collection limit are clamped", map[string]any{"type": "integer", "minimum": 1}),
			queryParameter("bbox", "CRS84 bbox: four or six comma-separated coordinates; longitude may cross the antimeridian", map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "oneOf": []any{map[string]any{"minItems": 4, "maxItems": 4}, map[string]any{"minItems": 6, "maxItems": 6}}}),
			queryParameter("datetime", "RFC3339 instant or interval; .. represents an open bound", map[string]any{"type": "string"}),
			queryParameter("cursor", "Provider-defined opaque pagination cursor (extension)", map[string]any{"type": "string"})}
		names := []string{}
		for _, c := range s.collections {
			if c.features != nil {
				names = append(names, c.features.Options.FilterProperties...)
			}
		}
		slices.Sort(names)
		names = slices.Compact(names)
		for _, name := range names {
			parameters = append(parameters, queryParameter(name, "Equality predicate on collections that declare this property", map[string]any{"type": "string"}))
		}
		paths["/collections/{collectionId}/items"] = map[string]any{"get": openOperation("items", "Query features", []string{common.GeoJSON}, parameters, "FeatureCollection")}
		paths["/collections/{collectionId}/items/{featureId}"] = map[string]any{"get": openOperation("item", "Read one feature", []string{common.GeoJSON}, []any{pathParameter("collectionId", featureIDs), pathParameter("featureId", nil)}, "Feature")}
	}
	if len(s.grids) > 0 {
		matrixIDs := []string{}
		for id := range s.grids {
			matrixIDs = append(matrixIDs, id)
		}
		slices.Sort(matrixIDs)
		paths["/tileMatrixSets"] = map[string]any{"get": openOperation("tileMatrixSets", "Available tile grids", []string{common.JSON}, nil, "")}
		paths["/tileMatrixSets/{tileMatrixSetId}"] = map[string]any{"get": openOperation("tileMatrixSet", "Tile matrix set definition", []string{common.JSON}, []any{pathParameter("tileMatrixSetId", matrixIDs)}, "")}
	}
	if len(tileIDs) > 0 {
		collection := pathParameter("collectionId", tileIDs)
		paths["/collections/{collectionId}/tiles"] = map[string]any{"get": openOperation("tilesets", "Collection tilesets", []string{common.JSON}, []any{collection}, "")}
		paths["/collections/{collectionId}/tiles/{tileMatrixSetId}"] = map[string]any{"get": openOperation("tileset", "Tileset metadata", []string{common.JSON}, []any{collection, pathParameter("tileMatrixSetId", nil)}, "")}
		params := []any{collection, pathParameter("tileMatrixSetId", nil), pathParameter("tileMatrix", nil), pathParameter("tileRow", nil), pathParameter("tileCol", nil)}
		paths["/collections/{collectionId}/tiles/{tileMatrixSetId}/{tileMatrix}/{tileRow}/{tileCol}"] = map[string]any{"get": openOperation("tile", "Read a tile", tileFormats, params, "binary")}
	}
	s.addCapabilityPaths(paths)
	return map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": title, "description": description, "version": "1.0.0"},
		"servers": []any{map[string]string{"url": s.baseURL}}, "paths": paths, "components": map[string]any{"schemas": openSchemas()}}
}

func openOperation(id, description string, media []string, parameters []any, schema string) map[string]any {
	parameters = append(slices.Clone(parameters), queryParameter("f", "Explicit output media type or documented short format name", map[string]any{"type": "string"}))
	content := map[string]any{}
	for _, format := range media {
		definition := map[string]any{"type": "object"}
		if schema == "raw" {
			definition = map[string]any{}
		} else if schema == "binary" {
			definition = map[string]any{"type": "string", "format": "binary"}
		} else if schema != "" {
			definition = map[string]any{"$ref": "#/components/schemas/" + schema}
		}
		content[format] = map[string]any{"schema": definition}
	}
	return map[string]any{"operationId": id, "summary": description, "parameters": parameters, "responses": map[string]any{
		"200":     map[string]any{"description": "Successful response", "content": content},
		"default": map[string]any{"description": "Protocol or backend error", "content": map[string]any{common.ProblemJSON: map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Problem"}}}},
	}}
}

func openSchemas() map[string]any {
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	array := func(name string) map[string]any { return map[string]any{"type": "array", "items": ref(name)} }
	text := map[string]any{"type": "string"}
	return map[string]any{
		"Link":              map[string]any{"type": "object", "required": []string{"href", "rel"}, "properties": map[string]any{"href": text, "rel": text, "type": text, "title": text, "templated": map[string]any{"type": "boolean"}}},
		"LandingPage":       map[string]any{"type": "object", "required": []string{"links"}, "properties": map[string]any{"title": text, "description": text, "links": array("Link")}},
		"Conformance":       map[string]any{"type": "object", "required": []string{"conformsTo"}, "properties": map[string]any{"conformsTo": map[string]any{"type": "array", "items": text}}},
		"Collection":        map[string]any{"type": "object", "required": []string{"id", "links"}, "properties": map[string]any{"id": text, "title": text, "description": text, "links": array("Link"), "extent": map[string]any{"type": "object"}, "itemType": text}},
		"Collections":       map[string]any{"type": "object", "required": []string{"collections", "links"}, "properties": map[string]any{"collections": array("Collection"), "links": array("Link")}},
		"Feature":           map[string]any{"type": "object", "required": []string{"type", "geometry", "properties"}, "properties": map[string]any{"type": map[string]any{"type": "string", "enum": []string{"Feature"}}, "id": map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}}}, "geometry": map[string]any{"type": "object", "nullable": true}, "properties": map[string]any{"type": "object", "nullable": true}, "links": array("Link")}},
		"FeatureCollection": map[string]any{"type": "object", "required": []string{"type", "features", "links", "numberReturned"}, "properties": map[string]any{"type": map[string]any{"type": "string", "enum": []string{"FeatureCollection"}}, "features": array("Feature"), "links": array("Link"), "numberReturned": map[string]any{"type": "integer", "minimum": 0}, "numberMatched": map[string]any{"type": "integer", "minimum": 0}, "timeStamp": map[string]any{"type": "string", "format": "date-time"}}},
		"Problem":           map[string]any{"type": "object", "required": []string{"type", "title", "status", "code", "description"}, "properties": map[string]any{"type": text, "title": text, "status": map[string]any{"type": "integer"}, "detail": text, "code": text, "description": text}},
	}
}

func pathParameter(name string, enum []string) map[string]any {
	schema := map[string]any{"type": "string"}
	if len(enum) > 0 {
		schema["enum"] = enum
	}
	return map[string]any{"name": name, "in": "path", "required": true, "schema": schema}
}

func queryParameter(name, description string, schema map[string]any) map[string]any {
	return map[string]any{"name": name, "in": "query", "required": false, "description": description, "style": "form", "explode": false, "schema": schema}
}
