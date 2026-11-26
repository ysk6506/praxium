package pgingest

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-890] [REQ-896]
func TestExpTheExpansionDoesNotBeginOnARunWhoseIdentificationIsNotDoneAndTheComputationOfEverythingGoesByUnitsOfRecords(t *testing.T) {
	w := expMakeWorld(t, pgiMappingJSON, 0, false)
	st, ctx := w.s.e.st, w.s.e.ctx
	start := ingest.ExpandStart{Cards: expCards()}
	// the job of the run is not the identification
	w.s.e.exec(t, `UPDATE sync_run SET calc_job = $2 WHERE run_no = $1`, w.l.No, pgschema.CalcJobRawDiff)
	if err := st.BeginExpand(ctx, w.l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a run that is not at the identification: %v", err)
	}
	// the identification has not come to its end
	w.s.e.exec(t, `UPDATE sync_run SET calc_job = $2, counts = counts || '{"identify":{"phase":"slot"}}'::jsonb WHERE run_no = $1`, w.l.No, pgschema.CalcJobIdentify)
	if err := st.BeginExpand(ctx, w.l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a run whose identification is not done: %v", err)
	}
	// the place of the identification cannot be read
	w.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"identify":"x"}'::jsonb WHERE run_no = $1`, w.l.No)
	if err := st.BeginExpand(ctx, w.l, start); err == nil {
		t.Errorf("a place of the identification that is not one")
	}
	// the tables of the computation are another run's
	w.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"identify":{"phase":"done"}}'::jsonb WHERE run_no = $1`, w.l.No)
	w.s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".calc_owner`, w.s.ref.KGNo))
	if err := st.BeginExpand(ctx, w.l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("tables of another run: %v", err)
	}
	w.s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, w.s.ref.KGNo), w.l.No)
	// a place of the expansion that is not one, and a rule that is not one
	expReset(w.s, t, w.l)
	if err := st.BeginExpand(ctx, w.l, start); err != nil {
		t.Fatal(err)
	}
	w.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"expand":{"rule":"zzz"}}'::jsonb WHERE run_no = $1`, w.l.No)
	if err := st.ExpandStep(ctx, w.l); err == nil {
		t.Errorf("a rule that is not one")
	}
	w.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"expand":"x"}'::jsonb WHERE run_no = $1`, w.l.No)
	if _, err := st.ExpandPlace(ctx, w.l); err == nil {
		t.Errorf("a place of the expansion that is not one")
	}
	// the computation of everything, a few records a unit, makes the same records as one unit does
	w.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"identify":{"phase":"done"}}'::jsonb WHERE run_no = $1`, w.l.No)
	_, one := w.expRun(t, 0, true)
	counts, few := w.expRun(t, 2, true)
	if !few.expSame(one) || counts.Everything == 0 {
		t.Errorf("units of two records: %s (%+v), and of one unit: %s", few.expText(), counts, one.expText())
	}
}

// [REQ-890] [REQ-896]
func TestExpAStatementThatFailsInAUnitLeavesTheUnitUndoneAndTheRunGoesOnFromIt(t *testing.T) {
	w := expMakeWorld(t, expRetiringMapping(t), 0, false)
	_, want := w.expRun(t, 0, false)
	st, ctx := w.s.e.st, w.s.e.ctx
	for _, full := range []bool{false, true} {
		for k := 1; k < 60 && (!full || k <= 4); k++ {
			expReset(w.s, t, w.l)
			calls, broke := 0, false
			st.expTest = &expHook{Seen: func(tx *sql.Tx, what, query string, args []any) {
				if calls++; calls == k {
					broke = true
					_, _ = tx.ExecContext(ctx, "SELECT 1/0")
				}
			}}
			start := ingest.ExpandStart{Full: full, Rows: 2, Cards: expCards()}
			if !full {
				start.Rows = 0
			}
			if err := st.BeginExpand(ctx, w.l, start); err != nil {
				t.Fatal(err)
			}
			failed := false
			for place, err := st.ExpandPlace(ctx, w.l); !place.Finished && err == nil; place, err = st.ExpandPlace(ctx, w.l) {
				if err := st.ExpandStep(ctx, w.l); err != nil {
					failed = true
					break
				}
			}
			if !broke {
				break
			}
			if !failed {
				t.Fatalf("the statement %d was broken and the unit went on", k)
			}
			// the unit that failed wrote nothing: the run goes on from the same place and does what it does without the failure
			st.expTest = nil
			if _, err := ingest.RunExpand(ctx, st, w.l, start); err != nil {
				t.Fatalf("after the statement %d: %v", k, err)
			}
			if full || k%6 != 1 {
				continue
			}
			if got := expSnapOf(w.s, t); !got.expSame(want) {
				t.Errorf("after the statement %d failed: %s, wanted %s", k, got.expText(), want.expText())
			}
		}
	}
}
