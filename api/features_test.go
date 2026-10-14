package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
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
