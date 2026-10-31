package common

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Horizontal axis orders describe native wire coordinates, never a reprojection.
const (
	XY AxisOrder = "xy"
	YX AxisOrder = "yx"
)

// AxisOrder identifies longitude/easting then latitude/northing, or the reverse.
// A third coordinate, when present, retains the CRS's vertical axis semantics.
type AxisOrder string

// CRS is an explicitly declared two- or three-dimensional coordinate system.
// Geographic means horizontal coordinates are longitude/latitude in degrees.
// URI is matched exactly, without SRID aliases, resolution or axis inference.
// Custom definitions are supplied by the host from authoritative CRS metadata.
type CRS struct {
	URI        string    `json:"uri"`
	AxisOrder  AxisOrder `json:"axisOrder"`
	Dimensions int       `json:"dimensions"`
	Geographic bool      `json:"geographic"`
}

// CRSByURI returns native axis metadata for a small fixed set of standard URIs.
// It neither enables a CRS for a collection nor resolves arbitrary authority codes.
func CRSByURI(uri string) (CRS, bool) {
	definition := CRS{URI: uri, AxisOrder: XY, Dimensions: 2}
	switch uri {
	case CRS84:
		definition.Geographic = true
	case CRS84h:
		definition.Geographic, definition.Dimensions = true, 3
	case "http://www.opengis.net/def/crs/EPSG/0/4326":
		definition.Geographic, definition.AxisOrder = true, YX
	case "http://www.opengis.net/def/crs/EPSG/0/4979":
		definition.Geographic, definition.AxisOrder, definition.Dimensions = true, YX, 3
	case "http://www.opengis.net/def/crs/EPSG/0/3857":
	default:
		return CRS{}, false
	}
	return definition, true
}

// Validate checks URI, dimension and explicit axes, including known URI consistency.
func (c CRS) Validate() error {
	if err := ValidateCRS(c.URI); err != nil {
		return err
	}
	if (c.Dimensions != 2 && c.Dimensions != 3) || (c.AxisOrder != XY && c.AxisOrder != YX) {
		return fmt.Errorf("CRS requires explicit 2D/3D native axis metadata")
	}
	if builtin, ok := CRSByURI(c.URI); ok && c != builtin {
		return fmt.Errorf("CRS metadata conflicts with its standard URI")
	}
	return nil
}

// ParseBBox reads a bbox in native CRS axes. It never swaps or transforms values.
func (c CRS) ParseBBox(value string) ([]float64, error) {
	bbox, err := parseBBoxValues(value)
	if err != nil {
		return nil, err
	}
	if err := c.ValidateBBox(bbox); err != nil {
		return nil, err
	}
	return bbox, nil
}

// ValidateBBox enforces finite, dimension-matching, native-axis ordered bounds.
// Only geographic longitude may wrap. Projected values have no degree-range check.
// Authority-specific areas of use and coordinate operations remain backend duties.
func (c CRS) ValidateBBox(bbox []float64) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if len(bbox) != 2*c.Dimensions {
		return Invalid("bbox")
	}
	longitude, latitude := 0, 1
	if c.AxisOrder == YX {
		longitude, latitude = 1, 0
	}
	for i, value := range bbox {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return Invalid("bbox")
		}
		if i < c.Dimensions && !(c.Geographic && i == longitude) && value > bbox[i+c.Dimensions] {
			return Invalid("bbox")
		}
	}
	if c.Geographic && (bbox[longitude] < -180 || bbox[longitude] > 180 || bbox[longitude+c.Dimensions] < -180 || bbox[longitude+c.Dimensions] > 180 ||
		bbox[latitude] < -90 || bbox[latitude] > 90 || bbox[latitude+c.Dimensions] < -90 || bbox[latitude+c.Dimensions] > 90) {
		return Invalid("bbox")
	}
	return nil
}

// ValidateCRS checks a published CRS URI for safe metadata and Content-Crs use.
// It does not resolve authority codes, infer axis order or perform reprojection.
func ValidateCRS(value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("invalid CRS URI encoding")
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._~:/?#[]@!$&'()*+,;=%", char)) {
			return fmt.Errorf("invalid CRS URI character")
		}
	}
	if _, err := url.PathUnescape(value); err != nil {
		return fmt.Errorf("invalid CRS URI escape")
	}
	uri, err := url.Parse(value)
	if err != nil || uri.Scheme == "" || uri.User != nil || uri.Fragment != "" {
		return fmt.Errorf("invalid CRS URI")
	}
	hierarchical := value[len(uri.Scheme)+1:]
	if strings.HasPrefix(hierarchical, "//") {
		authorityEnd := strings.IndexAny(hierarchical[2:], "/?#")
		if authorityEnd < 0 {
			hierarchical = ""
		} else {
			hierarchical = hierarchical[authorityEnd+2:]
		}
	}
	path, _, _ := strings.Cut(hierarchical, "?")
	if !validCRSComponent(path, false) || !validCRSComponent(uri.RawQuery, true) {
		return fmt.Errorf("invalid CRS URI component")
	}
	if strings.EqualFold(uri.Scheme, "http") || strings.EqualFold(uri.Scheme, "https") {
		if uri.Hostname() == "" || uri.Opaque != "" {
			return fmt.Errorf("invalid HTTP CRS URI")
		}
	}
	if strings.EqualFold(uri.Scheme, "urn") {
		nid, nss, separated := strings.Cut(uri.Opaque, ":")
		if !separated || len(nid) < 2 || len(nid) > 32 || nss == "" || nid[0] == '-' || nid[len(nid)-1] == '-' {
			return fmt.Errorf("invalid CRS URN")
		}
		for _, character := range nid {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
				return fmt.Errorf("invalid CRS URN namespace")
			}
		}
	}
	return nil
}

func validCRSComponent(value string, query bool) bool {
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._~:@!$&'()*+,;=%/", char) || query && char == '?' {
			continue
		}
		return false
	}
	return true
}
