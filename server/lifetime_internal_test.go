package server

import (
	"context"
	"testing"
	"time"

	"github.com/mapseekai/ogc-api/owscore"
	"github.com/mapseekai/ogc-api/response"
	"github.com/mapseekai/ogc-api/wmts"
)

func TestErrorDocumentHasBoundedIndependentContext(t *testing.T) {
	snapshot, err := NewSnapshot(nil, &wmts.Catalog{Service: owscore.Service{Title: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(Config{
		Catalog: Static(snapshot), WMTSURL: "https://example.test/wmts", Timeout: time.Nanosecond,
		Handlers: Handlers{GetTile: func(context.Context, *wmts.TileRequest) (response.Payload, error) {
			t.Fatal("invalid request reached provider")
			return response.Payload{}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.Handle(context.Background(), Input{Method: "GET", Protocol: owscore.WMTS})
	if err != nil {
		t.Fatal(err)
	}
	body := plan.Body.(*contextBody)
	deadline, ok := body.Context.Deadline()
	if !ok || time.Until(deadline) > time.Second || body.Context.Err() != nil {
		t.Errorf("unbounded or expired error context: deadline=%v error=%v", deadline, body.Context.Err())
	}
	if err := plan.Finish(0, nil); err != nil {
		t.Fatal(err)
	}
	if body.Context.Err() != context.Canceled {
		t.Fatal("error-document context was not released")
	}
}
