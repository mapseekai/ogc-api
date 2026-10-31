package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
)

const (
	apiEPSG4326 = "http://www.opengis.net/def/crs/EPSG/0/4326"
	apiEPSG3857 = "http://www.opengis.net/def/crs/EPSG/0/3857"
	apiEPSG4979 = "http://www.opengis.net/def/crs/EPSG/0/4979"
)

func TestFeatureErrorRetainsCleanupDiagnostic(t *testing.T) {
	closes := 0
	closeErr := errors.New("cursor cleanup failed")
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Collections[0].Features.Reader = failedFeaturePage{closed: &closes, failure: closeErr}
	e := engine(t, c, 1)
	plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Finish(0, nil)
	if closes != 1 || plan.Status != 404 || !errors.Is(plan.ProtocolError, closeErr) {
		t.Fatalf("lost ownership diagnostic: closes=%d status=%d error=%v", closes, plan.Status, plan.ProtocolError)
	}
}

func TestHeadClosesWithoutIterating(t *testing.T) {
	provider := &memoryFeatures{}
	e := engine(t, catalog(provider, tileReader("tile")), 1)
	plan, err := e.Handle(context.Background(), api.Input{Method: "HEAD", EscapedPath: "/ogc/collections/roads/items"})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if provider.closed.Load() != 1 {
		t.Fatal("HEAD did not close result")
	}
	next, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, next.Body); err != nil {
		t.Fatal(err)
	}
	if err := next.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if next.Status != 200 {
		t.Fatal("HEAD leaked admission")
	}
}

func TestFeaturePart3TypedRequestAndRetainedIterator(t *testing.T) {
	reader := &retainedFilterReader{}
	c := filterCatalog(reader)
	e := engine(t, c, 128)
	input := api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {`count >= 9007199254740993 AND name IS NOT NULL`}, "bbox": {"170,-10,-170,10"}, "datetime": {"2024-01-01T00:00:00Z/.."}, "name": {"legacy"}, "limit": {"2"}, "cursor": {"owned"}}.Encode()}
	plan, err := e.Handle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Finish(0, nil)
	if plan.Status != 200 || !strings.Contains(plan.Headers["Link"], "/collections/roads/queryables") {
		t.Fatal(plan.Status, plan.Headers)
	}
	requests := reader.retained()
	if len(requests) != 1 {
		t.Fatal(requests)
	}
	request := requests[0]
	if request.CollectionID != "roads" || request.ItemID != "" || request.Query.Filter == nil || request.Query.Filter.Expression.Operator != cql2.And || request.Query.Properties["name"] != "legacy" || len(request.Query.BBox) != 4 || request.Query.Datetime == nil || request.Query.Cursor != "owned" || request.Query.Limit != 2 {
		t.Fatal(request)
	}
	before, _ := json.Marshal(request)
	// The first result remains lazy while unrelated concurrent requests finish.
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: input.EscapedPath, RawQuery: "filter=name%3D%27other%27"})
			if status != 200 {
				t.Error(status)
			}
		}()
	}
	wg.Wait()
	body, err := io.ReadAll(plan.Body)
	if err != nil || !strings.Contains(string(body), "retained") {
		t.Fatal(string(body), err)
	}
	if err := plan.Finish(int64(len(body)), nil); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(reader.retained()[0])
	if string(before) != string(after) {
		t.Fatal("request changed after other Handle calls or Finish")
	}
	if reader.closes.Load() != 25 {
		t.Fatal("iterator cleanup lost", reader.closes.Load())
	}
}

func TestFeaturePart3InvalidRequestsNeverReachReader(t *testing.T) {
	reader := &retainedFilterReader{}
	e := engine(t, filterCatalog(reader), 16)
	for _, raw := range []string{
		"filter=", "filter=%20", "filter-lang=cql2-text", "filter-crs=", "filter=count%3D1&filter-lang=sql",
		"filter=unknown%3D1", "filter=count%3D%27x%27", "filter=count%3D1&filter=count%3D2", "filter-lang=cql2-text&filter-lang=cql2-json",
		"filter=name%20LIKE%20%27a%25%27", "filter=count%3D1&filter-crs=EPSG%3A3857", "filter=%7B&filter-lang=cql2-json",
		"filter=" + url.QueryEscape(`{"op":"=","args":[{"property":"count"},1],"op":"<>"}`) + "&filter-lang=cql2-json",
		"filter=" + url.QueryEscape(strings.Repeat("(", 100)+"true"+strings.Repeat(")", 100)),
	} {
		t.Run(raw, func(t *testing.T) {
			status, body, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: raw})
			if status != 400 || !strings.Contains(string(body), `"code":"InvalidParameterValue"`) {
				t.Fatal(status, string(body))
			}
		})
	}
	if len(reader.retained()) != 0 {
		t.Fatal("invalid filter reached external reader")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, err := e.Handle(ctx, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: "filter=count%3D1"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Finish(0, nil)
	if plan.Status != 408 || len(reader.retained()) != 0 {
		t.Fatal("canceled request invoked reader", plan.Status)
	}
}

func TestFeaturePart3JSONParityAndLinks(t *testing.T) {
	reader := &retainedFilterReader{}
	e := engine(t, filterCatalog(reader), 16)
	text := `count >= 9007199254740993`
	jsonFilter := `{"op":">=","args":[{"property":"count"},9007199254740993]}`
	for _, raw := range []string{url.Values{"filter": {text}}.Encode(), url.Values{"filter": {jsonFilter}, "filter-lang": {"cql2-json"}}.Encode()} {
		status, body, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: raw})
		if status != 200 || !strings.Contains(headers["Link"], `rel="http://www.opengis.net/def/rel/ogc/1.0/queryables"`) || !strings.Contains(string(body), "/collections/roads/queryables") {
			t.Fatal(status, string(body), headers)
		}
	}
	requests := reader.retained()
	if !reflect.DeepEqual(requests[0].Query.Filter.Expression, requests[1].Query.Filter.Expression) {
		t.Fatal("HTTP encodings differ")
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items/one", RawQuery: "filter=count%3D1"})
	if status != 400 {
		t.Fatal("item resource accepted collection filter", status)
	}
}

