package cql2_test

import (
	"errors"
	"testing"

	"github.com/mapseekai/ogc-api/cql2"
)

func TestJSONRejectsMalformedAndUnsupportedValues(t *testing.T) {
	cases := []string{
		`{`, `[]`, `null`, `"name"`, `123`, `true false`, `{"op":"=","args":[{"property":"name"},"a"],"op":"<>"}`,
		`{"op":"=","args":[{"property":"name","property":"population"},"a"]}`,
		`{"op":"=","args":[{"property":"name"},"a"],"extra":true}`,
		`{"op":"=","args":[{"property":"name","extra":true},"a"]}`,
		`{"op":"=","args":[{"property":"name"},null]}`,
		`{"op":"=","args":[{"property":"name"},"\ud800"]}`,
		`{"op":"=","args":[{"property":"name"},"\udc00"]}`,
		`{"op":"=","args":[{"property":"name"},"\ud800\u0041"]}`,
		`{"op":"=","args":[{"property":"population"},01]}`,
		`{"op":"=","args":[{"property":"population"},NaN]}`,
		`{"op":"=","args":[{"property":"population"},1e999999999]}`,
		`{"op":"=","args":[{"property":"name"}]}`,
		`{"op":"=","args":"name"}`, `{"op":"=","args":[{"property":1},"x"]}`,
		`{"op":"IN","args":[{"property":"population"},[1]]}`,
		`{"op":"in","args":[{"property":"population"},[]]}`,
		`{"op":"in","args":[{"property":"population"},1,2]}`,
		`{"op":"not","args":[]}`, `{"op":"and","args":[true]}`,
		`{"op":"and","args":[{"property":"active"},true]}`,
		`{"op":"+","args":[1,2]}`, `{"op":"casei","args":[{"property":"name"}]}`,
		`{"op":"=","args":[{"property":"day"},{"date":"2024-01-01","extra":1}]}`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if _, err := cql2.Parse(input, cql2.JSON, profile()); !errors.Is(err, cql2.ErrInvalid) {
				t.Fatal("accepted malformed input", err)
			}
		})
	}
}

func TestJSONPreservesStringValues(t *testing.T) {
	input := `{"op":"=","args":[{"property":"name"},"back\\slash\nline\t\"quoted\" 雪\ud83d\ude00"]}`
	value, err := cql2.Parse(input, cql2.JSON, profile())
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Args[1].Literal.Text; got != "back\\slash\nline\t\"quoted\" 雪😀" {
		t.Fatalf("changed literal %q", got)
	}
}
