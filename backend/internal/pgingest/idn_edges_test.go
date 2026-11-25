package pgingest

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// idnPoison makes the first run of each statement of the identification fail, one name after the other.
type idnPoison struct {
	done map[string]bool
	hit  int
}

func (p *idnPoison) idnPoisonAt(tx *sql.Tx, what, _ string, _ []any) {
	if p.done[what] {
		return
	}
	p.done[what] = true
	p.hit++
	_, _ = tx.Exec("SELECT 1/0")
}

// idnReceived receives and compares the datasets of a run and gives the lease.
func idnReceived(s *rdfSetup, t *testing.T, texts pgiTexts, size int) ingest.RunLease {
	t.Helper()
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan, Options: ingest.PageOptions{MaxRows: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfT0, size)); err != nil {
		t.Fatal(err)
	}
	return l
}

// [REQ-883]
func TestIdnEveryStatementOfAUnitThatFailsLeavesTheRunWhereItWasAndTheRunGoesOnToTheSameEnd(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	texts := idnPlaces("1,HQ,B1,F1,R1,rack a,RK1", "2,HQ,B1,F1,R1,rack b,RK2", "3,HQ,B1,F2,R2,rack c,RK3")
	l := idnReceived(s, t, texts, 1)
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	poison := &idnPoison{done: map[string]bool{}}
	identSeen = poison.idnPoisonAt
	defer func() { identSeen = nil }()
	var counts ingest.IdentifyCounts
	failed := 0
	for i := 0; i < 2000; i++ {
		c, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, idnStart(s, 1, 1), keys)
		if err == nil {
			counts = c
			break
		}
		failed++
	}
	if failed < 20 || failed != poison.hit {
		t.Errorf("%d runs failed for %d statements", failed, poison.hit)
	}
	if counts.NewObjects != 9 || counts.Unresolved != 0 {
		t.Errorf("%+v", counts)
	}
}

// [REQ-883] [REQ-885]
func TestIdnEveryStatementOfTheClaimsAndOfTheLookupThatFailsIsReportedAndTheNextRunIsTheSame(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, idnClaimSource, idnClaimMapping))
	l1, _ := idnPipeline(s, t, idnClaims("A,sn1,Zorvane,aa:aa:aa:aa:aa:01,on", "B,sn2,Zorvane,aa:aa:aa:aa:aa:02,on"), rdfT0, 0)
	idnApply(s, t)
	s.rdfSettle(t, l1)
	l := idnReceivedAt(s, t, idnClaims("A,sn1,Zorvane,aa:aa:aa:aa:aa:01,on", "B,sn2,Zorvane,aa:aa:aa:aa:aa:02,on", "C,sn3,Zorvane,aa:aa:aa:aa:aa:01,on",
		"D,sn4,Zorvane,aa:aa:aa:aa:aa:09,on", "H,snH,Zorvane,aa:aa:aa:aa:aa:08,off"))
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	poison := &idnPoison{done: map[string]bool{}}
	identSeen = poison.idnPoisonAt
	defer func() { identSeen = nil }()
	var counts ingest.IdentifyCounts
	for i := 0; i < 2000; i++ {
		c, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, idnStart(s, 2, 1), keys)
		if err == nil {
			counts = c
			break
		}
	}
	if poison.hit < 20 || counts.Bound == 0 {
		t.Errorf("%d statements failed, %+v", poison.hit, counts)
	}
}

func idnReceivedAt(s *rdfSetup, t *testing.T, texts pgiTexts) ingest.RunLease {
	t.Helper()
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan, Options: ingest.PageOptions{MaxRows: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfT0.Add(3600e9), 0)); err != nil {
		t.Fatal(err)
	}
	return l
}

// [REQ-883]
func TestIdnTheCallsOfTheIdentificationThatAreMadeOutOfOrderOrOnARunThereIsNotAreRefused(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	ctx := s.e.ctx
	if _, err := s.e.st.IdentifyPlace(ctx, 99999); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Errorf("place of no run: %v", err)
	}
	if _, err := s.e.st.IdentifyReport(ctx, 99999); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Errorf("report of no run: %v", err)
	}
	l := idnReceived(s, t, idnPlaces("1,HQ,B1,F1,R1,rack a,RK1"), 0)
	if p, err := s.e.st.IdentifyPlace(ctx, l.No); err != nil || p.Begun {
		t.Errorf("%+v %v", p, err)
	}
	for name, err := range map[string]error{
		"step":    s.e.st.IdentifyStep(ctx, l),
		"put":     s.e.st.PutRebuilt(ctx, l, 0, nil),
		"answers": s.e.st.PutAnswers(ctx, l, nil),
	} {
		if err == nil {
			t.Errorf("%s before the begin", name)
		}
	}
	if _, err := s.e.st.RebuildPage(ctx, l); err == nil {
		t.Error("a page before the begin")
	}
	if _, err := s.e.st.AskPage(ctx, l); err == nil {
		t.Error("asks before the begin")
	}
	if err := s.e.st.BeginIdentify(ctx, l, idnStart(s, 0, 0)); err != nil {
		t.Fatal(err)
	}
	// the place is the seed: neither the keys of Go nor the answers are wanted, and a unit of the wrong unit number is refused
	if _, err := s.e.st.RebuildPage(ctx, l); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page at the seed: %v", err)
	}
	if err := s.e.st.PutRebuilt(ctx, l, 0, nil); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("keys at the seed: %v", err)
	}
	if _, err := s.e.st.AskPage(ctx, l); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("asks at the seed: %v", err)
	}
	if err := s.e.st.PutAnswers(ctx, l, nil); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("answers at the seed: %v", err)
	}
	stale := l
	stale.Attempt += 5
	if err := s.e.st.IdentifyStep(ctx, stale); err == nil {
		t.Error("a step of a lease that is not the run's")
	}
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	for i := 0; i < 3; i++ {
		if err := s.e.st.IdentifyStep(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.e.st.PutRebuilt(ctx, l, 7, nil); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("keys of another unit: %v", err)
	}
	if _, err := ingest.RunIdentify(ctx, s.e.st, l, idnStart(s, 0, 0), keys); err != nil {
		t.Fatal(err)
	}
	if err := s.e.st.IdentifyStep(ctx, l); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a step of a finished identification: %v", err)
	}
}

