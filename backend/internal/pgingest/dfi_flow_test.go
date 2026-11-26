package pgingest

import (
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The difference held to what the KG becomes (ADR-0127): runs of a source, each up to its difference, applied with the staged apply
// (graph.StagedApplier) and cleaned, and what the KG has after each.

// dfiRacks and dfiServers are the captures of the first source the cases start from: two racks, three servers in them, a virtual machine.
var (
	dfiRacks   = []string{"RA,RK1", "RB,RK2", "RC,RK3"}
	dfiServers = []string{"SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on", "SV3,Zorvane,sn3,RK2,3,on"}
)

// [REQ-960]
func TestDfiAFirstRunAddsWhatItStagedAndTheApplyMakesTheKGTheHeadSays(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	k := dfiApplierOf(s, t)
	counts := dfiStep(s, t, k, rclTextsA(dfiRacks, dfiServers, []string{"u1"}), rdfT0, false)
	t.Logf("%+v", counts)
	for _, r := range dfiRows(s, t) {
		t.Log(r)
	}
	dfiHead(s, t, k)
	t.Log(dfiKGText(s, t))
	if counts.Of(ingest.DiffObject, ingest.DiffAdded) != 7 || counts.Of(ingest.DiffLink, ingest.DiffAdded) != 3 {
		t.Errorf("the counts %+v", counts)
	}
	// the same captures again change nothing
	again := dfiStep(s, t, k, rclTextsA(dfiRacks, dfiServers, []string{"u1"}), rdfT0.Add(time.Hour), false)
	if again.Rows != 0 || again.Chunks != 0 {
		t.Errorf("the same captures again: %+v", again)
	}
	if !strings.Contains(dfiKGText(s, t), "SV1") {
		t.Error("no SV1")
	}
}
