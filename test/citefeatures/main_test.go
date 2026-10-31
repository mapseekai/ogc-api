package main

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func TestFixtureDynamicListenerIdentityAndLinks(t *testing.T) {
	identity := fixtureIdentity{Nonce: strings.Repeat("n", 32), PID: os.Getpid(), Revision: "test-source"}
	server, listener, expected, err := newFixtureServer("127.0.0.1:0", identity)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Close()
		if err := <-done; err != http.ErrServerClosed {
			t.Error(err)
		}
	})
	path := filepath.Join(t.TempDir(), "ready.json")
	if err := writeFixtureIdentity(path, expected); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	var published fixtureIdentity
	if err != nil || json.Unmarshal(raw, &published) != nil || published != expected || strings.HasSuffix(expected.BaseURL, ":0") {
		t.Fatal("listener identity was not atomically published", string(raw), err)
	}
	for _, nonce := range []string{"", "wrong", identity.Nonce} {
		request, err := http.NewRequest(http.MethodGet, expected.BaseURL+"/.well-known/ogc-cite-ready", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-OGC-CITE-Nonce", nonce)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(readErr, closeErr)
		}
		if nonce != identity.Nonce {
			if response.StatusCode != 404 {
				t.Fatal("unmatched run identity accepted", response.StatusCode)
			}
			continue
		}
		var actual fixtureIdentity
		if response.StatusCode != 200 || json.Unmarshal(body, &actual) != nil || actual != expected {
			t.Fatal("readiness did not identify the owned listener", response.StatusCode, string(body))
		}
	}
	response, err := http.Get(expected.BaseURL + "/collections")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || strings.Contains(string(body), ":0/") || !strings.Contains(string(body), expected.BaseURL) {
		t.Fatal("discovery URLs did not use the owned port", string(body), err)
	}
}

func TestFixtureListenerRemainsLoopbackOnly(t *testing.T) {
	for _, address := range []string{":0", "0.0.0.0:0", "example.org:0", "localhost:0", "127.0.0.1:http", "127.0.0.1:65536"} {
		if _, listener, _, err := newFixtureServer(address, fixtureIdentity{}); err == nil {
			listener.Close()
			t.Fatal("fixture accepted an external or ambiguous listener", address)
		}
	}
}

func TestHTTPKnownCRSCoordinatesAndAllGeometryProperties(t *testing.T) {
	server := fixtureServer(t)
	for _, tc := range []struct {
		path, crs, header string
		coordinates       []float64
	}{
		{"/collections/regional/items/1", "", common.CRS84, []float64{7, 50}},
		{"/collections/regional/items/1", epsg4326, epsg4326, []float64{50, 7}},
		{"/collections/regional/items/1", epsg3857, epsg3857, []float64{779236.435552915, 6446275.841017161}},
		{"/three-dimensional/collections/elevated/items/0", "", common.CRS84h, []float64{7, 50, 75}},
		{"/three-dimensional/collections/elevated/items/0", epsg4979, epsg4979, []float64{50, 7, 75}},
		{"/three-dimensional/collections/elevated/items/0", epsg4326, epsg4326, []float64{50, 7}},
		{"/three-dimensional/collections/elevated/items/0", epsg3857, epsg3857, []float64{779236.435552915, 6446275.841017161}},
	} {
		values := url.Values{}
		if tc.crs != "" {
			values.Set("crs", tc.crs)
		}
		status, body, headers := fixtureGet(t, server, tc.path, values)
		if status != 200 || headers.Get("Content-Crs") != "<"+tc.header+">" || body.CRS != nil {
			t.Fatal(status, body, headers)
		}
		assertCoordinates(t, body.Geometry.Coordinates, tc.coordinates)
		assertCoordinates(t, body.Properties.Location.Coordinates, tc.coordinates)
	}
	for _, path := range []string{"/conformance", "/three-dimensional/conformance"} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var body common.Conformance
		decodeErr := json.NewDecoder(response.Body).Decode(&body)
		closeErr := response.Body.Close()
		if decodeErr != nil || closeErr != nil || !strings.Contains(strings.Join(body.ConformsTo, " "), features.CRSConformance) {
			t.Fatal("truthful test backend conformance missing", body, decodeErr, closeErr)
		}
	}
}

