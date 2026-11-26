package pgingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the computation of the links stages does not depend on the size of a page, on the order the rows come in, on the numbers the records
// have, or on whether the step stopped on the way and another server went on (ADR-0122). Every helper starts with rcl.

// rclRich is a world with every kind of end: a rack of two records of one tag (ambiguous), a missing rack, a server of three records (one
// link, the later wins, and the attributes of two of them differ), a server a rule holds, a wrong cell, and ends of the second source.
func rclRich(reverse bool) pgiTexts {
	racks := []string{"RA,RK1", "RB,RK2", "RC,RK3", "RD,RK1"}
	servers := []string{
		"SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on", "SV3,Zorvane,sn3,RK3,3,on", "SV3b,Zorvane,sn3,RK3,4,on", "SV3c,Zorvane,sn3,RK3,4,on",
		"SV2h,Zorvane,sn2,RK2,9,off", "SV4,Zorvane,sn4,RK9,4,on", "SV5,Zorvane,sn5,RK2,front,on", "SV6,Zorvane,sn6,RK3,,on",
	}
	if reverse {
		for _, rows := range [][]string{racks, servers} {
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	return rclTextsA(racks, servers, []string{"u1", "u2"})
}

// rclOutcome is everything a computation of the links made, without the numbers the run gave.
type rclOutcome struct {
	links    string
	problems string
	refs     string
	counts   ingest.RecomputeLinksCounts
}

func rclOutcomeOf(s *rdfSetup, t *testing.T, counts ingest.RecomputeLinksCounts) rclOutcome {
	t.Helper()
	return rclOutcome{links: rclText(rclStaged(s, t)), problems: strings.Join(rclProblems(s, t), "\n"), refs: strings.Join(rclRefs(s, t), "\n"), counts: counts}
}

// rclWhole does the computation of the links of a world in pages of the size given, and checks that the report counts the rows that were
// written.
func rclWhole(t *testing.T, texts pgiTexts, lines int) rclOutcome {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	l := rclPrepare(s, t, texts, rclAt(0), true)
	counts := rclDo(s, t, l, rclPlans(s), lines)
	rclRows(s, t, counts)
	if counts.Ends == 0 || counts.Ends < counts.Links {
		t.Errorf("the ends are %d for %d links", counts.Ends, counts.Links)
	}
	return rclOutcomeOf(s, t, counts)
}

// [REQ-873] [REQ-875]
func TestRclThePageSizeAndTheOrderOfTheRowsDoNotChangeWhatIsStagedAndTheReportCountsTheRowsThatWereWritten(t *testing.T) {
	want := rclWhole(t, rclRich(false), 0)
	if want.counts.Links != 4 || want.counts.Lost != 3 || want.counts.Missing == 0 || want.counts.Ambiguous == 0 || want.counts.Conflicts == 0 || want.counts.Cells == 0 {
		t.Fatalf("the world does not have every kind of end: %+v", want.counts)
	}
	for name, got := range map[string]rclOutcome{
		"pages of one end, the rows reversed": rclWhole(t, rclRich(true), 1),
		"pages of three ends":                 rclWhole(t, rclRich(false), 3),
	} {
		if got.links != want.links {
			t.Errorf("%s staged other links:\n%s\nand not\n%s", name, got.links, want.links)
		}
		if got.problems != want.problems {
			t.Errorf("%s: the problems are\n%s\nand not\n%s", name, got.problems, want.problems)
		}
		if got.refs != want.refs {
			t.Errorf("%s: the references are\n%s\nand not\n%s", name, got.refs, want.refs)
		}
		if got.counts != want.counts {
			t.Errorf("%s: the report is %+v and not %+v", name, got.counts, want.counts)
		}
	}
}

// rclRows counts the rows a report says it wrote.
func rclRows(s *rdfSetup, t *testing.T, counts ingest.RecomputeLinksCounts) {
	t.Helper()
	k := s.ref.KGNo
	for _, c := range []struct {
		what  string
		query string
		said  int
	}{
		{"links", fmt.Sprintf(`SELECT count(*) FROM "s_%d".stage_link`, k), counts.Links},
		{"keys", fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_tokey`, k), counts.Keys},
		{"records that lost", fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_linklost`, k), counts.Lost},
		{"references", fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_ref WHERE role IN (%d, %d)`, k, pgschema.RefRoleLinkFrom, pgschema.RefRoleLinkTo), counts.Refs},
		{"problems of the ends", fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem WHERE kind IN ('%s', '%s')`, k, pgschema.ProblemLinkTarget, pgschema.ProblemLinkAmbiguous), counts.Missing + counts.Ambiguous},
		{"link conflicts", fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem WHERE kind = '%s' AND reason LIKE '%s%%'`, k, pgschema.ProblemSourceConflict, rclConflictReason), counts.Conflicts},
		{"cells", fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_problem WHERE kind = '%s'`, k, pgschema.ProblemInvalidValue), counts.Cells},
	} {
		if got := s.e.count(t, c.query); got != c.said || got == 0 {
			t.Errorf("the report says %d %s and %d are written", c.said, c.what, got)
		}
	}
	if counts.Problems != counts.Missing+counts.Ambiguous+counts.Conflicts+counts.Cells {
		t.Errorf("the problems are %d and the parts are %+v", counts.Problems, counts)
	}
}

