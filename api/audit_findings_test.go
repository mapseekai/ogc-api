package api_test

import (
	"context"
	"testing"

	"github.com/mapseekai/ogc-api/api"
	"github.com/mapseekai/ogc-api/processes"
)

type auditJobs struct{}

func (auditJobs) Status(context.Context, string) (processes.Status, error) {
	return processes.Status{JobID: "job-1", ProcessID: "echo", Type: "process", Status: "running"}, nil
}
func (auditJobs) Results(context.Context, string) (processes.JobResults, error) {
	return processes.JobResults{}, nil
}
func (auditJobs) Dismiss(context.Context, string) (processes.Status, error) {
	return processes.Status{JobID: "job-1", ProcessID: "hidden-process", Type: "process", Status: "dismissed", Message: "hidden provider details"}, nil
}
func TestAuditDismissMustPreserveVisibleProcessIdentity(t *testing.T) {
	c := catalog(&memoryFeatures{}, tileReader("tile"))
	c.Processes = []processes.Service{echoProcess()}
	c.Jobs = &processes.Jobs{Reader: auditJobs{}, Dismisser: auditJobs{}}
	e := engine(t, c, 1)
	status, data, _ := read(t, e, api.Input{Method: "DELETE", EscapedPath: "/ogc/jobs/job-1"})
	if status == 200 {
		t.Fatalf("dismissed status from an unlisted process escaped validation: status=%d body=%s", status, data)
	}
}
func TestAuditWildcardWithoutETag(t *testing.T) {
	e := engine(t, catalog(&memoryFeatures{}, tileReader("tile")), 1)
	for _, method := range []string{"GET", "HEAD"} {
		plan, err := e.Handle(context.Background(), api.Input{Method: method, EscapedPath: "/ogc/collections/roads/tiles/local/L00/0/0", IfNoneMatch: "*"})
		if err != nil {
			t.Fatal(err)
		}
		_ = plan.Finish(0, nil)
		if plan.Status != 304 {
			t.Errorf("%s existing representation without ETag returned %d, want 304", method, plan.Status)
		}
	}
}
func TestAuditRejectUnroutableMatrixID(t *testing.T) {
	for _, id := range []string{".", "..", "bad\n"} {
		c := catalog(&memoryFeatures{}, tileReader("tile"))
		c.MatrixSets[0].Matrices[0].ID = id
		snapshot, err := api.NewSnapshot(c)
		if err == nil {
			t.Errorf("snapshot accepted matrix ID %q that resourcePath rejects (snapshot nonnil=%v)", id, snapshot != nil)
		}
	}
}