func TestFeatureInvalidTemporalSignaturesNeverReachReader(t *testing.T) {
	reader := &retainedFilterReader{}
	c := filterCatalog(reader)
	intervalOps := []cql2.Operator{cql2.TContains, cql2.TDuring, cql2.TFinishedBy, cql2.TFinishes, cql2.TMeets, cql2.TMetBy, cql2.TOverlappedBy, cql2.TOverlaps, cql2.TStartedBy, cql2.TStarts}
	c.Collections[0].Features.Options.Filter.Operators = append(c.Collections[0].Features.Options.Filter.Operators, intervalOps...)
	e := engine(t, c, 16)
	check := func(input string, language cql2.Language) {
		t.Helper()
		raw := url.Values{"filter": {input}, "filter-lang": {string(language)}}.Encode()
		status, body, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: raw})
		if status != 400 || !strings.Contains(string(body), `"code":"InvalidParameterValue"`) || len(reader.retained()) != 0 {
			t.Fatal("invalid temporal filter reached reader", language, status, len(reader.retained()), string(body))
		}
	}
	for _, stamp := range []string{"2024-01-01T00:00:00,1234567899Z", "2024-01-01T00:00:00,1Z", "2024-01-01T1:00:00Z", "2024-01-01T00:00:00.1234567899Z"} {
		check(`observed=TIMESTAMP('`+stamp+`')`, cql2.Text)
		wire, err := json.Marshal(map[string]any{"op": "=", "args": []any{map[string]any{"property": "observed"}, map[string]any{"timestamp": stamp}}})
		if err != nil {
			t.Fatal(err)
		}
		check(string(wire), cql2.JSON)
	}
	ends := []string{"2024-01-01T00:00:00,1234567899Z", "2024-01-01T00:00:00,1234567891Z"}
	check(`T_INTERSECTS(observed,INTERVAL('`+ends[0]+`','`+ends[1]+`'))`, cql2.Text)
	wire, err := json.Marshal(map[string]any{"op": "t_intersects", "args": []any{map[string]any{"property": "observed"}, map[string]any{"interval": ends}}})
	if err != nil {
		t.Fatal(err)
	}
	check(string(wire), cql2.JSON)
	for _, op := range intervalOps {
		check(strings.ToUpper(string(op))+`(observed,TIMESTAMP('2024-01-01T00:00:00Z'))`, cql2.Text)
		wire, err := json.Marshal(map[string]any{"op": op, "args": []any{map[string]any{"property": "observed"}, map[string]any{"timestamp": "2024-01-01T00:00:00Z"}}})
		if err != nil {
			t.Fatal(err)
		}
		check(string(wire), cql2.JSON)
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {`observed=TIMESTAMP('2024-01-01T00:00:00.123456789Z')`}}.Encode()})
	if status != 200 || len(reader.retained()) != 1 {
		t.Fatal("valid timestamp rejected", status)
	}
}