// [REQ-874]
func TestRclAPageIsDoneOnceAndAServerThatLostTheLeaseIsRefusedEvenToReadAndTheCallsInTheWrongOrderAreRefused(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	l, _ := idnPipeline(s, t, rclRich(false), rclAt(0), 0)
	rcpExpand(s, t, l, true)
	st, ctx, plans := s.e.st, s.e.ctx, rclPlans(s)
	start := rclStart(plans, 2)
	if err := st.BeginRecomputeLinks(ctx, l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("the links before the objects: %v", err)
	}
	rclObjects(s, t, l, plans, rcpSeen, 0)
	if _, err := st.RecomputeLinksPage(ctx, l); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page before the beginning: %v", err)
	}
	if _, err := st.RecomputeLinksReport(ctx, l); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a report before the beginning: %v", err)
	}
	if err := st.BeginRecomputeLinks(ctx, l, ingest.RecomputeLinksStart{Lines: -1}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a start that cannot work: %v", err)
	}
	if err := st.BeginRecomputeLinks(ctx, l, start); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginRecomputeLinks(ctx, l, start); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("the computation begun twice: %v", err)
	}
	maker := rclMaker(t, s, plans)
	page, err := st.RecomputeLinksPage(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	made, err := maker.MakeLinks(page)
	if err != nil {
		t.Fatal(err)
	}
	// a page that is not the place's, and one that ends before the place, are refused and change nothing
	wrong := made
	wrong.Unit++
	if err := st.PutRecomputedLinks(ctx, l, wrong); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page of another unit: %v", err)
	}
	wrong = made
	wrong.Phase = ingest.LinksPhaseFrom
	if err := st.PutRecomputedLinks(ctx, l, wrong); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("a page of another phase: %v", err)
	}
	wrong = made
	wrong.Last, wrong.Through = false, rclFirst()
	if err := st.PutRecomputedLinks(ctx, l, wrong); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a page that ends before the place: %v", err)
	}
	if err := st.PutRecomputedLinks(ctx, l, made); err != nil {
		t.Fatal(err)
	}
	before, _ := st.RecomputeLinksReport(ctx, l)
	if err := st.PutRecomputedLinks(ctx, l, made); kernel.CodeOf(err) != ingest.CodeRunStage {
		t.Errorf("the same page put twice: %v", err)
	}
	if after, _ := st.RecomputeLinksReport(ctx, l); after != before {
		t.Errorf("a page that was refused counted: %+v and %+v", before, after)
	}
	s.e.expire(t, l.No)
	if _, err := st.AcquireRun(ctx, l.No, "server-b", l.Term); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"begin":  func() error { return st.BeginRecomputeLinks(ctx, l, start) },
		"place":  func() error { _, err := st.RecomputeLinksPlace(ctx, l); return err },
		"page":   func() error { _, err := st.RecomputeLinksPage(ctx, l); return err },
		"put":    func() error { return st.PutRecomputedLinks(ctx, l, ingest.RecomputedLinksPage{Unit: 1}) },
		"report": func() error { _, err := st.RecomputeLinksReport(ctx, l); return err },
	} {
		if err := call(); kernel.CodeOf(err) != ingest.CodeRunLeaseLost {
			t.Errorf("%s by the server that lost the lease: %v", name, err)
		}
	}
	if after, _ := s.e.st.RecomputeLinksReport(ctx, ingest.RunLease{No: l.No, Attempt: l.Attempt + 1, Term: l.Term, Owner: "server-b"}); after != before {
		t.Errorf("a refused call changed the counts: %+v and %+v", before, after)
	}
}

// [REQ-868]
func TestRclALinkTheRegistryHasKeepsItsNumberAndANewOneGetsANumberNoLinkHas(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	texts := rclTextsA([]string{"RA,RK1", "RB,RK2"}, []string{"SV1,Zorvane,sn1,RK1,1,on"}, []string{"u1"})
	l := rclPrepare(s, t, texts, rclAt(0), true)
	first := rclDo(s, t, l, rclPlans(s), 0)
	if first.Links != 1 {
		t.Fatalf("%+v", first)
	}
	old := rclStaged(s, t)
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from)
		SELECT link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, 1 FROM "s_%[1]d".stage_link`, k))
	rcpApply(s, t)
	s.rdfSettle(t, l)
	texts = rclTextsA([]string{"RA,RK1", "RB,RK2"}, []string{"SV1,Zorvane,sn1,RK1,7,on", "SV2,Zorvane,sn2,RK2,2,on"}, []string{"u1"})
	l = rclPrepare(s, t, texts, rclAt(1), true)
	rclDo(s, t, l, rclPlans(s), 0)
	got := rclStaged(s, t)
	if len(got) != 2 {
		t.Fatalf("%d links: %s", len(got), rclText(got))
	}
	used := map[int64]bool{}
	for ref, g := range got {
		if used[g.No] {
			t.Errorf("the number %d is two links'", g.No)
		}
		used[g.No] = true
		if o, ok := old[ref]; ok && o.No != g.No {
			t.Errorf("the link %s had the number %d and has %d", ref, o.No, g.No)
		} else if !ok && g.No == 0 {
			t.Errorf("the new link %s has no number", ref)
		}
	}
	// and the record that made it says the number
	refs := strings.Join(rclRefs(s, t), "\n")
	for _, g := range got {
		if !strings.Contains(refs, "/2/0/"+g.Ref) {
			t.Errorf("no record says the link %s:\n%s", g.Ref, refs)
		}
	}
}
