package pgingest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// stlComputeOf does a run of the texts of a source of a KG of several sources up to the end of its computing, with the plans of all.
func stlComputeOf(s *rdfSetup, t *testing.T, texts pgiTexts, at time.Time, plans map[int64]*ingest.Plan) ingest.RunLease {
	t.Helper()
	l, _ := idnPipeline(s, t, texts, at, 0)
	rcpExpand(s, t, l, false)
	rclObjects(s, t, l, plans, at.UTC().Format(time.RFC3339), 0)
	rclDo(s, t, l, plans, 0)
	isoDo(s, t, l, plans, 0)
	return l
}

// stlCleanOf moves a run that computed to settling, applies its graph and cleans it.
func stlCleanOf(s *rdfSetup, t *testing.T, l ingest.RunLease) ingest.SettleCounts {
	t.Helper()
	stlSettling(s, t, l)
	stlGraph(s, t)
	return stlClean(s, t, l, 2)
}

// [REQ-985]
func TestStlARecordThatOnlyMakesALinkIsUsedAndTheNextRunOfItsSourceDoesNotReadItAgain(t *testing.T) {
	k := rclTwo(t, rclMappingB)
	stlCleanOf(k.a, t, stlComputeOf(k.a, t, rclTextsA([]string{"RA,RK1"}, []string{"SV7,Zorvane,sn7,RK1,3,on"}, []string{"u1"}), stlAt(0), k.plans))
	// the mounts of the second source add nothing to the server they find and make the link to the rack: they only make links
	mounts := rclTextsB([]string{"RA,RK1"}, []string{"M7,Zorvane,sn7,RK1,5"})
	stlCleanOf(k.b, t, stlComputeOf(k.b, t, mounts, stlAt(1), k.plans))
	if got := stlFate(k.b, t, "ds_mt", "M7"); got != int(pgschema.FateUsed) {
		t.Fatalf("the fate of the record that made the link is %d", got)
	}
	from := k.b.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".record_ref WHERE key = 'M7' AND role = %d AND state = %d AND target_no IS NOT NULL`,
		k.b.ref.KGNo, pgschema.RefRoleLinkFrom, pgschema.RefResolved))
	if from != 1 {
		t.Fatalf("the record that made the link has %d solved references of it", from)
	}
	// the same captures again: the receiving finds the record the same, and the run does not read it
	l := stlComputeOf(k.b, t, mounts, stlAt(2), k.plans)
	if got := k.b.e.text(t, `SELECT string_agg(key || '=' || change_kind, ' ' ORDER BY key) FROM `+pgiTable(k.b.ref, pgschema.TableStageRecord)+` WHERE dataset_no = $1`, k.b.rdfNo("ds_mt")); got != fmt.Sprintf("M7=%d", pgschema.ReceiveSame) {
		t.Errorf("the receiving found the record %s", got)
	}
	if rs := k.b.rdfRecords(t); len(rdfByKey(rs, "ds_mt")) != 0 || len(rdfByKey(rs, "ds_rkb")) != 0 {
		t.Errorf("the run reads the records of the source again: %+v", rs)
	}
	stlCleanOf(k.b, t, l)
}

// [REQ-986]
func TestStlTheStagedRowsOfAnotherSourceAreNotEmptiedAndItsRunWaitsAtTheGateWhileOneIsCleaned(t *testing.T) {
	k := rclTwo(t, rclMappingB)
	la := stlComputeOf(k.a, t, rclTextsA([]string{"RA,RK1"}, []string{"SV7,Zorvane,sn7,RK1,3,on"}, []string{"u1"}), stlAt(0), k.plans)
	stlSettling(k.a, t, la)
	stlGraph(k.a, t)
	// the second source receives while the first one's run is settling, and cannot compute: the receiving ends at the gate
	lb := k.b.e.open(t, k.b.kg, k.b.plan.Source.ID, "server-b")
	if _, err := k.b.e.st.Receive(k.b.e.ctx, lb, ingest.Intake{Reader: rclTextsB([]string{"RA,RK1"}, []string{"M7,Zorvane,sn7,RK1,5"}), Plan: k.b.plan}); !pgiCodeIs(err, ingest.CodeKGGateTaken) {
		t.Fatalf("the run of the other source moved to computing while the KG is cleaned: %v", err)
	}
	stlClean(k.a, t, la, 2)
	if n := k.b.e.count(t, `SELECT count(*) FROM `+pgiTable(k.b.ref, pgschema.TableStageRecord)); n == 0 {
		t.Error("the cleaning emptied what the other source received")
	}
	if err := k.b.e.st.AdvanceRun(k.b.e.ctx, lb, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Errorf("the run of the other source cannot compute after the cleaning: %v %v", err, expList(k.b.e, t, `SELECT run_no || '=' || stage FROM sync_run ORDER BY run_no`))
	}
}

