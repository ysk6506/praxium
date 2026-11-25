package pgingest

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-883]
func TestIdnTheIdentificationOfARunThatHasBegunOrWhoseComparisonIsNotDoneIsNotBegunAndWhatItMadeIsNotChanged(t *testing.T) {
	texts := idnServers("A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2", "C,sn3,Zorvane,u3", "D,sn4,Zorvane,u4")
	whole := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	idnPipeline(whole, t, texts, rdfT0, 1)
	want, wantKeys := idnState(whole, t), whole.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_key`, whole.ref.KGNo))

	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	l := idnReceived(s, t, texts, 1)
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	if err := s.e.st.BeginIdentify(s.e.ctx, l, idnStart(s, 1, 1)); err != nil {
		t.Fatal(err)
	}
	// a second begin is refused whether the run has done a unit or not, and the run goes on to the end it has without it
	if err := s.e.st.BeginIdentify(s.e.ctx, l, idnStart(s, 1, 1)); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a second begin at the start: %v", err)
	}
	for i := 0; i < 4; i++ {
		if err := s.e.st.IdentifyStep(s.e.ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.e.st.BeginIdentify(s.e.ctx, l, idnStart(s, 1, 1)); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a second begin in the middle: %v", err)
	}
	if _, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, idnStart(s, 1, 1), keys); err != nil {
		t.Fatal(err)
	}
	if err := s.e.st.BeginIdentify(s.e.ctx, l, idnStart(s, 1, 1)); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a second begin at the end: %v", err)
	}
	if got, n := idnState(s, t), s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_key`, s.ref.KGNo)); got != want || n != wantKeys {
		t.Errorf("the run made %d keys and\n%s\nthe whole one %d and\n%s", n, got, wantKeys, want)
	}

	// the comparison of a run that is not done is not begun on: only the first bucket of it is
	late := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	l2 := late.e.open(t, late.kg, late.plan.Source.ID, "server-a")
	if _, err := late.e.st.Receive(late.e.ctx, l2, ingest.Intake{Reader: texts, Plan: late.plan, Options: ingest.PageOptions{MaxRows: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := late.e.st.BeginRawDiff(late.e.ctx, l2, late.rdfStart(rdfT0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := late.e.st.DiffRawBucket(late.e.ctx, l2, 0); err != nil {
		t.Fatal(err)
	}
	if err := late.e.st.BeginIdentify(late.e.ctx, l2, idnStart(late, 1, 1)); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a begin while the comparison is at its first bucket: %v", err)
	}
}

// [REQ-883]
func TestIdnWhatTheTestsWatchOfTheStatementsOfAUnitIsOfTheStoreTheyAskItOfAndNotOfAnother(t *testing.T) {
	watched, other := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping)), rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	var seen, seenOther int
	watched.e.st.identSeen = func(_ *sql.Tx, _, _ string, _ []any) { seen++ }
	other.e.st.identSeen = func(_ *sql.Tx, _, _ string, _ []any) { seenOther++ }
	idnPipeline(watched, t, idnServers("A,sn1,Zorvane,u1"), rdfT0, 0)
	if seen == 0 || seenOther != 0 {
		t.Errorf("%d statements are seen by the store that ran, %d by the one that did not", seen, seenOther)
	}
	idnPipeline(other, t, idnServers("A,sn1,Zorvane,u1"), rdfT0, 0)
	if seenOther == 0 {
		t.Error("the other store sees none of its own")
	}
}
