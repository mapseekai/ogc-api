package cql2

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

type jsonParser struct {
	decoder *json.Decoder
	limits  Limits
	nodes   int
}

func parseJSON(input string, limits Limits) (Expression, error) {
	if !validJSONEscapes(input) {
		return Expression{}, invalid("JSON string encoding")
	}
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	p := jsonParser{decoder: decoder, limits: limits}
	value, err := p.value(1)
	if err != nil {
		return Expression{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Expression{}, invalid("trailing JSON")
	}
	return jsonExpression(value, limits, 1)
}

func (p *jsonParser) value(depth int) (any, error) {
	p.nodes++
	if depth > p.limits.MaxDepth || p.nodes > p.limits.MaxNodes {
		return nil, invalid("JSON complexity")
	}
	token, err := p.decoder.Token()
	if err != nil {
		return nil, invalid("JSON syntax")
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for p.decoder.More() {
			keyToken, err := p.decoder.Token()
			if err != nil {
				return nil, invalid("JSON member")
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, invalid("JSON member name")
			}
			if _, exists := object[key]; exists {
				return nil, invalid("duplicate JSON member")
			}
			value, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		end, err := p.decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, invalid("JSON object")
		}
		return object, nil
	case '[':
		var array []any
		for p.decoder.More() {
			value, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := p.decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, invalid("JSON array")
		}
		return array, nil
	default:
		return nil, invalid("JSON delimiter")
	}
}

func jsonExpression(value any, limits Limits, depth int) (Expression, error) {
	if depth > limits.MaxDepth {
		return Expression{}, invalid("JSON expression depth")
	}
	switch value := value.(type) {
	case string:
		return literal(Literal{Type: String, Text: value}), nil
	case json.Number:
		n, err := numberLiteral(string(value))
		return literal(n), err
	case bool:
		return literal(Literal{Type: Boolean, Boolean: value}), nil
	case nil:
		return Expression{}, invalid("unexpected literal")
	case map[string]any:
		if property, ok := value["property"]; ok {
			name, ok := property.(string)
			if !ok || name == "" || len(value) != 1 {
				return Expression{}, invalid("property reference")
			}
			return Expression{Kind: Property, Property: name}, nil
		}
		if operator, ok := value["op"]; ok {
			name, ok := operator.(string)
			if !ok || len(value) != 2 || !knownOperator(Operator(name)) {
				return Expression{}, invalid("JSON operator")
			}
			args, ok := value["args"].([]any)
			if !ok {
				return Expression{}, invalid("JSON operands")
			}
			if Operator(name) == In {
				if len(args) != 2 {
					return Expression{}, invalid("in arity")
				}
				list, ok := args[1].([]any)
				if !ok || len(list) == 0 {
					return Expression{}, invalid("in list")
				}
				args = append([]any{args[0]}, list...)
			}
			expression := operation(Operator(name))
			for _, arg := range args {
				child, err := jsonExpression(arg, limits, depth+1)
				if err != nil {
					return Expression{}, err
				}
				expression.Args = append(expression.Args, child)
			}
			return expression, nil
		}
		for _, entry := range []struct {
			key string
			typ Type
		}{{"date", Date}, {"timestamp", Timestamp}} {
			if raw, ok := value[entry.key]; ok {
				text, ok := raw.(string)
				if !ok || len(value) != 1 {
					return Expression{}, invalid("instant value")
				}
				instant, err := instantLiteral(entry.typ, text)
				return literal(instant), err
			}
		}
		if interval, ok := value["interval"]; ok {
			ends, ok := interval.([]any)
			if !ok || len(value) != 1 || len(ends) != 2 {
				return Expression{}, invalid("interval value")
			}
			var endpoints [2]*Literal
			for i, end := range ends {
				text, ok := end.(string)
				if !ok {
					return Expression{}, invalid("interval endpoint")
				}
				var err error
				endpoints[i], err = intervalEndpoint(text)
				if err != nil {
					return Expression{}, err
				}
			}
			result, err := intervalLiteral(endpoints[0], endpoints[1])
			return literal(result), err
		}
		geometry, err := jsonGeometry(value, limits, depth+1)
		return literal(Literal{Type: GeometryType, Geometry: geometry}), err
	default:
		return Expression{}, invalid("JSON expression value")
	}
}

// encoding/json replaces lone surrogate escapes with U+FFFD. Reject them before
// decoding rather than changing a property name or string predicate silently.
func validJSONEscapes(input string) bool {
	quoted := false
	for i := 0; i < len(input); i++ {
		if input[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || input[i] != '\\' {
			continue
		}
		i++
		if i >= len(input) {
			return false
		}
		if input[i] != 'u' {
			continue
		}
		if i+4 >= len(input) {
			return false
		}
		code, err := strconv.ParseUint(input[i+1:i+5], 16, 16)
		if err != nil {
			return false
		}
		i += 4
		r := rune(code)
		if !utf16.IsSurrogate(r) {
			continue
		}
		if r < 0xD800 || r > 0xDBFF || i+6 >= len(input) || input[i+1:i+3] != "\\u" {
			return false
		}
		low, err := strconv.ParseUint(input[i+3:i+7], 16, 16)
		if err != nil || low < 0xDC00 || low > 0xDFFF {
			return false
		}
		i += 6
	}
	return !quoted
}