func TestHTTPBBoxNativeAxesProjectionHeightAndExclusion(t *testing.T) {
	server := fixtureServer(t)
	for _, tc := range []struct {
		path   string
		values url.Values
		ids    []string
	}{
		{"/collections/regional/items", url.Values{"bbox": {"6.9,49.9,7.2,50.2"}}, []string{"1", "2"}},
		{"/collections/regional/items", url.Values{"bbox": {"49.9,6.9,50.2,7.2"}, "bbox-crs": {epsg4326}, "crs": {epsg3857}}, []string{"1", "2"}},
		{"/collections/regional/items", url.Values{"bbox": {"768000,6420000,801000,6490000"}, "bbox-crs": {epsg3857}, "crs": {epsg4326}}, []string{"1", "2"}},
		{"/collections/regional/items", url.Values{"bbox": {"6.9,49.9,7.2,50.2"}, "bbox-crs": {epsg4326}, "crs": {epsg3857}}, []string{}},
		{"/collections/world/items", url.Values{"bbox": {"177,65,-177,70"}, "crs": {epsg4326}}, []string{"2", "3"}},
		{"/collections/world/items", url.Values{"bbox": {"-90,-180,90,180"}, "bbox-crs": {epsg4326}}, []string{"0", "1", "2", "3", "4", "5", "6"}},
		{"/three-dimensional/collections/elevated/items", url.Values{"bbox": {"6.9,49.9,100,7.2,50.2,150"}, "crs": {epsg3857}}, []string{"1"}},
		{"/three-dimensional/collections/elevated/items", url.Values{"bbox": {"49.9,6.9,100,50.2,7.2,150"}, "bbox-crs": {epsg4979}, "crs": {common.CRS84h}}, []string{"1"}},
		{"/three-dimensional/collections/elevated/items", url.Values{"bbox": {"49.9,6.9,130,50.2,7.2,150"}, "bbox-crs": {epsg4979}}, []string{}},
	} {
		status, body, _ := fixtureGet(t, server, tc.path, tc.values)
		ids := []string{}
		for _, item := range body.Features {
			ids = append(ids, item.ID)
		}
		if status != 200 || !reflect.DeepEqual(ids, tc.ids) || body.NumberMatched == nil || *body.NumberMatched != uint64(len(tc.ids)) || body.NumberReturned != uint64(len(tc.ids)) {
			t.Fatal("bbox hit/exclusion failed", tc.path, tc.values, status, ids, body.NumberMatched)
		}
	}
}

func TestHTTPPaginationRetainsCoordinateSystemsAndPredicates(t *testing.T) {
	server := fixtureServer(t)
	values := url.Values{"bbox": {"48,1,52,9"}, "bbox-crs": {epsg4326}, "crs": {epsg3857}, "filter": {"population>=9007199254740994"}, "limit": {"1"}, "f": {common.GeoJSON}}
	next := server.URL + "/collections/regional/items?" + values.Encode()
	ids := []string{}
	for len(ids) < 4 && next != "" {
		response, err := server.Client().Get(next)
		if err != nil {
			t.Fatal(err)
		}
		var body fixtureDocument
		decodeErr := json.NewDecoder(response.Body).Decode(&body)
		closeErr := response.Body.Close()
		if decodeErr != nil || closeErr != nil || response.StatusCode != 200 || response.Header.Get("Content-Crs") != "<"+epsg3857+">" || len(body.Features) != 1 || body.NumberMatched == nil || *body.NumberMatched != 3 {
			t.Fatal(body, decodeErr, closeErr, response.StatusCode)
		}
		ids = append(ids, body.Features[0].ID)
		next = ""
		for _, link := range body.Links {
			if link.Rel != "self" && link.Rel != "next" {
				continue
			}
			parsed, err := url.Parse(link.Href)
			if err != nil {
				t.Fatal(err)
			}
			query := parsed.Query()
			query.Del("cursor")
			if query.Encode() != values.Encode() {
				t.Fatal("CRS/filter/paging parameters were lost", link)
			}
			if link.Rel == "next" {
				next = link.Href
			}
		}
	}
	if !reflect.DeepEqual(ids, []string{"1", "2", "3"}) || next != "" {
		t.Fatal("paging did not terminate with all selected features", ids, next)
	}
}