// [REQ-880]
func TestIdnTheCellsOfARecordAreReadBackWhateverTheyHold(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
		bad  bool
	}{
		{in: `{a,b}`, want: []string{"a", "b"}},
		{in: `{"a b","c\"d",NULL,e}`, want: []string{"a b", `c"d`, "", "e"}},
		{in: `{}`, want: []string{}},
		{in: `a,b`, bad: true},
		{in: ``, bad: true},
	} {
		got, err := parseTextArray(c.in)
		if (err != nil) != c.bad || (!c.bad && strings.Join(got, "|") != strings.Join(c.want, "|")) {
			t.Errorf("%q: %q %v", c.in, got, err)
		}
	}
}

// [REQ-882]
func TestIdnARunThatHasFewerRoundsThanTheKeysNeedLeavesTheRecordsUnresolvedAndSaysSo(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	l := idnReceived(s, t, idnPlaces("1,HQ,B1,F1,R1,rack a,RK1", "2,HQ,B1,F1,R1,rack b,RK2"), 1)
	keys := ingest.NewPlanKeys(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan})
	start := idnStart(s, 1, 1)
	start.Rounds, start.Turns = 1, 1
	counts, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, start, keys)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Unresolved == 0 {
		t.Errorf("%+v", counts)
	}
}

// [REQ-880] [REQ-884]
func TestIdnTheRecordsOfSmallUnitsWithReferencesNotSolvedAndWithEndsAreAllBound(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	_, counts := idnPipeline(s, t, pgiAll(), rdfT0, 1)
	if counts.NewObjects != 7 || counts.Unresolved != 0 {
		t.Errorf("%+v", counts)
	}
	s2 := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	idnPutPlace(s2, t, "1", 0, "1", "HQ", "B1", "F1", "R1", "rack a", "RK1")
	idnPutPlace(s2, t, "2", 0, "2", "HQ", "B1", "F1", "R1", "rack b", "RK2")
	idnPutPlace(s2, t, "3", 7, "3", "HQ", "B1", "F2", "R2", "rack c", "RK3")
	if _, c := idnPipeline(s2, t, pgiTexts{}, rdfT0, 1); c.Rebuilt != 2 {
		t.Errorf("%+v", c)
	}
}

// idnPutRaw keeps a raw record of a dataset with references that are not solved, for the feeds that read it.
func idnPutRaw(s *rdfSetup, t *testing.T, dataset, key string, cells ...string) {
	t.Helper()
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, $3, $4, $5, 0, 1, 0)`, pgiTable(s.ref, pgschema.TableRawRecord)),
		s.ref.SrcNo, s.rdfNo(dataset), key, rdfHash(key), textArray(cells))
	for feed := 0; feed < 5; feed++ {
		s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, state) VALUES ($1, $2, $3, 1, $4, 0, %d)`, k, pgschema.RefUnresolved),
			s.ref.SrcNo, s.rdfNo(dataset), key, feed)
	}
}

// [REQ-880] [REQ-885]
func TestIdnTheEndsOfTheLinksAndTheClaimsOfARecordGoErasedFromItsCellsAreKeptWithTheKeys(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	idnPutRaw(s, t, "ds_servers", "SV1", "SV1", "sn100", "Zorvane", "RK1", "on")
	idnPutRaw(s, t, "ds_servers", "SV2", "SV2", "sn200", "Zorvane", "RK2", "off")
	_, counts := idnPipeline(s, t, pgiTexts{}, rdfT0, 1)
	k := s.ref.KGNo
	if counts.Rebuilt != 2 || s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_end`, k)) == 0 {
		t.Errorf("%+v", counts)
	}
	c := rdfNew(t, pgiPlan(t, idnClaimSource, idnClaimMapping))
	idnPutRaw(c, t, "ds_s", "A", "A", "sn1", "Zorvane", "aa:aa:aa:aa:aa:01", "on")
	idnPutRaw(c, t, "ds_s", "B", "B", "sn2", "Zorvane", "aa:aa:aa:aa:aa:02", "on")
	_, counts = idnPipeline(c, t, pgiTexts{}, rdfT0, 1)
	if counts.Rebuilt != 2 || c.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_claim`, c.ref.KGNo)) == 0 {
		t.Errorf("%+v", counts)
	}
}