func TestFeatureEscapedAndCompoundNullFilterEncodingParity(t *testing.T) {
	for _, propertyComparisons := range []bool{false, true} {
		reader := &retainedFilterReader{}
		c := filterCatalog(reader)
		c.Collections[0].Features.Options.Filter.PropertyComparisons = propertyComparisons
		e := engine(t, c, 16)
		// Construct the expected control bytes independently of CQL2 wire escaping.
		escapedLiteral := "first" + string([]byte{7, 8, 9, 10, 11, 12, 13}) + `\'雪second`
		cases := []struct {
			text        string
			json        any
			wantLiteral string
		}{
			{`name='first\a\b\t\n\v\f\r\\\'雪second'`, map[string]any{"op": "=", "args": []any{map[string]any{"property": "name"}, escapedLiteral}}, escapedLiteral},
			{`(count=1) IS NULL OR (count>0) IS NOT NULL`, map[string]any{"op": "or", "args": []any{map[string]any{"op": "isNull", "args": []any{map[string]any{"op": "=", "args": []any{map[string]any{"property": "count"}, 1}}}}, map[string]any{"op": "not", "args": []any{map[string]any{"op": "isNull", "args": []any{map[string]any{"op": ">", "args": []any{map[string]any{"property": "count"}, 0}}}}}}}}, ""},
		}
		for _, tc := range cases {
			before := len(reader.retained())
			wire, err := json.Marshal(tc.json)
			if err != nil {
				t.Fatal(err)
			}
			for _, request := range []struct {
				input    string
				language cql2.Language
			}{{tc.text, cql2.Text}, {string(wire), cql2.JSON}} {
				status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {request.input}, "filter-lang": {string(request.language)}}.Encode()})
				if status != 200 {
					t.Fatal("valid typed filter rejected", request.language, status)
				}
			}
			requests := reader.retained()
			if len(requests) != before+2 || !reflect.DeepEqual(requests[before].Query.Filter.Expression, requests[before+1].Query.Filter.Expression) {
				t.Fatal("HTTP filter encodings differ")
			}
			if tc.wantLiteral != "" && requests[before].Query.Filter.Expression.Args[1].Literal.Text != tc.wantLiteral {
				t.Fatal("HTTP escape changed literal")
			}
		}
	}
}

func TestFeatureNullSuffixEncodingParity(t *testing.T) {
	op := func(name string, args ...any) any { return map[string]any{"op": name, "args": args} }
	prop := func(name string) any { return map[string]any{"property": name} }
	eq := op("=", prop("count"), 1)
	for _, enabled := range []bool{false, true} {
		reader := &retainedFilterReader{}
		c := filterCatalog(reader)
		c.Collections[0].Features.Options.Filter.PropertyComparisons = enabled
		e := engine(t, c, 16)
		for _, tc := range []struct {
			text string
			wire any
		}{
			{`count=1 IS NULL`, op("isNull", eq)},
			{`(count=1) IS NULL IS NULL`, op("isNull", op("isNull", eq))},
			{`count=1 IS NULL IS NOT NULL`, op("not", op("isNull", op("isNull", eq)))},
			{`NOT count=1 IS NULL OR name IS NOT NULL AND count=2`, op("or", op("not", op("isNull", eq)), op("and", op("not", op("isNull", prop("name"))), op("=", prop("count"), 2)))},
		} {
			before := len(reader.retained())
			wire, err := json.Marshal(tc.wire)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []struct {
				source   string
				language cql2.Language
			}{{tc.text, cql2.Text}, {string(wire), cql2.JSON}} {
				status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {input.source}, "filter-lang": {string(input.language)}}.Encode()})
				if status != 200 {
					t.Fatal("valid null suffix rejected", enabled, input.language, status)
				}
			}
			requests := reader.retained()
			if len(requests) != before+2 || !reflect.DeepEqual(requests[before].Query.Filter.Expression, requests[before+1].Query.Filter.Expression) {
				t.Fatal("null suffix encodings differ at Reader")
			}
		}
	}
}

func TestFeatureInvalidNullSuffixesNeverReachReader(t *testing.T) {
	op := func(name string, args ...any) any { return map[string]any{"op": name, "args": args} }
	prop := func(name string) any { return map[string]any{"property": name} }
	reader := &retainedFilterReader{}
	c := filterCatalog(reader)
	c.Collections[0].Features.Options.Filter.Operators = nil
	e := engine(t, c, 16)
	for _, tc := range []struct {
		text string
		wire any
	}{
		{`count=unknown IS NULL`, op("isNull", op("=", prop("count"), prop("unknown")))},
		{`count=count IS NULL IS NULL`, op("isNull", op("isNull", op("=", prop("count"), prop("count"))))},
		{`name LIKE 'x' IS NULL IS NULL`, op("isNull", op("isNull", op("like", prop("name"), "x")))},
		{`count='x' IS NULL`, op("isNull", op("=", prop("count"), "x"))},
		{`(count=1)=true IS NULL`, op("isNull", op("=", op("=", prop("count"), 1), true))},
	} {
		wire, err := json.Marshal(tc.wire)
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range []struct {
			source   string
			language cql2.Language
		}{{tc.text, cql2.Text}, {string(wire), cql2.JSON}} {
			status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {input.source}, "filter-lang": {string(input.language)}}.Encode()})
			if status != 400 {
				t.Fatal("invalid suffix changed validation outcome", input.language, status)
			}
		}
	}
	for _, input := range []string{`count=1 IS NULL IS`, `count=1 IS NOT`, `count=1 IS NULL IS NOT true`, `count=1` + strings.Repeat(" IS NULL", 100)} {
		status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {input}}.Encode()})
		if status != 400 {
			t.Fatal("malformed or over-depth null chain accepted", status)
		}
	}
	var chain any = op("=", prop("count"), 1)
	for range 100 {
		chain = op("isNull", chain)
	}
	wire, err := json.Marshal(chain)
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {string(wire)}, "filter-lang": {"cql2-json"}}.Encode()})
	if status != 400 {
		t.Fatal("over-depth JSON null chain accepted", status)
	}
	if len(reader.retained()) != 0 {
		t.Fatal("invalid null chain reached Reader")
	}
	nodeReader := &retainedFilterReader{}
	nodeCatalog := filterCatalog(nodeReader)
	nodeCatalog.Collections[0].Features.Options.Filter.Limits = cql2.Limits{MaxNodes: 4}
	nodeEngine := engine(t, nodeCatalog, 16)
	status, _, _ = read(t, nodeEngine, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"filter": {`count=1 IS NULL IS NULL`}}.Encode()})
	if status != 400 || len(nodeReader.retained()) != 0 {
		t.Fatal("suffix node budget bypassed before Reader", status)
	}
}

