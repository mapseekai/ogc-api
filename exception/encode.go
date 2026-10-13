// Package exception serializes protocol errors; it never exposes private causes.
package exception

import (
	"github.com/mapseekai/ogc-api/internal/xmlutil"
	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
)

// Encode maps a non-nil error to a bounded version/binding-specific response plan.
// Private causes are never serialized. The caller must finish the returned plan;
// a non-nil returned error means no response could be encoded.
func Encode(meta owscore.Meta, err error) (plan *response.Plan, resultErr error) {
	e := owscore.PublicError(err)
	defer func() {
		if plan != nil {
			plan.ProtocolError = e
		}
	}()
	status := Status(meta, e)
	if meta.Protocol != "WMS" && meta.Protocol != "WMTS" {
		return response.New(status, response.Bytes("text/plain; charset=utf-8", []byte(e.Message))), nil
	}
	w := xmlutil.New(64 << 10)
	media := "text/xml"
	if meta.Protocol == owscore.WMTS {
		w.Start("ows:ExceptionReport", xmlutil.A("xmlns:ows", "http://www.opengis.net/ows/1.1"), xmlutil.A("version", "1.1.0"))
		attrs := []xmlutil.Attr{xmlutil.A("exceptionCode", wmtsCode(e.Kind))}
		if e.Locator != "" {
			attrs = append(attrs, xmlutil.A("locator", e.Locator))
		}
		w.Start("ows:Exception", attrs...)
		w.Element("ows:ExceptionText", e.Message)
		w.End()
		w.End()
	} else if meta.Version == "1.0.0" {
		w.Element("WMTException", e.Message, xmlutil.A("version", "1.0.0"))
	} else {
		version := meta.Version
		if version != "1.1.0" && version != "1.1.1" && version != "1.3.0" {
			version = "1.3.0"
		}
		attrs := []xmlutil.Attr{xmlutil.A("version", version)}
		if version == "1.3.0" {
			attrs = append(attrs, xmlutil.A("xmlns", "http://www.opengis.net/ogc"))
		} else {
			media = "application/vnd.ogc.se_xml"
		}
		w.Start("ServiceExceptionReport", attrs...)
		attrs = nil
		if code := wmsCode(version, e.Kind); code != "" {
			attrs = append(attrs, xmlutil.A("code", code))
		}
		if version == "1.3.0" && e.Locator != "" {
			attrs = append(attrs, xmlutil.A("locator", e.Locator))
		}
		w.Element("ServiceException", e.Message, attrs...)
		w.End()
	}
	data, encodeErr := w.Bytes()
	if encodeErr != nil {
		return nil, encodeErr
	}
	return response.New(status, response.Bytes(media, data)), nil
}

// Status chooses an HTTP status from protocol, binding and a non-nil domain error.
// WMS protocol exceptions can use 200, so callers must not infer business success from status alone.
func Status(meta owscore.Meta, e *owscore.Error) int {
	switch e.Kind {
	case owscore.Internal:
		return 500
	case owscore.Forbidden:
		return 403
	case owscore.TooLarge:
		return 414
	}
	if meta.Protocol == owscore.WMS {
		return 200
	}
	if e.Kind == owscore.UnsupportedOperation {
		return 501
	}
	if meta.Binding == owscore.REST || e.Kind == owscore.NotFound {
		return 404
	}
	return 400
}
func wmtsCode(kind owscore.Kind) string {
	switch kind {
	case owscore.MissingDimension:
		return "MissingParameterValue"
	case owscore.LayerNotDefined, owscore.StyleNotDefined, owscore.InvalidFormat, owscore.InvalidDimension, owscore.NotFound, owscore.InvalidCRS:
		return "InvalidParameterValue"
	case owscore.Forbidden, owscore.TooLarge, owscore.Internal:
		return "NoApplicableCode"
	default:
		return string(kind)
	}
}
func wmsCode(version string, kind owscore.Kind) string {
	switch kind {
	case owscore.InvalidCRS:
		if version == "1.3.0" {
			return "InvalidCRS"
		}
		return "InvalidSRS"
	case owscore.PointOutOfRange:
		if version == "1.3.0" {
			return "InvalidPoint"
		}
	case owscore.LayerNotDefined, owscore.StyleNotDefined, owscore.InvalidFormat, owscore.LayerNotQueryable, owscore.InvalidDimension, owscore.MissingDimension, owscore.CurrentUpdateSequence, owscore.InvalidUpdateSequence:
		return string(kind)
	case owscore.UnsupportedOperation:
		if version == "1.3.0" {
			return "OperationNotSupported"
		}
	}
	return ""
}
