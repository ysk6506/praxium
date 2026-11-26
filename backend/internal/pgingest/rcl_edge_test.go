package pgingest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The cases at the edge of the computation of the links (ADR-0122): an object that cannot be linked to, or from, and a record that says
// nothing that can be linked. Every helper starts with rcl.

// rclDangling is how many staged links have an end that is no object of the KG and none the run stages.
func rclDangling(s *rdfSetup, t *testing.T) int {
	t.Helper()
	k := s.ref.KGNo
	return s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".stage_link l WHERE
		NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_object o WHERE o.obj_no = l.src UNION ALL SELECT 1 FROM "s_%[1]d".object o WHERE o.obj_no = l.src)
		OR NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_object o WHERE o.obj_no = l.dst UNION ALL SELECT 1 FROM "s_%[1]d".object o WHERE o.obj_no = l.dst)`, k))
}

func rclAt(n int) time.Time { return rdfT0.Add(time.Duration(n) * 13 * time.Hour) }

// [REQ-871] [REQ-879]
func TestRclALinkToAnObjectThatWasRolledBackIsNoLinkAndALinkFromOneIsNoLinkAndNoProblem(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclTagIdentity()))
	// the two racks of the tag T1 are one object of two lines, and the limit of one line leaves it out; so is the server that two records
	// stand for (S3 and S3b), and the rack T3 and the server S2 are made
	texts := rclTextsA([]string{"R1,T1", "R2,T1", "R3,T3"},
		[]string{"S1,Zorvane,sn1,T1,1,on", "S2,Zorvane,sn2,T3,2,on", "S3,Zorvane,sn3,T3,3,on", "S3b,Zorvane,sn3,T3,4,on"}, []string{"u1"})
	l := rclPrepareMax(s, t, texts, rclAt(0), true, 1)
	counts := rclDo(s, t, l, rclPlans(s), 0)
	got := rclStaged(s, t)
	if len(got) != 1 || counts.Links != 1 {
		t.Fatalf("the links: %s (%+v)", rclText(got), counts)
	}
	for _, g := range got {
		if g.Src == "" || g.Dst == "" {
			t.Errorf("a link has an end that no table knows: %s", g.rclText())
		}
	}
	if problems := rclProblems(s, t); strings.Join(problems, " ") != "link_target_missing/ds_sv/S1" {
		t.Errorf("the problems are %v: a rack that was rolled back is a missing target, and a server that was rolled back is no problem", problems)
	}
	if n := rclDangling(s, t); n != 0 {
		t.Errorf("%d links point at an object that is not there", n)
	}
	refs := strings.Join(rclRefs(s, t), "\n")
	for _, no := range []string{"/S3/2/", "/S3b/2/"} {
		if strings.Contains(refs, no) {
			t.Errorf("a record whose object was rolled back has a reference of a link (%s):\n%s", no, refs)
		}
	}
	if !strings.Contains(refs, "/S1/2/1/-") || !strings.Contains(refs, "/S2/2/0/lnk_") {
		t.Errorf("the references of the ends:\n%s", refs)
	}
}

// [REQ-871]
func TestRclALinkToAnObjectThatIsALeftAsItIsOrACandidateForRetirementIsMade(t *testing.T) {
	mapping := strings.Replace(rclTagIdentity(), `"creates_objects":true,"basis":"declared"`, `"creates_objects":true,"retires_objects":true,"basis":"declared"`, 1)
	s := rdfNew(t, pgiPlan(t, rclSourceA, mapping))
	servers := []string{"S1,Zorvane,sn1,T1,1,on", "S2,Zorvane,sn2,T3,2,on"}
	l := rclPrepare(s, t, rclTextsA([]string{"R1,T1", "R3,T3"}, servers, []string{"u1"}), rclAt(0), true)
	if n := rclDo(s, t, l, rclPlans(s), 0).Links; n != 2 {
		t.Fatalf("%d links in the first run", n)
	}
	rcpApply(s, t)
	s.rdfSettle(t, l)
	// the rack T3 has two records now and the limit is one line: the object is left as it is, and a link still is made to it; the rack T1 is
	// marked as a candidate for retirement by the run, and a link is made to it too
	l = rclPrepareMax(s, t, rclTextsA([]string{"R1,T1", "R3,T3", "R4,T3"}, servers, []string{"u1"}), rclAt(1), true, 1)
	k := s.ref.KGNo
	if left := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_left WHERE NOT is_new`, k)); left != 1 {
		t.Fatalf("%d existing objects are left as they are", left)
	}
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".calc_object (obj_no, why) SELECT target_no, %[2]d FROM "s_%[1]d".record_ref WHERE key = 'R1' AND role = %[3]d
		ON CONFLICT (obj_no) DO UPDATE SET why = excluded.why`, k, pgschema.ObjectWhyRetire, pgschema.RefRoleIdentity))
	if retiring := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_object WHERE why = %d`, k, pgschema.ObjectWhyRetire)); retiring != 1 {
		t.Fatalf("%d objects are candidates for retirement", retiring)
	}
	// the record of the rack T1 is one the run only reads again (it did not change): its To end is the one its reference says (the phase
	// ends) and its object the one its reference of the identification says, so the candidate for retirement is found that way too
	if again := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record WHERE key = 'R1' AND why = %d`, k, pgschema.CalcWhyExpanded)); again != 1 {
		t.Fatalf("the record of the rack T1 is not one the run only reads again (%d)", again)
	}
	counts := rclDo(s, t, l, rclPlans(s), 0)
	got := rclStaged(s, t)
	if counts.Links != 2 || len(got) != 2 || len(rclProblems(s, t)) != 0 || rclDangling(s, t) != 0 {
		t.Errorf("the links: %s (%+v, %v)", rclText(got), counts, rclProblems(s, t))
	}
	marked := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".stage_link l JOIN "s_%[1]d".calc_object o ON o.obj_no = l.dst AND o.why = %[2]d`, k, pgschema.ObjectWhyRetire))
	if marked != 1 {
		t.Errorf("%d links are to the candidate for retirement", marked)
	}
}