func TestFeaturePart2ListItemHeadersAndLinks(t *testing.T) {
	reader := &crsFeatureReader{}
	e := engine(t, part2Catalog(reader), 16)
	for _, item := range []bool{false, true} {
		for _, uri := range []string{"", common.CRS84, common.CRS84h, apiEPSG4326, apiEPSG3857, apiEPSG4979} {
			path := "/ogc/collections/roads/items"
			values := url.Values{"f": {common.GeoJSON}}
			if uri != "" {
				values.Set("crs", uri)
			}
			if item {
				path += "/known"
			} else {
				values.Set("bbox-crs", apiEPSG4326)
				values.Set("bbox", "40,110,50,130")
				values.Set("filter-crs", apiEPSG4326)
				values.Set("filter", "S_INTERSECTS(geom,POINT(45 120))")
			}
			status, data, headers := read(t, e, api.Input{Method: "GET", EscapedPath: path, RawQuery: values.Encode()})
			want := uri
			if want == "" {
				want = common.CRS84
			}
			if status != 200 || headers["Content-Crs"] != "<"+want+">" || headers["Content-Type"] != common.GeoJSON {
				t.Fatal(item, uri, status, string(data), headers)
			}
			var body struct {
				CRS      json.RawMessage
				Links    []common.Link
				Features []features.Feature
				Geometry struct{ Coordinates []float64 }
			}
			if err := json.Unmarshal(data, &body); err != nil || body.CRS != nil {
				t.Fatal("invalid representation or invented crs member", err, string(data))
			}
			self, next := "", ""
			for _, link := range body.Links {
				if link.Rel == "self" {
					self = link.Href
				}
				if link.Rel == "next" {
					next = link.Href
				}
			}
			parsed, err := url.Parse(self)
			if err != nil || parsed.Query().Encode() != values.Encode() {
				t.Fatal("representation self link lost parameters", self, values, err)
			}
			if !item {
				parsed, err := url.Parse(next)
				wantNext := maps.Clone(values)
				wantNext.Set("cursor", "next")
				if err != nil || parsed.Query().Encode() != wantNext.Encode() || len(body.Features) != 1 {
					t.Fatal("next link lost CRS or predicates", next, values, err)
				}
			} else if want == apiEPSG4326 && !reflect.DeepEqual(body.Geometry.Coordinates, []float64{45, 120}) {
				t.Fatal("native latitude/longitude changed", body.Geometry.Coordinates)
			}
		}
	}
	requests := reader.retained()
	for _, request := range requests {
		if request.Query.OutputCRS.Validate() != nil || request.ItemID == "" && (request.Query.BBoxCRS.URI != apiEPSG4326 || request.Query.Filter.CRS != apiEPSG4326) {
			t.Fatal("reader received ambiguous coordinate systems", request)
		}
	}
	status, _, headers := read(t, e, api.Input{Method: "HEAD", EscapedPath: "/ogc/collections/roads/items", RawQuery: url.Values{"crs": {apiEPSG3857}}.Encode()})
	if status != 200 || headers["Content-Crs"] != "<"+apiEPSG3857+">" {
		t.Fatal("HEAD omitted actual CRS", status, headers)
	}
}

func TestFeaturePart2InvalidRequestsDoNotReachReader(t *testing.T) {
	reader := &crsFeatureReader{}
	e := engine(t, part2Catalog(reader), 16)
	for _, raw := range []string{
		"crs=", "crs=EPSG%3A3857", "crs=3857", "crs=unknown", "crs=x&crs=x", "bbox-crs=x&bbox-crs=x",
		"bbox-crs=" + url.QueryEscape(apiEPSG3857),
		url.Values{"bbox": {"0,0,1,1"}, "bbox-crs": {common.CRS84h}}.Encode(),
		url.Values{"bbox": {"100,1,110,2"}, "bbox-crs": {apiEPSG4326}}.Encode(),
		url.Values{"bbox": {"2,0,1,1"}, "bbox-crs": {apiEPSG3857}}.Encode(),
		url.Values{"filter": {"S_INTERSECTS(geom,POINT(120 45))"}, "filter-crs": {apiEPSG4326}}.Encode(),
	} {
		status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: raw})
		if status != 400 {
			t.Fatal(raw, status)
		}
	}
	for _, raw := range []string{"crs=", "crs=x", "crs=x&crs=x", "bbox=0,0,1,1", "bbox-crs=" + url.QueryEscape(common.CRS84), "filter=true"} {
		status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items/known", RawQuery: raw})
		if status != 400 {
			t.Fatal(raw, status)
		}
	}
	if len(reader.retained()) != 0 {
		t.Fatal("invalid coordinate request reached reader")
	}
}

