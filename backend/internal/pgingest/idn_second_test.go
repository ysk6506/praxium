package pgingest

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The groups of many objects, the chains Go ends, the neighbors of a later round, the labels that are kept, the witness (ADR-0108).

var idnGroup = flag.Int("idn.group", 300, "records of the group of several objects the measurement identifies")

// idnConflict is m servers that each have their own serial and uuid and, in the second run, all take the uuid of a placeholder: one
// group of m records, each bound to another object. It gives what the units of the second run read and the time of the run.
func idnConflict(t *testing.T, m int, explain bool) (map[string]*idnRead, time.Duration, ingest.IdentifyCounts) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	var first, second []string
	for i := 0; i < m; i++ {
		first = append(first, "A"+strconv.Itoa(i)+",sn"+strconv.Itoa(i)+",Zorvane,u"+strconv.Itoa(i))
		second = append(second, "A"+strconv.Itoa(i)+",sn"+strconv.Itoa(i)+",Zorvane,PH")
	}
	l1, _ := idnPipelineWith(s, t, idnServers(first...), rdfT0, 1000, 500, 0, 0, 0)
	idnApply(s, t)
	s.rdfSettle(t, l1)
	s.e.exec(t, `VACUUM ANALYZE`)
	plans := &idnPlans{most: map[string]*idnRead{}}
	if explain {
		s.e.st.identSeen = plans.idnHook
		defer func() { s.e.st.identSeen = nil }()
	}
	began := idnNow.Now()
	_, counts := idnPipelineWith(s, t, idnServers(second...), rdfT0.Add(time.Hour), 1000, 500, 0, 0, 0)
	return plans.most, idnNow.Now().Sub(began), counts
}

// [REQ-883]
func TestIdnAGroupOfSeveralObjectsIsReadOnceForEachKeyAndWhatAUnitReadsOfItGrowsWithTheGroupAndNoFaster(t *testing.T) {
	small, _, counts := idnConflict(t, 300, true)
	large, _, _ := idnConflict(t, 900, true)
	if counts.Unresolved != 0 || len(small) < 5 {
		t.Fatalf("%d statements, %+v", len(small), counts)
	}
	var whats []string
	for what := range large {
		whats = append(whats, what)
	}
	sort.Strings(whats)
	for _, what := range whats {
		for table, n := range large[what].rows {
			// a group is one unit and a unit reads each of its pairs and keys: three times the records are three times the rows (and a
			// little over), where a read of the pairs that have a key once for each pair would be nine times
			if before, ok := small[what]; ok && n > 3.5*before.rows[table]+50 {
				t.Errorf("%s reads %v rows of %s for a group of 900 records and %v for one of 300", what, n, table, before.rows[table])
			}
			if n > 40*900 {
				t.Errorf("%s reads %v rows of %s for a group of 900 records", what, n, table)
			}
		}
	}
}

// [REQ-883]
func TestIdnMeasureTheIdentificationOfAGroupOfManyObjects(t *testing.T) {
	_, took, counts := idnConflict(t, *idnGroup, false)
	t.Logf("a group of %d records of %d objects: %v, %+v", *idnGroup, *idnGroup, took, counts)
}

