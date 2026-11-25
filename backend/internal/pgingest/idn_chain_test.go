package pgingest

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// idnPipelineWith is idnPipeline with the size of the pages the receiving cuts and the limits of the identification given (0 is the
// default of each).
func idnPipelineWith(s *rdfSetup, t *testing.T, texts pgiTexts, at time.Time, pageRows, size, turns, rounds int) (ingest.RunLease, ingest.IdentifyCounts) {
	t.Helper()
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan, Options: ingest.PageOptions{MaxRows: pageRows}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(at, size)); err != nil {
		t.Fatal(err)
	}
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	start := idnStart(s, size, 1)
	start.Turns, start.Rounds = turns, rounds
	counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, start, keys)
	if err != nil {
		t.Fatal(err)
	}
	return l, counts
}

// idnChain is n records that share a key one after another: the record i has its serial in common with the record before it and its
// uuid with the one after it. The records are named so that the smallest names are at the ends and in the middle, which is where the
// smallest label of a group starts from.
func idnChain(prefix string, n, valley int) []string {
	rank := func(i int) int {
		switch i {
		case 0:
			return 0
		case valley:
			return 1
		}
		return 100 + i
	}
	rows := make([]string, n)
	for i := 0; i < n; i++ {
		rows[i] = fmt.Sprintf("%s%03d,%ssn%d,Zorvane,%su%d", prefix, rank(i), prefix, i/2, prefix, (i+1)/2)
	}
	return rows
}

// idnObjectsOf is how many records each object of the run has, by the objects' ids.
func idnObjectsOf(s *rdfSetup, t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, b := range idnBounds(s, t) {
		out[b.Ref] = append(out[b.Ref], b.Key)
	}
	for _, keys := range out {
		sort.Strings(keys)
	}
	return out
}

// idnTurns is the most turns the search for the groups took in any round.
func idnTurns(s *rdfSetup, t *testing.T) int {
	t.Helper()
	return s.e.count(t, fmt.Sprintf(`SELECT coalesce(max(iter), 0) FROM "s_%d".calc_label`, s.ref.KGNo))
}

// [REQ-882]
func TestIdnAChainOfRecordsLongerThanTheTurnsOfTheDefaultIsOneObjectAndTheTurnsItTakesGrowWithTheLogarithmOfItsLength(t *testing.T) {
	turns := map[int]int{}
	for _, n := range []int{27, 110, 440} {
		s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
		_, counts := idnPipelineWith(s, t, idnServers(idnChain("r", n, n/2)...), rdfT0, 2, 0, 0, 0)
		objs := idnObjectsOf(s, t)
		if len(objs) != 1 || counts.NewObjects != 1 || counts.Unsettled != 0 || counts.Unresolved != 0 {
			t.Fatalf("a chain of %d records is %d objects: %+v", n, len(objs), counts)
		}
		turns[n] = idnTurns(s, t)
	}
	t.Logf("turns by the length of the chain: %v", turns)
	// a chain is followed by jumps: four times the records are a few turns more, not four times the turns
	if turns[110] > 14 || turns[440] > turns[110]+4 {
		t.Errorf("the turns grow with the length of the chain: %v", turns)
	}
}

// [REQ-882]
func TestIdnARoundWhoseSearchIsStoppedByItsLimitBindsNoGroupThatIsNotWholeAndLeavesTheRecordsOfItUnresolvedAndCounted(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	// a chain of 60 records, which two turns do not follow to its end, and three records that share their keys, which they do
	rows := append(idnChain("c", 60, 30), "s1,snS1,Zorvane,uS1", "s2,snS1,Zorvane,uS2", "s3,snS3,Zorvane,uS2")
	_, counts := idnPipelineWith(s, t, idnServers(rows...), rdfT0, 2, 0, 2, 0)
	objs := idnObjectsOf(s, t)
	if counts.Unsettled != 60 || counts.Unresolved != 60 || counts.NewObjects != 1 || len(objs) != 1 {
		t.Fatalf("%+v, %d objects: %v", counts, len(objs), objs)
	}
	for _, keys := range objs {
		if fmt.Sprint(keys) != "[s1 s2 s3]" {
			t.Errorf("the only object is of %v", keys)
		}
	}
	// no record of the chain is in an object, and none of the pairs is bound to a group that was a piece
	for _, b := range idnBounds(s, t) {
		if b.Key[0] == 'c' {
			t.Errorf("%+v", b)
		}
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem WHERE kind = 'identity_conflict'`, s.ref.KGNo)); n != 0 {
		t.Errorf("%d problems of conflict: the records were left, no record conflicts", n)
	}
}

// [REQ-882]
func TestIdnTheSameChainWithTheLimitOfTheTurnsRaisedIsOneObjectAndMakesTheSameObjectInAnyOrderOfTheRows(t *testing.T) {
	var want string
	for n, order := range []func([]string) []string{func(r []string) []string { return r }, idnReversed, func(r []string) []string { return idnRotated(r, 17) }} {
		s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
		_, counts := idnPipelineWith(s, t, idnServers(order(idnChain("c", 60, 30))...), rdfT0, 2+n, 7, 0, 0)
		if counts.NewObjects != 1 || counts.Unsettled != 0 {
			t.Fatalf("%+v", counts)
		}
		got := idnState(s, t)
		for ref := range idnObjectsOf(s, t) {
			got += ref
		}
		if n == 0 {
			want = got
		} else if got != want {
			t.Errorf("order %d makes\n%s\nand the first order\n%s", n, got, want)
		}
	}
}
