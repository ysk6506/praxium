package pgingest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The difference held to what the KG becomes (ADR-0127): runs of a source, each up to its difference, applied with the staged apply
// (graph.StagedApplier) and cleaned, and what the KG has after each.

// dfiRacks and dfiServers are the captures of the first source the cases start from: two racks, three servers in them, a virtual machine.
var (
	dfiRacks   = []string{"RA,RK1", "RB,RK2", "RC,RK3"}
	dfiServers = []string{"SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on", "SV3,Zorvane,sn3,RK2,3,on"}
)

// [REQ-960] [REQ-966] [REQ-967]
func TestDfiAFirstRunAddsWhatItStagedAndTheApplyMakesTheKGTheHeadSays(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	k := dfiApplierOf(s, t)
	// dfiStep holds the stored difference to its counts and to the digest in Go, and the chain the apply ends with to the fingerprint
	counts := dfiStep(s, t, k, rclTextsA(dfiRacks, dfiServers, []string{"u1"}), rdfT0, false)
	dfiHead(s, t, k)
	// three racks, three servers, a virtual machine, a key of each, the three mounts and the identifier of the virtual machine
	for _, c := range []struct {
		kind string
		want int
	}{{ingest.DiffObject, 7}, {ingest.DiffKey, 7}, {ingest.DiffLink, 3}, {ingest.DiffIdentifier, 1}} {
		if got := counts.Of(c.kind, ingest.DiffAdded); got != c.want {
			t.Errorf("%d %s rows are added, want %d: %+v", got, c.kind, c.want, counts)
		}
	}
	// the record of the run keeps the counts by kind, type and change, and the fingerprint of the change
	kept := s.e.text(t, `SELECT counts->'diff'->'tally' FROM sync_run WHERE kg_id = $1 ORDER BY run_no DESC LIMIT 1`, s.kg.ID.String())
	if !strings.Contains(kept, fmt.Sprintf(`"%d/%d/%d": 3`, pgschema.DiffKindLink, isoModelOf(t).mounted, pgschema.ChangeKindAdded)) {
		t.Errorf("the record of the run keeps the counts %s", kept)
	}
	if print := s.e.text(t, `SELECT encode(change_fingerprint, 'hex') FROM sync_run WHERE kg_id = $1 ORDER BY run_no DESC LIMIT 1`, s.kg.ID.String()); print != counts.Fingerprint.String() {
		t.Errorf("the record of the run keeps the fingerprint %s, not %s", print, counts.Fingerprint)
	}
	text := dfiKGText(s, t)
	for _, name := range []string{"RA", "RB", "RC", "SV1", "SV2", "SV3", "u1", "is.vm_instance_uuid=u1"} {
		if !strings.Contains(text, name) {
			t.Errorf("the KG has no %s:\n%s", name, text)
		}
	}
	// the same captures again change nothing: no row, and the apply is empty
	again := dfiStep(s, t, k, rclTextsA(dfiRacks, dfiServers, []string{"u1"}), rdfT0.Add(time.Hour), false)
	if again.Rows != 0 || again.Chunks != 0 || dfiKGText(s, t) != text {
		t.Errorf("the same captures again: %+v", again)
	}
}
