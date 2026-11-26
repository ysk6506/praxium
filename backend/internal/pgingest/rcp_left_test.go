package pgingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The objects the computation of the objects does not make (ADR-0119): the ones the run would have made are taken back with everything
// that named them, the ones the KG has are left as they are, and each of them is told in calc_left.

// rcpLeftDangling is what the run keeps that names an object that neither the KG nor the run has: it must be empty.
func rcpLeftDangling(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	k := s.ref.KGNo
	have := fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM "s_%[1]d".object o WHERE o.obj_no = %%[1]s) AND NOT EXISTS (SELECT 1 FROM "s_%[1]d".calc_newobj n WHERE n.obj_no = %%[1]s)`, k)
	var parts []string
	for _, c := range []struct{ table, col string }{{"calc_ref", "target_no"}, {"calc_ask", "parent_no"}, {"calc_newkey", "obj_no"}, {"calc_newkey", "ctx_no"}, {"calc_bind", "obj_no"}, {"calc_object", "obj_no"}} {
		cond := fmt.Sprintf(have, "x."+c.col)
		if c.col == "ctx_no" {
			cond += " AND x.ctx_no <> 0"
		}
		parts = append(parts, fmt.Sprintf(`SELECT '%[2]s.%[3]s=' || x.%[3]s FROM "s_%[1]d".%[2]s x WHERE x.%[3]s IS NOT NULL AND %[4]s`, k, c.table, c.col, cond))
	}
	return expList(s.e, t, strings.Join(parts, " UNION ALL ")+" ORDER BY 1")
}

// rcpLeftRows is the rows of calc_left as why/lines/is_new, in the order of the objects.
func rcpLeftRows(s *rdfSetup, t *testing.T) []string {
	t.Helper()
	return expList(s.e, t, fmt.Sprintf(`SELECT why || '/' || lines || '/' || is_new FROM "s_%d".calc_left ORDER BY obj_no`, s.ref.KGNo))
}

// rcpLeftServers is the world of the objects that have too many lines: four records say they are one server, and one says it is another.
func rcpLeftServers(t *testing.T) (*rdfSetup, ingest.RunLease) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	texts := idnServers("A1,sn1,Zorvane,u1", "A2,sn1,Zorvane,u1", "A3,sn1,Zorvane,u1", "A4,sn1,Zorvane,u1", "B,sn2,Zorvane,u2")
	l, _ := idnPipeline(s, t, texts, rdfT0, 0)
	rcpExpand(s, t, l, false)
	return s, l
}

// [REQ-971]
func TestRcpAnObjectTheRunWouldMakeThatHasTooManyLinesIsTakenBackWithEverythingThatNamedItAndTheRecordsUnderItAreProblems(t *testing.T) {
	s, l := rcpLeftServers(t)
	k := s.ref.KGNo
	counts := rcpDo(s, t, l, 0, 3)
	if counts.TooMany != 1 || counts.Left != 1 || counts.Isolated != 1 || counts.Objects != 1 {
		t.Errorf("%+v", counts)
	}
	if got := rcpLeftRows(s, t); len(got) != 1 || got[0] != fmt.Sprintf("%d/4/true", pgschema.LeftTooMany) {
		t.Errorf("calc_left: %v", got)
	}
	if got := rcpLeftDangling(s, t); len(got) != 0 {
		t.Errorf("the run keeps what names an object that is not there: %v", got)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_newobj`, k)); n != 1 {
		t.Errorf("%d new objects are kept, want the one that is made", n)
	}
	// the four records of the object are unresolved, each with a problem that names the identity; the one of the other object is resolved
	unresolved := expList(s.e, t, fmt.Sprintf(`SELECT c.key || '/' || f.state || '/' || COALESCE(f.target_no::text, 'null') FROM "s_%[1]d".calc_ref f JOIN "s_%[1]d".calc_record c ON c.rid = f.rid WHERE f.role = %[2]d ORDER BY 1`, k, pgschema.RefRoleIdentity))
	want := []string{"A1/%d/null", "A2/%d/null", "A3/%d/null", "A4/%d/null"}
	for i, w := range want {
		want[i] = fmt.Sprintf(w, pgschema.RefUnresolved)
	}
	if len(unresolved) != 5 || strings.Join(unresolved[:4], " ") != strings.Join(want, " ") || !strings.HasPrefix(unresolved[4], "B/") || strings.HasSuffix(unresolved[4], "/null") {
		t.Errorf("the references of the records: %v", unresolved)
	}
	problems := expList(s.e, t, fmt.Sprintf(`SELECT c.key || '/' || p.kind FROM "s_%[1]d".calc_problem p JOIN "s_%[1]d".calc_record c ON c.rid = p.rid WHERE p.kind = '%[2]s' ORDER BY 1`, k, pgschema.ProblemIdentityConflict))
	if strings.Join(problems, " ") != "A1/identity_conflict A2/identity_conflict A3/identity_conflict A4/identity_conflict" {
		t.Errorf("the problems: %v", problems)
	}
	if n := rcpCountOf(s, t, pgschema.TableStageObject); n != 1 {
		t.Errorf("%d objects are staged", n)
	}
	// the report is what the statements wrote
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_left`, k)); n != counts.Left {
		t.Errorf("%d rows in calc_left, the report says %d", n, counts.Left)
	}
}

// A page of one line, and a page of the default size, leave the same objects, with the same reasons.
// [REQ-971]
func TestRcpTheObjectsThatAreTakenBackAreTheSameWhateverThePageSizeIs(t *testing.T) {
	s1, l1 := rcpLeftServers(t)
	c1 := rcpDo(s1, t, l1, 0, 3)
	s2, l2 := rcpLeftServers(t)
	c2 := rcpDo(s2, t, l2, 3, 3)
	if c1 != c2 {
		t.Errorf("%+v and %+v", c1, c2)
	}
	if a, b := strings.Join(rcpLeftRows(s1, t), ","), strings.Join(rcpLeftRows(s2, t), ","); a != b || a == "" {
		t.Errorf("%s and %s", a, b)
	}
	if a, b := rcpText(rcpStaged(s1, t)), rcpText(rcpStaged(s2, t)); strings.Count(a, "\n") != strings.Count(b, "\n") {
		t.Errorf("%s and %s", a, b)
	}
	if len(rcpLeftDangling(s2, t)) != 0 {
		t.Errorf("%v", rcpLeftDangling(s2, t))
	}
}

// An object of the run that the registry does not have and no new object names crashed the step (its ref is NULL): it is left, told, and
// the run goes on.
// [REQ-972]
func TestRcpAnObjectOfTheRunThatNoRegistryAndNoNewObjectHasIsLeftAndDoesNotStopTheStep(t *testing.T) {
	s, l := rcpShared(t, 3)
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_object (obj_no, why) VALUES (777777, %d)`, k, pgschema.ObjectWhyOldRef))
	counts, err := ingest.RunRecompute(s.e.ctx, s.e.st, l, rcpStart(s, 0, 0), rcpMaker(t, s, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if counts.Isolated != 1 || counts.Left != 1 || counts.Objects != 4 {
		t.Errorf("%+v", counts)
	}
	if got := rcpLeftRows(s, t); len(got) != 1 || got[0] != fmt.Sprintf("%d/0/false", pgschema.LeftUnreadable) {
		t.Errorf("calc_left: %v", got)
	}
	if got := rcpLeftDangling(s, t); len(got) != 0 {
		t.Errorf("%v", got)
	}
}

// An object the KG has is left as it is, whatever the reason: it is never a candidate for retirement because of it.
// [REQ-973]
func TestRcpAnObjectTheKGHasThatIsLeftIsToldAndIsNeverACandidateForRetirement(t *testing.T) {
	const m = 6
	s, l := rcpShared(t, m)
	k := s.ref.KGNo
	// an object with no record under it, and one whose records are of an older declaration of their dataset
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".object (obj_no, ref, type_no, name, min_grade, seq_from) VALUES (900001, 'orphan-1', 1, 'orphan', 0, 1)`, k))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_object (obj_no, why) VALUES (900001, %d)`, k, pgschema.ObjectWhyOldRef))
	s.e.exec(t, fmt.Sprintf(`UPDATE %s SET decl_rev = decl_rev + 1000 WHERE key LIKE 'r2-%%'`, pgiTable(s.ref, pgschema.TableRawRecord)))
	counts := rcpDo(s, t, l, 0, 3)
	if counts.TooMany != 1 || counts.Empty != 1 || counts.Unreadable != 1 || counts.Left != 2 || counts.Isolated != 0 {
		t.Errorf("%+v", counts)
	}
	rows := expList(s.e, t, fmt.Sprintf(`SELECT why || '/' || is_new FROM "s_%d".calc_left ORDER BY why`, k))
	if want := fmt.Sprintf("%d/false %d/false %d/false", pgschema.LeftEmpty, pgschema.LeftUnreadable, pgschema.LeftTooMany); strings.Join(rows, " ") != want {
		t.Errorf("calc_left: %v, want %s", rows, want)
	}
	// none of them is a candidate for retirement (why 4), and none has a row in stage_object
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".calc_object o JOIN "s_%[1]d".calc_left l ON l.obj_no = o.obj_no WHERE o.why = %[2]d`, k, pgschema.ObjectWhyRetire)); n != 0 {
		t.Errorf("%d objects that were left are candidates for retirement", n)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".stage_object s JOIN "s_%[1]d".calc_left l ON l.obj_no = s.obj_no`, k)); n != 0 {
		t.Errorf("%d objects that were left are staged", n)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".calc_left l JOIN "s_%[1]d".object o ON o.obj_no = l.obj_no`, k)); n != 3 {
		t.Errorf("%d of the three are objects of the KG", n)
	}
	if got := rcpLeftDangling(s, t); len(got) != 0 {
		t.Errorf("%v", got)
	}
	// the limit is raised: the object that has too many lines is computed in full, and is not left
	s2, l2 := rcpShared(t, m)
	again := rcpDo(s2, t, l2, 0, 0)
	if again.TooMany != 0 || again.Left != 0 || again.Objects != m+1 || rcpCountOf(s2, t, pgschema.TableCalcLeft) != 0 {
		t.Errorf("%+v", again)
	}
}

