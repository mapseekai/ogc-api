package cql2

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type textParser struct {
	input    string
	position int
	token    token
	err      error
	limits   Limits
	nodes    int
}

type token struct {
	kind  byte
	text  string
	start int
}

func parseText(input string, limits Limits) (Expression, error) {
	p := textParser{input: input, limits: limits}
	p.advance()
	expression := p.boolean(1, Or)
	if p.err != nil {
		return Expression{}, p.err
	}
	if p.token.kind != 0 {
		return Expression{}, invalid("trailing text")
	}
	return expression, nil
}

func (p *textParser) boolean(depth int, level Operator) Expression {
	if !p.enter(depth) {
		return Expression{}
	}
	next := func() Expression { return p.primary(depth + 1) }
	if level == Or {
		next = func() Expression { return p.boolean(depth+1, And) }
	}
	left := next()
	var args []Expression
	for p.keyword(string(level)) && p.err == nil {
		p.advance()
		if args == nil {
			args = append(args, left)
		}
		args = append(args, next())
	}
	if args != nil {
		return p.node(operation(level, args...))
	}
	return left
}

func (p *textParser) primary(depth int) Expression {
	if !p.enter(depth) {
		return Expression{}
	}
	if p.keyword("NOT") {
		p.advance()
		return p.node(operation(Not, p.primary(depth+1)))
	}
	var value Expression
	if p.take('(') {
		value = p.boolean(depth+1, Or)
		p.expect(')')
		if value.Kind != Operation && (value.Literal == nil || value.Literal.Type != Boolean) {
			p.fail("parenthesized boolean operand")
			return Expression{}
		}
	} else {
		value = p.predicate(depth)
	}
	return p.nullSuffix(value, depth)
}

func (p *textParser) predicate(depth int) Expression {
	left := p.operand(depth + 1)
	if p.err != nil {
		return Expression{}
	}
	if p.token.kind == 'o' {
		op := comparisonOperator(p.token.text)
		p.advance()
		return p.node(operation(op, left, p.operand(depth+1)))
	}
	negate := p.keyword("NOT")
	if negate {
		p.advance()
	}
	var result Expression
	switch {
	case p.keyword("LIKE"):
		p.advance()
		result = p.node(operation(Like, left, p.operand(depth+1)))
	case p.keyword("BETWEEN"):
		p.advance()
		lower := p.operand(depth + 1)
		p.expectKeyword("AND")
		result = p.node(operation(Between, left, lower, p.operand(depth+1)))
	case p.keyword("IN"):
		p.advance()
		p.expect('(')
		args := []Expression{left, p.operand(depth + 1)}
		for p.take(',') && p.err == nil {
			args = append(args, p.operand(depth+1))
		}
		p.expect(')')
		result = p.node(operation(In, args...))
	default:
		if negate {
			p.fail("NOT predicate")
		}
		return left
	}
	if negate {
		return p.node(operation(Not, result))
	}
	return result
}

// A null operand may itself be a boolean predicate, with or without parentheses.
// Consume repeated suffixes iteratively after the complete predicate; applying
// them inside scalar operand parsing would turn n=1 IS NULL into n=(1 IS NULL).
func (p *textParser) nullSuffix(value Expression, depth int) Expression {
	for p.err == nil && p.keyword("IS") {
		depth++
		if !p.enter(depth) {
			return Expression{}
		}
		p.advance()
		negate := p.keyword("NOT")
		if negate {
			p.advance()
		}
		p.expectKeyword("NULL")
		if p.err != nil {
			return Expression{}
		}
		value = p.node(operation(IsNull, value))
		if negate {
			depth++
			if !p.enter(depth) {
				return Expression{}
			}
			value = p.node(operation(Not, value))
		}
	}
	return value
}

