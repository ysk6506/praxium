package pgingest

import (
	"database/sql"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The cleaning when a statement fails or the record of the run is not what the cleaning wrote (ADR-0128).

// stlFailing is a hook that breaks the transaction of the statement it is told of, before the statement runs.
func stlFailing(what *string) identObserver {
	return func(tx *sql.Tx, w, _ string, _ []any) {
		if w == *what {
			_, _ = tx.Exec("SELECT 1 / 0")
		}
	}
}

// [REQ-986]
func TestStlAStatementThatFailsLeavesThePlaceWhereItWasAndTheCleaningGoesOnToWhatOneThatDidNotFailLeaves(t *testing.T) {
	steps := stlSteps()
	ref := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	for n := range steps[:2] {
		stlWhole(ref, t, steps[n], stlAt(n), 2)
	}
	stlWhole(s, t, steps[0], stlAt(0), 2)
	l := stlCompute(s, t, steps[1], stlAt(1), false)
	stlSettling(s, t, l)
	stlGraph(s, t)
	var target string
	s.e.st.stlSeen = stlFailing(&target)
	// the statements in the order the cleaning runs them: each fails once, and the next try goes past it to the next
	for _, what := range []string{"take away the problems of what the run computed", "write the problems the receiving found", "take away the references the run wrote again",
		"write the references the run computed", "write the problems the run computed", "delete the records that are closed", "write the records received now",
		"write the fate of the other records", "delete the waits the source had", "write the waits of the run", "write the definition set the source applied",
		"write the time of the datasets read whole"} {
		target = what
		before, err := s.e.st.SettlePlace(s.e.ctx, l)
		if err != nil && !pgiCodeIs(err, ingest.CodeRunStage) {
			t.Fatal(err)
		}
		if _, err := ingest.RunSettle(s.e.ctx, s.e.st, l, stlStart(s, 1000), &stlConfirms{}); err == nil {
			t.Fatalf("%s: the cleaning did not fail", what)
		}
		after, err := s.e.st.SettlePlace(s.e.ctx, l)
		if err != nil {
			t.Fatal(err)
		}
		if after.Job < before.Job {
			t.Errorf("%s: the place went back from %+v to %+v", what, before, after)
		}
	}
	s.e.st.stlSeen = nil
	if _, err := ingest.RunSettle(s.e.ctx, s.e.st, l, stlStart(s, 1000), &stlConfirms{}); err != nil {
		t.Fatal(err)
	}
	stlSame(t, "after the failures", stlDump(ref, t, true), stlDump(s, t, true), "raw", "wait", "ref", "problem")
}

// [REQ-986] [REQ-988]
func TestStlARecordOfTheRunThatTheCleaningDidNotWriteIsRefused(t *testing.T) {
	f := stlNewFix(t)
	s, l := f.s, f.l
	if err := s.e.st.BeginSettle(s.e.ctx, l, stlStart(s, 2)); err != nil {
		t.Fatal(err)
	}
	set := func(job, part int, counts string) {
		s.e.exec(t, `UPDATE sync_run SET settle_job = $2, settle_part = $3, counts = counts || $4::jsonb WHERE run_no = $1`, l.No, job, part, counts)
	}
	// a job the cleaning does not have, and a table to measure past the last
	set(9, 0, `{}`)
	if _, err := s.e.st.SettleBucket(s.e.ctx, l, 9, 0); !pgiCodeIs(err, kernel.CodeInternal) {
		t.Errorf("a job the cleaning does not have: %v", err)
	}
	set(int(ingest.SettleJobAnalyze), 4, `{}`)
	if _, err := s.e.st.SettleBucket(s.e.ctx, l, ingest.SettleJobAnalyze, 4); !pgiCodeIs(err, kernel.CodeInternal) {
		t.Errorf("a table past the last: %v", err)
	}
	// a place that is not one the cleaning wrote
	set(int(ingest.SettleJobRefs), 0, `{"settle":5}`)
	if _, err := s.e.st.SettleBucket(s.e.ctx, l, ingest.SettleJobRefs, 0); !pgiCodeIs(err, kernel.CodeInternal) {
		t.Errorf("a place that is not the cleaning's: %v", err)
	}
	set(0, 0, `{"isolate":5}`)
	if err := s.e.st.BeginSettle(s.e.ctx, l, stlStart(s, 2)); !pgiCodeIs(err, kernel.CodeInternal) {
		t.Errorf("counts that are not the steps': %v", err)
	}
	if _, err := s.e.st.RunState(s.e.ctx, l.No); !pgiCodeIs(err, kernel.CodeInternal) {
		t.Errorf("the state of a run whose counts are not the steps': %v", err)
	}
	// a run that is not there any more
	s.e.exec(t, `DELETE FROM sync_run WHERE run_no = $1`, l.No)
	if _, err := s.e.st.SettlePlace(s.e.ctx, l); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
		t.Errorf("the place of a run that is not there: %v", err)
	}
}
