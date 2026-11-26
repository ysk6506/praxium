package pgingest

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// How the difference goes on (ADR-0127): it begins after the isolation, once; a unit is done once; a server that lost its lease is refused,
// reads too; a server that takes the run over goes on from the unit it stands at; and the outcome hangs neither on the order the objects and
// the links got their numbers in nor on the size of a unit.

// dfiUnits does n units of the difference with the lease, from where the run stands, and says the number of the unit it is at.
func dfiUnits(f *dfiFix, t *testing.T, l ingest.RunLease, n int, start ingest.DiffStart) int {
	t.Helper()
	ctx, st := f.s.e.ctx, f.s.e.st
	place, err := st.DiffPlace(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if !place.Begun {
		if err := st.BeginDiff(ctx, l, start); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		if place, err = st.DiffPlace(ctx, l); err != nil {
			t.Fatal(err)
		}
		if place.Done() {
			break
		}
		if err := st.DiffStep(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if place, err = st.DiffPlace(ctx, l); err != nil {
		t.Fatal(err)
	}
	return place.Unit
}

// [REQ-968]
func TestDfiAServerThatTakesTheRunOverAtAnyUnitEndsWhereOneThatDidNotStopEnds(t *testing.T) {
	start := ingest.DiffStart{Rows: 1, ChunkRows: 2}
	f := dfiAll(t, false)
	// the phase of each unit, done one by one
	var phases []string
	for {
		place, err := f.s.e.st.DiffPlace(f.s.e.ctx, f.l)
		if err != nil {
			t.Fatal(err)
		}
		if place.Done() {
			break
		}
		if !place.Begun {
			dfiUnits(f, t, f.l, 0, start)
			continue
		}
		phases = append(phases, place.Phase)
		dfiUnits(f, t, f.l, 1, start)
	}
	want, err := f.s.e.st.DiffReport(f.s.e.ctx, f.l)
	if err != nil {
		t.Fatal(err)
	}
	rows := dfiRows(f.s, t)
	// a stop in the middle of the rows, at the plan, after the first unit of the order and after the first unit of the print
	stops := []int{slices.Index(phases, ingest.DiffPhasePlan) / 2, slices.Index(phases, ingest.DiffPhasePlan),
		slices.Index(phases, ingest.DiffPhaseOrder) + 1, slices.Index(phases, ingest.DiffPhasePrint) + 1}
	if len(phases) < 30 || slices.Contains(stops, 0) || slices.Index(phases, ingest.DiffPhasePrint)+1 >= len(phases) {
		t.Fatalf("the difference had the units %v", phases)
	}
	for _, at := range stops {
		g := dfiAll(t, false)
		dfiUnits(g, t, g.l, at, start)
		next := rclTakeOver(g.s, t, g.l, "server-b")
		if _, err := g.s.e.st.DiffPlace(g.s.e.ctx, g.l); kernel.CodeOf(err) != ingest.CodeRunLeaseLost {
			t.Errorf("the server that lost its lease reads the place: %v", err)
		}
		got, err := ingest.RunDiff(g.s.e.ctx, g.s.e.st, next, start)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) || !slices.Equal(dfiRows(g.s, t), rows) {
			t.Errorf("stopped after %d units: %+v, want %+v", at, got, want)
		}
	}
}

// [REQ-968]
func TestDfiTheStepBeginsAfterTheIsolationOnceAndRefusesAStepWhenItIsDoneAndAServerThatLostItsLease(t *testing.T) {
	f := dfiAll(t, false)
	ctx, st, l := f.s.e.ctx, f.s.e.st, f.l
	start := ingest.DiffStart{Rows: 3}
	stage := func(what string, err error) {
		t.Helper()
		if kernel.CodeOf(err) != ingest.CodeRunStage {
			t.Errorf("%s: %v", what, err)
		}
	}
	// the isolation is not done
	f.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"isolate":{"finished":false}}'::jsonb WHERE run_no = $1`, l.No)
	stage("begin before the isolation is done", st.BeginDiff(ctx, l, start))
	place, err := st.DiffPlace(ctx, l)
	if err != nil || place.Begun {
		t.Fatalf("%+v %v", place, err)
	}
	stage("a unit before the step begins", st.DiffStep(ctx, l))
	_, err = st.DiffReport(ctx, l)
	stage("a report before the step begins", err)
	f.s.e.exec(t, `UPDATE sync_run SET counts = counts || '{"isolate":{"finished":true}}'::jsonb WHERE run_no = $1`, l.No)
	if err := st.BeginDiff(ctx, l, ingest.DiffStart{ChunkRows: -1}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a size below zero: %v", err)
	}
	if err := st.BeginDiff(ctx, l, start); err != nil {
		t.Fatal(err)
	}
	stage("begin twice", st.BeginDiff(ctx, l, start))
	if err := st.DiffStep(ctx, l); err != nil {
		t.Fatal(err)
	}
	_, err = st.DiffReport(ctx, l)
	stage("a report before the step is done", err)
	before := dfiRows(f.s, t)
	next := rclTakeOver(f.s, t, l, "server-b")
	for what, err := range map[string]error{
		"begin": st.BeginDiff(ctx, l, start), "place": func() error { _, err := st.DiffPlace(ctx, l); return err }(),
		"step": st.DiffStep(ctx, l), "report": func() error { _, err := st.DiffReport(ctx, l); return err }(),
	} {
		if kernel.CodeOf(err) != ingest.CodeRunLeaseLost {
			t.Errorf("%s of the server that lost its lease: %v", what, err)
		}
	}
	if !slices.Equal(dfiRows(f.s, t), before) {
		t.Error("a call that was refused changed the difference")
	}
	if _, err := ingest.RunDiff(ctx, st, next, start); err != nil {
		t.Fatal(err)
	}
	stage("a unit after the step is done", st.DiffStep(ctx, next))
	stage("begin after the step is done", st.BeginDiff(ctx, next, start))
	// a run whose tables of the computation are another's is refused
	f.s.e.exec(t, f.dfiQ(`UPDATE {k}.calc_owner SET run_no = run_no + 1000`))
	for what, err := range map[string]error{
		"begin": st.BeginDiff(ctx, next, start), "place": func() error { _, err := st.DiffPlace(ctx, next); return err }(),
		"step": st.DiffStep(ctx, next), "report": func() error { _, err := st.DiffReport(ctx, next); return err }(),
	} {
		stage(what+" of a run whose tables are another's", err)
	}
}

// [REQ-965] [REQ-967]
func TestDfiTheRowsAndTheChunksHangNeitherOnTheNumbersNorOnTheSizeOfAUnitAndTheFingerprintOnTheNumbersAlone(t *testing.T) {
	var want []string
	var last *dfiFix
	prints := map[bool]kernel.Hash{}
	numbers := map[bool]map[string]int64{}
	for _, reverse := range []bool{false, true} {
		for _, size := range []int{1, 0} {
			f := dfiAll(t, reverse)
			last = f
			counts := dfiDo(f.s, t, f.l, size, 2)
			dfiShape(f.s, t, counts, 2)
			got := dfiRows(f.s, t)
			if want == nil {
				want = got
			} else if !slices.Equal(got, want) {
				t.Errorf("numbers going down %v, units of %d: the difference is\n%v\nwant\n%v", reverse, size, got, want)
			}
			if p, ok := prints[reverse]; ok && p != counts.Fingerprint {
				t.Errorf("numbers going down %v, units of %d: the fingerprint %s, want %s", reverse, size, counts.Fingerprint, p)
			}
			prints[reverse], numbers[reverse] = counts.Fingerprint, f.no
		}
	}
	if len(want) < 20 || prints[false] == prints[true] {
		t.Fatalf("the difference has %d rows, the fingerprints %v", len(want), prints)
	}
	// the line of a row has the number of the object or the link (ADR-0095), so the fingerprint of the world whose numbers go down is the
	// fingerprint of the other world once each of its numbers is put for the number of the same id
	if got := dfiPrintWith(last.s, t, numbers[true], numbers[false]); got != prints[false] {
		t.Errorf("the fingerprint with the other numbers is %s, want %s", got, prints[false])
	}
}

// dfiPrintWith is the digest of the stored difference made in Go (pgschema.InputDigest) with the number of each object and link put from one
// numbering (by its id) to another.
func dfiPrintWith(s *rdfSetup, t *testing.T, from, to map[string]int64) kernel.Hash {
	t.Helper()
	ids := map[int64]string{}
	for id, no := range from {
		ids[no] = id
	}
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT chunk_no, kind, type_no, entity_no, change_kind, old_hash, new_hash FROM "s_%d".ingest_diff ORDER BY ord`, s.ref.KGNo))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var chunks [][]pgschema.DiffLine
	for rows.Next() {
		var c int
		var l pgschema.DiffLine
		if err := rows.Scan(&c, &l.Kind, &l.TypeNo, &l.EntityNo, &l.Change, &l.OldHash, &l.NewHash); err != nil {
			t.Fatal(err)
		}
		l.EntityNo = to[ids[l.EntityNo]]
		if c == len(chunks) {
			chunks = append(chunks, nil)
		}
		chunks[c] = append(chunks[c], l)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	d, err := pgschema.InputDigest(chunks)
	if err != nil {
		t.Fatal(err)
	}
	return kernel.Hash(d)
}
