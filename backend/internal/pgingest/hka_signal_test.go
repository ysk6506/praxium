package pgingest

import "testing"

// [REQ-863]
func TestHkaASignalIsMarkedOnceOnEveryRowOfTheSourceAndNeverTwice(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kgA, kgB := e.kg(t, 1), e.kg(t, 2)
	e.attach(t, kgA, plan)
	e.attach(t, kgB, plan)
	if n := e.count(t, `SELECT count(*) FROM source_state WHERE signal_pending_at IS NOT NULL`); n != 0 {
		t.Fatalf("%d sources have a signal before any came", n)
	}
	if err := e.st.Signal(e.ctx, plan.Source.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM source_state WHERE source_id = $1 AND signal_pending_at IS NOT NULL`, plan.Source.ID); n != 2 {
		t.Errorf("%d of the 2 rows of the source have the mark", n)
	}
	first := e.text(t, `SELECT min(signal_pending_at)::text FROM source_state WHERE source_id = $1`, plan.Source.ID)
	// A signal that comes again changes no row: the time is the time of the first one.
	e.exec(t, `SELECT pg_sleep(0.05)`)
	if err := e.st.Signal(e.ctx, plan.Source.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.text(t, `SELECT max(signal_pending_at)::text FROM source_state WHERE source_id = $1`, plan.Source.ID); got != first {
		t.Errorf("the time of the mark moved from %s to %s", first, got)
	}
	// A source of no KG has nothing to mark and the call is not an error; another source is not touched.
	if err := e.st.Signal(e.ctx, "no_such_source"); err != nil {
		t.Errorf("a source of no KG: %v", err)
	}
	e.exec(t, `UPDATE source_state SET signal_pending_at = NULL`)
	if err := e.st.Signal(e.ctx, "other"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM source_state WHERE signal_pending_at IS NOT NULL`); n != 0 {
		t.Errorf("a signal of another source marked %d rows", n)
	}
}
