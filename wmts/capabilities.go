package wmts

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/internal/xmlutil"
	"github.com/mapseekai/ogc-api/owscore"
)

const Namespace = "http://www.opengis.net/wmts/1.0"
const OWSNamespace = "http://www.opengis.net/ows/1.1"

// CapabilitiesOptions controls WMTS capability sections, resources and optional feature info.
// URLs must be trusted. Nil Sections means all; an empty non-nil slice means no sections.
// Minimal emits only the root and revision for an unchanged update sequence.
type CapabilitiesOptions struct {
	Endpoint      string
	PublicBaseURL string
	FeatureInfo   bool
	Router        *Router
	Limits        owscore.Limits
	Sections      []string
	Minimal       bool
}

// EncodeCapabilities validates the catalog and encodes bounded, escaped WMTS 1.0.0 XML.
// It filters REST resources against that same catalog without calling custom dimension validators.
// The caller owns the returned bytes; no external schema is fetched at runtime.
func EncodeCapabilities(c Catalog, options CapabilitiesOptions) ([]byte, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	w := xmlutil.New(options.Limits.Defaults().MaxCapabilitiesBytes)
	attrs := []xmlutil.Attr{
		xmlutil.A("xmlns", Namespace), xmlutil.A("xmlns:ows", OWSNamespace),
		xmlutil.A("xmlns:xlink", "http://www.w3.org/1999/xlink"), xmlutil.A("xmlns:xsi", "http://www.w3.org/2001/XMLSchema-instance"),
		xmlutil.A("xsi:schemaLocation", Namespace+" https://schemas.opengis.net/wmts/1.0/wmtsGetCapabilities_response.xsd"),
		xmlutil.A("version", "1.0.0"),
	}
	if c.UpdateSequence != "" {
		attrs = append(attrs, xmlutil.A("updateSequence", c.UpdateSequence))
	}
	w.Start("Capabilities", attrs...)
	if options.Minimal {
		w.End()
		return w.Bytes()
	}
	includes := func(section string) bool {
		return options.Sections == nil || slices.Contains(options.Sections, "All") || slices.Contains(options.Sections, section)
	}
	if includes("ServiceIdentification") {
		w.Start("ows:ServiceIdentification")
		w.Element("ows:Title", c.Service.Title)
		w.Optional("ows:Abstract", c.Service.Abstract)
		if len(c.Service.Keywords) > 0 {
			w.Start("ows:Keywords")
			for _, word := range c.Service.Keywords {
				w.Element("ows:Keyword", word)
			}
			w.End()
		}
		w.Element("ows:ServiceType", "OGC WMTS")
		w.Element("ows:ServiceTypeVersion", "1.0.0")
		w.Optional("ows:Fees", c.Service.Fees)
		w.Optional("ows:AccessConstraints", c.Service.AccessConstraints)
		w.End()
	}
	if includes("ServiceProvider") {
		w.Start("ows:ServiceProvider")
		w.Element("ows:ProviderName", c.Service.Title)
		w.Start("ows:ServiceContact")
		w.End()
		w.End()
	}
	if includes("OperationsMetadata") {
		w.Start("ows:OperationsMetadata")
		operations := []string{"GetCapabilities", "GetTile"}
		if options.FeatureInfo {
			operations = append(operations, "GetFeatureInfo")
		}
		for _, operation := range operations {
			w.Start("ows:Operation", xmlutil.A("name", operation))
			w.Start("ows:DCP")
			w.Start("ows:HTTP")
			w.Start("ows:Get", xmlutil.A("xlink:href", options.Endpoint))
			w.Start("ows:Constraint", xmlutil.A("name", "GetEncoding"))
			w.Start("ows:AllowedValues")
			w.Element("ows:Value", "KVP")
			w.End()
			w.End()
			w.End()
			w.End()
			w.End()
			w.End()
		}
		w.End()
	}
	if includes("Contents") {
		w.Start("Contents")
		for _, name := range slices.Sorted(maps.Keys(c.Layers)) {
			writeLayer(w, name, c, options)
		}
		used := map[string]bool{}
		for _, layer := range c.Layers {
			for key := range layer.MatrixSets {
				used[key] = true
			}
		}
		for _, name := range slices.Sorted(maps.Keys(used)) {
			writeMatrixSet(w, name, c.MatrixSets[name])
		}
		w.End()
	}
	if options.Router != nil && (options.Sections == nil || slices.Contains(options.Sections, "All")) {
		for _, path := range options.Router.MetadataPaths() {
			w.Start("ServiceMetadataURL", xmlutil.A("xlink:href", strings.TrimSuffix(options.PublicBaseURL, "/")+path))
			w.End()
		}
	}
	w.End()
	return w.Bytes()
}