func TestFeaturePart2RejectsUnacknowledgedOutputAndCloses(t *testing.T) {
	for _, mismatch := range []common.CRS{{}, part2CRS(apiEPSG4326)} {
		reader := &crsFeatureReader{badCRS: true, reportedCRS: mismatch}
		e := engine(t, part2Catalog(reader), 16)
		for _, suffix := range []string{"", "/known"} {
			status, _, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items" + suffix, RawQuery: url.Values{"crs": {apiEPSG3857}}.Encode()})
			if status != 500 || headers["Content-Crs"] != "" {
				t.Fatal("reader ignored request but response was mislabeled", status, headers)
			}
		}
		if reader.closed.Load() != 1 {
			t.Fatal("rejected page leaked iterator", reader.closed.Load())
		}
	}
}

func TestFeaturePart2RejectsNestedEmptyPositionsAndCloses(t *testing.T) {
	for _, geometry := range []string{
		`{"type":"MultiPoint","coordinates":[[]]}`,
		`{"type":"LineString","coordinates":[[1,2],[]]}`,
		`{"type":"Polygon","coordinates":[[]]}`,
		`{"type":"MultiPolygon","coordinates":[[[]]]}`,
	} {
		t.Run(geometry, func(t *testing.T) {
			reader := &crsFeatureReader{geometry: json.RawMessage(geometry)}
			e := engine(t, part2Catalog(reader), 16)
			status, _, headers := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items/known"})
			if status != 500 || headers["Content-Crs"] != "" {
				t.Fatal("invalid item geometry was served successfully", status, headers)
			}
			plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items"})
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(plan.Body)
			finishErr := plan.Finish(int64(len(body)), readErr)
			if readErr == nil || finishErr == nil || strings.Contains(string(body), `"coordinates"`) || reader.closed.Load() != 1 {
				t.Fatal("invalid streamed geometry escaped validation or leaked iterator", string(body), readErr, finishErr, reader.closed.Load())
			}
		})
	}
}

func TestFeaturePart2RetainedRequestsAreIndependent(t *testing.T) {
	reader := &crsFeatureReader{}
	c := part2Catalog(reader)
	e := engine(t, c, 128)
	first := url.Values{"crs": {apiEPSG3857}, "bbox": {"40,110,50,130"}, "bbox-crs": {apiEPSG4326}, "filter": {"S_INTERSECTS(geom,POINT(45 120))"}, "filter-crs": {apiEPSG4326}}
	plan, err := e.Handle(context.Background(), api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: first.Encode()})
	if err != nil || plan.Status != 200 {
		t.Fatal(plan, err)
	}
	defer plan.Finish(0, nil)
	before, _ := json.Marshal(reader.retained()[0])
	c.Collections[0].Features.Options.CRS.Supported[0].URI = "mutated"
	c.Collections[0].Features.Options.Filter.AdditionalCRS[0].URI = "mutated"
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items/known", RawQuery: url.Values{"crs": {apiEPSG4326}}.Encode()})
			if status != 200 {
				t.Error(status)
			}
		}()
	}
	wg.Wait()
	_, err = io.Copy(io.Discard, plan.Body)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(reader.retained()[0])
	if string(before) != string(after) {
		t.Fatal("retained request changed after publication mutation or concurrent Handle")
	}
}

func TestFeatureFilter4096ByteBudgetControlsReaderAdmission(t *testing.T) {
	reader := &retainedFilterReader{}
	catalog := filterCatalog(reader)
	catalog.Collections[0].Features.Options.Filter.Limits = cql2.Limits{MaxBytes: 4096}
	e := engine(t, catalog, 1)
	literal := strings.Repeat("雪", (4096-len("name=''"))/len("雪"))
	literal += strings.Repeat("x", 4096-len("name=''")-len(literal))
	for _, extra := range []string{"", "x"} {
		input := "name='" + literal + extra + "'"
		raw := url.Values{"filter": {input}, "filter-lang": {string(cql2.Text)}}.Encode()
		if len(input) != 4096+len(extra) || len(raw) <= 4096 {
			t.Fatal("incorrect decoded/encoded boundary fixture", len(input), len(raw))
		}
		status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: raw})
		if extra == "" && status != 200 || extra != "" && status != 400 {
			t.Fatal("filter budget not enforced independently of URI budget", len(input), status, string(data))
		}
		requests := reader.retained()
		if len(requests) != 1 || requests[0].Query.Filter.Expression.Args[1].Literal.Text != literal {
			t.Fatal("4096-byte literal changed or over-budget request reached Reader", len(requests))
		}
	}
	if reader.closes.Load() != 1 {
		t.Fatal("accepted boundary request leaked iterator", reader.closes.Load())
	}
}