// A parent that is taken back takes back the objects whose keys it is the context of, at every depth, whatever the page.
// [REQ-971]
func TestRcpAnObjectWhoseContextParentIsTakenBackIsTakenBackToo(t *testing.T) {
	var states [2]string
	for i, lines := range []int{1, 0} {
		s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
		l, _ := idnPipeline(s, t, idnPlaces("1,HQ,B1,F1,R1,rack a,RK1", "2,HQ,B1,F1,R1,rack b,RK2", "3,HQ,B1,F2,R2,rack c,RK3"), rdfT0, 0)
		rcpExpand(s, t, l, false)
		counts := rcpDo(s, t, l, lines, 1)
		k := s.ref.KGNo
		if got := rcpLeftDangling(s, t); len(got) != 0 {
			t.Errorf("lines %d: %v", lines, got)
		}
		if counts.Objects != 0 || counts.Isolated != 9 || counts.TooMany == 0 || counts.Left != counts.TooMany {
			t.Errorf("lines %d: %+v", lines, counts)
		}
		if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_left WHERE why = %d`, k, pgschema.LeftParent)); n != counts.Isolated-counts.TooMany {
			t.Errorf("lines %d: %d objects are left for their parent, %+v", lines, n, counts)
		}
		if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_ref WHERE role = %d AND state <> %d`, k, pgschema.RefRoleIdentity, pgschema.RefUnresolved)); n != 0 {
			t.Errorf("lines %d: %d references are not unresolved", lines, n)
		}
		// the objects are the same whatever the page; the reason of one that is too big and is under a parent that was taken back first is
		// the one of the first the run meets (ADR-0119), so the reasons are not compared
		states[i] = fmt.Sprintf("%d %d %d", counts.Objects, counts.Isolated, rcpCountOf(s, t, pgschema.TableCalcLeft))
	}
	if states[0] != states[1] || states[0] != "0 9 9" {
		t.Errorf("a page of one line: %s, the default page: %s", states[0], states[1])
	}
}

