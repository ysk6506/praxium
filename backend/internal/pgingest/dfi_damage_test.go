package pgingest

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The difference on a server whose tables were damaged, and on a run whose place was (ADR-0127): a unit that needs a table that is gone fails
// with an error and changes nothing, so the run goes on once the table is back; a place that cannot be read is an error and not a guess.

// dfiFailAt is a hook that makes the n-th statement of a unit fail (the transaction is made to fail just before it), and counts the
// statements it was told.
type dfiFailAt struct {
	n, seen int
	what    string
}

func (h *dfiFailAt) dfiHook(tx *sql.Tx, what, _ string, _ []any) {
	h.seen++
	if h.seen == h.n {
		h.what = what
		_, _ = tx.Exec("SELECT 1 / 0") // the error is the point: the statement after it fails in a transaction that failed
	}
}

// [REQ-968]
func TestDfiAUnitWhoseStatementFailsChangesNothingAndTheRunGoesOnFromIt(t *testing.T) {
	f := dfiAll(t, false)
	ctx, st, l := f.s.e.ctx, f.s.e.st, f.l
	if err := st.BeginDiff(ctx, l, ingest.DiffStart{Rows: 4, ChunkRows: 3}); err != nil {
		t.Fatal(err)
	}
	failed := map[string]bool{}
	for units := 0; units < 100; units++ {
		place, err := st.DiffPlace(ctx, l)
		if err != nil {
			t.Fatal(err)
		}
		if place.Done() {
			break
		}
		// the unit fails at each of its statements in turn, and is done when none is left to fail at
		for n := 1; ; n++ {
			h := &dfiFailAt{n: n}
			st.diffSeen = h.dfiHook
			err := st.DiffStep(ctx, l)
			st.diffSeen = nil
			if h.what == "" {
				if err != nil {
					t.Fatal(err)
				}
				break
			}
			if err == nil {
				t.Fatalf("the unit %d went on after %q failed", place.Unit, h.what)
			}
			failed[h.what] = true
			if after, perr := st.DiffPlace(ctx, l); perr != nil || after.Unit != place.Unit {
				t.Fatalf("a unit that failed at %q moved the place to %+v (%v)", h.what, after, perr)
			}
		}
	}
	if len(failed) < 15 {
		t.Errorf("the statements that failed are %v", failed)
	}
	// the run that went through the failures made the difference a run without them makes
	got, err := st.DiffReport(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	g := dfiAll(t, false)
	if want := dfiDo(g.s, t, g.l, 4, 3); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the difference that went through the failures is %+v, want %+v", got, want)
	}
}

// [REQ-968]
func TestDfiAPlaceThatCannotBeReadIsAnErrorAndNotAGuess(t *testing.T) {
	f := dfiAll(t, false)
	ctx, st, l := f.s.e.ctx, f.s.e.st, f.l
	set := func(path, value string) {
		f.s.e.exec(t, `UPDATE sync_run SET counts = jsonb_set(counts, $2::text[], $3::jsonb) WHERE run_no = $1`, l.No, path, value)
	}
	internal := func(what string, err error) {
		t.Helper()
		if kernel.CodeOf(err) != kernel.CodeInternal {
			t.Errorf("%s: %v", what, err)
		}
	}
	set("{isolate}", `"not a place"`)
	internal("the place of the isolation", st.BeginDiff(ctx, l, ingest.DiffStart{}))
	set("{isolate}", `{"finished":true}`)
	// a KG that is being deleted takes no call of the step
	f.s.e.exec(t, `INSERT INTO kg_deleting (kg_id, started_at) VALUES ($1, now())`, f.s.kg.ID.String())
	if err := st.BeginDiff(ctx, l, ingest.DiffStart{}); kernel.CodeOf(err) != graph.CodeKGNotFound {
		t.Errorf("a KG that is being deleted: %v", err)
	}
	if _, err := st.DiffPlace(ctx, l); kernel.CodeOf(err) != graph.CodeKGNotFound {
		t.Errorf("the place in a KG that is being deleted: %v", err)
	}
	f.s.e.exec(t, `DELETE FROM kg_deleting`)
	if err := st.BeginDiff(ctx, l, ingest.DiffStart{Rows: 2, ChunkRows: 2}); err != nil {
		t.Fatal(err)
	}
	good := f.s.e.text(t, `SELECT counts->>'diff' FROM sync_run WHERE run_no = $1`, l.No)
	restore := func() { set("{diff}", good) }
	set("{diff}", `"not a place"`)
	_, err := st.DiffPlace(ctx, l)
	internal("the place of the difference", err)
	restore()
	// the place in the keys, in the order and of the print, and a count, that are not what the step writes
	for _, c := range []struct{ path, value, phase string }{
		{"{diff,key,sig}", `"zz"`, ""}, {"{diff,tie}", `"zz"`, ingest.DiffPhaseOrder}, {"{diff,chain}", `"zz"`, ingest.DiffPhasePrint},
		{"{diff,tally}", `{"1/2":3}`, ingest.DiffPhasePlan}, {"{diff,tally}", `{"1/x/0":3}`, ingest.DiffPhasePlan},
	} {
		set("{diff,phase}", fmt.Sprintf("%q", c.phase))
		if c.phase == "" {
			set("{diff,phase}", fmt.Sprintf("%q", ingest.DiffPhaseRows))
			set("{diff,part}", fmt.Sprintf("%q", diffPartKeys))
		}
		set(c.path, c.value)
		internal(c.path+"="+c.value, st.DiffStep(ctx, l))
		restore()
	}
	zero := fmt.Sprintf("%q", strings.Repeat("0", 64))
	set("{diff,finished}", "true")
	set("{diff,chain}", `"zz"`)
	_, err = st.DiffReport(ctx, l)
	internal("a report with a chain that is not one", err)
	set("{diff,chain}", zero)
	set("{diff,tally}", `{"x":1}`)
	_, err = st.DiffReport(ctx, l)
	internal("a report with a count that is not one", err)
	restore()
	// a chunk that is not there
	set("{diff,phase}", fmt.Sprintf("%q", ingest.DiffPhasePrint))
	set("{diff,chunks}", "1")
	set("{diff,next}", "7")
	set("{diff,chain}", zero)
	internal("a chunk that is not there", st.DiffStep(ctx, l))
	restore()
	// a place that has no counts yet counts from none
	set("{diff,tally}", "null")
	// a plan that does not agree with the rows there are
	for i := 0; i < 100; i++ {
		place, err := st.DiffPlace(ctx, l)
		if err != nil {
			t.Fatal(err)
		}
		if place.Phase == ingest.DiffPhaseOrder {
			break
		}
		if err := st.DiffStep(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	f.s.e.exec(t, f.dfiQ(`UPDATE {k}.calc_diffplan SET lines = lines + 100`))
	var last error
	for i := 0; i < 100 && last == nil; i++ {
		last = st.DiffStep(ctx, l)
	}
	internal("a plan that counts more rows than there are", last)
}