// [REQ-986]
func TestStlADatasetLeftOutWholeKeepsItsRawRecordsAndWaitsAndARunThatChangedNothingIsStillCleaned(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	steps := stlSteps()
	stlWhole(s, t, steps[0], stlAt(0), 2)
	before := stlDump(s, t, true)
	// the machines are not given: their dataset is left out whole
	part := pgiTexts{"ds_rk": steps[0]["ds_rk"], "ds_sv": steps[0]["ds_sv"]}
	stlWhole(s, t, part, stlAt(1), 2)
	after := stlDump(s, t, true)
	of := func(lines []string, prefix string) string {
		var out []string
		for _, l := range lines {
			if strings.HasPrefix(l, prefix) {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n")
	}
	if a, b := of(before["raw"], "ds_vm/"), of(after["raw"], "ds_vm/"); a != b || a == "" {
		t.Errorf("the raw records of the dataset left out:\n%s\n---\n%s", a, b)
	}
	// the run changed nothing of the servers, and the cleaning still counts the wait of the one a rule filters out and marks the time
	waits := of(after["wait"], "ds_sv/SV3/")
	if !strings.Contains(waits, fmt.Sprintf("/%d/2/", pgschema.WaitFiltered)) {
		t.Errorf("the wait of the filtered server after the second run: %s", waits)
	}
	var set []byte
	var at time.Time
	if err := s.e.admin.QueryRowContext(s.e.ctx, `SELECT s.applied_definition_set, d.captured_at FROM source_state s JOIN source_dataset d ON d.src_no = s.src_no
		WHERE s.src_no = $1 AND d.dataset_id = 'ds_sv'`, s.ref.SrcNo).Scan(&set, &at); err != nil {
		t.Fatal(err)
	}
	if len(set) == 0 || !at.Equal(stlAt(1)) {
		t.Errorf("the source keeps the definition set %x and the time %v", set, at)
	}
	if n := s.e.count(t, `SELECT count(*) FROM source_dataset WHERE src_no = $1 AND dataset_id = 'ds_vm' AND captured_at = $2`, s.ref.SrcNo, stlAt(1)); n != 0 {
		t.Error("the dataset left out is marked read at the time of the run")
	}
}

// [REQ-984]
func TestStlTheProblemsOfALinkDefinitionKeepANumberBelowZeroAndTheOnesOfAFeedOneOfZeroAndUp(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	// two records make one link with other attributes (a conflict of the link), one has a cell of the link that gives no value, one finds no
	// rack, one cannot be told (no serial: the receiving's problem of its feed), and two servers want one rack place of a type held to one
	texts := rclTextsA([]string{"RA,RK1", "RB,RK2"}, []string{"S1,Zorvane,sn1,RK1,1,on", "S1b,Zorvane,sn1,RK1,5,on", "S2,Zorvane,sn2,RK2,front,on",
		"S4,Zorvane,sn4,RK9,4,on", "S6,Zorvane,,RK1,1,on"}, []string{"u1"})
	l := stlCompute(s, t, texts, stlAt(0), false)
	linkKinds := "('" + strings.Join([]string{pgschema.ProblemLinkTarget, pgschema.ProblemLinkAmbiguous, pgschema.ProblemCardinality}, "', '") + "')"
	feedKinds := "('" + strings.Join([]string{pgschema.ProblemMissingIdentity, pgschema.ProblemOrphanValue, pgschema.ProblemMissingName, pgschema.ProblemContextParent}, "', '") + "')"
	wrong := func(table string) string {
		return fmt.Sprintf(`SELECT count(*) FROM "s_%d".%s WHERE (def_no >= 0 AND (kind IN %s OR reason LIKE 'the link %%')) OR (def_no < 0 AND kind IN %s)`, s.ref.KGNo, table, linkKinds, feedKinds)
	}
	if n := s.e.count(t, wrong("calc_problem")); n != 0 {
		t.Errorf("%d problems the run computed keep a number of the other kind of definition", n)
	}
	stlCleanOf(s, t, l)
	if n := s.e.count(t, wrong("record_problem")); n != 0 {
		t.Errorf("%d problems of the records keep a number of the other kind of definition", n)
	}
	got := strings.Join(stlDump(s, t, false)["problem"], " ")
	for _, w := range []string{"ds_sv/S1/-1/source_conflict/", "ds_sv/S2/-1/invalid_value/", "ds_sv/S4/-1/link_target_missing/", "ds_sv/S6/1/missing_identity/"} {
		if !strings.Contains(got, w) {
			t.Errorf("no problem %s in %s", w, got)
		}
	}
}

// [REQ-984] [REQ-986]
func TestStlAfterTheCleaningTheRecordsNotSolvedAreTheStartOfTheNextRunAndNoReferencePointsAtWhatTheKGHasNot(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	steps := isoSteps()
	notSolved := fmt.Sprintf(`SELECT DISTINCT d.dataset_id || '/' || x.key FROM "s_%d".record_ref x JOIN source_dataset d ON d.dataset_no = x.dataset_no WHERE x.state <> 0 ORDER BY 1`, s.ref.KGNo)
	dangling := fmt.Sprintf(`SELECT count(*) FROM "s_%[1]d".record_ref x WHERE x.target_no IS NOT NULL AND CASE WHEN x.role = %[2]d
		THEN NOT EXISTS (SELECT 1 FROM "s_%[1]d".link l WHERE l.link_no = x.target_no) ELSE NOT EXISTS (SELECT 1 FROM "s_%[1]d".object o WHERE o.obj_no = x.target_no) END`, s.ref.KGNo, pgschema.RefRoleLinkFrom)
	seen := 0
	for n, texts := range append(steps, steps[1]) {
		l := stlCompute(s, t, texts, stlAt(n), false)
		if n > 0 {
			// the records the cleaning before left not solved are records of this run, as not solved unless they changed
			var started []string
			for _, r := range s.rdfRecords(t) {
				started = append(started, r.Dataset+"/"+r.Key)
			}
			have := strings.Join(started, " ")
			for _, key := range expList(s.e, t, notSolved) {
				if !strings.Contains(" "+have+" ", " "+key+" ") {
					t.Errorf("run %d: %s is not solved and not a record of the run (%s)", n, key, have)
				}
				seen++
			}
		}
		stlSettling(s, t, l)
		stlGraph(s, t)
		stlClean(s, t, l, 3)
		if d := s.e.count(t, dangling); d != 0 {
			t.Errorf("run %d: %d references point at what the KG has not: %v", n, d, stlDump(s, t, false)["ref"])
		}
	}
	if seen < 3 {
		t.Fatalf("the case has %d records that are not solved", seen)
	}
}

// The cleaning of whole runs (ADR-0128): what it leaves in the permanent tables, held to the cleaning the tests of the steps before did
// (rdfSettle), and what the next run reads of it.

// stlSteps are the captures of four runs of the first source: a rack, a server that two rows say, a server a rule filters out and two
// machines; then the server's row changes, the key stops repeating and a machine is gone for three runs (13 hours apart), which closes it.
func stlSteps() []pgiTexts {
	racks := []string{"RA,RK1", "RB,RK2"}
	first := rclTextsA(racks, []string{"SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,3,on", "SV2,Zorvane,sn2,RK2,4,on", "SV3,Zorvane,sn3,RK1,5,off"}, []string{"u1", "u2"})
	later := rclTextsA(racks, []string{"SV1,Zorvane,sn1,RK1,2,on", "SV2,Zorvane,sn2,RK2,4,on", "SV3,Zorvane,sn3,RK1,5,off"}, []string{"u1"})
	return []pgiTexts{first, later, later, later}
}

// [REQ-985]
func TestStlTheCleaningLeavesTheRawRecordsTheWaitsAndTheRepeatedKeysTheTestCleaningOfTheComparisonLeft(t *testing.T) {
	a := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	b := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	closed, dups, waits := 0, 0, 0
	for n, texts := range stlSteps() {
		l := stlCompute(a, t, texts, stlAt(n), false)
		rcpApply(a, t)
		isoApplyLinks(a, t)
		a.rdfSettle(t, l)
		counts := stlWhole(b, t, texts, stlAt(n), 2)
		closed += counts.Closed
		got := stlDump(b, t, false)
		stlSame(t, fmt.Sprintf("run %d", n), stlDump(a, t, false), got, "raw", "wait", "dup")
		dups, waits = dups+len(got["dup"]), waits+len(got["wait"])
		if left := stlEmpty(b, t); left != 0 {
			t.Errorf("run %d: %d staged rows are left", n, left)
		}
	}
	if left := b.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".raw_record WHERE key = 'u2'`, b.ref.KGNo)); closed != 1 || left != 0 {
		t.Errorf("the machine that was gone for three runs: %d closed, %d raw records left", closed, left)
	}
	if dups != 1 || waits < 3 {
		t.Errorf("the case compares %d lines of a repeated key and %d waits", dups, waits)
	}
	if got := stlFate(b, t, "ds_sv", "SV3"); got != int(pgschema.FateFiltered) {
		t.Errorf("the fate of the server a rule filters out is %d", got)
	}
}
