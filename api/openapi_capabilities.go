package api

import (
	"slices"

	"github.com/mapseekai/ogc-api/common"
)

func (s *Snapshot) addCapabilityPaths(paths map[string]any) {
	mapIDs, coverageIDs, schemaIDs := []string{}, []string{}, []string{}
	domainIDs := []string{}
	mapFormats, coverageFormats := []string{}, []string{}
	for id, collection := range s.collections {
		if collection.portrayal != nil {
			mapIDs = append(mapIDs, id)
			mapFormats = append(mapFormats, collection.portrayal.Formats...)
		}
		if collection.coverage != nil {
			coverageIDs = append(coverageIDs, id)
			coverageFormats = append(coverageFormats, collection.coverage.Formats...)
			if len(collection.coverage.Schema) > 0 {
				schemaIDs = append(schemaIDs, id)
			}
			if len(collection.coverage.DomainSet) > 0 {
				domainIDs = append(domainIDs, id)
			}
		}
	}
	for _, values := range [][]string{mapIDs, coverageIDs, schemaIDs, domainIDs, mapFormats, coverageFormats} {
		slices.Sort(values)
	}
	mapFormats, coverageFormats = slices.Compact(mapFormats), slices.Compact(coverageFormats)
	text := map[string]any{"type": "string"}
	bbox := map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 4, "maxItems": 4}
	if len(mapIDs) > 0 {
		parameters := []any{pathParameter("collectionId", mapIDs), queryParameter("bbox", "Bounding box in bbox-crs axes, default CRS84", bbox),
			queryParameter("bbox-crs", "Reference system for bbox; must be declared by the collection map", text),
			queryParameter("crs", "Output reference system declared by the collection map", text),
			queryParameter("width", "Image width in pixels; omitted width is derived by the renderer when height is supplied", map[string]any{"type": "integer", "minimum": 1}),
			queryParameter("height", "Image height in pixels; omitted height is derived by the renderer when width is supplied", map[string]any{"type": "integer", "minimum": 1}),
			queryParameter("datetime", "RFC3339 instant or interval", text),
			queryParameter("transparent", "Transparent map background", map[string]any{"type": "boolean"}),
			queryParameter("bgcolor", "Background color as 0xRRGGBB", map[string]any{"type": "string", "pattern": "^0x[0-9A-Fa-f]{6}$"})}
		operation := openOperation("map", "Render a collection map (bbox/scaling profile)", mapFormats, parameters, "binary")
		operation["x-ogc-profile"] = "OGC API Maps 1.0 bbox/scaling retrieval profile"
		paths["/collections/{collectionId}/map"] = map[string]any{"get": operation}
		if s.portrayal != nil {
			parameters = append([]any{queryParameter("collections", "Ordered collection identifiers or local collection URLs; omitted selects all collection maps", map[string]any{"type": "array", "items": text})}, parameters[1:]...)
			paths["/map"] = map[string]any{"get": openOperation("datasetMap", "Render selected dataset collections", s.portrayal.Formats, parameters, "binary")}
		}
	}
	if len(coverageIDs) > 0 {
		parameters := []any{pathParameter("collectionId", coverageIDs), queryParameter("bbox", "Spatial subset bounding box", map[string]any{"oneOf": []any{bbox, map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 6, "maxItems": 6}}}),
			queryParameter("bbox-crs", "CRS of bbox", text), queryParameter("crs", "Output CRS", text),
			queryParameter("subset-crs", "CRS of spatial subset axes", text), queryParameter("subset", "axis(value) or axis(lower:upper), comma-separated; axes must be declared by this collection", text),
			queryParameter("datetime", "RFC3339 instant or interval", text), queryParameter("properties", "Comma-separated declared range fields", text)}
		operation := openOperation("coverage", "Retrieve coverage content (OGC 19-087 1.0.0rc4 draft profile)", coverageFormats, parameters, "binary")
		operation["x-ogc-profile"] = "OGC 19-087 / 1.0.0rc4 / retrieval profile"
		paths["/collections/{collectionId}/coverage"] = map[string]any{"get": operation}
	}
	if len(schemaIDs) > 0 {
		paths["/collections/{collectionId}/schema"] = map[string]any{"get": openOperation("coverageSchema", "Coverage range field schema", []string{"application/schema+json", common.JSON}, []any{pathParameter("collectionId", schemaIDs)}, "")}
	}
	for _, name := range []string{"domainset", "rangetype"} {
		if len(domainIDs) > 0 {
			operationID := "coverageDomainSet"
			if name == "rangetype" {
				operationID = "coverageRangeType"
			}
			paths["/collections/{collectionId}/coverage/"+name] = map[string]any{"get": openOperation(operationID, "Coverage CIS "+name+" metadata", []string{common.JSON}, []any{pathParameter("collectionId", domainIDs)}, "")}
		}
	}
	if len(s.processes) > 0 {
		s.addProcessPaths(paths)
	}
}

