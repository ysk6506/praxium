package pgingest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the computation of the objects reads and what it refuses, each case made so that the statement or the check it is about is the
// only thing that tells the right result from a wrong one (the review of ADR-0115 found these unguarded).

// rcpKillMapping is the mapping of the servers of the tests with feeds that retire objects and that are approximate (their values are
// assumed, grade 1).
func rcpKillMapping(t *testing.T) string {
	t.Helper()
	m := strings.ReplaceAll(idnMoreMapping, `"object_type":"PhysicalServer","creates_objects":true,`, `"object_type":"PhysicalServer","approximate":true,"creates_objects":true,"retires_objects":true,`)
	if strings.Count(m, "retires_objects") != 2 {
		t.Fatal("the mapping of the test was not made")
	}
	return m
}

// rcpKillWorld is a KG whose runs but the last are computed and applied, thirteen hours apart, with the last received and identified
// (not expanded).
func rcpKillWorld(t *testing.T, runs ...[]string) (*rdfSetup, ingest.RunLease) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, idnMoreSource, rcpKillMapping(t)))
	for i, rows := range runs {
		l, _ := idnPipeline(s, t, pgiTexts{"ds_a": idnMoreTexts(rows, true), "ds_b": idnMoreTexts(nil, false)}, rdfT0.Add(time.Duration(i)*13*time.Hour), 0)
		if i == len(runs)-1 {
			return s, l
		}
		rcpExpand(s, t, l, true)
		rcpDo(s, t, l, 0, 0)
		rcpApply(s, t)
		// the identifiers the objects have are the registry's now: a claim of another object to one of them is lost
		s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".object_identifier (obj_no, scheme, value, scope, seq_from) SELECT i.obj_no, i.scheme, i.value, '', 1 FROM "s_%[1]d".stage_ident i
			WHERE NOT EXISTS (SELECT 1 FROM "s_%[1]d".object_identifier x WHERE x.scheme = i.scheme AND x.value = i.value)`, s.ref.KGNo))
		s.rdfSettle(t, l)
	}
	t.Fatal("no run")
	return nil, ingest.RunLease{}
}

// [REQ-975]
func TestRcpWhatTheRunReadsIsTheReferencesThatStandAndOnlyOfTheObjectsThatAreNotCandidatesForRetirement(t *testing.T) {
	// A0 finds the object of A1 by its serial and says the identifier of B1, which that object has: its claim is lost, and stays kept (its
	// key is the smaller, so a claim that was read as one of the object would be the one the object has). D1 is not
	// shown for three runs and a day, so the object it stood under is a candidate for retirement. In the last run A2 is another object
	// (its serial and its uuid are new) and H1 is new.
	s, l := rcpKillWorld(t,
		[]string{"A1,sn1,Zorvane,u1,aa:bb:cc:dd:ee:01", "A2,sn1,Zorvane,u1,aa:bb:cc:dd:ee:01", "B1,sn2,Zorvane,u2,aa:bb:cc:dd:ee:02", "D1,sn4,Zorvane,u4,aa:bb:cc:dd:ee:04"},
		[]string{"A1,sn1,Zorvane,u1,aa:bb:cc:dd:ee:01", "A2,sn1,Zorvane,u1,aa:bb:cc:dd:ee:01", "B1,sn2,Zorvane,u2,aa:bb:cc:dd:ee:02", "A0,sn1,Zorvane,uE,aa:bb:cc:dd:ee:02"},
		[]string{"A1,sn1,Zorvane,u1,aa:bb:cc:dd:ee:01", "A2,sn1,Zorvane,u1,aa:bb:cc:dd:ee:01", "B1,sn2,Zorvane,u2,aa:bb:cc:dd:ee:02", "A0,sn1,Zorvane,uE,aa:bb:cc:dd:ee:02"},
		[]string{"A1,sn1,Zorvane,u1,aa:bb:cc:dd:ee:01", "A2,sn9,Zorvane,u8,aa:bb:cc:dd:ee:08", "B1,sn2,Zorvane,u2,aa:bb:cc:dd:ee:02", "A0,sn1,Zorvane,uE,aa:bb:cc:dd:ee:02", "H1,sn8,Zorvane,uh,aa:bb:cc:dd:ee:09"})
	rcpExpand(s, t, l, true)
	counts := rcpDo(s, t, l, 1, 0)
	staged := rcpStaged(s, t)
	t.Logf("%+v", counts)
	// the object that D1 stood under is a candidate for retirement: it is not read as an object, so it is not empty and not left
	if counts.Retiring != 1 || counts.Empty != 0 || counts.Left != 0 || rcpCountOf(s, t, pgschema.TableCalcLeft) != 0 || counts.Objects != 4 || len(staged) != 4 {
		t.Errorf("%+v, %d staged", counts, len(staged))
	}
	// and it is not counted in the lines of a page: a page of one line has one object, so the pages are the objects
	if place, err := s.e.st.RecomputePlace(s.e.ctx, l); err != nil || place.Unit != counts.Objects {
		t.Errorf("%d pages for %d objects: %v", place.Unit, counts.Objects, err)
	}
	// the record A2 said the object A before: its reference of before is not a line of A, so A has the records A0 and A1, which name it
	// differently (one conflict), and the record A2, which is under another object now, is no part of it
	if got := rcpProblems(s, t); len(got) != 1 || strings.Contains(got[0], "/A2/") {
		t.Errorf("problems: %v", got)
	}
	// the identifier that two objects claimed is one object's: the claim that was lost is no claim of the object that lost it
	seen := map[string]string{}
	for ref, o := range staged {
		for _, id := range o.Idents {
			if other, ok := seen[id]; ok {
				t.Errorf("%s is an identifier of %s and of %s", id, other, ref)
			}
			seen[id] = ref
		}
		if o.Grade != 1 {
			t.Errorf("%s has the grade %d: the values are assumed", ref, o.Grade)
		}
	}
	if len(seen) == 0 {
		t.Error("no identifier is staged")
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".record_ref WHERE state = %d`, s.ref.KGNo, pgschema.RefLostClaim)); n == 0 {
		t.Error("the case has no claim that was lost")
	}
	// the run that follows the rules computes the objects it touched alone: A is one (its record A2 left it), and A0, whose claim was lost
	// and is not read again, is one of its kept lines. The claim it lost is not a claim of A, so A has no identifier that B has.
	rcpReset(s, t, l)
	rcpExpand(s, t, l, false)
	rcpDo(s, t, l, 0, 0)
	touched := rcpStaged(s, t)
	withA0 := 0
	for _, o := range touched {
		if strings.Contains(o.SourceRefs, "ds_a/A0") {
			withA0++
		}
	}
	if withA0 != 1 {
		t.Errorf("%d of the %d objects touched hold A0", withA0, len(touched))
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".stage_ident i JOIN "s_%[1]d".object_identifier x ON x.scheme = i.scheme AND x.value = i.value AND x.obj_no <> i.obj_no`, s.ref.KGNo)); n != 0 {
		t.Errorf("%d identifiers are staged for an object that another object has", n)
	}
}

