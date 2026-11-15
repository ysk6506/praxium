package ingest

import (
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// cardinalityRig is a derivation with objects of the given types (by name) and links of one link type between them,
// so that checkCardinality can be run on exactly those links.
func cardinalityRig(t *testing.T, linkType string, objects map[string]string, links [][2]string) *derivation {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	lt, ok := reg.LinkTypeByName(linkType)
	if !ok {
		t.Fatalf("no link type %s", linkType)
	}
	d := newDerivation(&Plan{reg: reg}, nil, &Report{}, time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := map[string]graph.ObjectID{}
	n := 0
	for name, typeName := range objects {
		typ, ok := reg.ObjectTypeByName(typeName)
		if !ok {
			t.Fatalf("no type %s", typeName)
		}
		n++
		id, err := kernel.ParseID[graph.ObjectKind]("obj_" + strings.Repeat("0", 31) + string(rune('a'+n)))
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
		d.objects[id.String()] = &objAcc{id: id, typ: typ, nameFall: name}
	}
	for i, l := range links {
		d.links[string(rune('a'+i))] = desiredLink{up: graph.LinkUpsert{Type: lt.ID, Src: ids[l[0]], Dst: ids[l[1]]}, dataset: "ds", row: Row{Key: l[0], Line: i + 2}}
	}
	return d
}

// [REQ-421]
func TestCircuitWithTwoOwningOrganizationsLosesTheSecondLink(t *testing.T) {
	// Circuit to Organization over OWNED_BY is N:1 because the identity key of a circuit has its owner in it, so a
	// second owning organization is set aside and goes to the list of problems (ADR-0065).
	d := cardinalityRig(t, "OWNED_BY", map[string]string{"c1": "Circuit", "o1": "Organization", "o2": "Organization"}, [][2]string{{"c1", "o1"}, {"c1", "o2"}})
	d.checkCardinality()
	if len(d.links) != 1 || d.rep.ProblemCounts[ProblemCardinality] != 1 {
		t.Fatalf("links %d, problems %v", len(d.links), d.rep.ProblemCounts)
	}
	if _, kept := d.links["a"]; !kept {
		t.Errorf("the first link (by link id) is kept, the later one is set aside: %v", d.links)
	}
	if p := d.rep.Problems[0]; p.Line != 3 || !strings.Contains(p.Reason, "OWNED_BY") || !strings.Contains(p.Reason, "N:1") {
		t.Errorf("the problem names the row and the cardinality: %+v", p)
	}
}

// [REQ-421]
func TestWidenedBindingsStillTakeMoreThanOneEnd(t *testing.T) {
	// The two bindings that the priority makes the winner and that stay N:M keep taking what the older, wider
	// binding took: a machine with two managers, a cluster node in two failure domains.
	vm := cardinalityRig(t, "MANAGED_BY", map[string]string{"v": "VirtualMachine", "m1": "VirtualizationManager", "m2": "VirtualizationManager"}, [][2]string{{"v", "m1"}, {"v", "m2"}})
	vm.checkCardinality()
	node := cardinalityRig(t, "IN_FAILURE_DOMAIN", map[string]string{"n": "ClusterNode", "f1": "FailureDomain", "f2": "FailureDomain"}, [][2]string{{"n", "f1"}, {"n", "f2"}})
	node.checkCardinality()
	if len(vm.links) != 2 || len(node.links) != 2 || len(vm.rep.ProblemCounts)+len(node.rep.ProblemCounts) != 0 {
		t.Errorf("links %d and %d, problems %v %v", len(vm.links), len(node.links), vm.rep.ProblemCounts, node.rep.ProblemCounts)
	}
}