// idnShuffled is the rows of a chain of n records in a shuffled row order. It does not shuffle the pair numbers: those follow the keys, and
// the keys run along the chain (the random-key chain is in ADR-0108).
func idnShuffled(n int, seed int64) []string {
	rows := idnChain("c", n, n/2)
	x := uint64(seed)*2862933555777941757 + 3037000493
	for i := len(rows) - 1; i > 0; i-- {
		x = x*6364136223846793005 + 1442695040888963407
		j := int((x >> 33) % uint64(i+1))
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}

// [REQ-882]
func TestIdnAChainOfRecordsInAnyOrderIsOneObjectWhenTheTurnsEndBeforeItsEndAndGoEndsTheSearchAndNothingIsLeftWhenItCannotHoldIt(t *testing.T) {
	for _, tc := range []struct {
		n, turns, settle int
		whole            bool
	}{{1600, 0, 0, true}, {6000, 0, 0, true}, {400, 2, 0, true}, {400, 2, 1, false}} {
		s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
		_, counts := idnPipelineWith(s, t, idnServers(idnShuffled(tc.n, int64(tc.n))...), rdfT0, 1000, 500, tc.turns, 0, tc.settle)
		objs := idnObjectsOf(s, t)
		t.Logf("a chain of %d records, turns %d, Go holds %d pairs: %d objects, %d turns, %+v", tc.n, tc.turns, tc.settle, len(objs), idnTurns(s, t), counts)
		if tc.whole && (len(objs) != 1 || counts.NewObjects != 1 || counts.Unsettled != 0 || counts.Unresolved != 0) {
			t.Errorf("%+v: a chain of %d records is %d objects", tc, tc.n, len(objs))
		}
		if !tc.whole && (len(objs) != 0 || counts.Unsettled != tc.n || counts.Unresolved != tc.n) {
			t.Errorf("%+v: %d objects, %+v: Go could not hold the chain, so it is left, counted", tc, len(objs), counts)
		}
	}
}

// idnSettleCut gives the port to the run until it has done n calls, the reads and the writes of Go included, and then fails as a
// server that dies does. It says which call it failed.
type idnSettleCut struct {
	idnCut
	died  *string
	calls *[]string
}

func (c *idnSettleCut) fail(what string) error {
	*c.calls = append(*c.calls, what)
	if err := c.idnTick(); err != nil {
		*c.died = what
		return err
	}
	return nil
}

func (c *idnSettleCut) IdentifyStep(ctx context.Context, l ingest.RunLease) error {
	if err := c.fail("step"); err != nil {
		return err
	}
	return c.Identifier.IdentifyStep(ctx, l)
}

func (c *idnSettleCut) SettleRead(ctx context.Context, l ingest.RunLease, after int64) (ingest.SettlePage, error) {
	if err := c.fail("settle read"); err != nil {
		return ingest.SettlePage{}, err
	}
	return c.Identifier.SettleRead(ctx, l, after)
}

func (c *idnSettleCut) PutSettled(ctx context.Context, l ingest.RunLease, rows []ingest.SettledPair, end ingest.SettleEnd) error {
	if err := c.fail("settle write"); err != nil {
		return err
	}
	return c.Identifier.PutSettled(ctx, l, rows, end)
}

// [REQ-883]
func TestIdnARunThatStopsWhileGoEndsTheSearchForTheGroupsGoesOnAndMakesWhatARunThatDoesNotStopMakes(t *testing.T) {
	texts := idnServers(idnShuffled(300, 7)...)
	var calls []string
	run := func(cut int) (*rdfSetup, string) {
		s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
		l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
		if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan, Options: ingest.PageOptions{MaxRows: 100}}); err != nil {
			t.Fatal(err)
		}
		if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfT0, 100)); err != nil {
			t.Fatal(err)
		}
		keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
		start := idnStart(s, 100, 1)
		start.Turns = 2
		died := ""
		if cut >= 0 {
			port := &idnSettleCut{idnCut: idnCut{Identifier: s.e.st, left: cut}, died: &died, calls: &calls}
			if _, err := ingest.RunIdentify(s.e.ctx, port, l, start, keys); err == nil || err.Error() != idnDied.Error() {
				t.Fatalf("cut %d: %v", cut, err)
			}
			s.e.expire(t, l.No)
			l2, err := s.e.st.AcquireRun(s.e.ctx, l.No, "server-b", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			l = l2
		}
		if _, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, start, keys); err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		return s, died
	}
	// the whole run, once, through the port that counts: which of its calls are the reads and the writes of Go
	whole := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	{
		l := whole.e.open(t, whole.kg, whole.plan.Source.ID, "server-a")
		if _, err := whole.e.st.Receive(whole.e.ctx, l, ingest.Intake{Reader: texts, Plan: whole.plan, Options: ingest.PageOptions{MaxRows: 100}}); err != nil {
			t.Fatal(err)
		}
		if _, err := ingest.RunRawDiff(whole.e.ctx, whole.e.st, l, whole.rdfStart(rdfT0, 100)); err != nil {
			t.Fatal(err)
		}
		start := idnStart(whole, 100, 1)
		start.Turns = 2
		died := ""
		port := &idnSettleCut{idnCut: idnCut{Identifier: whole.e.st, left: 1 << 30}, died: &died, calls: &calls}
		if _, err := ingest.RunIdentify(whole.e.ctx, port, l, start, ingest.NewPlanKeys(map[int64]*ingest.Plan{whole.ref.SrcNo: whole.plan})); err != nil {
			t.Fatal(err)
		}
	}
	want := idnState(whole, t)
	if n := len(idnObjectsOf(whole, t)); n != 1 {
		t.Fatalf("%d objects", n)
	}
	var cuts []int
	for i, c := range calls {
		if strings.HasPrefix(c, "settle") {
			cuts = append(cuts, i, i+1)
		}
	}
	if len(cuts) == 0 {
		t.Fatalf("Go did not end the search: %v", calls)
	}
	for _, cut := range cuts {
		s, died := run(cut)
		if got := idnState(s, t); got != want {
			t.Errorf("cut %d (%s): the run made\n%s\nand the whole one\n%s", cut, died, got, want)
		}
	}
}