func TestFeatureOffsetContinuationsAreExplicitAndOwned(t *testing.T) {
	zero, one, two, maximum := uint64(0), uint64(1), uint64(2), uint64(math.MaxUint64)
	for _, tc := range []struct {
		name, query, cursor string
		next, matched       *uint64
		status              int
	}{
		{"unknown-total-next", "offset=0&limit=2", "", &one, nil, 200},
		{"full-terminal", "offset=0&limit=1", "", nil, nil, 200},
		{"legacy-cursor", "cursor=prior", "next-token", nil, nil, 200},
		{"mixed-result", "offset=0", "next-token", nil, nil, 500},
		{"unexpected-result-mode", "", "", &one, nil, 500},
		{"nonadvancing", "offset=1", "", &one, nil, 500},
		{"backwards", "offset=1", "", &zero, nil, 500},
		{"known-total-terminal", "offset=0", "", &one, &one, 500},
		{"known-total-next", "offset=0", "", &one, &two, 200},
		{"maximum-next", "offset=18446744073709551614", "", &maximum, nil, 200},
		{"maximum-terminal", "offset=18446744073709551615", "", nil, nil, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &offsetMetadataReader{next: tc.next, cursor: tc.cursor, matched: tc.matched}
			c := filterCatalog(reader)
			c.Collections[0].Features.Options.OffsetPagination = true
			e := engine(t, c, 1)
			status, data, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: tc.query})
			if status != tc.status || reader.closed.Load() != 1 {
				t.Fatal("wrong continuation status or leaked page", status, string(data), reader.closed.Load())
			}
			if status == 200 {
				var body struct{ Links []common.Link }
				if err := json.Unmarshal(data, &body); err != nil {
					t.Fatal(err)
				}
				next := ""
				for _, link := range body.Links {
					if link.Rel == "next" {
						next = link.Href
					}
				}
				if (next != "") != (tc.next != nil || tc.cursor != "") {
					t.Fatal("page length inferred a false next link", tc.name, next)
				}
			}
		})
	}
	reader := &offsetMetadataReader{}
	c := filterCatalog(reader)
	e := engine(t, c, 1)
	status, _, _ := read(t, e, api.Input{Method: "GET", EscapedPath: "/ogc/collections/roads/items", RawQuery: "offset=0"})
	if status != 400 || reader.closed.Load() != 0 {
		t.Fatal("disabled offset reached Reader", status)
	}
}

func TestFeatureFilterReaderRejectionIsSafeOverHTTP(t *testing.T) {
	private := errors.New("SENSITIVE_SQL SELECT private_column FROM tenant_data; postgresql://private-db.invalid/tenant password=redaction-only; goroutine 99 backend/filter.go:999")
	validation := common.Invalid("filter")
	validation.Cause = private
	for _, tc := range []struct {
		name         string
		failure      error
		status       int
		code, detail string
	}{
		{"validation", fmt.Errorf("private validator context: %w", validation), 400, "InvalidParameterValue", "Invalid parameter: filter"},
		{"forbidden", errors.Join(common.ErrForbidden, private), 403, "Forbidden", "Forbidden"},
		{"unavailable", errors.Join(common.ErrUnavailable, private), 503, "ServerBusy", "Service Unavailable"},
		{"unexpected", fmt.Errorf("backend filter validation failed: %w", private), 500, "NoApplicableCode", "Internal Server Error"},
	} {
		for _, media := range []string{common.ProblemJSON, common.JSON} {
			t.Run(tc.name+"/"+media, func(t *testing.T) {
				reader := &rejectingFilterReader{failure: tc.failure, requests: make(chan features.Request, 1)}
				e := engine(t, filterCatalog(reader), 1)
				type result struct{ handling, protocol, sending, finishing error }
				results := make(chan result, 1)
				// This transparent test host copies the engine's plan; it performs
				// no error rewriting/redaction of its own. Observe diagnostics only
				// in-process, never through headers or the response document.
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					plan, err := e.Handle(r.Context(), api.Input{Method: r.Method, EscapedPath: r.URL.EscapedPath(), RawQuery: r.URL.RawQuery, Accept: r.Header.Get("Accept")})
					if err != nil {
						results <- result{handling: err}
						http.Error(w, "test host failure", 500)
						return
					}
					var n int64
					var sent error
					defer func() {
						results <- result{protocol: plan.ProtocolError, sending: sent, finishing: plan.Finish(n, sent)}
					}()
					for name, value := range plan.Headers {
						w.Header().Set(name, value)
					}
					w.WriteHeader(plan.Status)
					n, sent = io.Copy(w, plan.Body)
				}))
				t.Cleanup(server.Close)
				client := server.Client()
				client.Timeout = 5 * time.Second
				request, err := http.NewRequest(http.MethodGet, server.URL+"/ogc/collections/roads/items?"+url.Values{"filter": {"count=1"}}.Encode(), nil)
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Accept", common.GeoJSON+", "+media)
				reply, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				data, readErr := io.ReadAll(reply.Body)
				closeErr := reply.Body.Close()
				var problem common.Problem
				if readErr != nil || closeErr != nil || json.Unmarshal(data, &problem) != nil || reply.StatusCode != tc.status || reply.Header.Get("Content-Type") != media || reply.Header.Get("Vary") != "Accept" {
					t.Fatal("incorrect HTTP error contract", reply.StatusCode, reply.Header, string(data), readErr, closeErr)
				}
				if problem.Status != tc.status || problem.Code != tc.code || problem.Detail != tc.detail || problem.Description != tc.detail || problem.Title != http.StatusText(tc.status) || problem.Type != "about:blank" {
					t.Fatal("incorrect safe problem document", problem)
				}
				for _, marker := range []string{"SENSITIVE_SQL", "SELECT", "private_column", "private-db", "password", "goroutine", "backend/filter.go", "private validator context", "\"cause\"", "\"stack\""} {
					if strings.Contains(string(data), marker) || strings.Contains(fmt.Sprint(reply.Header), marker) {
						t.Fatal("private backend detail leaked over HTTP", marker)
					}
				}
				wantRetry := ""
				if tc.status == 503 {
					wantRetry = "1"
				}
				if reply.Header.Get("Retry-After") != wantRetry {
					t.Fatal("incorrect retry contract", reply.Header)
				}
				select {
				case observed := <-results:
					if observed.handling != nil || observed.sending != nil || observed.finishing != nil || !errors.Is(observed.protocol, tc.failure) || !errors.Is(observed.protocol, private) {
						t.Fatal("lost original private ProtocolError or misreported transport failure", observed)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("HTTP plan was not finished")
				}
				select {
				case received := <-reader.requests:
					if received.Query.Filter == nil || received.Query.Filter.Expression.Operator != cql2.Equal || received.Query.Filter.Expression.Args[0].Property != "count" || string(received.Query.Filter.Expression.Args[1].Literal.Number) != "1" {
						t.Fatal("backend did not validate the actual typed filter", received)
					}
				default:
					t.Fatal("failure occurred before the Reader instead of a backend rejection")
				}
				// MaxConcurrent=1: an ordinary HTTP request must still succeed after
				// the rejected filter response has released its response ownership.
				probe, err := client.Get(server.URL + "/ogc/")
				if err != nil {
					t.Fatal(err)
				}
				_, readErr = io.Copy(io.Discard, probe.Body)
				closeErr = probe.Body.Close()
				if probe.StatusCode != 200 || readErr != nil || closeErr != nil {
					t.Fatal("backend rejection leaked admission", probe.StatusCode, readErr, closeErr)
				}
				select {
				case observed := <-results:
					if observed != (result{}) {
						t.Fatal("post-rejection request failed", observed)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("post-rejection plan was not finished")
				}
			})
		}
	}
}

