package memstore

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
)

// [REQ-251]
func TestIdentifiersOfListsTheLiveIdentifiersOfOneObject(t *testing.T) {
	r := newRig(t, graph.Options{})
	r.apply(r.fx.World(1, 1, 2))
	scheme, _ := r.fx.Reg.IdentifierSchemeByName("redfish_system_id")
	one, other := r.fx.ServerID("SN-000000"), r.fx.ServerID("SN-000001")
	r.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{
		{Object: one, Scheme: scheme.ID, Value: "a", Scope: "x"}, {Object: other, Scheme: scheme.ID, Value: "b", Scope: "x"}}})
	list := func() []graph.IdentifierRecord {
		snap, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest})
		if err != nil {
			t.Fatal(err)
		}
		lister, ok := snap.(graph.IdentifierLister)
		if !ok {
			t.Fatal("the in-memory snapshot must list identifiers by object")
		}
		recs, err := lister.IdentifiersOf(r.ctx, one)
		if err != nil {
			t.Fatal(err)
		}
		return recs
	}
	if recs := list(); len(recs) != 1 || recs[0].Value != "a" {
		t.Fatalf("records %+v", recs)
	}
	r.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "a", Scope: "x", Close: true}}})
	if recs := list(); len(recs) != 0 {
		t.Fatalf("a closed identifier is still listed: %+v", recs)
	}
	snap, _ := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest})
	if _, err := r.a.Restore(r.ctx, r.kg.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := snap.(graph.IdentifierLister).IdentifiersOf(r.ctx, one); err == nil {
		t.Fatal("a snapshot of an older generation must refuse to read")
	}
}
