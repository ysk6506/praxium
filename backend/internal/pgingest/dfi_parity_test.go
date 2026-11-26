package pgingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The KG the new flow makes held to the one the old pipeline makes from the same captures (ADR-0127): a step after another, the captures
// are given to the code that was there (Runner, which applies by rows) and to the new flow up to its difference, which the staged apply
// writes. The two KGs have the same state hash, and the new one is what its head says (Verify).

// dfiParitySteps are the captures of the first source, 13 hours apart: the first run; a server whose position changes and one that moves
// to another rack, a virtual machine and a rack that are not seen any more (the rack's server says it still); the same twice, so that the
// two that are not seen are closed and their objects retire, the rack with the links at it.
func dfiParitySteps() []pgiTexts {
	first := rclTextsA(dfiRacks, dfiServers, []string{"u1", "u2"})
	moved := rclTextsA([]string{"RA,RK1", "RC,RK3"}, []string{"SV1,Zorvane,sn1,RK1,5,on", "SV2,Zorvane,sn2,RK2,2,on", "SV3,Zorvane,sn3,RK3,3,on"}, []string{"u1"})
	return []pgiTexts{first, moved, moved, moved, moved}
}

// dfiMappingRetire is the mapping of the first source whose feeds retire the objects they make once nothing shows them.
var dfiMappingRetire = strings.ReplaceAll(rclMappingA, `"creates_objects":true`, `"creates_objects":true,"retires_objects":true`)

// [REQ-970]
func TestDfiTheKGTheStagedApplyWritesIsTheOneTheOldPipelineMakesFromTheSameCaptures(t *testing.T) {
	o := rcpOldNew(t, rclSourceA, dfiMappingRetire)
	s := rdfNew(t, pgiPlan(t, rclSourceA, dfiMappingRetire))
	k := dfiApplierOf(s, t)
	retired := 0
	for n, texts := range dfiParitySteps() {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		for name, text := range texts {
			o.fetch.text[name] = text
		}
		o.clock.Set(at)
		if _, err := o.runner.Run(context.Background(), o.plan, o.kg.ID); err != nil {
			t.Fatalf("step %d: the old run: %v", n, err)
		}
		counts := dfiStep(s, t, k, texts, at, false)
		retired += counts.Of(ingest.DiffObject, ingest.DiffRetired)
		old, err := o.runner.Store.Head(context.Background(), o.kg.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := dfiHead(s, t, k); !got.Equal(old.Hash) {
			t.Errorf("step %d: the state hash of the new flow is %+v over %d rows and the old pipeline's %+v over %d", n, got.Sum, got.Rows, old.Hash.Sum, old.Hash.Rows)
			t.Log(dfiKGText(s, t))
		}
		t.Logf("step %d: %+v", n, counts)
	}
	if retired < 2 {
		t.Errorf("the steps retired %d objects", retired)
	}
}