type rejectingFilterReader struct {
	failure  error
	requests chan features.Request
}

func (r *rejectingFilterReader) Items(_ context.Context, request features.Request) (features.Page, error) {
	r.requests <- request
	return features.Page{}, r.failure
}

func (r *rejectingFilterReader) Item(context.Context, features.Request) (features.Feature, error) {
	return features.Feature{}, common.ErrNotFound
}

type failedFeaturePage struct {
	closed  *int
	failure error
}

func (p failedFeaturePage) Items(context.Context, features.Request) (features.Page, error) {
	return features.Page{Items: p}, common.ErrNotFound
}

func (p failedFeaturePage) Item(context.Context, features.Request) (features.Feature, error) {
	return features.Feature{}, common.ErrNotFound
}

func (p failedFeaturePage) Next(context.Context) (features.Feature, error) {
	return features.Feature{}, io.EOF
}

func (p failedFeaturePage) Close() error { *p.closed++; return p.failure }

type memoryFeatures struct{ closed atomic.Int64 }

type featureCursor struct {
	owner     *memoryFeatures
	remaining int
	closed    bool
}

func (c *featureCursor) Next(ctx context.Context) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if c.remaining == 0 {
		return features.Feature{}, io.EOF
	}
	c.remaining--
	return features.Feature{ID: "one", Geometry: json.RawMessage(`{"type":"Point","coordinates":[1,2]}`), Properties: json.RawMessage(`{"name":"road"}`)}, nil
}

func (c *featureCursor) Close() error {
	if !c.closed {
		c.closed = true
		c.owner.closed.Add(1)
	}
	return nil
}

func (p *memoryFeatures) Items(_ context.Context, _ features.Request) (features.Page, error) {
	return features.Page{Items: &featureCursor{owner: p, remaining: 1}}, nil
}

func (p *memoryFeatures) Item(_ context.Context, _ features.Request) (features.Feature, error) {
	return features.Feature{ID: "one", Properties: json.RawMessage(`{}`)}, nil
}

type retainedFilterReader struct {
	mu       sync.Mutex
	requests []features.Request
	closes   atomic.Int64
}

type retainedFilterCursor struct {
	owner   *retainedFilterReader
	request features.Request
	done    bool
}

func (r *retainedFilterReader) Items(ctx context.Context, request features.Request) (features.Page, error) {
	if err := ctx.Err(); err != nil {
		return features.Page{}, err
	}
	r.mu.Lock()
	r.requests = append(r.requests, request)
	r.mu.Unlock()
	return features.Page{Items: &retainedFilterCursor{owner: r, request: request}}, nil
}

func (r *retainedFilterReader) Item(context.Context, features.Request) (features.Feature, error) {
	return features.Feature{}, common.ErrNotFound
}

