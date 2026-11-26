package pgingest

import (
	"errors"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-684]
func TestPgiAKGBeingDeletedRefusesTheIngestionFromTheFirstStepOfTheDeleteUntilItEnds(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	stop := errors.New("the server stops here")
	// the delete stops in the transaction that takes the first source off: the mark stays, and so do the source and its run.
	e.pst.StopDeleteAt("source", stop)
	if err := e.pst.DeleteKG(e.ctx, kg.ID); !errors.Is(err, stop) {
		t.Fatalf("the delete that stops: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM sync_run WHERE kg_id = $1`, kg.ID.String()); n != 1 {
		t.Fatalf("%d runs", n)
	}
	if _, err := e.st.Receive(e.ctx, lease, ingest.Intake{Reader: pgiAll(), Plan: plan}); !pgiCodeIs(err, graph.CodeKGNotFound) {
		t.Errorf("the run that was open receives: %v", err)
	}
	if _, err := e.st.AttachSource(e.ctx, kg.ID, "another", []string{"ds_a"}); !pgiCodeIs(err, graph.CodeKGNotFound) {
		t.Errorf("a source is put to the KG: %v", err)
	}
	if _, err := e.st.OpenRun(e.ctx, ingest.RunOpen{KG: kg.ID, Source: plan.Source.ID, Owner: "server-b", Term: time.Minute}); !pgiCodeIs(err, graph.CodeKGNotFound) {
		t.Errorf("a run is opened: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM pg_tables WHERE schemaname = 's_1' AND tablename LIKE 'stage_record%'`); n < 2 {
		t.Errorf("the refused calls changed the tables: %d", n)
	}
	if err := e.pst.DeleteKG(e.ctx, kg.ID); err != nil {
		t.Fatalf("the delete called again: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM sync_run`) + e.count(t, `SELECT count(*) FROM source_state`) + e.count(t, `SELECT count(*) FROM kg_deleting`); n != 0 {
		t.Errorf("%d rows of the KG stayed", n)
	}
}
