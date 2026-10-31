package cql2_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mapseekai/ogc-api/cql2"
)

func TestTextCharacterEscapeParity(t *testing.T) {
	for _, tc := range []struct{ wire, want string }{
		{`\a`, "\a"}, {`\b`, "\b"}, {`\t`, "\t"}, {`\n`, "\n"}, {`\v`, "\v"}, {`\f`, "\f"}, {`\r`, "\r"},
		{`O''Brien`, "O'Brien"}, {`O\'Brien`, "O'Brien"}, {`a\\`, "a\\"},
		{`a\\\'b`, "a\\'b"}, {`a\\\\b`, "a\\\\b"}, {`a\\n`, `a\n`}, {`a\\\n`, "a\\\n"},
		{`a''\\b`, "a'\\b"}, {`雪😀`, "雪😀"}, {`"quoted"`, `"quoted"`},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			want := "first" + tc.want + "second"
			wire, err := json.Marshal(map[string]any{"op": "=", "args": []any{map[string]any{"property": "name"}, want}})
			if err != nil {
				t.Fatal(err)
			}
			a, err := cql2.Parse(`name='first`+tc.wire+`second'`, cql2.Text, profile())
			if err != nil {
				t.Fatal(err)
			}
			b, err := cql2.Parse(string(wire), cql2.JSON, profile())
			if err != nil {
				t.Fatal(err)
			}
			if a.Args[1].Literal.Text != want || !reflect.DeepEqual(a, b) {
				t.Fatalf("escape changed value: text=%q JSON=%q want=%q", a.Args[1].Literal.Text, b.Args[1].Literal.Text, want)
			}
		})
	}
	// Backslash is consumed once, not recursively applied to its decoded output.
	for _, input := range []string{`name='bad\q'`, `name='bad\u0041'`, `name='bad\'`, `name='bad\`} {
		if _, err := cql2.Parse(input, cql2.Text, profile()); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatal("invalid escape accepted", input, err)
		}
	}
}

func TestTextLikePatternBackslashParity(t *testing.T) {
	text := `name LIKE 'A\\_%\\%B'`
	want := `A\_%\%B`
	wire, err := json.Marshal(map[string]any{"op": "like", "args": []any{map[string]any{"property": "name"}, want}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := cql2.Parse(text, cql2.Text, profile())
	if err != nil {
		t.Fatal(err)
	}
	b, err := cql2.Parse(string(wire), cql2.JSON, profile())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || a.Args[1].Literal.Text != want {
		t.Fatal("LIKE pattern was interpreted or double-decoded")
	}
}

func TestTextBooleanNullOperandParityAndPrecedence(t *testing.T) {
	cases := []struct{ text, json string }{
		{`(population=1) IS NULL`, `{"op":"isNull","args":[{"op":"=","args":[{"property":"population"},1]}]}`},
		{`(population=1) IS NOT NULL`, `{"op":"not","args":[{"op":"isNull","args":[{"op":"=","args":[{"property":"population"},1]}]}]}`},
		{`(population=1 AND population>0) IS NULL OR active=true AND (name='x') IS NOT NULL`, `{"op":"or","args":[{"op":"isNull","args":[{"op":"and","args":[{"op":"=","args":[{"property":"population"},1]},{"op":">","args":[{"property":"population"},0]}]}]},{"op":"and","args":[{"op":"=","args":[{"property":"active"},true]},{"op":"not","args":[{"op":"isNull","args":[{"op":"=","args":[{"property":"name"},"x"]}]}]}]}]}`},
		{`(NOT (population=1) OR active=false) IS NOT NULL`, `{"op":"not","args":[{"op":"isNull","args":[{"op":"or","args":[{"op":"not","args":[{"op":"=","args":[{"property":"population"},1]}]},{"op":"=","args":[{"property":"active"},false]}]}]}]}`},
		{`NOT (population=1) IS NULL`, `{"op":"not","args":[{"op":"isNull","args":[{"op":"=","args":[{"property":"population"},1]}]}]}`},
	}
	for _, enabled := range []bool{false, true} {
		o := profile()
		o.PropertyComparisons = enabled
		for _, tc := range cases {
			t.Run(tc.text+"_"+map[bool]string{false: "basic", true: "property"}[enabled], func(t *testing.T) {
				a, err := cql2.Parse(tc.text, cql2.Text, o)
				if err != nil {
					t.Fatal(err)
				}
				b, err := cql2.Parse(tc.json, cql2.JSON, o)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(a, b) {
					t.Fatal("different null operand ASTs", a, b)
				}
			})
		}
		// A null wrapper must not bypass a missing declared extension or the
		// property-comparison policy of any nested predicate.
		o.Operators = nil
		for _, tc := range []struct{ text, json string }{
			{`(name LIKE 'x') IS NULL`, `{"op":"isNull","args":[{"op":"like","args":[{"property":"name"},"x"]}]}`},
		} {
			for _, tc := range []struct {
				input    string
				language cql2.Language
			}{{tc.text, cql2.Text}, {tc.json, cql2.JSON}} {
				if _, err := cql2.Parse(tc.input, tc.language, o); !errors.Is(err, cql2.ErrInvalid) {
					t.Fatal("null wrapper enabled an undeclared extension", err)
				}
			}
		}
	}

	for _, enabled := range []bool{false, true} {
		o := profile()
		o.PropertyComparisons = enabled
		a, ae := cql2.Parse(`(population=population) IS NULL`, cql2.Text, o)
		b, be := cql2.Parse(`{"op":"isNull","args":[{"op":"=","args":[{"property":"population"},{"property":"population"}]}]}`, cql2.JSON, o)
		if enabled {
			if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
				t.Fatal("declared property comparison rejected", ae, be)
			}
		} else if !errors.Is(ae, cql2.ErrInvalid) || !errors.Is(be, cql2.ErrInvalid) {
			t.Fatal("null wrapper enabled undeclared property comparison", ae, be)
		}
	}
	for _, input := range []string{`(1) IS NULL`, `(name) IS NULL`, `(population=1) = true`} {
		if _, err := cql2.Parse(input, cql2.Text, profile()); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatal("invalid parenthesized operand accepted", input, err)
		}
	}
}

func TestTextNullSuffixesFollowCompletePredicates(t *testing.T) {
	op := func(name string, args ...any) any { return map[string]any{"op": name, "args": args} }
	prop := func(name string) any { return map[string]any{"property": name} }
	eq := op("=", prop("population"), 1)
	cases := []struct {
		text string
		wire any
	}{
		{`population=1 IS NULL`, op("isNull", eq)},
		{`population=1 IS NOT NULL`, op("not", op("isNull", eq))},
		{`(population=1) IS NULL IS NULL`, op("isNull", op("isNull", eq))},
		{`population=1 IS NULL IS NOT NULL`, op("not", op("isNull", op("isNull", eq)))},
		{`NOT population=1 IS NULL IS NULL`, op("not", op("isNull", op("isNull", eq)))},
		{`(NOT population=1 IS NULL) IS NULL`, op("isNull", op("not", op("isNull", eq)))},
		{`population=1 IS NULL AND population=2 OR population=3`, op("or", op("and", op("isNull", eq), op("=", prop("population"), 2)), op("=", prop("population"), 3))},
		{`population=1 OR population=2 IS NULL`, op("or", eq, op("isNull", op("=", prop("population"), 2)))},
		{`(population=1 OR population=2) IS NULL IS NULL`, op("isNull", op("isNull", op("or", eq, op("=", prop("population"), 2))))},
		{`population BETWEEN 0 AND 2 IS NULL AND active=true`, op("and", op("isNull", op("between", prop("population"), 0, 2)), op("=", prop("active"), true))},
		{`population NOT BETWEEN 0 AND 2 IS NULL`, op("isNull", op("not", op("between", prop("population"), 0, 2)))},
		{`population IN (1,2) IS NULL`, op("isNull", op("in", prop("population"), []any{1, 2}))},
		{`population NOT IN (1,2) IS NOT NULL IS NULL`, op("isNull", op("not", op("isNull", op("not", op("in", prop("population"), []any{1, 2})))))},
		{`name LIKE 'x' IS NULL IS NULL`, op("isNull", op("isNull", op("like", prop("name"), "x")))},
		{`name IS NOT NULL IS NULL`, op("isNull", op("not", op("isNull", prop("name"))))},
		{`true IS NULL IS NULL`, op("isNull", op("isNull", true))},
		{`S_INTERSECTS(geom,POINT(1 2)) IS NULL IS NULL`, op("isNull", op("isNull", op("s_intersects", prop("geom"), map[string]any{"type": "Point", "coordinates": []any{1, 2}})))},
	}
	for _, enabled := range []bool{false, true} {
		options := profile()
		options.PropertyComparisons = enabled
		for _, tc := range cases {
			t.Run(tc.text+"_"+map[bool]string{false: "basic", true: "property"}[enabled], func(t *testing.T) {
				wire, err := json.Marshal(tc.wire)
				if err != nil {
					t.Fatal(err)
				}
				a, ae := cql2.Parse(tc.text, cql2.Text, options)
				b, be := cql2.Parse(string(wire), cql2.JSON, options)
				if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
					t.Fatal("null suffix changed predicate or precedence", ae, be)
				}
			})
		}
	}
}

func TestTextNullSuffixesPreserveCapabilitiesAndBudgets(t *testing.T) {
	op := func(name string, args ...any) any { return map[string]any{"op": name, "args": args} }
	prop := func(name string) any { return map[string]any{"property": name} }
	for _, enabled := range []bool{false, true} {
		options := profile()
		options.Operators = nil
		options.PropertyComparisons = enabled
		for _, tc := range []struct {
			text string
			wire any
		}{
			{`name LIKE 'x' IS NULL IS NULL`, op("isNull", op("isNull", op("like", prop("name"), "x")))},
			{`population=unknown IS NULL`, op("isNull", op("=", prop("population"), prop("unknown")))},
			{`population='x' IS NULL`, op("isNull", op("=", prop("population"), "x"))},
			{`(population=1)=true IS NULL`, op("isNull", op("=", op("=", prop("population"), 1), true))},
		} {
			wire, err := json.Marshal(tc.wire)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []struct {
				source   string
				language cql2.Language
			}{{tc.text, cql2.Text}, {string(wire), cql2.JSON}} {
				if _, err := cql2.Parse(input.source, input.language, options); !errors.Is(err, cql2.ErrInvalid) {
					t.Fatal("null suffix bypassed validation", input.source, err)
				}
			}
		}
		wire, _ := json.Marshal(op("isNull", op("=", prop("population"), prop("population"))))
		a, ae := cql2.Parse(`population=population IS NULL`, cql2.Text, options)
		b, be := cql2.Parse(string(wire), cql2.JSON, options)
		if enabled {
			if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
				t.Fatal("declared property comparison rejected", ae, be)
			}
		} else if !errors.Is(ae, cql2.ErrInvalid) || !errors.Is(be, cql2.ErrInvalid) {
			t.Fatal("null suffix enabled property comparison", ae, be)
		}
	}
	for _, input := range []string{`population=1 IS`, `population=1 IS NOT`, `population=1 IS NULL IS`, `population=1 IS NULL IS NOT true`, `population=1 IS NULL = true`, `(population=1) LIKE 'x' IS NULL`} {
		if _, err := cql2.Parse(input, cql2.Text, profile()); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatal("malformed null suffix accepted", input, err)
		}
	}
	options := profile()
	options.Limits = cql2.Limits{MaxNodes: 4}
	if _, err := cql2.Parse(`population=1 IS NULL`, cql2.Text, options); err != nil {
		t.Fatal("baseline bounded suffix rejected", err)
	}
	for _, input := range []string{`population=1 IS NULL IS NULL`, `population=1 IS NOT NULL`} {
		if _, err := cql2.Parse(input, cql2.Text, options); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatal("suffix nodes not budgeted", err)
		}
	}
	for _, negate := range []bool{false, true} {
		word := " IS NULL"
		if negate {
			word = " IS NOT NULL"
		}
		text := `population=1` + strings.Repeat(word, 1000)
		var tree any = op("=", prop("population"), 1)
		for range 1000 {
			tree = op("isNull", tree)
			if negate {
				tree = op("not", tree)
			}
		}
		wire, err := json.Marshal(tree)
		if err != nil {
			t.Fatal(err)
		}
		for _, limits := range []cql2.Limits{{}, {MaxDepth: 12}, {MaxNodes: 20}} {
			options := profile()
			options.Limits = limits
			for _, input := range []struct {
				source   string
				language cql2.Language
			}{{text, cql2.Text}, {string(wire), cql2.JSON}} {
				if _, err := cql2.Parse(input.source, input.language, options); !errors.Is(err, cql2.ErrInvalid) {
					t.Fatal("unbounded null chain accepted", input.language, limits, err)
				}
			}
		}
	}
}

func FuzzTextNullSuffixParity(f *testing.F) {
	f.Add([]byte{}, false, false)
	f.Add([]byte{0}, false, false)
	f.Add([]byte{0, 1, 0}, true, false)
	f.Add([]byte{1, 1, 0}, false, true)
	f.Fuzz(func(t *testing.T, pattern []byte, prefixNot, grouped bool) {
		if len(pattern) > 12 {
			return
		}
		op := func(name string, args ...any) any { return map[string]any{"op": name, "args": args} }
		var tree any = op("=", map[string]any{"property": "population"}, 1)
		text := `population=1`
		if grouped {
			text = "(" + text + ")"
		}
		for _, b := range pattern {
			tree = op("isNull", tree)
			if b&1 == 1 {
				text += " IS NOT NULL"
				tree = op("not", tree)
			} else {
				text += " IS NULL"
			}
		}
		if prefixNot {
			text = "NOT " + text
			tree = op("not", tree)
		}
		wire, err := json.Marshal(tree)
		if err != nil {
			t.Fatal(err)
		}
		a, ae := cql2.Parse(text, cql2.Text, profile())
		b, be := cql2.Parse(string(wire), cql2.JSON, profile())
		if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
			t.Fatal("unparenthesized null chain differs between encodings", ae, be)
		}
	})
}

func FuzzTextCharacterEscapes(f *testing.F) {
	for _, value := range []string{"simple", "O'Brien", `literal\n`, "line\nnext", string([]byte{7, 8, 9, 10, 11, 12, 13}), `\\'雪😀`} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) || len(value) > 8<<10 {
			return
		}
		for _, r := range value {
			if r < 7 || (r > 13 && r < 32) {
				return
			}
		}
		wire, err := json.Marshal(map[string]any{"op": "=", "args": []any{map[string]any{"property": "name"}, value}})
		if err != nil {
			t.Fatal(err)
		}
		a, ae := cql2.Parse("name="+textCharacterLiteral(value), cql2.Text, profile())
		b, be := cql2.Parse(string(wire), cql2.JSON, profile())
		if ae != nil || be != nil || !reflect.DeepEqual(a, b) || a.Args[1].Literal.Text != value {
			t.Fatal("character literal encoding changed the predicate", ae, be)
		}
	})
}

func textCharacterLiteral(value string) string {
	var out strings.Builder
	out.WriteByte('\'')
	for _, r := range value {
		switch r {
		case '\\':
			out.WriteString(`\\`)
		case '\'':
			out.WriteString(`''`)
		case 7:
			out.WriteString(`\a`)
		case 8:
			out.WriteString(`\b`)
		case 9:
			out.WriteString(`\t`)
		case 10:
			out.WriteString(`\n`)
		case 11:
			out.WriteString(`\v`)
		case 12:
			out.WriteString(`\f`)
		case 13:
			out.WriteString(`\r`)
		default:
			out.WriteRune(r)
		}
	}
	out.WriteByte('\'')
	return out.String()
}