func TestHTTPFourPredicatesExecuteAsConjunction(t *testing.T) {
	server := fixtureServer(t)
	// This Reader explicitly maps datetime to its published observedAt value,
	// 2020-01-01T00:00:00Z; the protocol layer must not infer a time field.
	baseline := url.Values{
		"filter":   {"population=9007199254740994"},
		"bbox":     {"6.9,49.9,7.2,50.2"},
		"datetime": {"2019-12-31T23:59:59Z/2020-01-01T00:00:01Z"},
		"name":     {"sample-1"},
		"limit":    {"1"},
	}
	assertResult := func(t *testing.T, values url.Values, expected []string) {
		t.Helper()
		status, body, _ := fixtureGet(t, server, "/collections/regional/items", values)
		ids := []string{}
		for _, feature := range body.Features {
			ids = append(ids, feature.ID)
			if feature.Properties.Name != "sample-1" || feature.Properties.ObservedAt != "2020-01-01T00:00:00Z" || feature.Properties.Population.String() != "9007199254740994" {
				t.Fatal("wrong actually executed feature/properties", feature)
			}
		}
		if status != 200 || !reflect.DeepEqual(ids, expected) || body.NumberMatched == nil || *body.NumberMatched != uint64(len(expected)) || body.NumberReturned != uint64(len(expected)) {
			t.Fatal("AND execution/count mismatch", values, status, ids, body.NumberMatched, body.NumberReturned)
		}
		for _, link := range body.Links {
			if link.Rel == "next" {
				t.Fatal("zero/one matching feature unexpectedly continued paging", link)
			}
		}
	}
	t.Run("all-four-match", func(t *testing.T) { assertResult(t, baseline, []string{"1"}) })
	for _, predicate := range []struct{ name, nonmatching string }{
		{"filter", "population=9007199254740993"},
		{"bbox", "0,-1,1,1"},
		{"datetime", "2020-01-01T00:00:01Z/.."},
		{"name", "sample-2"},
	} {
		t.Run(predicate.name, func(t *testing.T) {
			values := url.Values{}
			for key, value := range baseline {
				values[key] = append([]string(nil), value...)
			}
			// Exactly one predicate is false; ignoring it would return feature1.
			values.Set(predicate.name, predicate.nonmatching)
			assertResult(t, values, []string{})
			// Removing just that false predicate restores the same known feature,
			// so empty responses alone cannot make this test pass vacuously.
			values.Del(predicate.name)
			assertResult(t, values, []string{"1"})
		})
	}
}

func TestHTTPOffsetExecutesAfterPredicatesAndRetainsLinks(t *testing.T) {
	server := fixtureServer(t)
	base := url.Values{"filter": {"population>=9007199254740994 OR name='雪+%27'"}, "filter-lang": {"cql2-text"}, "filter-crs": {common.CRS84}, "bbox": {"48,1,52,9"}, "bbox-crs": {epsg4326}, "datetime": {"2019-12-31T23:59:59Z/2020-01-01T00:00:01Z"}, "crs": {epsg3857}, "f": {common.GeoJSON}, "limit": {"1"}, "offset": {"0"}}
	position, ids := "0", []string{}
	for page := 0; page < 3; page++ {
		base.Set("offset", position)
		status, body, _ := fixtureGet(t, server, "/collections/regional/items", base)
		if status != 200 || len(body.Features) != 1 || body.NumberMatched == nil || *body.NumberMatched != 3 || body.NumberReturned != 1 {
			t.Fatal("offset was not applied to actual filtered features", status, body)
		}
		ids = append(ids, body.Features[0].ID)
		next, previous := "", ""
		for _, link := range body.Links {
			if link.Rel != "self" && link.Rel != "next" && link.Rel != "prev" {
				continue
			}
			parsed, err := url.Parse(link.Href)
			if err != nil {
				t.Fatal(err)
			}
			values := parsed.Query()
			gotOffset := values.Get("offset")
			values.Del("offset")
			want := url.Values{}
			for k, v := range base {
				if k != "offset" {
					want[k] = v
				}
			}
			if values.Encode() != want.Encode() || values.Has("cursor") || parsed.User != nil {
				t.Fatal("offset link lost/double-decoded a filter/CRS selector", link, values)
			}
			if link.Rel == "next" {
				next = gotOffset
			}
			if link.Rel == "prev" {
				previous = gotOffset
			}
		}
		if page == 2 && next != "" || page < 2 && next == "" || page == 0 && previous != "" || page > 0 && previous == "" {
			t.Fatal("wrong first/middle/full-terminal continuation", page, next, previous)
		}
		if page < 2 && next != strconv.Itoa(page+1) || page > 0 && previous != strconv.Itoa(page-1) {
			t.Fatal("incorrect next/previous position", page, next, previous)
		}
		position = next
	}
	if !reflect.DeepEqual(ids, []string{"1", "2", "3"}) {
		t.Fatal("wrong actual offset traversal", ids)
	}
	for _, offset := range []string{"3", "999", "18446744073709551615"} {
		base.Set("offset", offset)
		status, body, _ := fixtureGet(t, server, "/collections/regional/items", base)
		if status != 200 || len(body.Features) != 0 || body.NumberReturned != 0 {
			t.Fatal("beyond-end offset not empty", offset, status, body)
		}
		for _, link := range body.Links {
			if link.Rel == "next" {
				t.Fatal("false beyond-end next", link)
			}
		}
	}
	base.Set("offset", "0")
	base.Set("name", "sample-2")
	status, body, _ := fixtureGet(t, server, "/collections/regional/items", base)
	if status != 200 || len(body.Features) != 1 || body.Features[0].ID != "2" || body.NumberMatched == nil || *body.NumberMatched != 1 {
		t.Fatal("ordinary property predicate was lost before offset", status, body)
	}
	for _, link := range body.Links {
		if link.Rel != "self" {
			continue
		}
		parsed, err := url.Parse(link.Href)
		if err != nil || parsed.Query().Get("name") != "sample-2" {
			t.Fatal("ordinary property lost in link", link, err)
		}
	}
	base.Del("name")
	base.Set("filter", "population<0")
	status, body, _ = fixtureGet(t, server, "/collections/regional/items", base)
	if status != 200 || len(body.Features) != 0 || body.NumberMatched == nil || *body.NumberMatched != 0 {
		t.Fatal("empty predicate result not empty", status, body)
	}
	for _, query := range []string{"offset=-1", "offset=0&offset=1", "offset=18446744073709551616", "offset=0&cursor=", "cursor=not-a-fixture-position"} {
		response, err := server.Client().Get(server.URL + "/collections/regional/items?" + query)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if response.StatusCode != 400 || err != nil || closeErr != nil {
			t.Fatal("invalid paging token accepted", query, response.StatusCode, err, closeErr)
		}
	}
}

