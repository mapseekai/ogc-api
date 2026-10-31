package features

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mapseekai/ogc-api/common"
)

// CRSConformance identifies the Part 2 server conformance class. Hosts may add
// it to Catalog.ConformsTo only after verifying their reader's actual operations.
const CRSConformance = "http://www.opengis.net/spec/ogcapi-features-2/1.0/conf/crs"

// CRSOptions declares the actual coordinate systems supported by one reader.
// Supported applies to explicit crs and bbox-crs parameters. DefaultCRS is only
// CRS84 or CRS84h, never the storage CRS or the first supported entry. An empty
// DefaultCRS selects CRS84. Implicit bbox CRS still follows its 2D/3D dimension,
// independent of output, as required by Part 1. No transformation is performed.
type CRSOptions struct {
	Supported  []common.CRS
	DefaultCRS string
}

// Defaults returns independently owned CRS policy with the standard output default.
func (o CRSOptions) Defaults() CRSOptions {
	o.Supported = slices.Clone(o.Supported)
	if o.DefaultCRS == "" {
		o.DefaultCRS = common.CRS84
	}
	return o
}

// Validate requires exact URI identities and unambiguous native axes/dimensions.
func (o CRSOptions) Validate() error {
	if o.DefaultCRS == "" {
		o.DefaultCRS = common.CRS84
	}
	if len(o.Supported) == 0 || len(o.Supported) > 256 {
		return fmt.Errorf("feature CRS policy requires 1 to 256 coordinate systems")
	}
	if o.DefaultCRS != common.CRS84 && o.DefaultCRS != common.CRS84h {
		return fmt.Errorf("feature output default must be CRS84 or CRS84h")
	}
	for i, crs := range o.Supported {
		if err := crs.Validate(); err != nil {
			return err
		}
		for _, previous := range o.Supported[:i] {
			if crs.URI == previous.URI {
				return fmt.Errorf("duplicate feature CRS declaration")
			}
		}
	}
	if _, ok := o.lookup(o.DefaultCRS); !ok {
		return fmt.Errorf("feature CRS declaration must include the standard output default")
	}
	return nil
}

// ParseItemQuery parses only single-feature representation parameters. The Query
// carries the same OutputCRS contract used for item collections, with no predicates.
func ParseItemQuery(values common.Query, options Options) (Query, error) {
	if err := options.Validate(); err != nil {
		return Query{}, err
	}
	allowed := []string{"f"}
	if options.CRS != nil {
		allowed = append(allowed, "crs")
	}
	if err := values.Only(allowed...); err != nil {
		return Query{}, err
	}
	crs, err := options.outputCRS(values)
	if err != nil {
		return Query{}, err
	}
	return Query{OutputCRS: crs}, nil
}

func (o CRSOptions) lookup(uri string) (common.CRS, bool) {
	for _, crs := range o.Supported {
		if uri == crs.URI {
			crs.URI = strings.Clone(crs.URI)
			return crs, true
		}
	}
	return common.CRS{}, false
}

func (o Options) outputCRS(values common.Query) (common.CRS, error) {
	if o.CRS == nil {
		crs, _ := common.CRSByURI(common.CRS84)
		return crs, nil
	}
	policy := *o.CRS
	if policy.DefaultCRS == "" {
		policy.DefaultCRS = common.CRS84
	}
	uri := policy.DefaultCRS
	if value, present := values["crs"]; present {
		uri = value
	}
	crs, ok := policy.lookup(uri)
	if !ok {
		return common.CRS{}, common.Invalid("crs")
	}
	return crs, nil
}

func (o Options) bboxCRS(values common.Query) (common.CRS, error) {
	raw, hasBBox := values["bbox"]
	if uri, explicit := values["bbox-crs"]; explicit {
		if !hasBBox || o.CRS == nil {
			return common.CRS{}, common.Invalid("bbox-crs")
		}
		if crs, ok := o.CRS.lookup(uri); ok {
			return crs, nil
		}
		return common.CRS{}, common.Invalid("bbox-crs")
	}
	uri := common.CRS84
	if strings.Count(raw, ",") == 5 {
		uri = common.CRS84h
	}
	crs, _ := common.CRSByURI(uri)
	return crs, nil
}
