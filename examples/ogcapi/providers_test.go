package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"io"
	"testing"

	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/coverages"
	"github.com/mapseekai/ogc-api/maps"
)

func TestOriginPortrayal(t *testing.T) {
	service, err := (maps.Service{Renderer: maps.RendererFunc(renderMap), Formats: []string{"image/png"}, DefaultWidth: 64, DefaultHeight: 32}).Prepare()
	if err != nil {
		t.Fatal(err)
	}
	for _, bbox := range []string{"-180,-90,180,90", "10,10,20,20"} {
		request, err := maps.ParseQuery(common.Query{"bbox": bbox, "transparent": "true"}, service)
		if err != nil {
			t.Fatal(err)
		}
		request.Format, request.Layers = "image/png", []maps.Layer{{ID: "world"}}
		result, err := renderMap(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(result.Content.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := result.Content.Close(); err != nil {
			t.Fatal(err)
		}
		picture, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, alpha := picture.At(32, 16).RGBA()
		if (alpha != 0) != (bbox == "-180,-90,180,90") {
			t.Fatal("map point did not follow its geographic position", bbox, alpha)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := renderMap(ctx, maps.Request{}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestCoverageMetadataMatchesCell(t *testing.T) {
	result, err := readCoverage(context.Background(), coverages.Request{Format: common.JSON})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	var coverage struct {
		DomainSet json.RawMessage
		RangeType json.RawMessage
		RangeSet  struct{ DataBlock struct{ Values []float64 } }
	}
	if err := json.Unmarshal(data, &coverage); err != nil {
		t.Fatal(err)
	}
	if len(coverage.RangeSet.DataBlock.Values) != 1 || coverage.RangeSet.DataBlock.Values[0] != 20 || !bytes.Equal(coverage.DomainSet, []byte(sampleDomainSet)) || !bytes.Equal(coverage.RangeType, []byte(sampleRangeType)) {
		t.Fatal("CIS values and published metadata differ", string(data))
	}
	if _, err := readCoverage(context.Background(), coverages.Request{Format: common.JSON, BBox: []float64{10, 10, 20, 20}}); err == nil {
		t.Fatal("subset excluding cell returned values")
	}
	for _, coordinate := range []string{"0.0", "-0", "0e0"} {
		result, err := readCoverage(context.Background(), coverages.Request{Format: common.JSON, Subsets: []coverages.Subset{{Axis: "x", Lower: coordinate, Slice: true}}})
		if err != nil {
			t.Fatal("equivalent numeric slice excluded the cell", coordinate, err)
		}
		if err := result.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
