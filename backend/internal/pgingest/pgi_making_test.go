package pgingest

import (
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-920]
func TestPgiAKGBeingMadeRefusesTheIngestionUntilItsMakingEnds(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	// the mark of a making that has not ended (ADR-0126): the KG is closed as one being deleted is.
	e.exec(t, `INSERT INTO kg_making (kg_id, expires_at) VALUES ($1, now() + interval '1 hour')`, kg.ID.String())
	if _, err := e.st.AttachSource(e.ctx, kg.ID, plan.Source.ID, []string{"ds_a"}); !pgiCodeIs(err, graph.CodeKGNotFound) {
		t.Errorf("a source is put to a KG being made: %v", err)
	}
	if _, err := e.st.OpenRun(e.ctx, ingest.RunOpen{KG: kg.ID, Source: plan.Source.ID, Owner: "server-a", Term: time.Minute}); !pgiCodeIs(err, graph.CodeKGNotFound) {
		t.Errorf("a run is opened for a KG being made: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM source_state`); n != 0 {
		t.Errorf("the refused calls left %d sources", n)
	}
	e.exec(t, `DELETE FROM kg_making WHERE kg_id = $1`, kg.ID.String())
	e.attach(t, kg, plan)
	e.open(t, kg, plan.Source.ID, "server-a")
}
