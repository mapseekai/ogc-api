package cql2

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Numbers remain exact decimal strings. Large scales use canonical scientific
// notation to avoid expanding a short hostile exponent into kilobytes per value.
func numberLiteral(raw string) (Literal, error) {
	if len(raw) == 0 || len(raw) > 1024 {
		return Literal{}, invalid("numeric literal length")
	}
	negative := false
	if raw[0] == '+' || raw[0] == '-' {
		negative = raw[0] == '-'
		raw = raw[1:]
	}
	mantissa, expText, exponentPresent := strings.Cut(strings.ToLower(raw), "e")
	exponent := 0
	if exponentPresent {
		var err error
		exponent, err = strconv.Atoi(expText)
		if err != nil || exponent < -1024 || exponent > 1024 {
			return Literal{}, invalid("numeric exponent")
		}
	}
	whole, fraction, dot := strings.Cut(mantissa, ".")
	if whole == "" && (!dot || fraction == "") {
		return Literal{}, invalid("numeric literal")
	}
	digits := whole + fraction
	if digits == "" {
		return Literal{}, invalid("numeric literal")
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return Literal{}, invalid("numeric literal")
		}
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return Literal{Type: Number, Number: "0"}, nil
	}
	power := exponent - len(fraction)
	trimmed := strings.TrimRight(digits, "0")
	power += len(digits) - len(trimmed)
	digits = trimmed
	point := len(digits) + power
	length := len(digits) + 1
	if point <= 0 {
		length = 2 - point + len(digits)
	}
	if point >= len(digits) {
		length = point
	}
	var normalized string
	switch {
	case length > 64:
		normalized = digits[:1]
		if len(digits) > 1 {
			normalized += "." + digits[1:]
		}
		normalized += "e" + strconv.Itoa(point-1)
	case point <= 0:
		normalized = "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		normalized = digits + strings.Repeat("0", point-len(digits))
	default:
		normalized = digits[:point] + "." + digits[point:]
	}
	if negative {
		normalized = "-" + normalized
	}
	return Literal{Type: Number, Number: json.Number(strings.Clone(normalized))}, nil
}

func instantLiteral(typ Type, raw string) (Literal, error) {
	if !validInstantText(typ, raw) {
		return Literal{}, invalid("temporal literal syntax or precision")
	}
	layout := time.DateOnly
	if typ == Timestamp {
		layout = time.RFC3339Nano
	}
	t, err := time.Parse(layout, raw)
	if err != nil {
		return Literal{}, invalid("temporal literal")
	}
	return Literal{Type: typ, Text: t.Format(layout)}, nil
}

// time.Parse deliberately accepts some forms outside the CQL2 grammar, including
// comma fractions and single-digit hours, and truncates excess fractional digits.
// Validate the complete wire lexeme before using it for calendar/range checks.
func validInstantText(typ Type, raw string) bool {
	if len(raw) < 10 || raw[4] != '-' || raw[7] != '-' {
		return false
	}
	for _, i := range []int{0, 1, 2, 3, 5, 6, 8, 9} {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}
	if typ == Date {
		return len(raw) == 10
	}
	if typ != Timestamp || len(raw) < 20 || len(raw) > 30 || raw[10] != 'T' || raw[13] != ':' || raw[16] != ':' || raw[len(raw)-1] != 'Z' {
		return false
	}
	for _, i := range []int{11, 12, 14, 15, 17, 18} {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}
	if len(raw) == 20 {
		return raw[19] == 'Z'
	}
	if len(raw) < 22 || raw[19] != '.' {
		return false
	}
	for i := 20; i < len(raw)-1; i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}
	return true
}

func intervalLiteral(start, end *Literal) (Literal, error) {
	if start == nil && end == nil {
		return Literal{}, invalid("unbounded interval")
	}
	if start != nil && end != nil {
		if start.Type != end.Type {
			return Literal{}, invalid("interval granularity")
		}
		// Both endpoints are normalized UTC or YYYY-MM-DD, so timestamps with
		// optional fractions must be compared as instants, not lexicographically.
		layout := time.DateOnly
		if start.Type == Timestamp {
			layout = time.RFC3339Nano
		}
		a, _ := time.Parse(layout, start.Text)
		b, _ := time.Parse(layout, end.Text)
		if a.After(b) {
			return Literal{}, invalid("interval endpoint order")
		}
	}
	return Literal{Type: IntervalType, Interval: &Interval{Start: start, End: end}}, nil
}

func intervalEndpoint(raw string) (*Literal, error) {
	if raw == ".." {
		return nil, nil
	}
	typ := Date
	if strings.Contains(raw, "T") {
		typ = Timestamp
	}
	value, err := instantLiteral(typ, raw)
	return &value, err
}
