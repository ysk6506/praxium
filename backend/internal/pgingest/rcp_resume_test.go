package pgingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the computation of the objects does when it is called in the wrong order, stopped, taken over by another server, or given pages
// of another size (ADR-0115).

// rcpCountOf counts the rows of a table of the computation.
func rcpCountOf(s *rdfSetup, t *testing.T, table string) int {
	t.Helper()
	return s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d"."%s"`, s.ref.KGNo, table))
}

// rcpOnce does the pages of a world that is reset first, with the page size given, and gives what it staged.
func rcpOnce(w *expWorld, t *testing.T, lines int) (string, ingest.RecomputeCounts) {
	t.Helper()
	rcpReset(w.s, t, w.l)
	rcpExpand(w.s, t, w.l, true)
	counts := rcpDo(w.s, t, w.l, lines, 0)
	return rcpText(rcpStaged(w.s, t)), counts
}

// [REQ-937]
func TestRcpAPageIsDoneOnceAndAServerThatLostTheLeaseIsRefusedEvenToRead(t *testing.T) {
	w := rcpWorld(t, 0)
	st, ctx, s := w.s.e.st, w.s.e.ctx, w.s
	start := rcpStart(s, 3, 0)
	if _, err := st.RecomputePage(ctx, w.l); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page before the beginning: %v", err)
	}
	if err := st.BeginRecompute(ctx, w.l, ingest.RecomputeStart{Seen: "x"}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a time that is not a time: %v", err)
	}
	if err := st.BeginRecompute(ctx, w.l, start); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginRecompute(ctx, w.l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("the computation begun twice: %v", err)
	}
	page, err := st.RecomputePage(ctx, w.l)
	if err != nil {
		t.Fatal(err)
	}
	made, err := rcpMaker(t, s, 3, 0).Make(page)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutRecomputed(ctx, w.l, made); err != nil {
		t.Fatal(err)
	}
	before, _ := st.RecomputeReport(ctx, w.l)
	if err := st.PutRecomputed(ctx, w.l, made); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("the same page put twice: %v", err)
	}
	if after, _ := st.RecomputeReport(ctx, w.l); after != before {
		t.Errorf("a page that was refused counted: %+v and %+v", before, after)
	}
	if n := rcpCountOf(s, t, pgschema.TableStageObject); n != before.Objects || n == 0 {
		t.Errorf("%d objects are staged and the report says %d", n, before.Objects)
	}
	s.e.expire(t, w.l.No)
	if _, err := st.AcquireRun(ctx, w.l.No, "server-b", w.l.Term); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"begin":  func() error { return st.BeginRecompute(ctx, w.l, start) },
		"place":  func() error { _, err := st.RecomputePlace(ctx, w.l); return err },
		"page":   func() error { _, err := st.RecomputePage(ctx, w.l); return err },
		"put":    func() error { return st.PutRecomputed(ctx, w.l, ingest.RecomputedPage{Unit: 1}) },
		"report": func() error { _, err := st.RecomputeReport(ctx, w.l); return err },
	} {
		if err := call(); kernel.CodeOf(err) != ingest.CodeRunLeaseLost {
			t.Errorf("%s by the server that lost the lease: %v", name, err)
		}
	}
	if n := rcpCountOf(s, t, pgschema.TableStageObject); n != before.Objects {
		t.Errorf("a refused call changed the staged rows: %d and %d", n, before.Objects)
	}
}