func (r *retainedFilterReader) retained() []features.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]features.Request(nil), r.requests...)
}

func (c *retainedFilterCursor) Next(ctx context.Context) (features.Feature, error) {
	if err := ctx.Err(); err != nil {
		return features.Feature{}, err
	}
	if c.done {
		return features.Feature{}, io.EOF
	}
	c.done = true
	if c.request.Query.Filter == nil {
		return features.Feature{}, errors.New("reader did not receive typed filter")
	}
	return features.Feature{ID: "retained", Geometry: json.RawMessage(`null`), Properties: json.RawMessage(`{"name":"retained"}`)}, nil
}

func (c *retainedFilterCursor) Close() error { c.owner.closes.Add(1); return nil }

func filterCatalog(reader features.Reader) api.Catalog {
	return api.Catalog{Title: "Filter tests", BaseURL: "https://example.test/ogc", Collections: []api.Collection{{Metadata: common.Collection{ID: "roads", Title: "Roads"}, Features: &features.Service{Reader: reader, Options: features.Options{FilterProperties: []string{"name"}, Filter: &features.FilterOptions{Queryables: map[string]features.Queryable{"name": {Type: cql2.String}, "count": {Type: cql2.Integer}, "observed": {Type: cql2.Timestamp}, "geom": {Type: cql2.GeometryType, Format: "geometry-linestring"}}, Operators: []cql2.Operator{cql2.SIntersects, cql2.TIntersects}}}}}}}
}

type crsFeatureReader struct {
	mu          sync.Mutex
	requests    []features.Request
	closed      atomic.Int64
	badCRS      bool
	reportedCRS common.CRS
	geometry    json.RawMessage
}

type crsFeatureCursor struct {
	reader *crsFeatureReader
	crs    common.CRS
	done   bool
}

func (r *crsFeatureReader) Items(_ context.Context, request features.Request) (features.Page, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	r.mu.Unlock()
	crs := request.Query.OutputCRS
	if r.badCRS {
		crs = r.reportedCRS
	}
	matched := uint64(1)
	return features.Page{CRS: crs, Items: &crsFeatureCursor{reader: r, crs: crs}, NumberMatched: &matched, NextCursor: "next"}, nil
}

func (r *crsFeatureReader) Item(_ context.Context, request features.Request) (features.Feature, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	r.mu.Unlock()
	feature := knownCRSFeature(request.Query.OutputCRS)
	if r.badCRS {
		feature.CRS = r.reportedCRS
	}
	if len(r.geometry) != 0 {
		feature.Geometry = r.geometry
	}
	return feature, nil
}

func (r *crsFeatureReader) retained() []features.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]features.Request(nil), r.requests...)
}

func (c *crsFeatureCursor) Next(context.Context) (features.Feature, error) {
	if c.done {
		return features.Feature{}, io.EOF
	}
	c.done = true
	feature := knownCRSFeature(c.crs)
	if len(c.reader.geometry) != 0 {
		feature.Geometry = c.reader.geometry
	}
	return feature, nil
}

func (c *crsFeatureCursor) Close() error { c.reader.closed.Add(1); return nil }

func knownCRSFeature(crs common.CRS) features.Feature {
	position := []float64{120, 45}
	if crs.URI == apiEPSG3857 {
		position = []float64{13358338.895192828, 5621521.486192066}
	} else if crs.AxisOrder == common.YX {
		position = []float64{45, 120}
	}
	if crs.Dimensions == 3 {
		position = append(position, 123)
	}
	geometry, _ := json.Marshal(map[string]any{"type": "Point", "coordinates": position})
	properties, _ := json.Marshal(map[string]any{"name": "known", "secondaryGeometry": json.RawMessage(geometry)})
	return features.Feature{CRS: crs, ID: "known", Geometry: geometry, Properties: properties}
}

func part2CRS(uri string) common.CRS {
	crs, _ := common.CRSByURI(uri)
	return crs
}

func part2Catalog(reader features.Reader) api.Catalog {
	c := filterCatalog(reader)
	c.Collections[0].Features.Options.CRS = &features.CRSOptions{Supported: []common.CRS{part2CRS(apiEPSG3857), part2CRS(common.CRS84), part2CRS(common.CRS84h), part2CRS(apiEPSG4326), part2CRS(apiEPSG4979)}}
	c.Collections[0].Features.Options.Filter.AdditionalCRS = []common.CRS{part2CRS(apiEPSG4326)}
	c.Collections[0].Metadata.StorageCRS = apiEPSG3857
	return c
}

type offsetMetadataReader struct {
	memoryFeatures
	next    *uint64
	cursor  string
	matched *uint64
}

func (r *offsetMetadataReader) Items(_ context.Context, _ features.Request) (features.Page, error) {
	// A metadata/cleanup probe only. Actual offset execution is covered by the
	// real HTTP fixture Reader, not this fixed-row response.
	return features.Page{Items: &featureCursor{owner: &r.memoryFeatures, remaining: 1}, NextOffset: r.next, NextCursor: r.cursor, NumberMatched: r.matched}, nil
}