func (s *Snapshot) addProcessPaths(paths map[string]any) {
	ids := make([]string, 0, len(s.processes))
	for id := range s.processes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	paths["/processes"] = map[string]any{"get": openOperation("processes", "Published process descriptions", []string{common.JSON}, []any{queryParameter("limit", "Maximum number of process summaries", map[string]any{"type": "integer", "minimum": 1, "maximum": 1000, "default": 10}), queryParameter("cursor", "Publication-relative process pagination cursor", map[string]any{"type": "string"})}, "")}
	paths["/processes/{processId}"] = map[string]any{"get": openOperation("process", "Process inputs, outputs and execution capabilities", []string{common.JSON}, []any{pathParameter("processId", ids)}, "")}
	execution := resultOperation("execute", "Validate and execute or submit a process", []any{pathParameter("processId", ids)})
	execution["parameters"] = []any{pathParameter("processId", ids), map[string]any{"name": "Prefer", "in": "header", "schema": map[string]any{"type": "string"}, "description": "respond-async requests external asynchronous submission when available"}}
	execution["requestBody"] = map[string]any{"required": true, "content": map[string]any{common.JSON: map[string]any{"schema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"inputs": map[string]any{"type": "object", "additionalProperties": true},
		"outputs": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"transmissionMode": map[string]any{"type": "string", "enum": []string{"value", "reference"}}, "format": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"mediaType": map[string]any{"type": "string"}, "encoding": map[string]any{"type": "string"},
				"schema": map[string]any{"oneOf": []any{map[string]any{"type": "string", "format": "uri"}, map[string]any{"type": "object"}}},
			}}}}},
		"response": map[string]any{"type": "string", "enum": []string{"raw", "document"}, "default": "raw"},
	}}}}}
	responses := execution["responses"].(map[string]any)
	asynchronous := false
	for _, service := range s.processes {
		asynchronous = asynchronous || service.Submitter != nil
	}
	if asynchronous {
		responses["201"] = map[string]any{"description": "External job accepted", "headers": map[string]any{"Location": map[string]any{"schema": map[string]any{"type": "string", "format": "uri"}}}, "content": map[string]any{common.JSON: map[string]any{"schema": map[string]any{"type": "object"}}}}
	}

	paths["/processes/{processId}/execution"] = map[string]any{"post": execution}
	if s.jobs == nil {
		return
	}
	parameters := []any{pathParameter("jobId", nil)}
	status := openOperation("jobStatus", "External job status", []string{common.JSON}, parameters, "")
	status["parameters"] = parameters
	jobPath := map[string]any{"get": status}
	if s.jobs.Dismisser != nil {
		dismiss := openOperation("dismissJob", "Dismiss an external job", []string{common.JSON}, parameters, "")
		dismiss["parameters"] = parameters
		jobPath["delete"] = dismiss
	}
	paths["/jobs/{jobId}"] = jobPath
	results := resultOperation("jobResults", "Completed external job results", parameters)
	paths["/jobs/{jobId}/results"] = map[string]any{"get": results}
	if s.jobs.Lister != nil {
		listing := openOperation("jobs", "Visible jobs (limit/cursor profile)", []string{common.JSON}, nil, "")
		listing["parameters"] = []any{queryParameter("limit", "Maximum returned jobs", map[string]any{"type": "integer", "minimum": 1, "maximum": 1000, "default": 10}), queryParameter("cursor", "Opaque next-page token", map[string]any{"type": "string"})}
		paths["/jobs"] = map[string]any{"get": listing}
	}
}

// Raw JSON can be a scalar, array or object. The wildcard covers provider-defined
// media types without pretending that their binary content is a JSON object.
func resultOperation(id, description string, parameters []any) map[string]any {
	operation := openOperation(id, description, []string{common.JSON}, parameters, "raw")
	operation["parameters"] = parameters
	responses := operation["responses"].(map[string]any)
	content := responses["200"].(map[string]any)["content"].(map[string]any)
	for _, media := range []string{"multipart/related", "application/octet-stream", "*/*"} {
		content[media] = map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}
	}
	responses["204"] = map[string]any{"description": "Results returned by reference in Link headers", "headers": map[string]any{"Link": map[string]any{"schema": map[string]any{"type": "string"}}}}
	return operation
}