// [REQ-976]
func TestRcpTheComputationRefusesToBeginTwiceAndToRunOnTablesThatAreNotTheRunsAndAPageAfterTheEnd(t *testing.T) {
	s, l := rcpLeftServers(t)
	st, ctx, k := s.e.st, s.e.ctx, s.ref.KGNo
	owned := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_owner WHERE run_no = $1`, k), l.No)
	if owned != 1 {
		t.Fatalf("the tables are owned by %d runs", owned)
	}
	disown := func() { s.e.exec(t, fmt.Sprintf(`UPDATE "s_%d".calc_owner SET run_no = run_no + 1000`, k)) }
	own := func() { s.e.exec(t, fmt.Sprintf(`UPDATE "s_%d".calc_owner SET run_no = $1`, k), l.No) }
	// tables that are another run's: the computation is not begun, and a place that was begun is not read or written
	disown()
	if err := st.BeginRecompute(ctx, l, rcpStart(s, 3, 3)); kernel.CodeOf(err) != ingest.CodeRunStage || !strings.Contains(err.Error(), "not the run") {
		t.Errorf("begun on the tables of another run: %v", err)
	}
	own()
	if err := st.BeginRecompute(ctx, l, rcpStart(s, 3, 3)); err != nil {
		t.Fatal(err)
	}
	// begun twice: told as that, and not as an expansion that is not done
	if err := st.BeginRecompute(ctx, l, rcpStart(s, 3, 3)); kernel.CodeOf(err) != ingest.CodeRunStage || !strings.Contains(err.Error(), "has begun already") {
		t.Errorf("begun twice: %v", err)
	}
	page, err := st.RecomputePage(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	made, err := rcpMaker(t, s, 3, 3).Make(page)
	if err != nil {
		t.Fatal(err)
	}
	disown()
	if _, err := st.RecomputePage(ctx, l); kernel.CodeOf(err) != ingest.CodeRunStage || !strings.Contains(err.Error(), "not the run") {
		t.Errorf("a page of the tables of another run: %v", err)
	}
	if err := st.PutRecomputed(ctx, l, made); kernel.CodeOf(err) != ingest.CodeRunStage || !strings.Contains(err.Error(), "not the run") {
		t.Errorf("a page put to the tables of another run: %v", err)
	}
	own()
	if _, err := ingest.RunRecompute(ctx, st, l, rcpStart(s, 3, 3), rcpMaker(t, s, 3, 3)); err != nil {
		t.Fatal(err)
	}
	// a page that is the next one and that goes on, put after the last: only the end of the computation refuses it
	place, err := st.RecomputePlace(ctx, l)
	if err != nil || !place.Finished {
		t.Fatalf("%+v %v", place, err)
	}
	next := made
	next.Unit, next.Through, next.Last = place.Unit, place.Cursor+1, true
	if err := st.PutRecomputed(ctx, l, next); kernel.CodeOf(err) != ingest.CodeRunStage || !strings.Contains(err.Error(), "done") {
		t.Errorf("a page put after the last: %v", err)
	}
}
