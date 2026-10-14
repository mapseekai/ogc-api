// Package kvp parses raw query strings without losing empty or repeated fields.
package kvp

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mapseekai/ogc-api/owscore"
)

// Params owns a parsed query and preserves missing, empty and repeated fields.
// Its zero value is an empty query. Read methods are safe for concurrent use;
// returned maps and slices are independent copies.
type Params struct {
	raw    string
	values map[string][]Value
}

// Parse validates and copies a raw query before decoding each field once.
// It applies default limits to zero fields and rejects malformed text and escapes.
// On error, discard the possibly partial Params value.
func Parse(raw string, limits owscore.Limits) (Params, error) {
	if err := limits.Validate(); err != nil {
		return Params{}, err
	}
	limits = limits.Defaults()
	if len(raw) > limits.MaxQueryBytes {
		return Params{}, owscore.Fail(owscore.TooLarge, "QUERY", "Query exceeds the configured limit")
	}
	count := 0
	if raw != "" {
		count = strings.Count(raw, "&") + 1
	}
	if count > limits.MaxParameters {
		return Params{}, owscore.Fail(owscore.TooLarge, "QUERY", "Too many query parameters")
	}
	raw = strings.Clone(raw)
	p := Params{raw: raw, values: make(map[string][]Value, count)}
	for item := range strings.SplitSeq(raw, "&") {
		if item == "" {
			continue
		}
		name, value, _ := strings.Cut(item, "=")
		n, err := url.QueryUnescape(name)
		if err != nil || n == "" || !validText(n) {
			return p, owscore.Invalid("QUERY")
		}
		v, err := url.QueryUnescape(value)
		if err != nil || !validText(v) {
			return p, owscore.Invalid(Key(n))
		}
		key := Key(n)
		p.values[key] = append(p.values[key], Value{n, name, value, v})
	}
	return p, nil
}

// RawQuery returns the original escaped query, not a serialization of merged route values.
// It may contain credentials and should not be logged without redaction.
func (p Params) RawQuery() string { return p.raw }

// Has reports whether a case-insensitive parameter name occurs, even with an empty value.
func (p Params) Has(name string) bool { return len(p.values[Key(name)]) != 0 }

// One returns a decoded singleton value and whether it was present.
// Repeated names, including names differing only in ASCII case, return an error.
func (p Params) One(name string) (string, bool, error) {
	vs := p.values[Key(name)]
	if len(vs) == 0 {
		return "", false, nil
	}
	if len(vs) > 1 {
		return "", true, owscore.Fail(owscore.InvalidParameter, name, "Repeated singleton parameter")
	}
	return vs[0].Text, true, nil
}

// Unique rejects repeated occurrences of the named parameters; absent names are allowed.
func (p Params) Unique(names ...string) error {
	for _, name := range names {
		if _, _, err := p.One(name); err != nil {
			return err
		}
	}
	return nil
}

// All returns an independently mutable map of ASCII-folded names to decoded values.
// Value order within each parameter is preserved.
func (p Params) All() map[string][]string {
	out := make(map[string][]string, len(p.values))
	for k, vs := range p.values {
		for _, v := range vs {
			out[k] = append(out[k], v.Text)
		}
	}
	return out
}

// Except returns a copy of all decoded fields except the case-insensitive names supplied.
func (p Params) Except(names ...string) map[string][]string {
	out := p.All()
	for _, name := range names {
		delete(out, Key(name))
	}
	return out
}

// List splits literal commas before decoding, so %2C stays inside an identifier.
func (p Params) List(name string) ([]string, bool, error) {
	return p.ListLimit(name, 0)
}

// ListLimit counts literal commas before allocating or decoding the list.
// A zero maximum is unlimited; a negative maximum is an invalid configuration.
func (p Params) ListLimit(name string, maximum int) ([]string, bool, error) {
	if maximum < 0 {
		return nil, false, owscore.Invalid("LIMIT")
	}
	_, exists, err := p.One(name)
	if err != nil || !exists {
		return nil, exists, err
	}
	raw := p.values[Key(name)][0].RawValue
	if maximum > 0 && strings.Count(raw, ",") >= maximum {
		return nil, true, owscore.Fail(owscore.TooLarge, name, "Too many list items")
	}
	parts := strings.Split(raw, ",")
	for i, part := range parts {
		parts[i], err = url.QueryUnescape(part)
		if err != nil {
			return nil, true, owscore.Invalid(name)
		}
	}
	return parts, true, nil
}

// Merge binds trusted route values, rejecting conflicting query values.
// Equal values are tolerated, but repeated query parameters are not.
func (p Params) Merge(values map[string]string) (Params, error) {
	out := Params{raw: p.raw, values: make(map[string][]Value, len(p.values)+len(values))}
	for k, v := range p.values {
		out.values[k] = append([]Value(nil), v...)
	}
	for name, value := range values {
		name = Key(name)
		existing, present, err := out.One(name)
		if err != nil {
			return Params{}, err
		}
		if present && existing != value {
			return Params{}, owscore.Fail(owscore.InvalidParameter, name, "Conflicting route and query values")
		}
		if !validText(value) {
			return Params{}, owscore.Invalid(name)
		}
		out.values[name] = []Value{{Name: name, RawName: name, RawValue: url.QueryEscape(value), Text: value}}
	}
	return out, nil
}

// Reader keeps the first error while collecting typed fields.
type Reader struct {
	Params Params
	Err    error
}

// String reads a singleton field and records missing, empty or duplicate-value errors.
// Required controls presence; allowEmpty controls an explicitly empty value.
func (r *Reader) String(name string, required, allowEmpty bool) string {
	value, exists, err := r.Params.One(name)
	r.SetError(err)
	if required && !exists {
		r.SetError(owscore.Missing(name))
	}
	if exists && !allowEmpty && value == "" {
		r.SetError(owscore.Invalid(name))
	}
	return value
}

// List reads an unbounded list and records missing or duplicate-value errors.
// Use ListLimit when an operation imposes a smaller item budget.
func (r *Reader) List(name string, required bool) []string {
	return r.ListLimit(name, required, 0)
}

// ListLimit reads a list with a pre-allocation item limit and retains the first error.
func (r *Reader) ListLimit(name string, required bool, maximum int) []string {
	value, exists, err := r.Params.ListLimit(name, maximum)
	r.SetError(err)
	if required && !exists {
		r.SetError(owscore.Missing(name))
	}
	return value
}

// Uint reads an unsigned decimal singleton and retains any parsing error.
// Fallback is used only for an absent field; a missing required field still sets Err.
func (r *Reader) Uint(name string, required bool, fallback uint64) uint64 {
	value := r.String(name, required, false)
	if !r.Params.Has(name) {
		return fallback
	}
	n, err := ParseUint(value)
	if err != nil {
		r.SetError(owscore.Invalid(name))
	}
	return n
}

// SetError retains the first non-nil error; later errors do not replace its locator.
func (r *Reader) SetError(err error) {
	if r.Err == nil {
		r.Err = err
	}
}

// Value preserves the original spelling and escaped bytes alongside decoded text.
// Resource values are never case-folded.
type Value struct{ Name, RawName, RawValue, Text string }

// Key folds ASCII parameter names only. Resource identifiers remain unchanged.
func Key(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// ParseUint accepts only non-empty ASCII decimal digits that fit in uint64.
// Signs, fractions, whitespace and overflow return an error.
func ParseUint(value string) (uint64, error) {
	if value == "" {
		return 0, owscore.Invalid("NUMBER")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, owscore.Invalid("NUMBER")
		}
	}
	return strconv.ParseUint(value, 10, 64)
}

func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n") }
