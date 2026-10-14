package wms

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/internal/xmlutil"
	"github.com/mapseekai/ogc-api/owscore"
)

const Namespace = "http://www.opengis.net/wms"

// CapabilitiesOptions selects a WMS XML profile and advertised operations.
// Endpoint must be a trusted public URL. Zero limit fields use deployment defaults.
type CapabilitiesOptions struct {
	Version     string
	Endpoint    string
	FeatureInfo bool
	Limits      owscore.Limits
}

// EncodeCapabilities writes escaped XML from the same catalog used for validation.
// Historical profiles have separate element/attribute rules, not only a version label.
func EncodeCapabilities(c Catalog, options CapabilitiesOptions) ([]byte, error) {
	if !slices.Contains(Versions(), options.Version) {
		return nil, fmt.Errorf("unsupported WMS metadata version")
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	limits := options.Limits.Defaults()
	w := xmlutil.New(limits.MaxCapabilitiesBytes)
	legacy := options.Version == "1.0.0"
	modern := options.Version == "1.3.0"
	root := "WMT_MS_Capabilities"
	attrs := []xmlutil.Attr{xmlutil.A("version", options.Version)}
	if modern {
		root = "WMS_Capabilities"
		attrs = append(attrs, xmlutil.A("xmlns", Namespace), xmlutil.A("xmlns:xsi", "http://www.w3.org/2001/XMLSchema-instance"), xmlutil.A("xsi:schemaLocation", Namespace+" https://schemas.opengis.net/wms/1.3.0/capabilities_1_3_0.xsd"))
	} else {
		path := "https://schemas.opengis.net/wms/" + options.Version + "/"
		file := "capabilities_" + strings.ReplaceAll(options.Version, ".", "_") + ".dtd"
		if options.Version == "1.1.1" {
			file = "WMS_MS_Capabilities.dtd"
		}
		w.Directive(`DOCTYPE WMT_MS_Capabilities SYSTEM "` + path + file + `"`)
	}
	if c.UpdateSequence != "" {
		attrs = append(attrs, xmlutil.A("updateSequence", c.UpdateSequence))
	}
	w.Start(root, attrs...)
	w.Start("Service")
	serviceName := "OGC:WMS"
	if modern {
		serviceName = "WMS"
	}
	w.Element("Name", serviceName)
	w.Element("Title", c.Service.Title)
	w.Optional("Abstract", c.Service.Abstract)
	if len(c.Service.Keywords) > 0 {
		if legacy {
			w.Element("Keywords", strings.Join(c.Service.Keywords, " "))
		} else {
			w.Start("KeywordList")
			for _, word := range c.Service.Keywords {
				w.Element("Keyword", word)
			}
			w.End()
		}
	}
	if legacy {
		w.Element("OnlineResource", options.Endpoint)
	} else {
		onlineResource(w, options.Endpoint)
	}
	w.Element("Fees", orNone(c.Service.Fees))
	w.Element("AccessConstraints", orNone(c.Service.AccessConstraints))
	if modern {
		w.Element("LayerLimit", strconv.Itoa(limits.MaxLayers))
		w.Element("MaxWidth", strconv.FormatUint(limits.MaxImageWidth, 10))
		w.Element("MaxHeight", strconv.FormatUint(limits.MaxImageHeight, 10))
	}
	w.End()
	w.Start("Capability")
	w.Start("Request")
	if legacy {
		writeOperation(w, "Capabilities", []string{"WMS_XML"}, options.Endpoint, true)
		var formats []string
		for _, format := range c.Formats {
			if name := LegacyFormat(format); name != "" {
				formats = append(formats, name)
			}
		}
		if len(formats) == 0 {
			return nil, fmt.Errorf("no WMS 1.0.0 map formats are available")
		}
		writeOperation(w, "Map", formats, options.Endpoint, true)
		if options.FeatureInfo && len(c.InfoFormats) > 0 {
			writeOperation(w, "FeatureInfo", []string{"MIME"}, options.Endpoint, true)
		}
	} else {
		writeOperation(w, "GetCapabilities", []string{CapabilitiesMediaType(options.Version)}, options.Endpoint, false)
		writeOperation(w, "GetMap", c.Formats, options.Endpoint, false)
		if options.FeatureInfo && len(c.InfoFormats) > 0 {
			writeOperation(w, "GetFeatureInfo", c.InfoFormats, options.Endpoint, false)
		}
	}
	w.End()
	w.Start("Exception")
	formats := []string{XMLExceptionFormat(options.Version)}
	if legacy {
		w.Start("Format")
	}
	for _, format := range formats {
		if legacy {
			w.Start(format)
			w.End()
		} else {
			w.Element("Format", format)
		}
	}
	if legacy {
		w.End()
	}
	w.End()
	w.Start("Layer")
	w.Element("Title", c.Service.Title)
	for _, name := range slices.Sorted(maps.Keys(c.Layers)) {
		layer := c.Layers[name]
		if legacy && len(layer.Dimensions) > 0 {
			return nil, fmt.Errorf("WMS 1.0.0 cannot advertise standard dimensions; disable this version for this catalog")
		}
		writeLayer(w, name, layer, options)
	}
	w.End()
	w.End()
	w.End()
	return w.Bytes()
}

// CapabilitiesMediaType returns the response MIME type for a supported WMS version.
// Use a version from Versions; this helper does not validate arbitrary version strings.
func CapabilitiesMediaType(version string) string {
	if version == "1.1.0" || version == "1.1.1" {
		return "application/vnd.ogc.wms_xml"
	}
	return "text/xml"
}

func writeOperation(w *xmlutil.Writer, name string, formats []string, endpoint string, legacy bool) {
	w.Start(name)
	if legacy {
		w.Start("Format")
	}
	for _, format := range formats {
		if legacy {
			w.Start(format)
			w.End()
		} else {
			w.Element("Format", format)
		}
	}
	if legacy {
		w.End()
	}
	w.Start("DCPType")
	w.Start("HTTP")
	if legacy {
		w.Start("Get", xmlutil.A("onlineResource", queryPrefix(endpoint)))
	} else {
		w.Start("Get")
		onlineResource(w, queryPrefix(endpoint))
	}
	w.End()
	w.End()
	w.End()
	w.End()
}

func writeLayer(w *xmlutil.Writer, name string, layer Layer, options CapabilitiesOptions) {
	queryable := "0"
	if layer.Queryable && options.FeatureInfo {
		queryable = "1"
	}
	w.Start("Layer", xmlutil.A("queryable", queryable))
	w.Element("Name", name)
	w.Element("Title", layer.Title)
	w.Optional("Abstract", layer.Abstract)
	modern := options.Version == "1.3.0"
	if options.Version == "1.0.0" || options.Version == "1.1.0" {
		w.Element("SRS", strings.Join(layer.CRS, " "))
	} else {
		element := "SRS"
		if modern {
			element = "CRS"
		}
		for _, crs := range layer.CRS {
			w.Element(element, crs)
		}
	}
	bbox := layer.GeographicBounds
	f := func(n float64) string { return strconv.FormatFloat(n, 'g', -1, 64) }
	if modern {
		w.Start("EX_GeographicBoundingBox")
		w.Element("westBoundLongitude", f(bbox[0]))
		w.Element("eastBoundLongitude", f(bbox[2]))
		w.Element("southBoundLatitude", f(bbox[1]))
		w.Element("northBoundLatitude", f(bbox[3]))
		w.End()
	} else {
		w.Start("LatLonBoundingBox", xmlutil.A("minx", f(bbox[0])), xmlutil.A("miny", f(bbox[1])), xmlutil.A("maxx", f(bbox[2])), xmlutil.A("maxy", f(bbox[3])))
		w.End()
	}
	writeDimensions(w, layer, options.Version)
	for _, name := range slices.Sorted(maps.Keys(layer.Styles)) {
		style := layer.Styles[name]
		w.Start("Style")
		w.Element("Name", name)
		w.Element("Title", style.Title)
		w.Optional("Abstract", style.Abstract)
		w.End()
	}
	w.End()
}

func writeDimensions(w *xmlutil.Writer, layer Layer, version string) {
	if version == "1.0.0" {
		return
	}
	keys := slices.Sorted(maps.Keys(layer.Dimensions))
	for _, name := range keys {
		d := layer.Dimensions[name]
		attrs := []xmlutil.Attr{xmlutil.A("name", name), xmlutil.A("units", d.Units)}
		if d.UnitSymbol != "" {
			attrs = append(attrs, xmlutil.A("unitSymbol", d.UnitSymbol))
		}
		if version == "1.3.0" {
			attrs = append(attrs, dimensionAttrs(d, version)...)
			w.Element("Dimension", strings.Join(d.Values, ","), attrs...)
		} else {
			w.Start("Dimension", attrs...)
			w.End()
		}
	}
	if version != "1.3.0" {
		for _, name := range keys {
			d := layer.Dimensions[name]
			attrs := append([]xmlutil.Attr{xmlutil.A("name", name)}, dimensionAttrs(d, version)...)
			w.Element("Extent", strings.Join(d.Values, ","), attrs...)
		}
	}
}

func dimensionAttrs(d owscore.Dimension, version string) []xmlutil.Attr {
	var attrs []xmlutil.Attr
	if d.Default != "" {
		attrs = append(attrs, xmlutil.A("default", d.Default))
	}
	if version == "1.3.0" {
		for _, item := range []struct {
			name  string
			value bool
		}{{"multipleValues", d.MultipleValues}, {"nearestValue", d.NearestValue}, {"current", d.Current}} {
			if item.value {
				attrs = append(attrs, xmlutil.A(item.name, "1"))
			}
		}
	}
	if version == "1.1.1" && d.NearestValue {
		attrs = append(attrs, xmlutil.A("nearestValue", "1"))
	}
	return attrs
}

func onlineResource(w *xmlutil.Writer, endpoint string) {
	w.Start("OnlineResource", xmlutil.A("xmlns:xlink", "http://www.w3.org/1999/xlink"), xmlutil.A("xlink:type", "simple"), xmlutil.A("xlink:href", endpoint))
	w.End()
}

func queryPrefix(endpoint string) string {
	if strings.HasSuffix(endpoint, "?") || strings.HasSuffix(endpoint, "&") {
		return endpoint
	}
	if strings.Contains(endpoint, "?") {
		return endpoint + "&"
	}
	return endpoint + "?"
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
