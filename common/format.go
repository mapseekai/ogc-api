package common

import (
	"mime"
	"strconv"
	"strings"
)

// Negotiate chooses an offered media type. An explicit f parameter takes precedence.
// Accept quality uses the most specific matching range; q=0 exclusions are respected.
func Negotiate(accept, format string, offered []string) (string, error) {
	if format != "" {
		for _, media := range offered {
			if format == media || format == formatAlias(media) || (format == "json" && media == GeoJSON) {
				return media, nil
			}
		}
		return "", Invalid("f")
	}
	if accept == "" {
		if len(offered) > 0 {
			return offered[0], nil
		}
		return "", notAcceptable()
	}
	ranges, err := parseAccept(accept)
	if err != nil {
		return "", err
	}
	best, bestQ := "", 0.0
	for _, offeredType := range offered {
		media, mediaParams, err := mime.ParseMediaType(offeredType)
		if err != nil {
			continue
		}
		specificity, quality := -1, 0.0
		for _, candidate := range ranges {
			score := -1
			switch {
			case candidate.media == media:
				score = 2
			case candidate.media == strings.SplitN(media, "/", 2)[0]+"/*":
				score = 1
			case candidate.media == "*/*":
				score = 0
			}
			if score < 0 {
				continue
			}
			matches := true
			for name, value := range candidate.params {
				if mediaParams[name] != value {
					matches = false
				}
			}
			if !matches {
				continue
			}
			score = score*100 + len(candidate.params)
			if score > specificity || (score == specificity && candidate.quality > quality) {
				specificity, quality = score, candidate.quality
			}
		}
		if quality > bestQ {
			best, bestQ = offeredType, quality
		}
	}
	if best == "" {
		return "", notAcceptable()
	}
	return best, nil
}

type mediaRange struct {
	media   string
	params  map[string]string
	quality float64
}

func parseAccept(raw string) ([]mediaRange, error) {
	if len(raw) > 8192 {
		return nil, notAcceptable()
	}
	var ranges []mediaRange
	start, quoted, escaped := 0, false, false
	for i := 0; i <= len(raw); i++ {
		if i < len(raw) {
			c := raw[i]
			if escaped {
				escaped = false
				continue
			}
			if quoted && c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = !quoted
			}
			if c != ',' || quoted {
				continue
			}
		}
		media, params, err := mime.ParseMediaType(strings.TrimSpace(raw[start:i]))
		if err != nil || !strings.Contains(media, "/") {
			return nil, notAcceptable()
		}
		quality := 1.0
		if rawQ, ok := params["q"]; ok {
			quality, err = strconv.ParseFloat(rawQ, 64)
			if err != nil || !(quality >= 0 && quality <= 1) {
				return nil, notAcceptable()
			}
			delete(params, "q")
		}
		ranges = append(ranges, mediaRange{media: media, params: params, quality: quality})
		start = i + 1
	}
	return ranges, nil
}

func formatAlias(media string) string {
	switch media {
	case JSON:
		return "json"
	case GeoJSON:
		return "geojson"
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpeg"
	case "image/tiff":
		return "tiff"
	case "application/vnd.mapbox-vector-tile":
		return "mvt"
	case "application/prs.coverage+json":
		return "covjson"
	default:
		return ""
	}
}

func notAcceptable() *Error {
	return &Error{Status: 406, Code: "NotAcceptable", Description: "No acceptable representation is available"}
}