// idnPlacesTexts is the places the next tests give the identification, with the other datasets empty.
func idnPlacesTexts(rows ...string) pgiTexts { return idnPlaces(rows...) }

// [REQ-882]
func TestIdnAChainThatIsLeftAndARecordOfALaterRoundThatSharesAKeyWithItAreLeftTogetherAndThenMadeOneObject(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	first := []string{"a0,S,B,F,R1,rkA,tA", "k0,S,B,F,R9,rkK,tK"}
	l1, _ := idnPipelineWith(s, t, idnPlacesTexts(first...), rdfT0, 2, 0, 0, 0, 0)
	idnApply(s, t)
	s.rdfSettle(t, l1)
	// a chain of racks in a room there is, which the run leaves (a turn, and Go holds one pair), one of the tags of which is a tag of a
	// rack there is; and a rack in a room that is new, which is ready in a later round and has the last tag of the chain
	second := append([]string{}, first...)
	for i := 0; i < 12; i++ {
		tag := fmt.Sprintf("t%d", i/2)
		if i/2 == 2 {
			tag = "tK"
		}
		second = append(second, fmt.Sprintf("c%02d,S,B,F,R1,n%d,%s", i, (i+1)/2, tag))
	}
	second = append(second, "q0,S,B,F,RN,nq,t5")
	l2, counts2 := idnPipelineWith(s, t, idnPlacesTexts(second...), rdfT0.Add(time.Hour), 2, 0, 1, 0, 1)
	if counts2.Unsettled == 0 {
		t.Fatalf("the limited run left nothing: %+v", counts2)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem WHERE kind = 'identity_conflict'`, s.ref.KGNo)); n != 0 {
		t.Errorf("%d conflicts in the limited run: what shares a key with what is left is left", n)
	}
	for _, b := range idnBounds(s, t) {
		if b.Feed == 4 && (strings.HasPrefix(b.Key, "c") || b.Key == "q0") {
			t.Errorf("%+v: bound in the run that left its group", b)
		}
	}
	idnApply(s, t)
	s.rdfSettle(t, l2)
	_, counts3 := idnPipelineWith(s, t, idnPlacesTexts(second...), rdfT0.Add(2*time.Hour), 2, 0, 0, 0, 0)
	refs := map[string]bool{}
	for _, b := range idnBounds(s, t) {
		if b.Feed == 4 && (strings.HasPrefix(b.Key, "c") || b.Key == "q0" || b.Key == "k0") {
			refs[b.Ref] = true
		}
	}
	if len(refs) != 1 || counts3.Unsettled != 0 {
		t.Errorf("the chain, the rack that shares a tag with it and the rack that has the tag are in %d objects: %v, %+v", len(refs), refs, counts3)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem WHERE kind = 'identity_conflict'`, s.ref.KGNo)); n != 0 {
		t.Errorf("%d conflicts in the run that ends the search", n)
	}
}

