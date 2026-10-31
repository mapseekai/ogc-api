package cql2_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/mapseekai/ogc-api/cql2"
)

func TestEncodingParity(t *testing.T) {
	cases := []struct{ name, text, json string }{
		{"precedence", `name = 'main' OR population >= 9007199254740993 AND NOT active = false`, `{"op":"or","args":[{"op":"=","args":[{"property":"name"},"main"]},{"op":"and","args":[{"op":">=","args":[{"property":"population"},9007199254740993]},{"op":"not","args":[{"op":"=","args":[{"property":"active"},false]}]}]}]}`},
		{"unicode_escapes", `"名称" = 'O''Brien\'s 路😀'`, `{"op":"=","args":[{"property":"名称"},"O'Brien's 路\ud83d\ude00"]}`},
		{"exact_decimal", `population = +09007199254740993.000e+0`, `{"op":"=","args":[{"property":"population"},9007199254740993]}`},
		{"decimal", `height >= -.125e+2`, `{"op":">=","args":[{"property":"height"},-12.5]}`},
		{"null", `name IS NOT NULL`, `{"op":"not","args":[{"op":"isNull","args":[{"property":"name"}]}]}`},
		{"advanced", `population BETWEEN 1 AND 10 AND name NOT LIKE 'bad_%' AND population IN (1, 2, 3)`, `{"op":"and","args":[{"op":"between","args":[{"property":"population"},1,10]},{"op":"not","args":[{"op":"like","args":[{"property":"name"},"bad_%"]}]},{"op":"in","args":[{"property":"population"},[1,2,3]]}]}`},
		{"date", `day >= DATE('2024-02-29')`, `{"op":">=","args":[{"property":"day"},{"date":"2024-02-29"}]}`},
		{"timestamp", `observed = TIMESTAMP('2024-02-29T12:34:56.1200Z')`, `{"op":"=","args":[{"property":"observed"},{"timestamp":"2024-02-29T12:34:56.12Z"}]}`},
		{"point", `S_INTERSECTS(geom,POINT(12.50 35))`, `{"op":"s_intersects","args":[{"property":"geom"},{"type":"Point","coordinates":[12.5,35]}]}`},
		{"bbox", `S_INTERSECTS(geom,BBOX(170,-10,-170,10))`, `{"op":"s_intersects","args":[{"property":"geom"},{"bbox":[170,-10,-170,10]}]}`},
		{"polygon", `S_WITHIN(geom,POLYGON((0 0,1 0,1 1,0 0)))`, `{"op":"s_within","args":[{"property":"geom"},{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}]}`},
		{"interval", `T_INTERSECTS(observed,INTERVAL('2024-01-01T00:00:00Z','..'))`, `{"op":"t_intersects","args":[{"property":"observed"},{"interval":["2024-01-01T00:00:00Z",".."]}]}`},
		{"boolean", `TRUE OR FALSE`, `{"op":"or","args":[true,false]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, err := cql2.Parse(tc.text, cql2.Text, profile())
			if err != nil {
				t.Fatal(err)
			}
			json, err := cql2.Parse(tc.json, cql2.JSON, profile())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(text, json) {
				t.Fatalf("different ASTs:\n%#v\n%#v", text, json)
			}
		})
	}
}

func TestProfileAndTypeValidation(t *testing.T) {
	cases := []string{
		`unknown = 1`, `population = '1'`, `active = 'true'`, `day = '2024-01-01'`, `day = TIMESTAMP('2024-01-01T00:00:00Z')`,
		`population = population`, `1 = population`, `name LIKE 2`, `population BETWEEN 1 AND '2'`, `population IN (1, '2')`, `name IN ()`,
		`S_INTERSECTS(name,POINT(1 2))`, `T_INTERSECTS(name,DATE('2024-01-01'))`, `T_INTERSECTS(day,TIMESTAMP('2024-01-01T00:00:00Z'))`,
		`CASEI(name)='a'`, `population+1 = 2`, `A_EQUALS(name,('a'))`, `S_DWITHIN(geom,POINT(1 2),3)`, `name='x';drop table t`, `active`, `NULL`, `name=NULL`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := cql2.Parse(input, cql2.Text, profile())
			if !errors.Is(err, cql2.ErrInvalid) {
				t.Fatalf("accepted or unclassified error: %v", err)
			}
			if strings.Contains(err.Error(), input) {
				t.Fatal("error echoes input")
			}
		})
	}
	basic := profile()
	basic.Operators = nil
	for _, input := range []string{`name LIKE 'x'`, `population IN (1)`, `S_INTERSECTS(geom,POINT(1 2))`, `T_BEFORE(day,DATE('2024-01-01'))`} {
		if _, err := cql2.Parse(input, cql2.Text, basic); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatalf("extension enabled implicitly: %v", err)
		}
	}
	if _, err := cql2.Parse(`population=1`, "sql", profile()); !errors.Is(err, cql2.ErrInvalid) {
		t.Fatal(err)
	}
	property := profile()
	property.PropertyComparisons = true
	for _, input := range []string{`population=population`, `9007199254740993=population`} {
		if _, err := cql2.Parse(input, cql2.Text, property); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAllDeclaredSpatialTemporalOperators(t *testing.T) {
	spatial := []cql2.Operator{cql2.SIntersects, cql2.SEquals, cql2.SDisjoint, cql2.STouches, cql2.SWithin, cql2.SOverlaps, cql2.SCrosses, cql2.SContains}
	temporal := []cql2.Operator{cql2.TAfter, cql2.TBefore, cql2.TContains, cql2.TDisjoint, cql2.TDuring, cql2.TEquals, cql2.TFinishedBy, cql2.TFinishes, cql2.TIntersects, cql2.TMeets, cql2.TMetBy, cql2.TOverlappedBy, cql2.TOverlaps, cql2.TStartedBy, cql2.TStarts}
	options := profile()
	options.Operators = append(spatial, temporal...)
	options.Properties["period"] = cql2.IntervalType
	for _, op := range options.Operators {
		t.Run(string(op), func(t *testing.T) {
			text := strings.ToUpper(string(op)) + `(geom,POINT(1 2))`
			input := `{"op":"` + string(op) + `","args":[{"property":"geom"},{"type":"Point","coordinates":[1,2]}]}`
			if strings.HasPrefix(string(op), "t_") {
				text = strings.ToUpper(string(op)) + `(observed,TIMESTAMP('2024-01-01T00:00:00Z'))`
				input = `{"op":"` + string(op) + `","args":[{"property":"observed"},{"timestamp":"2024-01-01T00:00:00Z"}]}`
			}
			if intervalOnly(op) {
				text = strings.ToUpper(string(op)) + `(period,INTERVAL('2024-01-01T00:00:00Z','2024-02-01T00:00:00Z'))`
				input = `{"op":"` + string(op) + `","args":[{"property":"period"},{"interval":["2024-01-01T00:00:00Z","2024-02-01T00:00:00Z"]}]}`
			}
			a, err := cql2.Parse(text, cql2.Text, options)
			if err != nil {
				t.Fatal(err)
			}
			b, err := cql2.Parse(input, cql2.JSON, options)
			if err != nil {
				t.Fatal(err)
			}
			if a.Operator != op || !reflect.DeepEqual(a, b) {
				t.Fatal("operator normalization failed", a, b)
			}
		})
	}
}

func TestTemporalIntervalOnlySignatures(t *testing.T) {
	options := profile()
	options.Properties["period"] = cql2.IntervalType
	for _, op := range []cql2.Operator{cql2.TContains, cql2.TDuring, cql2.TFinishedBy, cql2.TFinishes, cql2.TMeets, cql2.TMetBy, cql2.TOverlappedBy, cql2.TOverlaps, cql2.TStartedBy, cql2.TStarts} {
		options.Operators = []cql2.Operator{op}
		for _, tc := range []struct {
			left, right         string
			leftJSON, rightJSON any
			valid               bool
		}{
			{`period`, `INTERVAL('2024-01-01T00:00:00Z','2024-02-01T00:00:00Z')`, map[string]any{"property": "period"}, map[string]any{"interval": []string{"2024-01-01T00:00:00Z", "2024-02-01T00:00:00Z"}}, true},
			{`INTERVAL('2024-01-01','2024-01-02')`, `INTERVAL('2023-01-01','2025-01-01')`, map[string]any{"interval": []string{"2024-01-01", "2024-01-02"}}, map[string]any{"interval": []string{"2023-01-01", "2025-01-01"}}, true},
			{`observed`, `TIMESTAMP('2024-01-01T00:00:00Z')`, map[string]any{"property": "observed"}, map[string]any{"timestamp": "2024-01-01T00:00:00Z"}, false},
			{`day`, `DATE('2024-01-01')`, map[string]any{"property": "day"}, map[string]any{"date": "2024-01-01"}, false},
			{`observed`, `INTERVAL('2024-01-01T00:00:00Z','2024-02-01T00:00:00Z')`, map[string]any{"property": "observed"}, map[string]any{"interval": []string{"2024-01-01T00:00:00Z", "2024-02-01T00:00:00Z"}}, false},
			{`period`, `TIMESTAMP('2024-01-01T00:00:00Z')`, map[string]any{"property": "period"}, map[string]any{"timestamp": "2024-01-01T00:00:00Z"}, false},
		} {
			t.Run(string(op)+tc.left+tc.right, func(t *testing.T) {
				text := strings.ToUpper(string(op)) + "(" + tc.left + "," + tc.right + ")"
				wire, err := json.Marshal(map[string]any{"op": op, "args": []any{tc.leftJSON, tc.rightJSON}})
				if err != nil {
					t.Fatal(err)
				}
				a, ae := cql2.Parse(text, cql2.Text, options)
				b, be := cql2.Parse(string(wire), cql2.JSON, options)
				if tc.valid {
					if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
						t.Fatal("valid interval relation rejected or encoding mismatch", ae, be)
					}
				} else if !errors.Is(ae, cql2.ErrInvalid) || !errors.Is(be, cql2.ErrInvalid) {
					t.Fatal("instant accepted for interval-only relation", ae, be)
				}
			})
		}
	}
	// The other five functions expressly allow mixed interval/instant operands.
	for _, op := range []cql2.Operator{cql2.TAfter, cql2.TBefore, cql2.TDisjoint, cql2.TEquals, cql2.TIntersects} {
		options.Operators = []cql2.Operator{op}
		for _, input := range []string{strings.ToUpper(string(op)) + `(observed,TIMESTAMP('2024-01-01T00:00:00Z'))`, strings.ToUpper(string(op)) + `(period,TIMESTAMP('2024-01-01T00:00:00Z'))`, strings.ToUpper(string(op)) + `(observed,INTERVAL('2024-01-01T00:00:00Z','2024-02-01T00:00:00Z'))`} {
			if _, err := cql2.Parse(input, cql2.Text, options); err != nil {
				t.Fatal("mixed-capable temporal function restricted", input, err)
			}
		}
	}
}

func TestOwnedRetainedASTAndConcurrency(t *testing.T) {
	options := profile()
	first, err := cql2.Parse(`name = 'retained' AND S_INTERSECTS(geom,POINT(1 2))`, cql2.Text, options)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				next, err := cql2.Parse(`name = 'different' AND S_INTERSECTS(geom,POINT(3 4))`, cql2.Text, options)
				if err != nil {
					t.Error(err)
					return
				}
				next.Args[0].Args[1].Literal.Text = "mutation"
				next.Args[1].Args[1].Literal.Geometry.Coordinates.Position[0] = "99"
			}
		}()
	}
	wg.Wait()
	delete(options.Properties, "name")
	options.Operators[0] = cql2.Not
	after, err := json.Marshal(first)
	if err != nil || string(snapshot) != string(after) {
		t.Fatal("retained AST was changed", err)
	}
}

func TestComplexityBudgets(t *testing.T) {
	for _, language := range []cql2.Language{cql2.Text, cql2.JSON} {
		for _, input := range []string{"", strings.Repeat("x", (64<<10)+1), string([]byte{0xff})} {
			if _, err := cql2.Parse(input, language, profile()); !errors.Is(err, cql2.ErrInvalid) {
				t.Fatal("accepted invalid input", err)
			}
		}
	}
	cases := []struct {
		language cql2.Language
		input    string
	}{
		{cql2.Text, strings.Repeat("(", 1000) + "TRUE" + strings.Repeat(")", 1000)},
		{cql2.Text, strings.Repeat("NOT ", 1000) + "TRUE"},
		{cql2.Text, strings.Repeat("name='x' OR ", 2000) + "FALSE"},
		{cql2.JSON, strings.Repeat(`{"op":"not","args":[`, 1000) + `true` + strings.Repeat(`]}`, 1000)},
		{cql2.JSON, `{"op":"in","args":[{"property":"population"},[` + strings.Repeat("1,", 5000) + "1]]}"},
	}
	for i, tc := range cases {
		if _, err := cql2.Parse(tc.input, tc.language, profile()); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	for _, limits := range []cql2.Limits{{MaxBytes: -1}, {MaxDepth: 65}, {MaxNodes: 5000}, {MaxBytes: 65537}} {
		o := profile()
		o.Limits = limits
		if err := o.Validate(); !errors.Is(err, cql2.ErrInvalid) {
			t.Fatal(err)
		}
	}
	o := profile()
	o.Limits = cql2.Limits{MaxNodes: 2}
	if _, err := cql2.Parse(`name='x'`, cql2.Text, o); !errors.Is(err, cql2.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestBorrowedInputDetached(t *testing.T) {
	for _, tc := range []struct {
		language cql2.Language
		input    string
	}{
		{cql2.Text, `name='retained' AND height=123.45 AND S_INTERSECTS(geom,POINT(1 2))`},
		{cql2.JSON, `{"op":"and","args":[{"op":"=","args":[{"property":"name"},"retained"]},{"op":"=","args":[{"property":"height"},123.45]},{"op":"s_intersects","args":[{"property":"geom"},{"type":"Point","coordinates":[1,2]}]}]}`},
	} {
		t.Run(string(tc.language), func(t *testing.T) {
			buffer := []byte(tc.input)
			// Simulate a transport string borrowed from a reusable request buffer.
			input := unsafe.String(unsafe.SliceData(buffer), len(buffer))
			e, err := cql2.Parse(input, tc.language, profile())
			if err != nil {
				t.Fatal(err)
			}
			before, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			for i := range buffer {
				buffer[i] = 'x'
			}
			after, err := json.Marshal(e)
			if err != nil || string(before) != string(after) {
				t.Fatal("AST retained transport bytes", err, string(before), string(after))
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	for _, input := range []string{`name='a'`, `{"op":"=","args":[{"property":"name"},"a"]}`, `S_INTERSECTS(geom,POINT(1 2))`, `day IS NULL`, `population>=9007199254740993`, `{"op":"in","args":[{"property":"population"},[1,2,3]]}`, `T_INTERSECTS(day,INTERVAL('2024-01-01','..'))`} {
		f.Add(input)
	}
	options := profile()
	f.Fuzz(func(t *testing.T, input string) {
		for _, language := range []cql2.Language{cql2.Text, cql2.JSON} {
			e, err := cql2.Parse(input, language, options)
			if err != nil {
				if !errors.Is(err, cql2.ErrInvalid) {
					t.Fatal(err)
				}
				continue
			}
			if e.Kind != cql2.Operation && (e.Literal == nil || e.Literal.Type != cql2.Boolean) {
				t.Fatal("non-predicate result")
			}
			if _, err := json.Marshal(e); err != nil {
				t.Fatal("unencodable AST", err)
			}
		}
	})
}

func BenchmarkParse(b *testing.B) {
	options := profile()
	cases := []struct {
		name     string
		language cql2.Language
		input    string
	}{
		{"ordinary_text", cql2.Text, `name='main' AND population>=9007199254740993`},
		{"ordinary_json", cql2.JSON, `{"op":"and","args":[{"op":"=","args":[{"property":"name"},"main"]},{"op":">=","args":[{"property":"population"},9007199254740993]}]}`},
		{"complex", cql2.Text, `NOT (name LIKE 'bad_%' OR population BETWEEN 1 AND 10) AND S_INTERSECTS(geom,POLYGON((0 0,1 0,1 1,0 0))) AND T_INTERSECTS(observed,INTERVAL('2024-01-01T00:00:00Z','..'))`},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := cql2.Parse(tc.input, tc.language, options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := cql2.Parse(cases[2].input, cql2.Text, options); err != nil {
					b.Error(err)
					return
				}
			}
		})
	})
}

func profile() cql2.Options {
	return cql2.Options{Properties: map[string]cql2.Type{"name": cql2.String, "名称": cql2.String, "population": cql2.Integer, "height": cql2.Number, "active": cql2.Boolean, "geom": cql2.GeometryType, "day": cql2.Date, "observed": cql2.Timestamp},
		Operators: []cql2.Operator{cql2.Like, cql2.Between, cql2.In, cql2.SIntersects, cql2.SWithin, cql2.TIntersects, cql2.TBefore}}
}

// The expected operator signatures are intentionally independent of parser helpers.
func intervalOnly(op cql2.Operator) bool {
	switch op {
	case cql2.TContains, cql2.TDuring, cql2.TFinishedBy, cql2.TFinishes, cql2.TMeets, cql2.TMetBy, cql2.TOverlappedBy, cql2.TOverlaps, cql2.TStartedBy, cql2.TStarts:
		return true
	default:
		return false
	}
}