// [REQ-938]
func TestRcpARunStoppedBetweenPagesGoesOnFromTheNextPageAndStagesWhatOneThatDidNotStopStages(t *testing.T) {
	w := rcpWorld(t, 0)
	s := w.s
	want, wantCounts := rcpOnce(w, t, 3)
	if wantCounts.Lines < 7 {
		t.Fatalf("%d lines are too few to tell the pages apart", wantCounts.Lines)
	}
	rcpReset(s, t, w.l)
	rcpExpand(s, t, w.l, true)
	st, ctx := s.e.st, s.e.ctx
	if err := st.BeginRecompute(ctx, w.l, rcpStart(s, 3, 0)); err != nil {
		t.Fatal(err)
	}
	maker := rcpMaker(t, s, 3, 0)
	for i := 0; i < 2; i++ {
		page, err := st.RecomputePage(ctx, w.l)
		if err != nil || page.Unit != i {
			t.Fatalf("page %d: %v, it is the page %d", i, err, page.Unit)
		}
		made, err := maker.Make(page)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.PutRecomputed(ctx, w.l, made); err != nil {
			t.Fatal(err)
		}
	}
	// the server stops here; the next one begins with the place and goes on
	got, err := ingest.RunRecompute(ctx, st, w.l, rcpStart(s, 3, 0), rcpMaker(t, s, 3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != wantCounts {
		t.Errorf("a run that stopped counted %+v, one that did not %+v", got, wantCounts)
	}
	if text := rcpText(rcpStaged(s, t)); text != want {
		t.Errorf("a run that stopped staged:\n%s\none that did not:\n%s", text, want)
	}
	for table, n := range map[string]int{pgschema.TableStageObject: got.Objects, pgschema.TableStageVolat: got.Volatile, pgschema.TableStageIdent: got.Idents} {
		if c := rcpCountOf(s, t, table); c != n {
			t.Errorf("%s has %d rows and the report says %d", table, c, n)
		}
	}
	if place, _ := st.RecomputePlace(ctx, w.l); !place.Done() {
		t.Errorf("%+v", place)
	}
}

// [REQ-938]
func TestRcpThePageSizeDoesNotChangeWhatIsStaged(t *testing.T) {
	w := rcpWorld(t, 0)
	want, wantCounts := rcpOnce(w, t, 1000)
	if wantCounts.Objects == 0 {
		t.Fatal("nothing was staged")
	}
	for _, lines := range []int{1, 3, 7} {
		got, counts := rcpOnce(w, t, lines)
		if got != want {
			t.Errorf("pages of %d lines stage:\n%s\npages of 1000 lines:\n%s", lines, got, want)
		}
		if counts != wantCounts {
			t.Errorf("pages of %d lines count %+v, pages of 1000 lines %+v", lines, counts, wantCounts)
		}
		if parts := w.s.e.count(t, `SELECT calc_part FROM sync_run WHERE run_no = $1`, w.l.No); parts < 2 {
			t.Errorf("pages of %d lines were %d", lines, parts)
		}
	}
}

// [REQ-939]
func TestRcpAnObjectThatHasMoreLinesThanTheLimitIsLeftAsItIsAndCounted(t *testing.T) {
	const m = 20
	s, l := rcpShared(t, m)
	counts := rcpDo(s, t, l, 1000, 3)
	if counts.Objects != m || counts.TooMany != 1 || counts.Left != 1 || counts.Lines != 3*m {
		t.Errorf("%+v", counts)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".stage_object WHERE obj_no = $1`, s.ref.KGNo), m+1); n != 0 {
		t.Errorf("the object that has too many lines was staged")
	}
	if n := rcpCountOf(s, t, pgschema.TableStageObject); n != m {
		t.Errorf("%d objects are staged", n)
	}
	// the records under the objects that were computed are records of the run now; the ones under the object that was left are not
	if n := rcpCountOf(s, t, pgschema.TableCalcRecord); n != 3*m || counts.Added != 3*m {
		t.Errorf("%d records were made records of the run (the report says %d), want %d", n, counts.Added, 3*m)
	}
	if ingest.DefaultRecomputeObjectMax < ingest.DefaultRecomputeLines {
		t.Errorf("the limit of an object is %d lines, less than a page of %d", ingest.DefaultRecomputeObjectMax, ingest.DefaultRecomputeLines)
	}
}

// [REQ-940]
func TestRcpTheReportIsWhatTheStatementsWroteAndAnObjectWithoutRecordsOrWithOnesThatCannotBeReadIsLeft(t *testing.T) {
	w := rcpWorld(t, 0)
	s := w.s
	k := s.ref.KGNo
	rcpReset(s, t, w.l)
	rcpExpand(s, t, w.l, true)
	base := rcpDo(s, t, w.l, 0, 0)
	rcpReset(s, t, w.l)
	rcpExpand(s, t, w.l, true)
	problems := rcpCountOf(s, t, pgschema.TableCalcProblem)
	// an object with no record under it that is no candidate, and one with a record of a source that has no plan
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".object (obj_no, ref, type_no, name, min_grade, seq_from) VALUES (900001, 'orphan-1', 1, 'orphan', 0, 1), (900002, 'orphan-2', 1, 'orphan', 0, 1)`, k))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_object (obj_no, why) VALUES (900001, %d), (900002, %d)`, k, pgschema.ObjectWhyOldRef, pgschema.ObjectWhyOldRef))
	counts := rcpDo(s, t, w.l, 0, 0)
	if counts.Empty != base.Empty+2 || counts.Objects != base.Objects || counts.Objects == 0 {
		t.Errorf("%+v", counts)
	}
	if n := rcpCountOf(s, t, pgschema.TableStageObject); n != counts.Objects {
		t.Errorf("%d objects are staged and the report says %d", n, counts.Objects)
	}
	if n := rcpCountOf(s, t, pgschema.TableStageVolat); n != counts.Volatile {
		t.Errorf("%d volatile values are staged and the report says %d", n, counts.Volatile)
	}
	if n := rcpCountOf(s, t, pgschema.TableStageIdent); n != counts.Idents {
		t.Errorf("%d identifiers are staged and the report says %d", n, counts.Idents)
	}
	if n := rcpCountOf(s, t, pgschema.TableCalcProblem) - problems; n != counts.Problems {
		t.Errorf("%d problems were written and the report says %d", n, counts.Problems)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".stage_object WHERE obj_no IN (900001, 900002)`, k)); n != 0 {
		t.Errorf("an object with no record under it was staged")
	}
	if counts.Retiring != s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_object WHERE why = %d`, k, pgschema.ObjectWhyRetire)) {
		t.Errorf("the candidates for retirement are %d", counts.Retiring)
	}
	if got := strings.Count(rcpText(rcpStaged(s, t)), "\n") + 1; got != counts.Objects {
		t.Errorf("%d rows were read back", got)
	}
}