// [REQ-869] [REQ-870] [REQ-872]
func TestRclTheLinkOfADuplicateIsMadeByTheRecordThatComesLastACellThatGaveNoValueIsSaidAndAHeldRecordOrABlankKeyIsNoProblem(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	// three records of the server sn1 make one link; the place of S2 is a word (no value), the one of S3 is blank (none, and no problem).
	// The server sn1 shows a fourth time in a row a rule holds, which says a rack that is not there (no problem, and no link); S4 says no
	// rack at all (no link, no reference); S5 is held and was never an object (nothing)
	texts := rclTextsA([]string{"RA,RK1", "RB,RK2"}, []string{"S1,Zorvane,sn1,RK1,1,on", "S1b,Zorvane,sn1,RK1,5,on", "S1c,Zorvane,sn1,RK1,5,on",
		"S2,Zorvane,sn2,RK2,front,on", "S3,Zorvane,sn3,RK2,,on", "S1h,Zorvane,sn1,RK9,2,off", "S4,Zorvane,sn4,,4,on", "S5,Zorvane,sn5,RK1,3,off"}, []string{"u1"})
	l := rclPrepare(s, t, texts, rclAt(0), true)
	counts := rclDo(s, t, l, rclPlans(s), 0)
	got := rclStaged(s, t)
	if len(got) != 3 || counts.Links != 3 || counts.Lost != 2 || counts.Cells != 1 {
		t.Fatalf("the links: %s (%+v)", rclText(got), counts)
	}
	var link rclLink
	for _, g := range got {
		if strings.Contains(g.SourceRefs, "/S1") {
			link = g
		}
	}
	if !strings.Contains(link.Doc, "5") || !strings.Contains(link.SourceRefs, "S1c") {
		t.Errorf("the record that comes last does not make the link: %s", link.rclText())
	}
	cells := expList(s.e, t, fmt.Sprintf(`SELECT c.key || '/' || p.kind || '/' || p.def_no || '/' || p.field FROM "s_%[1]d".calc_problem p JOIN "s_%[1]d".calc_record c ON c.rid = p.rid
		WHERE p.kind = 'invalid_value' ORDER BY 1`, s.ref.KGNo))
	if strings.Join(cells, " ") != "S2/invalid_value/-1/" {
		t.Errorf("the problems of the cells are %v: a cell that is blank gives none", cells)
	}
	refs := strings.Join(rclRefs(s, t), "\n")
	for _, w := range []string{"ds_sv/S1/2/1/-", "ds_sv/S1b/2/1/-", "ds_sv/S1c/2/0/" + link.Ref, "ds_sv/S1h/2/3/-"} {
		if !strings.Contains(refs, w) {
			t.Errorf("no reference %s in:\n%s", w, refs)
		}
	}
	if strings.Contains(refs, "/S4/2/") || strings.Contains(refs, "/S5/") {
		t.Errorf("a record with a blank key, or one that was never an object, has a reference of a link:\n%s", refs)
	}
	// the chain of the records: S1 and S1b made it with other attributes (S1 is told), S1b and S1c with the same (nobody is); the held
	// record and the blank key are no problem
	if problems := strings.Join(rclProblems(s, t), " "); problems != "link_conflict/ds_sv/S1" {
		t.Errorf("the problems are %v", problems)
	}
}

// [REQ-878]
func TestRclARecordThatTheRunOnlyReadsAgainBecauseItsEndMovedIsResolvedByTheEndsItsReferencesSay(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	servers := []string{"SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on"}
	l := rclPrepare(s, t, rclTextsA([]string{"RA,RK1", "RB,RK2"}, servers, []string{"u1"}), rclAt(0), false)
	if n := rclDo(s, t, l, rclPlans(s), 0).Links; n != 2 {
		t.Fatalf("%d links in the first run", n)
	}
	rcpApply(s, t)
	s.rdfSettle(t, l)
	// the rack RB says another tag: the server that looked for the old one is read again (and is not a record of this capture), and finds none
	l = rclPrepare(s, t, rclTextsA([]string{"RA,RK1", "RB,RK3"}, servers, []string{"u1"}), rclAt(1), false)
	counts := rclDo(s, t, l, rclPlans(s), 0)
	if counts.Carried == 0 || counts.Links != 0 {
		t.Errorf("%+v", counts)
	}
	if got := rclProblems(s, t); strings.Join(got, " ") != "link_target_missing/ds_sv/SV2" {
		t.Errorf("the problems are %v", got)
	}
	refs := strings.Join(rclRefs(s, t), "\n")
	if !strings.Contains(refs, "ds_sv/SV2/2/1/-") || strings.Contains(refs, "ds_sv/SV1/") || !strings.Contains(refs, "ds_rk/RB/3/0/obj_") {
		t.Errorf("the references:\n%s", refs)
	}
}
