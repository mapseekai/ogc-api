package maps

import (
	"slices"
	"strconv"
	"strings"

	"github.com/mapseekai/ogc-api/common"
)

// ParseQuery validates the explicitly supported map query profile against a prepared service.
// It returns a fresh request; the service must have been returned by Prepare.
func ParseQuery(values common.Query, service Service) (Request, error) {
	if err := values.Only("f", "bbox", "bbox-crs", "crs", "width", "height", "datetime", "transparent", "bgcolor"); err != nil {
		return Request{}, err
	}
	request := Request{
		BBox: slices.Clone(service.DefaultBBox), BBoxCRS: common.CRS84,
		CRS: service.DefaultCRS, Background: "0xFFFFFF", Limits: service.Limits,
	}
	if crs, ok := values["crs"]; ok {
		if !slices.Contains(service.CRS, crs) {
			return Request{}, common.Invalid("crs")
		}
		request.CRS = crs
	}
	if crs, ok := values["bbox-crs"]; ok {
		if !slices.Contains(service.CRS, crs) {
			return Request{}, common.Invalid("bbox-crs")
		}
		if _, present := values["bbox"]; !present {
			return Request{}, common.Invalid("bbox-crs")
		}
		request.BBoxCRS = crs
	}
	var err error
	if bbox, ok := values["bbox"]; ok {
		request.BBox, err = common.ParseBBox(bbox, request.BBoxCRS == common.CRS84)
		if err != nil || len(request.BBox) != 4 {
			return Request{}, common.Invalid("bbox")
		}
	}
	if request.BBox[0] == request.BBox[2] || request.BBox[1] == request.BBox[3] {
		return Request{}, common.Invalid("bbox")
	}
	request.Width, err = values.Integer("width", 0, 1, service.Limits.MaxImageWidth)
	if err != nil {
		return Request{}, err
	}
	request.Height, err = values.Integer("height", 0, 1, service.Limits.MaxImageHeight)
	if err != nil {
		return Request{}, err
	}
	if request.Width == 0 && request.Height == 0 {
		request.Width, request.Height = service.DefaultWidth, service.DefaultHeight
	}
	if err := checkSize(request.Width, request.Height, service.Limits); err != nil {
		return Request{}, err
	}
	if value, ok := values["transparent"]; ok {
		if value != "true" && value != "false" {
			return Request{}, common.Invalid("transparent")
		}
		request.Transparent = value == "true"
	}
	if value, ok := values["bgcolor"]; ok {
		if len(value) != 8 || !strings.HasPrefix(value, "0x") {
			return Request{}, common.Invalid("bgcolor")
		}
		if _, err := strconv.ParseUint(value[2:], 16, 24); err != nil {
			return Request{}, common.Invalid("bgcolor")
		}
		request.Background = value
	}
	if value, ok := values["datetime"]; ok {
		interval, err := common.ParseDatetime(value)
		if err != nil {
			return Request{}, err
		}
		request.Datetime = &interval
	}
	return request, nil
}
