package jsonutil

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

// Marshal checks the value before encoding so rejected large fields do not cause
// an unbounded temporary allocation. Raw JSON whitespace counts against the input
// budget; escaped strings and base64 bytes count at their encoded size. The final
// output is checked as well. Temporary memory is O(limit), not exactly limit bytes.
// This internal encoder accepts native JSON values and time.Time, not arbitrary
// custom marshalers whose allocation or output size cannot be checked in advance.
func Marshal(value any, limit int) ([]byte, error) {
	if err := CheckSize(value, limit); err != nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}

// CheckSize preflights project-owned JSON models without cloning their payloads.
// Input graphs must remain immutable during this call and subsequent encoding.
func CheckSize(value any, limit int) error {
	if limit <= 0 {
		return ErrTooLarge
	}
	remaining := Budget(limit)
	return remaining.value(reflect.ValueOf(value), 0)
}

// Budget accounts for encoded bytes without retaining or copying input data.
type Budget int

// Take reserves fixed-size JSON syntax or previously measured bytes.
func (b *Budget) Take(size int) error {
	if size < 0 || size > int(*b) {
		return ErrTooLarge
	}
	*b -= Budget(size)
	return nil
}

// String reserves a JSON string, including HTML and Unicode escaping.
func (b *Budget) String(value string) error {
	if len(value) > int(*b) {
		return ErrTooLarge
	}
	if err := b.Take(2); err != nil {
		return err
	}
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			return fmt.Errorf("invalid UTF-8 in protocol JSON string")
		}
		encoded := size
		switch {
		case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
			encoded = 2
		case r < 32 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
			encoded = 6
		}
		if err := b.Take(encoded); err != nil {
			return err
		}
		value = value[size:]
	}
	return nil
}

func (b *Budget) value(v reflect.Value, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON value nesting exceeds limit")
	}
	if !v.IsValid() {
		return b.Take(4)
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return b.Take(4)
		}
		if v.Kind() == reflect.Pointer {
			switch v.Interface().(type) {
			case *json.RawMessage, *time.Time:
			case json.Marshaler, encoding.TextMarshaler:
				return fmt.Errorf("custom JSON encoders are not permitted in bounded protocol models")
			}
		}
		return b.value(v.Elem(), depth+1)
	}
	switch value := v.Interface().(type) {
	case json.RawMessage:
		return b.Raw(value)
	case json.Number:
		if value == "" {
			return b.Take(1)
		}
		return b.Take(len(value))
	case time.Time:
		data, err := value.MarshalJSON()
		if err != nil {
			return err
		}
		return b.Take(len(data))
	case json.Marshaler, encoding.TextMarshaler:
		return fmt.Errorf("custom JSON encoders are not permitted in bounded protocol models")
	}
	if v.CanAddr() && v.Addr().CanInterface() {
		switch v.Addr().Interface().(type) {
		case json.Marshaler, encoding.TextMarshaler:
			return fmt.Errorf("custom JSON encoders are not permitted in bounded protocol models")
		}
	}
	switch v.Kind() {
	case reflect.String:
		return b.String(v.String())
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		data, err := json.Marshal(v.Interface()) // Scalars have a fixed, small size.
		if err != nil {
			return err
		}
		return b.Take(len(data))
	case reflect.Slice:
		if v.IsNil() {
			return b.Take(4)
		}
		if v.Type().Elem() == reflect.TypeFor[byte]() {
			if v.Len() > int(*b) {
				return ErrTooLarge
			}
			return b.Take(2 + base64.StdEncoding.EncodedLen(v.Len()))
		}
		fallthrough
	case reflect.Array:
		if v.Len() > int(*b) {
			return ErrTooLarge
		}
		if err := b.Take(2); err != nil {
			return err
		}
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				if err := b.Take(1); err != nil {
					return err
				}
			}
			if err := b.value(v.Index(i), depth+1); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		return b.mapping(v, depth)
	case reflect.Struct:
		return b.structure(v, depth)
	default:
		return fmt.Errorf("unsupported bounded JSON kind %s", v.Kind())
	}
}

func emptyJSON(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
		return value.IsZero()
	default:
		return false
	}
}

// Raw reserves raw JSON including escaping; whitespace remains charged.
func (b *Budget) Raw(value json.RawMessage) error {
	if value == nil {
		return b.Take(4)
	}
	if err := b.Take(len(value)); err != nil {
		return err
	}
	if !utf8.Valid(value) {
		return fmt.Errorf("invalid UTF-8 in raw protocol JSON")
	}
	// encoding/json escapes these literal characters in RawMessage strings too.
	for _, c := range value {
		if c == '<' || c == '>' || c == '&' {
			if err := b.Take(5); err != nil {
				return err
			}
		}
	}
	for i := 0; i+2 < len(value); i++ {
		if value[i] == 0xe2 && value[i+1] == 0x80 && (value[i+2] == 0xa8 || value[i+2] == 0xa9) {
			if err := b.Take(3); err != nil {
				return err
			}
		}
	}
	return nil
}

// Value reserves a native JSON value. Custom encoders are rejected.
func (b *Budget) Value(value any) error { return b.value(reflect.ValueOf(value), 0) }

func (b *Budget) mapping(v reflect.Value, depth int) error {
	if v.IsNil() {
		return b.Take(4)
	}
	if v.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("protocol JSON keys must be strings")
	}
	if v.Len() > int(*b) {
		return ErrTooLarge
	}
	if err := b.Take(2); err != nil {
		return err
	}
	iter, first := v.MapRange(), true
	for iter.Next() {
		if !first {
			if err := b.Take(1); err != nil {
				return err
			}
		}
		first = false
		if err := b.String(iter.Key().String()); err != nil {
			return err
		}
		if err := b.Take(1); err != nil {
			return err
		}
		if err := b.value(iter.Value(), depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (b *Budget) structure(v reflect.Value, depth int) error {
	if err := b.Take(2); err != nil {
		return err
	}
	first := true
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "-" && (field.Anonymous || options != "" && options != "omitempty") {
			return fmt.Errorf("unsupported JSON field options in bounded protocol model")
		}
		if name == "-" || options == "omitempty" && emptyJSON(v.Field(i)) {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if !first {
			if err := b.Take(1); err != nil {
				return err
			}
		}
		first = false
		if err := b.String(name); err != nil {
			return err
		}
		if err := b.Take(1); err != nil {
			return err
		}
		if err := b.value(v.Field(i), depth+1); err != nil {
			return err
		}
	}
	return nil
}