func TestHTTPUnsupportedCRSAndDimensionalRequests(t *testing.T) {
	server := fixtureServer(t)
	for _, tc := range []struct {
		path   string
		values url.Values
	}{
		{"/collections/world/items", url.Values{"crs": {epsg3857}}},
		{"/collections/world/items", url.Values{"bbox": {"-180,-90,180,90"}, "bbox-crs": {epsg4326}}},
		{"/collections/regional/items", url.Values{"crs": {"EPSG:3857"}}},
		{"/collections/regional/items/1", url.Values{"crs": {""}}},
		{"/collections/regional/items", url.Values{"crs": {epsg3857, epsg3857}}},
		{"/collections/regional/items", url.Values{"bbox": {"0,0,1,1"}, "bbox-crs": {epsg3857, epsg3857}}},
		{"/collections/regional/items", url.Values{"bbox": {"0,0,0,1,1,1"}}},
		{"/three-dimensional/collections/elevated/items", url.Values{"bbox": {"49,6,52,9"}, "bbox-crs": {epsg4979}}},
		{"/collections/regional/items", url.Values{"filter": {"name='sample-1'"}, "filter-crs": {epsg3857}}},
	} {
		status, _, headers := fixtureGet(t, server, tc.path, tc.values)
		if status != 400 || headers.Get("Content-Crs") != "" {
			t.Fatal("unsupported operation was ignored or mislabeled", tc, status, headers)
		}
	}
}

func TestHTTPHeadIncludesActualCRS(t *testing.T) {
	server := fixtureServer(t)
	request, err := http.NewRequest(http.MethodHead, server.URL+"/collections/regional/items/1?"+url.Values{"crs": {epsg4326}}.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(body) != 0 || response.StatusCode != 200 || response.Header.Get("Content-Crs") != "<"+epsg4326+">" {
		t.Fatal(response.StatusCode, response.Header, string(body), readErr, closeErr)
	}
}

type fixtureGeometry struct {
	Coordinates []float64 `json:"coordinates"`
}

type fixtureDocument struct {
	ID         string
	CRS        json.RawMessage
	Geometry   fixtureGeometry
	Properties struct {
		Location   fixtureGeometry
		Name       string
		Population json.Number
		ObservedAt string
	}
	Features       []fixtureDocument
	Links          []common.Link
	NumberMatched  *uint64
	NumberReturned uint64
}

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	handler, err := newHandler("http://" + server.Listener.Addr().String())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	server.Config.Handler = handler
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func fixtureGet(t *testing.T, server *httptest.Server, path string, values url.Values) (int, fixtureDocument, http.Header) {
	t.Helper()
	response, err := server.Client().Get(server.URL + path + "?" + values.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var document fixtureDocument
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, document, response.Header
}

func assertCoordinates(t *testing.T, actual, expected []float64) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatal("coordinate dimension mismatch", actual, expected)
	}
	for i, value := range expected {
		if math.Abs(actual[i]-value) > 1e-7 {
			t.Fatal("known coordinate mismatch", actual, expected)
		}
	}
}