func (p *textParser) operand(depth int) Expression {
	if !p.enter(depth) {
		return Expression{}
	}
	current := p.token
	p.advance()
	switch current.kind {
	case 's':
		return p.node(literal(Literal{Type: String, Text: current.text}))
	case 'n':
		value, err := numberLiteral(current.text)
		if err != nil {
			p.err = err
		}
		return p.node(literal(value))
	case 'q':
		return p.node(Expression{Kind: Property, Property: current.text})
	case 'i':
		switch strings.ToUpper(current.text) {
		case "TRUE", "FALSE":
			return p.node(literal(Literal{Type: Boolean, Boolean: strings.EqualFold(current.text, "TRUE")}))
		case "NULL":
			p.fail("unexpected literal")
			return Expression{}
		}
		z := false
		if geometryName(current.text) != "" && p.keyword("Z") {
			z = true
			p.advance()
		}
		if p.token.kind != '(' {
			return p.node(Expression{Kind: Property, Property: current.text})
		}
		p.advance()
		switch strings.ToUpper(current.text) {
		case "DATE", "TIMESTAMP":
			if p.token.kind != 's' {
				p.fail("instant literal")
				return Expression{}
			}
			typ := Date
			if strings.EqualFold(current.text, "TIMESTAMP") {
				typ = Timestamp
			}
			value, err := instantLiteral(typ, p.token.text)
			if err != nil {
				p.err = err
			}
			p.advance()
			p.expect(')')
			return p.node(literal(value))
		case "INTERVAL":
			start := p.endpoint()
			p.expect(',')
			end := p.endpoint()
			p.expect(')')
			value, err := intervalLiteral(start, end)
			if err != nil {
				p.err = err
			}
			return p.node(literal(value))
		case "POINT", "LINESTRING", "POLYGON", "MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON", "GEOMETRYCOLLECTION", "BBOX":
			geometry := p.geometry(current.text, depth+1, z)
			return p.node(literal(Literal{Type: GeometryType, Geometry: &geometry}))
		default:
			op, ok := textOperator(current.text)
			if !ok {
				p.fail("unsupported function")
				return Expression{}
			}
			args := []Expression{p.operand(depth + 1)}
			for p.take(',') && p.err == nil {
				args = append(args, p.operand(depth+1))
			}
			p.expect(')')
			return p.node(operation(op, args...))
		}
	default:
		p.fail("expected operand")
		return Expression{}
	}
}

func (p *textParser) endpoint() *Literal {
	if p.token.kind != 's' {
		p.fail("literal interval endpoint")
		return nil
	}
	value, err := intervalEndpoint(p.token.text)
	if err != nil {
		p.err = err
	}
	p.advance()
	return value
}

func (p *textParser) enter(depth int) bool {
	if p.err != nil {
		return false
	}
	if depth > p.limits.MaxDepth {
		p.fail("text nesting")
		return false
	}
	return true
}

func (p *textParser) node(e Expression) Expression {
	p.nodes++
	if p.nodes > p.limits.MaxNodes {
		p.fail("text node count")
	}
	return e
}

func (p *textParser) advance() {
	if p.err != nil {
		return
	}
	for p.position < len(p.input) {
		r, size := utf8.DecodeRuneInString(p.input[p.position:])
		if !unicode.IsSpace(r) {
			break
		}
		p.position += size
	}
	start := p.position
	p.token = token{start: start}
	if start == len(p.input) {
		return
	}
	c := p.input[start]
	p.position++
	switch c {
	case '(', ')', ',':
		p.token.kind = c
	case '=', '<', '>':
		if p.position < len(p.input) && ((c == '<' && (p.input[p.position] == '>' || p.input[p.position] == '=')) || (c == '>' && p.input[p.position] == '=')) {
			p.position++
		}
		p.token = token{kind: 'o', text: p.input[start:p.position], start: start}
	case '\'', '"':
		var result strings.Builder
		closed := false
		for p.position < len(p.input) {
			b := p.input[p.position]
			p.position++
			if b == c {
				if p.position < len(p.input) && p.input[p.position] == c {
					result.WriteByte(c)
					p.position++
					continue
				}
				closed = true
				break
			}
			if b == '\\' && c == '\'' {
				if p.position == len(p.input) {
					p.fail("unterminated escape")
					return
				}
				escaped, ok := characterEscape(p.input[p.position])
				if !ok {
					p.fail("unsupported character escape")
					return
				}
				result.WriteByte(escaped)
				p.position++
				continue
			}
			if b == 0 {
				p.fail("string encoding")
				return
			}
			result.WriteByte(b)
		}
		if !closed {
			p.fail("unterminated quoted value")
			return
		}
		kind := byte('s')
		if c == '"' {
			kind = 'q'
			if !validIdentifier(result.String()) {
				p.fail("property identifier")
				return
			}
		}
		p.token = token{kind: kind, text: result.String(), start: start}
	default:
		if c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9') {
			for p.position < len(p.input) {
				x := p.input[p.position]
				if (x >= '0' && x <= '9') || x == '.' || x == 'e' || x == 'E' || ((x == '+' || x == '-') && (p.input[p.position-1] == 'e' || p.input[p.position-1] == 'E')) {
					p.position++
					continue
				}
				break
			}
			p.token = token{kind: 'n', text: p.input[start:p.position], start: start}
			return
		}
		r, size := utf8.DecodeRuneInString(p.input[start:])
		if !identifierStart(r) {
			p.fail("unexpected character")
			return
		}
		p.position = start + size
		for p.position < len(p.input) {
			r, size = utf8.DecodeRuneInString(p.input[p.position:])
			if !identifierPart(r) {
				break
			}
			p.position += size
		}
		p.token = token{kind: 'i', text: strings.Clone(p.input[start:p.position]), start: start}
	}
}

