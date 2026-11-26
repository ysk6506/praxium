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

// The records the run only reads again (ADR-0122, decision 3): their ends are the ones their references say, given to them by the phase
// ends; a run is taken over in the phases and stages the same; and the records of another source that make the same link are held in the
// same order. Every helper starts with rcl.

// rclAgain does a first run of the texts to the end and a second one of the same texts up to the end of the computation of the objects:
// the records of the second run are ones it only reads again, but the ones that have a reference that is not solved (the run reads them
// as unresolved).
func rclAgain(t *testing.T, texts pgiTexts) (*rdfSetup, ingest.RunLease) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	l := rclPrepare(s, t, texts, rclAt(0), true)
	rclDo(s, t, l, rclPlans(s), 0)
	rcpApply(s, t)
	s.rdfSettle(t, l)
	return s, rclPrepare(s, t, texts, rclAt(1), true)
}

// rclEndsOfReadAgain is how many ends the references of the records the run only reads again have.
func rclEndsOfReadAgain(s *rdfSetup, t *testing.T) int {
	t.Helper()
	k := s.ref.KGNo
	return s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".calc_record c JOIN "s_%[1]d".record_ref r ON r.src_no = c.src_no AND r.dataset_no = c.dataset_no
		AND r.key = c.key WHERE c.why = %[2]d AND r.role IN (%[3]d, %[4]d) AND r.lookup_hash IS NOT NULL`, k, pgschema.CalcWhyExpanded, pgschema.RefRoleLinkFrom, pgschema.RefRoleLinkTo))
}

// rclInject gives the server SV5 of rclRich, a record the run only reads again, its end of the From side in the run already, as the key of
// the rack RC (as a step before the links would have written it).
func rclInject(s *rdfSetup, t *testing.T) {
	t.Helper()
	k := s.ref.KGNo
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record WHERE key = 'SV5' AND why = %d`, k, pgschema.CalcWhyExpanded)); n != 1 {
		t.Fatalf("the server SV5 is not a record the run only reads again (%d)", n)
	}
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_end (rid, def_no, side, lookup_hash, held)
		SELECT c.rid, r.def_no, r.role, (SELECT x.lookup_hash FROM "s_%[1]d".record_ref x WHERE x.key = 'RC' AND x.role = %[3]d), false
		FROM "s_%[1]d".calc_record c JOIN "s_%[1]d".record_ref r ON r.src_no = c.src_no AND r.dataset_no = c.dataset_no AND r.key = c.key AND r.role = %[2]d
		WHERE c.key = 'SV5'`, k, pgschema.RefRoleLinkFrom, pgschema.RefRoleLinkTo))
}

// rclEndKept checks that the end that was in the run already is kept: it is not given the one its reference says on top (the other ends of
// the records read again are), it has one end, and the server is linked to the rack its end in the run says.
func rclEndKept(s *rdfSetup, t *testing.T, carried int, counts ingest.RecomputeLinksCounts) {
	t.Helper()
	k := s.ref.KGNo
	if carried < 4 || counts.Carried != carried-1 {
		t.Errorf("the references have %d ends and %d were given: %+v", carried, counts.Carried, counts)
	}
	ends := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".calc_end e JOIN "s_%[1]d".calc_record c ON c.rid = e.rid WHERE c.key = 'SV5' AND e.side = %[2]d`, k, pgschema.RefRoleLinkFrom))
	if ends != 1 {
		t.Errorf("the server has %d ends of the From side", ends)
	}
	rc := expList(s.e, t, fmt.Sprintf(`SELECT o.ref FROM "s_%[1]d".record_ref r JOIN "s_%[1]d".object o ON o.obj_no = r.target_no WHERE r.key = 'RC' AND r.role = %[2]d`, k, pgschema.RefRoleIdentity))
	linked := expList(s.e, t, fmt.Sprintf(`SELECT COALESCE(o.ref, '') FROM "s_%[1]d".calc_ref r JOIN "s_%[1]d".calc_record c ON c.rid = r.rid JOIN "s_%[1]d".stage_link l ON l.link_no = r.target_no
		LEFT JOIN "s_%[1]d".object o ON o.obj_no = l.dst WHERE c.key = 'SV5' AND r.role = %[2]d`, k, pgschema.RefRoleLinkFrom))
	if len(rc) != 1 || strings.Join(linked, " ") != rc[0] {
		t.Errorf("the server is linked to %v and not to the rack its end in the run says (%v)", linked, rc)
	}
}

