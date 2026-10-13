package server

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wms"
	"github.com/mapseekai/ogc-api/wmts"
)

func (e *Engine) encoded(ctx context.Context, request *CapabilitiesRequest, expected string) (result response.Payload, contentType string, resultErr error) {
	payload, err := e.config.EncodedCapabilities(ctx, request)
	defer func() { resultErr = errors.Join(resultErr, payload.Close()) }()
	if err != nil {
		return result, "", err
	}
	if err := payload.Validate(expected); err != nil {
		return result, "", err
	}
	if payload.ContentEncoding != "" {
		return result, "", fmt.Errorf("encoded capabilities must not be compressed before validation")
	}
	limit := int64(e.config.Limits.MaxCapabilitiesBytes)
	if payload.Length > limit {
		return result, "", fmt.Errorf("encoded capabilities exceeds size limit")
	}
	reader := &response.Reader{Context: ctx, Source: payload.Body, Length: payload.Length}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return result, "", err
	}
	if int64(len(data)) > limit {
		return result, "", fmt.Errorf("encoded capabilities exceeds size limit")
	}
	if err := validateDocument(data, request.Meta); err != nil {
		return result, "", err
	}
	result = response.Bytes(payload.ContentType, data)
	result.Cache = payload.Cache
	return result, expected, nil
}

func validateDocument(data []byte, meta owscore.Meta) error {
	expectedRoot, namespace := "WMT_MS_Capabilities", ""
	if meta.Protocol == owscore.WMTS {
		expectedRoot, namespace = "Capabilities", wmts.Namespace
	}
	if meta.Protocol == owscore.WMS && meta.Version == "1.3.0" {
		expectedRoot, namespace = "WMS_Capabilities", wms.Namespace
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid capabilities XML: %w", err)
		}
		switch token := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots != 1 || token.Name.Local != expectedRoot || token.Name.Space != namespace {
					return fmt.Errorf("capabilities root or namespace does not match the selected protocol")
				}
				version := ""
				for _, a := range token.Attr {
					if a.Name.Local == "version" && a.Name.Space == "" {
						version = a.Value
					}
				}
				if version != meta.Version {
					return fmt.Errorf("capabilities version does not match the negotiated version")
				}
			}
			depth++
			if depth > 128 {
				return fmt.Errorf("capabilities nesting limit exceeded")
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return fmt.Errorf("unexpected text outside capabilities root")
			}
		case xml.Directive:
			// Standard-library XML never resolves external entities. Reject custom
			// entity declarations as well, while permitting a legacy DOCTYPE URL.
			if strings.Contains(strings.ToUpper(string(token)), "ENTITY") {
				return fmt.Errorf("entity declarations are not allowed")
			}
		}
	}
	if roots != 1 || depth != 0 {
		return fmt.Errorf("incomplete capabilities document")
	}
	return nil
}
