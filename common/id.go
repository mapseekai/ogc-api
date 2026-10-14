package common

import "unicode/utf8"

// ValidID reports whether an identifier can round-trip through a URL path segment.
// Unicode and escaped slashes are supported; empty IDs, dot segments, malformed
// UTF-8 and control characters are rejected at publication and request boundaries.
func ValidID(value string) bool {
	if value == "" || value == "." || value == ".." || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