// rclOnePage reads, makes and puts the page the place says, and gives the place after it.
func rclOnePage(s *rdfSetup, t *testing.T, l ingest.RunLease, maker ingest.LinkMaker) ingest.RecomputeLinksPlace {
	t.Helper()
	st, ctx := s.e.st, s.e.ctx
	page, err := st.RecomputeLinksPage(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	made, err := maker.MakeLinks(page)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutRecomputedLinks(ctx, l, made); err != nil {
		t.Fatal(err)
	}
	place, err := st.RecomputeLinksPlace(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	return place
}

// rclTakeOver lets the lease of the run run out and has another server take the run over.
func rclTakeOver(s *rdfSetup, t *testing.T, l ingest.RunLease, owner string) ingest.RunLease {
	t.Helper()
	s.e.expire(t, l.No)
	next, err := s.e.st.AcquireRun(s.e.ctx, l.No, owner, l.Term)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// rclStopTwice does the computation of the links of the second run, stopped in the phase ends after a page (the next page is read and
// made and the server is gone before it is put) and taken over, and stopped again in the phase from and taken over again.
func rclStopTwice(s *rdfSetup, t *testing.T, l ingest.RunLease) (ingest.RunLease, ingest.RecomputeLinksCounts) {
	t.Helper()
	st, ctx, plans := s.e.st, s.e.ctx, rclPlans(s)
	maker := rclMaker(t, s, plans)
	if err := st.BeginRecomputeLinks(ctx, l, rclStart(plans, 2)); err != nil {
		t.Fatal(err)
	}
	if p := rclOnePage(s, t, l, maker); p.Phase != ingest.LinksPhaseEnds || p.Unit != 1 || p.Done() {
		t.Fatalf("the place after a page: %+v", p)
	}
	page, err := st.RecomputeLinksPage(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maker.MakeLinks(page); err != nil {
		t.Fatal(err)
	}
	l2 := rclTakeOver(s, t, l, "server-b")
	other := rclStart(plans, 2)
	other.Defs = append(append([]ingest.RecomputeLinkDef{}, other.Defs...), ingest.RecomputeLinkDef{SrcNo: 99, Def: 0, FromFeed: 1, ToFeed: 2})
	if _, err := ingest.RunRecomputeLinks(ctx, st, l2, other, maker); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Errorf("a takeover with other definitions: %v", err)
	}
	// the second server goes on to a page of the phase from, and is gone
	for p, n := rclOnePage(s, t, l2, maker), 0; p.Phase != ingest.LinksPhaseFrom; p, n = rclOnePage(s, t, l2, maker), n+1 {
		if n > 50 || p.Done() {
			t.Fatalf("the phase from is not reached: %+v", p)
		}
	}
	if p := rclOnePage(s, t, l2, maker); p.Phase != ingest.LinksPhaseFrom {
		t.Fatalf("the phase from has one page: %+v", p)
	}
	l3 := rclTakeOver(s, t, l2, "server-c")
	counts, err := ingest.RunRecomputeLinks(ctx, st, l3, rclStart(plans, 2), maker)
	if err != nil {
		t.Fatal(err)
	}
	return l3, counts
}

// [REQ-874] [REQ-878]
func TestRclARunTakenOverInThePhasesStagesWhatOneThatDidNotStopStagesAndAnEndInTheRunAlreadyIsKept(t *testing.T) {
	s, l := rclAgain(t, rclRich(false))
	carried := rclEndsOfReadAgain(s, t)
	rclInject(s, t)
	counts := rclDo(s, t, l, rclPlans(s), 2)
	rclEndKept(s, t, carried, counts)
	want := rclOutcomeOf(s, t, counts)
	if counts.Links == 0 || counts.Problems == 0 {
		t.Fatalf("the case makes nothing: %+v", counts)
	}

	s, l = rclAgain(t, rclRich(false))
	rclInject(s, t)
	l3, counts := rclStopTwice(s, t, l)
	if got := rclOutcomeOf(s, t, counts); got != want {
		t.Errorf("a run that was taken over made:\n%+v\none that was not:\n%+v", got, want)
	}
	// a run that is done is done: another page is refused, and the report is the same
	st, ctx := s.e.st, s.e.ctx
	if place, err := st.RecomputeLinksPlace(ctx, l3); err != nil || !place.Done() || place.Phase != ingest.LinksPhaseDone {
		t.Errorf("%+v %v", place, err)
	}
	if _, err := st.RecomputeLinksPage(ctx, l3); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page of a step that is done: %v", err)
	}
	if err := st.PutRecomputedLinks(ctx, l3, ingest.RecomputedLinksPage{}); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a put to a step that is done: %v", err)
	}
	if again, err := ingest.RunRecomputeLinks(ctx, st, l3, rclStart(rclPlans(s), 2), rclMaker(t, s, rclPlans(s))); err != nil || again != counts {
		t.Errorf("a step that is done is done again: %+v %v", again, err)
	}
}

// [REQ-869] [REQ-870]
func TestRclTheSameLinkMadeByTheRecordsOfTwoSourcesIsMadeByTheLaterSourceAndTheOtherIsUnresolvedAndTold(t *testing.T) {
	k := rclTwo(t, rclMappingB)
	la := rclPrepare(k.a, t, rclTextsA([]string{"RA,RK1"}, []string{"SV7,Zorvane,sn7,RK1,3,on"}, []string{"u1"}), rclAt(0), true)
	if n := rclDo(k.a, t, la, k.plans, 0).Links; n != 1 {
		t.Fatalf("%d links in the run of the first source", n)
	}
	rcpApply(k.a, t)
	k.a.rdfSettle(t, la)
	// the second source finds the rack and the server the first one made and says the server is mounted in the rack at another place; the
	// run of the whole KG reads the records of the first source again, with the plans of both
	lb, _ := idnPipeline(k.b, t, rclTextsB([]string{"RA,RK1"}, []string{"M7,Zorvane,sn7,RK1,5"}), rclAt(1), 0)
	rcpExpand(k.b, t, lb, true)
	rclObjects(k.b, t, lb, k.plans, rclAt(1).UTC().Format(time.RFC3339), 0)
	counts := rclDo(k.b, t, lb, k.plans, 0)
	got := rclStaged(k.b, t)
	if len(got) != 1 || counts.Links != 1 || counts.Lost != 1 || counts.Carried == 0 {
		t.Fatalf("the links: %s (%+v)", rclText(got), counts)
	}
	var link rclLink
	for _, g := range got {
		link = g
	}
	if !strings.Contains(link.SourceRefs, "src_rcl_b/ds_mt/M7") || strings.Contains(link.SourceRefs, "src_rcl_a") || !strings.Contains(link.Doc, "5") {
		t.Errorf("the record of the later source does not make the link: %s", link.rclText())
	}
	refs := strings.Join(rclRefs(k.b, t), "\n")
	if !strings.Contains(refs, "ds_sv/SV7/2/1/-") || !strings.Contains(refs, "ds_mt/M7/2/0/"+link.Ref) {
		t.Errorf("the references:\n%s", refs)
	}
	if problems := strings.Join(rclProblems(k.b, t), " "); problems != "link_conflict/ds_sv/SV7" {
		t.Errorf("the problems are %v: the record of the first source made the link with other attributes", problems)
	}
}