// [REQ-883]
func TestIdnTheLabelsOfTheTurnsBeforeTheLastTwoAreTakenAwayAndTheRowsOfThemAreAtMostTwoForEachPair(t *testing.T) {
	for _, turns := range []int{0, 3} {
		s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
		idnPipelineWith(s, t, idnServers(idnShuffled(1600, 5)...), rdfT0, 1000, 500, turns, 0, 0)
		k := s.ref.KGNo
		pairs := s.e.count(t, fmt.Sprintf(`SELECT count(DISTINCT pid) FROM "s_%d".calc_label`, k))
		rows := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_label`, k))
		t.Logf("turns %d: %d turns, %d pairs, %d label rows", turns, idnTurns(s, t), pairs, rows)
		if idnTurns(s, t) < 4 {
			t.Errorf("%d turns: too few for the test to say anything", idnTurns(s, t))
		}
		if rows > 2*pairs {
			t.Errorf("%d rows of labels for %d pairs", rows, pairs)
		}
	}
}

// idnX is the records of a server, an interface of it and a transceiver of the interface, each with a context key (the transceiver has
// no key but the interface it is in).
const idnXSource = `{"id":"src_x","name":"x","adapter":"file","datasets":[
 {"id":"ds_x","parser":"csv","natural_key":["row_id"],"columns":[{"name":"row_id"},{"name":"serial"},{"name":"manufacturer"},{"name":"ifname"},{"name":"xname"}]}]}`

const idnXParent = `{"context":"PART_OF","parent":{"object_type":"NetworkDevice","identity":{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}]}}}`

const idnXMapping = `{"feeds":[
 {"id":"f_srv","dataset":"ds_x","object_type":"NetworkDevice","creates_objects":true,"basis":"observed","attributes":[{"attribute":"name","column":"serial"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_if","dataset":"ds_x","object_type":"Interface","creates_objects":true,"basis":"observed","attributes":[{"attribute":"name","column":"ifname"}],
  "identity":[{"components":[` + idnXParent + `,{"property":"name","column":"ifname"}],"priority":1}]},
 {"id":"f_x","dataset":"ds_x","object_type":"Transceiver","creates_objects":true,"basis":"observed","attributes":[{"attribute":"name","column":"xname"}],
  "identity":[{"components":[{"context":"PART_OF","parent":{"object_type":"Interface","identity":{"components":[` + idnXParent + `,{"property":"name","column":"ifname"}]}}}],"priority":1}]}]}`

// [REQ-880]
func TestIdnTheRecordThatNamesTheObjectOfAGroupIsTheOneThatHasANameWhateverTheKeysOfTheOthersAndTheOrderOfTheRows(t *testing.T) {
	rows := []string{"0,sn1,Zorvane,eth0,", "1,sn1,Zorvane,eth0,SFP-A", "2,sn1,Zorvane,eth0,", "3,sn1,Zorvane,eth0,SFP-B"}
	var want string
	for n, p := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {1, 3, 0, 2}} {
		s := rdfNew(t, pgiPlan(t, idnXSource, idnXMapping))
		var order []string
		for _, i := range p {
			order = append(order, rows[i])
		}
		l := idnReceivedAt(s, t, pgiTexts{"ds_x": "row_id,serial,manufacturer,ifname,xname\n" + idnJoin(order...)})
		keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
		counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, idnStart(s, 2, 1), keys)
		if err != nil {
			t.Fatal(err)
		}
		if counts.NewObjects != 3 {
			t.Errorf("order %v makes %d objects, want the server, its interface and its transceiver: %+v", p, counts.NewObjects, counts)
		}
		got := idnState(s, t)
		if n == 0 {
			want = got
		} else if got != want {
			t.Errorf("order %v makes\n%s\nthe first order\n%s", p, got, want)
		}
	}
}

// [REQ-885]
// The code that was there does not read a key that is a place in a context (a feed that has one is not compiled by it), so no group
// whose witness is chosen among records of such a key is held to it: the ADR says so.
func TestIdnTheOldPipelineDoesNotCompileTheMappingOfTheKeysOfAContextThatTheWitnessIsChosenFor(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(idnXSource))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(idnXMapping))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Compile(reg, src, mp); err == nil {
		t.Error("the old compile reads the keys of a context: the witness can be held to the old pipeline")
	}
}
