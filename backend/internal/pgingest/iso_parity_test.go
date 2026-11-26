package pgingest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The fifth step held to the old pipeline (ADR-0123): the same captures to the code that was there (checkCardinality of linkbuild.go)
// and to the new flow up to the fifth step, and what the new flow leaves to the next run.

// isoSteps are the captures of the first source: a server two records of which say two racks, one three of whose records say three, a
// server that finds no rack, and in the second step one more server of two racks.
func isoSteps() []pgiTexts {
	racks := []string{"RA,RK1", "RB,RK2", "RC,RK3"}
	sv := []string{"SV1,Zorvane,sn1,RK1,1,on", "SV1b,Zorvane,sn1,RK2,2,on", "SV2,Zorvane,sn2,RK2,3,on", "SV3,Zorvane,sn3,RK3,4,on",
		"SV3b,Zorvane,sn3,RK1,5,on", "SV3c,Zorvane,sn3,RK2,6,on", "SV4,Zorvane,sn4,RK9,7,on"}
	more := append(append([]string{}, sv...), "SV5,Zorvane,sn5,RK1,8,on", "SV5b,Zorvane,sn5,RK3,9,on")
	return []pgiTexts{rclTextsA(racks, sv, []string{"u1"}), rclTextsA(racks, more, []string{"u1"})}
}

// isoOldProblems is the problems of a report of the kinds given, as kind/dataset/key.
func isoOldProblems(r ingest.Report, kinds ...string) []string {
	var out []string
	for _, p := range r.Problems {
		for _, k := range kinds {
			if p.Kind == k {
				out = append(out, p.Kind+"/"+p.Dataset+"/"+p.Key)
			}
		}
	}
	sort.Strings(out)
	return out
}