// A server that takes the run over gives the time of the run and the schemes it has; the ones the run began with are the ones it uses, and
// others are a conflict that is told before it reads a page.
// [REQ-972]
func TestRcpAServerThatTakesTheRunOverUsesWhatTheRunBeganWithAndRefusesWhatIsDifferent(t *testing.T) {
	s, l := rcpLeftServers(t)
	st, ctx := s.e.st, s.e.ctx
	if err := st.BeginRecompute(ctx, l, rcpStart(s, 3, 3)); err != nil {
		t.Fatal(err)
	}
	place, err := st.RecomputePlace(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if place.Seen != rcpSeen || place.Schemes != (ingest.RecomputeStart{Schemes: s.plan.SchemeNumbers()}).SchemesText() || place.Schemes == "" {
		t.Errorf("%+v", place)
	}
	other := rcpStart(s, 3, 3)
	other.Seen = rdfT0.UTC().Format("2006-01-02T15:04:05Z")
	_, err = ingest.RunRecompute(ctx, st, l, other, rcpMaker(t, s, 3, 3))
	if kernel.CodeOf(err) != kernel.CodeConflict {
		t.Errorf("another time: %v", err)
	}
	if page, _ := st.RecomputePlace(ctx, l); page.Unit != place.Unit || page.Done() {
		t.Errorf("a page was read: %+v", page)
	}
	if _, err := ingest.RunRecompute(ctx, st, l, rcpStart(s, 3, 3), rcpMaker(t, s, 3, 3)); err != nil {
		t.Errorf("the same time and schemes: %v", err)
	}
}