func writeLayer(w *xmlutil.Writer, name string, catalog Catalog, options CapabilitiesOptions) {
	layer := catalog.Layers[name]
	w.Start("Layer")
	w.Element("ows:Title", layer.Title)
	w.Optional("ows:Abstract", layer.Abstract)
	w.Element("ows:Identifier", name)
	for _, id := range slices.Sorted(maps.Keys(layer.Styles)) {
		style := layer.Styles[id]
		attrs := []xmlutil.Attr{}
		if id == layer.DefaultStyle {
			attrs = append(attrs, xmlutil.A("isDefault", "true"))
		}
		w.Start("Style", attrs...)
		w.Element("ows:Title", style.Title)
		w.Optional("ows:Abstract", style.Abstract)
		w.Element("ows:Identifier", id)
		w.End()
	}
	for _, format := range layer.Formats {
		w.Element("Format", format)
	}
	if options.FeatureInfo {
		for _, format := range layer.InfoFormats {
			w.Element("InfoFormat", format)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(layer.Dimensions)) {
		d := layer.Dimensions[name]
		w.Start("Dimension")
		w.Element("ows:Identifier", name)
		if d.Units != "" {
			w.Element("ows:UOM", d.Units)
		}
		w.Optional("UnitSymbol", d.UnitSymbol)
		w.Optional("Default", d.Default)
		if d.Current {
			w.Element("Current", "true")
		}
		for _, value := range d.Values {
			w.Element("Value", value)
		}
		w.End()
	}
	for _, id := range slices.Sorted(maps.Keys(layer.MatrixSets)) {
		link := layer.MatrixSets[id]
		w.Start("TileMatrixSetLink")
		w.Element("TileMatrixSet", id)
		if len(link.Limits) > 0 {
			w.Start("TileMatrixSetLimits")
			for _, matrix := range slices.Sorted(maps.Keys(link.Limits)) {
				limit := link.Limits[matrix]
				w.Start("TileMatrixLimits")
				w.Element("TileMatrix", matrix)
				w.Element("MinTileRow", strconv.FormatUint(limit.MinTileRow, 10))
				w.Element("MaxTileRow", strconv.FormatUint(limit.MaxTileRow, 10))
				w.Element("MinTileCol", strconv.FormatUint(limit.MinTileCol, 10))
				w.Element("MaxTileCol", strconv.FormatUint(limit.MaxTileCol, 10))
				w.End()
			}
			w.End()
		}
		w.End()
	}
	if options.Router != nil {
		for _, resource := range options.Router.Resources(name, catalog, options.PublicBaseURL, options.FeatureInfo && len(layer.InfoFormats) > 0) {
			w.Start("ResourceURL", xmlutil.A("format", resource.Format), xmlutil.A("resourceType", resource.ResourceType), xmlutil.A("template", resource.Template))
			w.End()
		}
	}
	w.End()
}
func writeMatrixSet(w *xmlutil.Writer, name string, set MatrixSet) {
	w.Start("TileMatrixSet")
	w.Optional("ows:Title", set.Title)
	w.Element("ows:Identifier", name)
	w.Element("ows:SupportedCRS", set.CRS)
	w.Optional("WellKnownScaleSet", set.WellKnownScaleSet)
	for _, id := range slices.Sorted(maps.Keys(set.Matrices)) {
		m := set.Matrices[id]
		w.Start("TileMatrix")
		w.Element("ows:Identifier", id)
		w.Element("ScaleDenominator", strconv.FormatFloat(m.ScaleDenominator, 'g', -1, 64))
		w.Element("TopLeftCorner", strconv.FormatFloat(m.TopLeftCorner[0], 'g', -1, 64)+" "+strconv.FormatFloat(m.TopLeftCorner[1], 'g', -1, 64))
		w.Element("TileWidth", strconv.FormatUint(m.TileWidth, 10))
		w.Element("TileHeight", strconv.FormatUint(m.TileHeight, 10))
		w.Element("MatrixWidth", strconv.FormatUint(m.MatrixWidth, 10))
		w.Element("MatrixHeight", strconv.FormatUint(m.MatrixHeight, 10))
		w.End()
	}
	w.End()
}