// isoApplyLinks does to the links of the KG what the later steps will do with the links the run staged, so that the next run finds them:
// the staged ones are there, and the ones the run made before and does not stage any more are not.
func isoApplyLinks(s *rdfSetup, t *testing.T) {
	t.Helper()
	k := s.ref.KGNo
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%[1]d".link k WHERE NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_link l WHERE l.link_no = k.link_no)`, k))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from)
		SELECT link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, 1 FROM "s_%[1]d".stage_link ON CONFLICT (link_no) DO NOTHING`, k))
}

// [REQ-849] [REQ-853]
func TestIsoTheLinksTheCardinalityTakesOutAreTheOnesTheOldPipelineSetsAsideFromTheSameCaptures(t *testing.T) {
	o := rclOldNew(t)
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	plans := rclPlans(s)
	compared, set := 0, 0
	for n, texts := range isoSteps() {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		for name, text := range texts {
			o.fetch.text[name] = text
		}
		o.clock.Set(at)
		report, err := o.runner.Run(context.Background(), o.plans[0], o.kg.ID)
		if err != nil {
			t.Fatalf("step %d: the old run: %v", n, err)
		}
		l := rclPrepare(s, t, texts, at, true)
		rclDo(s, t, l, plans, 0)
		counts := isoDo(s, t, l, plans, 0)
		old := isoOldProblems(report, ingest.ProblemCardinality, ingest.ProblemLinkTarget)
		if got := isoProblems(s, t); strings.Join(got, " ") != strings.Join(old, " ") {
			t.Errorf("step %d: the old pipeline reports %v, the new flow %v", n, old, got)
		}
		set += len(isoOldProblems(report, ingest.ProblemCardinality))
		if counts.Cardinality != len(isoOldProblems(report, ingest.ProblemCardinality)) {
			t.Errorf("step %d: the counts %+v", n, counts)
		}
		staged := expList(s.e, t, fmt.Sprintf(`SELECT ref FROM "s_%d".stage_link ORDER BY ref`, s.ref.KGNo))
		var oldRefs []string
		for ref := range o.rclLinks(t) {
			oldRefs = append(oldRefs, ref)
		}
		sort.Strings(oldRefs)
		if strings.Join(staged, " ") != strings.Join(oldRefs, " ") {
			t.Errorf("step %d: the old pipeline has the links\n%v\nthe new flow stages\n%v", n, oldRefs, staged)
		}
		compared += len(staged)
		rcpApply(s, t)
		isoApplyLinks(s, t)
		s.rdfSettle(t, l)
	}
	if compared == 0 || set < 4 {
		t.Fatalf("the case compares %d links and %d set aside", compared, set)
	}
}

// [REQ-854]
func TestIsoARecordWhoseLinkWasTakenOutIsReadAgainByTheNextRunAsOneThatIsNotSolved(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	plans := rclPlans(s)
	texts := isoSteps()[0]
	l := rclPrepare(s, t, texts, rdfT0, true)
	rclDo(s, t, l, plans, 0)
	isoDo(s, t, l, plans, 0)
	var lost []string
	for _, p := range isoProblems(s, t) {
		if strings.HasPrefix(p, pgschema.ProblemCardinality+"/") {
			lost = append(lost, strings.TrimPrefix(p, pgschema.ProblemCardinality+"/ds_sv/"))
		}
	}
	if len(lost) != 3 {
		t.Fatalf("the links taken out: %v", lost)
	}
	rcpApply(s, t)
	isoApplyLinks(s, t)
	s.rdfSettle(t, l)
	// the next run of the same captures: no record changed, and the records whose link was taken out are records of the run, as the ones
	// that are not solved (ADR-0114, the start records (나))
	idnPipeline(s, t, texts, rdfT0.Add(time.Hour), 0)
	byKey := rdfByKey(s.rdfRecords(t), "ds_sv")
	for _, key := range lost {
		if r, ok := byKey[key]; !ok || r.Why != pgschema.CalcWhyUnresolved {
			t.Errorf("the record %s is %+v in the next run", key, r)
		}
	}
	if _, ok := byKey["SV2"]; ok {
		t.Error("a record whose link stayed is read again")
	}
}

// [REQ-849]
func TestIsoWhereTheOldPipelineKeptTheLinkOfTheSmallerIDTheNewFlowKeepsTheLinkTheKGHas(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	plans := rclPlans(s)
	racks := []string{"RA,RK1", "RB,RK2", "RC,RK3", "RD,RK4", "RE,RK5", "RF,RK6"}
	first := rclTextsA(racks, []string{"SV1,Zorvane,sn1,RK1,1,on"}, []string{"u1"})
	l := rclPrepare(s, t, first, rdfT0, true)
	rclDo(s, t, l, plans, 0)
	isoDo(s, t, l, plans, 0)
	rcpApply(s, t)
	isoApplyLinks(s, t)
	s.rdfSettle(t, l)
	kept := s.e.text(t, fmt.Sprintf(`SELECT ref FROM "s_%d".link`, s.ref.KGNo))
	// a second record of the server says a rack whose link to the server has a smaller id than the one the KG has: the old pipeline keeps
	// the smaller id (it sets the later one aside in the order of the ids), and the new flow the one the KG has
	server := expRefOf(s, t, "ds_sv", "SV1")
	mounted, ok := isoModelOf(t).reg.LinkTypeByName("MOUNTED_IN")
	if !ok {
		t.Fatal("no MOUNTED_IN")
	}
	src, err := kernel.ParseID[graph.ObjectKind](server)
	if err != nil {
		t.Fatal(err)
	}
	tag := ""
	for _, r := range racks[1:] {
		dst, err := kernel.ParseID[graph.ObjectKind](expRefOf(s, t, "ds_rk", strings.Split(r, ",")[0]))
		if err != nil {
			t.Fatal(err)
		}
		id, err := graph.NewLinkID(mounted.ID, src, dst, "")
		if err != nil {
			t.Fatal(err)
		}
		if id.String() < kept {
			tag = strings.Split(r, ",")[1]
			break
		}
	}
	if tag == "" {
		t.Fatal("no rack gives a link of a smaller id")
	}
	second := rclTextsA(racks, []string{"SV1,Zorvane,sn1,RK1,1,on", "SV1b,Zorvane,sn1," + tag + ",2,on"}, []string{"u1"})
	l = rclPrepare(s, t, second, rdfT0.Add(time.Hour), true)
	rclDo(s, t, l, plans, 0)
	isoDo(s, t, l, plans, 0)
	if got := strings.Join(expList(s.e, t, fmt.Sprintf(`SELECT ref FROM "s_%d".stage_link`, s.ref.KGNo)), " "); got != kept {
		t.Errorf("the new flow stages %s, not the link the KG has (%s)", got, kept)
	}
	if got := strings.Join(isoProblems(s, t), " "); got != "cardinality/ds_sv/SV1b" {
		t.Errorf("the problems: %s", got)
	}
}
