package common

import (
	"fmt"
	"net/url"
	"unicode/utf8"
)

// ValidateCRS checks a published CRS URI for safe metadata and Content-Crs use.
// It does not resolve authority codes, infer axis order or perform reprojection.
func ValidateCRS(value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("invalid CRS URI encoding")
	}
	for _, char := range value {
		if char <= 32 || char == 127 || char == '<' || char == '>' || char == '"' {
			return fmt.Errorf("invalid CRS URI character")
		}
	}
	uri, err := url.Parse(value)
	if err != nil || uri.Scheme == "" || uri.User != nil || uri.Fragment != "" {
		return fmt.Errorf("invalid CRS URI")
	}
	return nil
}
