package graph

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func shuffled(b Batch, seed uint64) Batch {
	rng := kernel.NewSeededRng(seed)
	swap := func(n int, f func(i, j int)) {
		for i := n - 1; i > 0; i-- {
			f(i, int(rng.Uint64()%uint64(i+1)))
		}
	}
	swap(len(b.Objects), func(i, j int) { b.Objects[i], b.Objects[j] = b.Objects[j], b.Objects[i] })
	swap(len(b.Links), func(i, j int) { b.Links[i], b.Links[j] = b.Links[j], b.Links[i] })
	swap(len(b.Volatile), func(i, j int) { b.Volatile[i], b.Volatile[j] = b.Volatile[j], b.Volatile[i] })
	return b
}

func TestChunksAreCutBySortedIDsAndDoNotDependOnInputOrder(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	base, err := buildPlan(v, fx.World(2, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	if len(base.items) != 18+16 /* objects, links */ {
		t.Fatalf("%d candidates", len(base.items))
	}
	for i := 1; i < len(base.items); i++ {
		a, b := base.items[i-1], base.items[i]
		if (a.entity == entityLink && b.entity == entityObject) || (a.entity == b.entity && a.id >= b.id) {
			t.Fatalf("candidates are not in (entity, id) order at %d", i)
		}
	}
	for seed := uint64(1); seed <= 5; seed++ {
		other, err := buildPlan(v, shuffled(fx.World(2, 2, 3), seed))
		if err != nil {
			t.Fatal(err)
		}
		if other.digest != base.digest {
			t.Errorf("seed %d: the digest depends on the order of the batch", seed)
		}
		for i := range base.items {
			if base.items[i].id != other.items[i].id {
				t.Fatalf("seed %d: the cut depends on the order of the batch", seed)
			}
		}
	}
	for _, c := range []struct{ limit, chunks, lastLen int }{{5, 7, 4}, {6, 6, 4}, {30, 2, 4}, {1000, 1, 34}, {1, 34, 1}} {
		if got := base.chunkCount(c.limit); got != c.chunks {
			t.Errorf("limit %d: %d chunks, want %d", c.limit, got, c.chunks)
		}
		items, last := base.chunkItems(base.chunkCount(c.limit)-1, c.limit)
		if !last || len(items) != c.lastLen {
			t.Errorf("limit %d: last chunk has %d items (last=%v), want %d", c.limit, len(items), last, c.lastLen)
		}
		if _, notLast := base.chunkItems(0, c.limit); notLast && c.chunks > 1 {
			t.Errorf("limit %d: the first chunk claims to be last", c.limit)
		}
	}
	empty, err := buildPlan(v, Batch{})
	if err != nil || empty.chunkCount(10) != 1 {
		t.Fatalf("an empty batch is one chunk: %v", err)
	}
	if items, last := empty.chunkItems(0, 10); len(items) != 0 || !last {
		t.Errorf("the one chunk of an empty batch is last and empty")
	}
	if items, _ := empty.chunkItems(5, 10); len(items) != 0 {
		t.Errorf("a chunk past the end has items")
	}
}

func TestBatchShapeRefusals(t *testing.T) {
	fx := mustFixture(t)
	v := newValidator(fx.Reg)
	site := fx.SiteID("S")
	scheme, _ := fx.Reg.IdentifierSchemeByName("bmc_mac")
	cases := map[string]Batch{
		"unknown batch kind":       {Kind: "other"},
		"seal as a batch":          {Event: OrderSeal},
		"zero object retirement":   {RetireObjects: []ObjectID{{}}},
		"zero link retirement":     {RetireLinks: []LinkID{{}}},
		"same link retired twice":  {RetireLinks: []LinkID{linkOf(t, fx), linkOf(t, fx)}},
		"volatile of zero object":  {Volatile: []VolatileWrite{{AttrNo: AttrServerPower}}},
		"volatile twice":           {Volatile: []VolatileWrite{fx.Power(site, "on", ""), fx.Power(site, "off", "")}},
		"identifier of zero":       {Identifiers: []IdentifierWrite{{Scheme: scheme.ID, Value: "x"}}},
		"identifier of a bad mac":  {Identifiers: []IdentifierWrite{{Object: site, Scheme: scheme.ID, Value: "nope"}}},
		"event of an unknown kind": {Events: []IdentityEvent{{Kind: "other", Retired: site, Into: site}}},
		"event without objects":    {Events: []IdentityEvent{{Kind: EventSplit}}},
		"object retired and added": {Objects: []ObjectUpsert{fx.Site("S", "")}, RetireObjects: []ObjectID{site}},
	}
	for name, b := range cases {
		if _, err := buildPlan(v, b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, kind := range OrderKinds() {
		if !kind.Valid() {
			t.Errorf("%s is in the list but not valid", kind)
		}
	}
	if len(OrderKinds()) != 8 || OrderKind("other").Valid() {
		t.Errorf("the ordering events are a closed list of eight")
	}
}

func linkOf(t *testing.T, fx *Fixture) LinkID {
	t.Helper()
	id, err := NewLinkID(fx.linkID("LOCATED_IN"), fx.SiteID("a"), fx.SiteID("b"), "")
	if err != nil {
		t.Fatal(err)
	}
	return id
}
