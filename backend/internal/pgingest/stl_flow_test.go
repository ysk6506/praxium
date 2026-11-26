package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

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