func (p *textParser) keyword(word string) bool {
	return p.token.kind == 'i' && strings.EqualFold(p.token.text, word)
}
func (p *textParser) take(kind byte) bool {
	if p.err != nil || p.token.kind != kind {
		return false
	}
	p.advance()
	return true
}
func (p *textParser) expect(kind byte) {
	if !p.take(kind) {
		p.fail("expected delimiter")
	}
}
func (p *textParser) expectKeyword(word string) {
	if !p.keyword(word) {
		p.fail("expected keyword")
		return
	}
	p.advance()
}
func (p *textParser) fail(reason string) {
	if p.err == nil {
		p.err = invalid(reason)
	}
}
func textOperator(name string) (Operator, bool) {
	for _, op := range []Operator{SIntersects, SEquals, SDisjoint, STouches, SWithin, SOverlaps, SCrosses, SContains, TAfter, TBefore, TContains, TDisjoint, TDuring, TEquals, TFinishedBy, TFinishes, TIntersects, TMeets, TMetBy, TOverlappedBy, TOverlaps, TStartedBy, TStarts} {
		if strings.EqualFold(name, string(op)) {
			return op, true
		}
	}
	return "", false
}
func validIdentifier(name string) bool {
	first := true
	for _, r := range name {
		if first {
			if !identifierStart(r) {
				return false
			}
			first = false
		} else if !identifierPart(r) {
			return false
		}
	}
	return !first
}
func identifierStart(r rune) bool {
	return r == ':' || r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= 0xC0 && r <= 0xD6) || (r >= 0xD8 && r <= 0xF6) || (r >= 0xF8 && r <= 0x2FF) || (r >= 0x370 && r <= 0x37D) || (r >= 0x37F && r <= 0x1FFE) || (r >= 0x200C && r <= 0x200D) || (r >= 0x2070 && r <= 0x218F) || (r >= 0x2C00 && r <= 0x2FEF) || (r >= 0x3001 && r <= 0xD7FF) || (r >= 0xF900 && r <= 0xFDCF) || (r >= 0xFDF0 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0xEFFFF)
}
func identifierPart(r rune) bool {
	return identifierStart(r) || r == '.' || (r >= '0' && r <= '9') || (r >= 0x300 && r <= 0x36F) || (r >= 0x203F && r <= 0x2040)
}

func comparisonOperator(text string) Operator {
	switch text {
	case "<>":
		return NotEqual
	case "<":
		return Less
	case "<=":
		return LessEqual
	case ">":
		return Greater
	case ">=":
		return GreaterEqual
	default:
		return Equal // The lexer only emits these six comparison tokens.
	}
}

// CQL2 Text /req/cql2-text/escaping defines these control escapes. A
// literal backslash is escaped too; decoded output is never unescaped again.
func characterEscape(c byte) (byte, bool) {
	switch c {
	case '\\', '\'':
		return c, true
	case 'a':
		return '\a', true
	case 'b':
		return '\b', true
	case 't':
		return '\t', true
	case 'n':
		return '\n', true
	case 'v':
		return '\v', true
	case 'f':
		return '\f', true
	case 'r':
		return '\r', true
	default:
		return 0, false
	}
}
