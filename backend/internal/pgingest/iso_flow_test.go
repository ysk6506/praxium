package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// How the fifth step goes on (ADR-0123): it begins after the fourth step, a page is put once, a server that lost its lease is refused, a
// server that takes the run over goes on from the page it stands at, and the outcome hangs neither on the size of a page nor on the order
// the objects and the links got their numbers in.

// isoPages does n pages of the step with the lease, from where the run stands, and says the number of the page it is at.
func isoPages(f *isoFix, t *testing.T, l ingest.RunLease, n, lines int) int {
	t.Helper()
	ctx, st := f.s.e.ctx, f.s.e.st
	m := ingest.NewIsolationMaker(f.m.reg)
	place, err := st.IsolatePlace(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if !place.Begun {
		if err := st.BeginIsolate(ctx, l, ingest.IsolateStart{Lines: lines, Cards: ingest.ExpandCards(f.m.reg)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		if place, err = st.IsolatePlace(ctx, l); err != nil {
			t.Fatal(err)
		}
		if place.Done() {
			break
		}
		page, err := st.IsolatePage(ctx, l)
		if err != nil {
			t.Fatal(err)
		}
		made, err := m.MakeIsolated(page)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.PutIsolated(ctx, l, made); err != nil {
			t.Fatal(err)
		}
	}
	place, err = st.IsolatePlace(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	return place.Unit
}

// isoResume does the step of a world once without a stop and then, for each place to stop at, again with a stop there: the server lets
// its lease run out, another takes the run over and does the rest, and the outcome is the same.
func isoResume(t *testing.T, world func(*testing.T) *isoFix, lines int) {
	t.Helper()
	f := world(t)
	f.isoRun(t, lines)
	want, units := f.isoState(t), isoPages(f, t, f.l, 0, lines)
	if units < 4 {
		t.Fatalf("the step had %d pages", units)
	}
	for _, at := range []int{1, units / 2, units - 1} {
		g := world(t)
		isoPages(g, t, g.l, at, lines)
		next := rclTakeOver(g.s, t, g.l, "server-b")
		if _, err := g.s.e.st.IsolatePlace(g.s.e.ctx, g.l); kernel.CodeOf(err) != ingest.CodeRunLeaseLost {
			t.Errorf("the server that lost its lease reads the place: %v", err)
		}
		counts, err := ingest.RunIsolate(g.s.e.ctx, g.s.e.st, next, ingest.IsolateStart{Lines: lines, Cards: ingest.ExpandCards(g.m.reg)}, ingest.NewIsolationMaker(g.m.reg))
		if err != nil {
			t.Fatal(err)
		}
		if got := g.isoState(t); got != want {
			t.Errorf("stopped after %d pages:\n%s\nwant\n%s", at, got, want)
		}
		if n := g.s.e.count(t, g.isoQ(`SELECT count(*) FROM {k}.calc_isolated`)); n != counts.Idents+counts.Missing+counts.Retiring+counts.Cardinality {
			t.Errorf("stopped after %d pages: %d rows taken out and the counts %+v", at, n, counts)
		}
	}
}

// [REQ-846]
func TestIsoAServerThatTakesTheRunOverInThePhaseOfTheIdentifiersEndsWhereOneThatDidNotStopEnds(t *testing.T) {
	isoResume(t, func(t *testing.T) *isoFix { return isoIdentWorld(t, false) }, 2)
}

// [REQ-846]
func TestIsoAServerThatTakesTheRunOverInThePhasesOfTheCardinalityEndsWhereOneThatDidNotStopEnds(t *testing.T) {
	isoResume(t, func(t *testing.T) *isoFix { return isoCardWorld(t, false) }, 3)
}

// [REQ-846]
func TestIsoAServerThatTakesTheRunOverInThePhaseOfTheEndsEndsWhereOneThatDidNotStopEnds(t *testing.T) {
	isoResume(t, isoEndsWorld, 1)
}

// [REQ-846]
func TestIsoTheStepBeginsAfterTheFourthStepOncePutsAPageOnceAndRefusesAServerThatLostItsLease(t *testing.T) {
	f := isoCardWorld(t, false)
	ctx, st, l := f.s.e.ctx, f.s.e.st, f.l
	start := ingest.IsolateStart{Lines: 1, Cards: ingest.ExpandCards(f.m.reg)}
	m := ingest.NewIsolationMaker(f.m.reg)
	stage := func(what string, err error) {
		t.Helper()
		if kernel.CodeOf(err) != ingest.CodeRunStage {
			t.Errorf("%s: %v", what, err)
		}
	}
	// the links are not done
	f.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"recompute_links":{"finished":false}}'::jsonb WHERE run_no = $1`, l.No)
	stage("begin before the links are done", st.BeginIsolate(ctx, l, start))
	place, err := st.IsolatePlace(ctx, l)
	if err != nil || place.Begun {
		t.Fatalf("%+v %v", place, err)
	}
	_, err = st.IsolatePage(ctx, l)
	stage("a page before the step begins", err)
	_, err = st.IsolateReport(ctx, l)
	stage("a report before the step begins", err)
	stage("a page put before the step begins", st.PutIsolated(ctx, l, ingest.IsolatedPage{}))
	f.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"recompute_links":{"finished":true}}'::jsonb WHERE run_no = $1`, l.No)
	if err := st.BeginIsolate(ctx, l, start); err != nil {
		t.Fatal(err)
	}
	stage("begin twice", st.BeginIsolate(ctx, l, start))
	if err := st.BeginIsolate(ctx, l, ingest.IsolateStart{Lines: -1}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a size below zero: %v", err)
	}
	// the pages up to the first of the cardinality that is not the last of its pass, each put twice
	for i := 0; i < 40; i++ {
		if place, err = st.IsolatePlace(ctx, l); err != nil {
			t.Fatal(err)
		}
		if place.Phase == ingest.IsolatePhaseSeats && place.Cursor.Ref != "" {
			break
		}
		page, err := st.IsolatePage(ctx, l)
		if err != nil {
			t.Fatal(err)
		}
		made, err := m.MakeIsolated(page)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.PutIsolated(ctx, l, made); err != nil {
			t.Fatal(err)
		}
		stage(fmt.Sprintf("page %d put twice", i), st.PutIsolated(ctx, l, made))
	}
	place, err = st.IsolatePlace(ctx, l)
	if err != nil || place.Phase != ingest.IsolatePhaseSeats || place.Cursor.Ref == "" {
		t.Fatalf("%+v %v", place, err)
	}
	before := f.isoState(t)
	back := ingest.IsolatedPage{Unit: place.Unit, Phase: place.Phase, Through: place.Cursor}
	if err := st.PutIsolated(ctx, l, back); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a page that ends where the place is: %v", err)
	}
	stage("a page of another phase", st.PutIsolated(ctx, l, ingest.IsolatedPage{Unit: place.Unit, Phase: ingest.IsolatePhaseIdents, Last: true}))
	if f.isoState(t) != before {
		t.Error("a page that was refused changed what the step staged")
	}
	next := rclTakeOver(f.s, t, l, "server-b")
	for what, err := range map[string]error{
		"begin": st.BeginIsolate(ctx, l, start), "place": func() error { _, err := st.IsolatePlace(ctx, l); return err }(),
		"page": func() error { _, err := st.IsolatePage(ctx, l); return err }(), "put": st.PutIsolated(ctx, l, back),
		"report": func() error { _, err := st.IsolateReport(ctx, l); return err }(),
	} {
		if kernel.CodeOf(err) != ingest.CodeRunLeaseLost {
			t.Errorf("%s of the server that lost its lease: %v", what, err)
		}
	}
	if _, err := ingest.RunIsolate(ctx, st, next, start, m); err != nil {
		t.Fatal(err)
	}
	_, err = st.IsolatePage(ctx, next)
	stage("a page after the step is done", err)
	stage("a page put after the step is done", st.PutIsolated(ctx, next, ingest.IsolatedPage{Last: true}))
	stage("begin after the step is done", st.BeginIsolate(ctx, next, start))
}

// isoSame does a world's step with pages of each size and the numbers given in each order, and holds what it left to one outcome.
func isoSame(t *testing.T, world func(*testing.T, bool) *isoFix, sizes []int) string {
	t.Helper()
	want := ""
	for _, reverse := range []bool{false, true} {
		for _, size := range sizes {
			f := world(t, reverse)
			f.isoRun(t, size)
			got := f.isoState(t)
			if want == "" {
				want = got
			} else if got != want {
				t.Errorf("numbers reversed %v, pages of %d:\n%s\nwant\n%s", reverse, size, got, want)
			}
		}
	}
	return want
}

// [REQ-852]
func TestIsoThePlacesOfTheCardinalityHangNeitherOnThePagesNorOnTheNumbersOfTheLinks(t *testing.T) {
	if got := isoSame(t, isoCardWorld, []int{1, 0}); got == "" {
		t.Fatal("nothing was compared")
	}
}

// [REQ-852]
func TestIsoTheIdentifiersTakenOutHangNeitherOnThePagesNorOnTheNumbersOfTheObjects(t *testing.T) {
	if got := isoSame(t, isoIdentWorld, []int{1, 0}); got == "" {
		t.Fatal("nothing was compared")
	}
}
