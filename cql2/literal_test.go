package cql2_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mapseekai/ogc-api/cql2"
)

func TestExactNumericNormalization(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"9007199254740993", "9007199254740993"}, {"18446744073709551616000", "18446744073709551616000"}, {"1e30", "1000000000000000000000000000000"}, {"1.25e-3", "0.00125"}, {"-0.000", "0"}, {"+000.12500", "0.125"}, {".5", "0.5"}, {"1.", "1"}, {"1e-20", "0.00000000000000000001"}, {"1e-1024", "1e-1024"}, {"1e1024", "1e1024"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			e, err := cql2.Parse("height="+tc.input, cql2.Text, profile())
			if err != nil {
				t.Fatal(err)
			}
			if got := string(e.Args[1].Literal.Number); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	for _, value := range []string{"1e1025", "1e-1025", "1e", "+", ".", "--1", "1.2.3"} {
		if _, err := cql2.Parse("height="+value, cql2.Text, profile()); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatal(value, err)
		}
	}
}

func TestLargeScaleNormalizationIsCompactAndEncodingIndependent(t *testing.T) {
	options := profile()
	a, err := cql2.Parse("height=1e80", cql2.Text, options)
	if err != nil {
		t.Fatal(err)
	}
	b, err := cql2.Parse(`{"op":"=","args":[{"property":"height"},1`+strings.Repeat("0", 80)+`]}`, cql2.JSON, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || string(a.Args[1].Literal.Number) != "1e80" {
		t.Fatal("large scale lost precision or canonical form")
	}
	// A short exponent must not allocate an expanded 1,000-character decimal.
	e, err := cql2.Parse(`S_INTERSECTS(geom,POINT(1e-1024 2e-1024))`, cql2.Text, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range e.Args[1].Literal.Geometry.Coordinates.Position {
		if len(n) > 16 {
			t.Fatal("number amplified input", len(n))
		}
	}
}

func TestTimestampStrictWireSyntaxAndPrecision(t *testing.T) {
	for _, stamp := range []string{
		"2024-01-01T00:00:00,1Z", "2024-01-01T00:00:00,1234567899Z",
		"2024-01-01T1:00:00Z", "2024-01-01T1:00:00.1Z", "2024-01-01T00:0:00Z", "2024-01-01T00:00:0Z",
		"2024-01-01T00:00:00.1234567899Z", "2024-01-01T00:00:00.Z", "2024-01-01T00:00:00.12345678aZ",
		"2024-01-01t00:00:00Z", "2024-01-01T00:00:00z", "2024-01-01T00:00:00+00:00", "2024-01-01T00:00:00Zx",
	} {
		t.Run(stamp, func(t *testing.T) {
			wire, err := json.Marshal(map[string]any{"op": "=", "args": []any{map[string]any{"property": "observed"}, map[string]any{"timestamp": stamp}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				input    string
				language cql2.Language
			}{{`observed=TIMESTAMP('` + stamp + `')`, cql2.Text}, {string(wire), cql2.JSON}} {
				if _, err := cql2.Parse(tc.input, tc.language, profile()); !errors.Is(err, cql2.ErrInvalid) {
					t.Fatal("non-CQL2 or lossy timestamp accepted", tc.language, err)
				}
			}
		})
	}
	for _, stamp := range []string{"2024-01-01T01:02:03Z", "2024-01-01T01:02:03.1Z", "2024-01-01T01:02:03.123456789Z"} {
		a, err := cql2.Parse(`observed=TIMESTAMP('`+stamp+`')`, cql2.Text, profile())
		if err != nil {
			t.Fatal(err)
		}
		if a.Args[1].Literal.Text != stamp {
			t.Fatal("valid instant lost precision", a.Args[1].Literal.Text, stamp)
		}
	}
	// These endpoints used to become equal after the library silently truncated
	// comma fractions. Invalid/reversed input must never produce a shortened AST.
	for _, endpoints := range [][2]string{
		{"2024-01-01T00:00:00,1234567899Z", "2024-01-01T00:00:00,1234567891Z"},
		{"2024-01-01T00:00:00.1234567899Z", "2024-01-01T00:00:00.1234567891Z"},
		{"2024-01-01T1:00:00Z", "2024-01-01T00:59:59Z"},
		{"2024-01-01T00:00:00.123456789Z", "2024-01-01T00:00:00.123456788Z"},
	} {
		wire, err := json.Marshal(map[string]any{"op": "t_intersects", "args": []any{map[string]any{"property": "observed"}, map[string]any{"interval": endpoints}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			input    string
			language cql2.Language
		}{{`T_INTERSECTS(observed,INTERVAL('` + endpoints[0] + `','` + endpoints[1] + `'))`, cql2.Text}, {string(wire), cql2.JSON}} {
			if _, err := cql2.Parse(tc.input, tc.language, profile()); !errors.Is(err, cql2.ErrInvalid) {
				t.Fatal("malformed or reversed interval accepted", tc.language, err)
			}
		}
	}
}

func TestTemporalLiteralValidation(t *testing.T) {
	for _, input := range []string{`day=DATE('2023-02-29')`, `day=DATE('2024-1-01')`, `observed=TIMESTAMP('2024-01-01T00:00:00+00:00')`, `observed=TIMESTAMP('2024-01-01T00:00:00.1234567891Z')`, `observed=TIMESTAMP('2024-01-01T00:00:60Z')`, `T_INTERSECTS(day,INTERVAL('2024-02-01','2024-01-01'))`, `T_INTERSECTS(day,INTERVAL('..','..'))`, `T_INTERSECTS(day,INTERVAL('2024-01-01','2024-02-01T00:00:00Z'))`, `T_INTERSECTS(day,INTERVAL(day,'2024-01-01'))`} {
		t.Run(input, func(t *testing.T) {
			if _, err := cql2.Parse(input, cql2.Text, profile()); !errors.Is(err, cql2.ErrInvalid) {
				t.Fatal("invalid temporal literal accepted", err)
			}
		})
	}
	for _, input := range []string{`T_INTERSECTS(day,INTERVAL('..','2024-01-01'))`, `T_INTERSECTS(observed,INTERVAL('2024-01-01T00:00:00Z','2024-01-01T00:00:00.5Z'))`} {
		if _, err := cql2.Parse(input, cql2.Text, profile()); err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzTimestampLiteral(f *testing.F) {
	for _, stamp := range []string{"2024-01-01T01:02:03Z", "2024-01-01T01:02:03.123456789Z", "2024-01-01T01:02:03,1234567899Z", "2024-01-01T1:02:03Z", "2024-02-30T01:02:03Z"} {
		f.Add(stamp)
	}
	// This independent wire-grammar oracle deliberately does not use parser helpers.
	grammar := regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$`)
	f.Fuzz(func(t *testing.T, stamp string) {
		if !utf8.ValidString(stamp) || len(stamp) > 1024 {
			return
		}
		parsed, calendarErr := time.Parse(time.RFC3339Nano, stamp)
		valid := grammar.MatchString(stamp) && calendarErr == nil
		wire, err := json.Marshal(map[string]any{"op": "=", "args": []any{map[string]any{"property": "observed"}, map[string]any{"timestamp": stamp}}})
		if err != nil {
			t.Fatal(err)
		}
		a, ae := cql2.Parse("observed=TIMESTAMP("+textCharacterLiteral(stamp)+")", cql2.Text, profile())
		b, be := cql2.Parse(string(wire), cql2.JSON, profile())
		if !valid {
			if !errors.Is(ae, cql2.ErrInvalid) || !errors.Is(be, cql2.ErrInvalid) {
				t.Fatal("invalid timestamp accepted", ae, be)
			}
			return
		}
		if ae != nil || be != nil || !reflect.DeepEqual(a, b) || a.Args[1].Literal.Text != parsed.Format(time.RFC3339Nano) {
			t.Fatal("valid timestamp lost lexical or precision semantics", ae, be)
		}
	})
}
