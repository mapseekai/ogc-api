// Package common defines transport-independent OGC API resource descriptions.
package common

// Standard media types and the longitude/latitude coordinate reference system.
const (
	JSON        = "application/json"
	GeoJSON     = "application/geo+json"
	ProblemJSON = "application/problem+json"
	CRS84       = "http://www.opengis.net/def/crs/OGC/1.3/CRS84"
)

// Link describes a representation or related resource. Href is a public URI.
type Link struct {
	Href      string `json:"href"`
	Rel       string `json:"rel"`
	Type      string `json:"type,omitempty"`
	Title     string `json:"title,omitempty"`
	Templated bool   `json:"templated,omitempty"`
}

// LandingPage provides discovery links for one published API.
type LandingPage struct {
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Links       []Link   `json:"links"`
	Extent      *Extent  `json:"extent,omitempty"`
	CRS         []string `json:"crs,omitempty"`
	StorageCRS  string   `json:"storageCrs,omitempty"`
}

// Conformance lists the conformance classes declared by the published deployment.
type Conformance struct {
	ConformsTo []string `json:"conformsTo"`
}

// SpatialExtent contains CRS-axis-ordered bounding boxes with four or six coordinates.
type SpatialExtent struct {
	BBox [][]float64 `json:"bbox"`
	CRS  string      `json:"crs,omitempty"`
}

// TemporalExtent contains RFC3339 intervals. A nil endpoint denotes an open bound.
type TemporalExtent struct {
	Interval [][2]*string `json:"interval"`
	TRS      string       `json:"trs,omitempty"`
}

// Extent describes optional spatial and temporal coverage of a collection.
type Extent struct {
	Spatial  *SpatialExtent  `json:"spatial,omitempty"`
	Temporal *TemporalExtent `json:"temporal,omitempty"`
}

// Collection is public metadata, independent of a database, renderer or HTTP framework.
// API snapshots copy it at publication time; backend handles are stored separately.
type Collection struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Links       []Link   `json:"links"`
	Extent      *Extent  `json:"extent,omitempty"`
	ItemType    string   `json:"itemType,omitempty"`
	CRS         []string `json:"crs,omitempty"`
	StorageCRS  string   `json:"storageCrs,omitempty"`
}

// Collections is a discoverable collection list. Empty lists encode as JSON arrays.
type Collections struct {
	Collections []Collection `json:"collections"`
	Links       []Link       `json:"links"`
}
