package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// rig is an applier on a memstore with the test fixture's registry.
type rig struct {
	t     *testing.T
	ctx   context.Context
	s     *Store
	fx    *graph.Fixture
	a     *graph.Applier
	kg    graph.KG
	lease graph.Lease
	clock *kernel.ManualClock
}

func newRig(t *testing.T, opts graph.Options) *rig {
	t.Helper()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, ctx: context.Background(), s: New(), fx: fx, clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))}
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(11))
	if r.kg, err = graph.NewTestKG(r.ctx, r.s, fx.Reg, ids, r.clock, graph.KGSpec{Name: "main-kg", Role: graph.RoleProduction}); err != nil {
		t.Fatal(err)
	}
	opts.Semantic = 1
	if r.a, err = graph.NewApplier(r.s, fx.Reg, graph.NewTestKeyProvider("ms"), r.clock, ids, opts); err != nil {
		t.Fatal(err)
	}
	if r.lease, err = r.a.Acquire(r.ctx, r.kg.ID); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) apply(b graph.Batch) graph.Result {
	r.t.Helper()
	res, err := r.a.Apply(r.ctx, r.kg.ID, r.lease, b)
	if err != nil {
		r.t.Fatalf("apply: %v", err)
	}
	return res
}

func (r *rig) count(rp graph.ReadPoint) (int, int) {
	r.t.Helper()
	rp.KG = r.kg.ID
	snap, err := r.s.Snapshot(r.ctx, rp)
	if err != nil {
		r.t.Fatalf("snapshot %+v: %v", rp, err)
	}
	objs, err := snap.ScanObjects(r.ctx, graph.ObjectID{}, 1000)
	if err != nil {
		r.t.Fatal(err)
	}
	links, err := snap.ScanLinks(r.ctx, graph.LinkID{}, 1000)
	if err != nil {
		r.t.Fatal(err)
	}
	return len(objs), len(links)
}

// [REQ-132] [REQ-135] [REQ-246] [REQ-247] [REQ-248] [REQ-249]
func TestAChangeOfEveryKindCanBeReadBackAtItsOwnNumber(t *testing.T) {
	r := newRig(t, graph.Options{ChunkRows: 4, VerifyOnSeal: true})
	r.apply(r.fx.World(1, 2, 4))
	objs, links := r.count(graph.ReadPoint{Kind: graph.ReadLatest})
	if objs != 11 || links != 10 {
		t.Fatalf("world has %d objects and %d links", objs, links)
	}
	sealed, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil || !sealed.Created {
		t.Fatalf("seal: %+v %v", sealed, err)
	}
	one := r.fx.ServerID("SN-000000")
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00")
	two := r.fx.ServerID("SN-000001")
	twoLink := r.fx.MountedInID(two, r.fx.RackID(r.fx.SiteID("S00"), "R00"))
	scheme, _ := r.fx.Reg.IdentifierSchemeByName("redfish_system_id")
	r.apply(graph.Batch{
		Objects:     []graph.ObjectUpsert{r.fx.Site("S00", "Else")},
		Links:       []graph.LinkUpsert{r.fx.MountedIn(one, rack, 9)},
		Volatile:    []graph.VolatileWrite{r.fx.Power(one, "on", "2030-01-01T00:00:00Z")},
		Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a"}},
	})
	r.apply(graph.Batch{RetireObjects: []graph.ObjectID{two}, RetireLinks: []graph.LinkID{twoLink},
		Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a", Close: true}}})

	snap, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := snap.Object(r.ctx, two); ok {
		t.Errorf("the retired server is readable in the latest state")
	}
	if row, ok, _ := snap.Link(r.ctx, twoLink); ok {
		t.Errorf("the retired link is readable: %+v", row)
	}
	if v, ok, err := snap.Volatile(r.ctx, one, graph.AttrServerPower); err != nil || !ok || v.Value != "on" {
		t.Errorf("volatile %+v %v %v", v, ok, err)
	}
	if recs, _ := snap.Identifiers(r.ctx, scheme.ID.String(), "sys-1"); len(recs) != 0 {
		t.Errorf("a closed identifier is still held: %+v", recs)
	}
	if hist, _ := snap.IdentifierHistory(r.ctx, scheme.ID.String(), "sys-1"); len(hist) != 1 {
		t.Errorf("identifier history %+v", hist)
	}
	if _, err := snap.IdentityEvents(r.ctx); err != nil {
		t.Fatal(err)
	}
	for _, k := range []graph.Direction{graph.Out, graph.In, graph.Both} {
		if _, err := snap.LinksOf(r.ctx, rack, k); err != nil {
			t.Fatal(err)
		}
	}

	// The old number and the sealed version still give the old world.
	if o, _ := r.count(graph.ReadPoint{Kind: graph.ReadSeq, Seq: sealed.Version.SealedSeq}); o != 11 {
		t.Errorf("%d objects at the sealed number", o)
	}
	if o, l := r.count(graph.ReadPoint{Kind: graph.ReadVersion, Version: sealed.Version.ID}); o != 11 || l != 10 {
		t.Errorf("%d objects %d links in the sealed version", o, l)
	}
	if _, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadSeq, Seq: 9999}); kernel.CodeOf(err) != graph.CodeNotReadable {
		t.Errorf("a number past the head: %v", err)
	}
	if _, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync); err != nil {
		t.Fatal(err)
	}
	if err := r.a.Verify(r.ctx, r.kg.ID); err != nil {
		t.Fatal(err)
	}
	vs, _ := r.s.Versions(r.ctx, r.kg.ID)
	if len(vs) != 2 {
		t.Errorf("%d versions", len(vs))
	}
	if ev, _ := r.s.OrderEvents(r.ctx, r.kg.ID, 0, 100); len(ev) < 3 {
		t.Errorf("%d order events", len(ev))
	}
	if ch, _ := r.s.AppliedChanges(r.ctx, r.kg.ID, 0, 1000); len(ch) == 0 {
		t.Errorf("no applied changes")
	}
	if lc, _ := r.s.LightChanges(r.ctx, r.kg.ID, 0, 1000); len(lc) == 0 {
		t.Errorf("no light changes")
	}
	if n, err := r.s.WaitReadable(r.ctx, r.kg.ID, 0, time.Second); err != nil || n == 0 {
		t.Errorf("wait: %d %v", n, err)
	}
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := r.s.WaitReadable(ctx, r.kg.ID, 1<<20, time.Millisecond); kernel.CodeOf(err) != graph.CodeWaitTimeout {
		t.Errorf("wait beyond the head: %v", err)
	}
}

// [REQ-246]
func TestAnAbandonedApplyGoesAndItsNumbersComeBack(t *testing.T) {
	r := newRig(t, graph.Options{ChunkRows: 3})
	r.apply(r.fx.World(1, 1, 3))
	before, _ := r.s.Head(r.ctx, r.kg.ID)
	run, err := r.a.Begin(r.ctx, r.kg.ID, r.lease, r.fx.World(1, 3, 6))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2 && !run.Done(); i++ {
		if err := run.Next(r.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.a.Abandon(r.ctx, r.kg.ID, r.lease); err != nil {
		t.Fatal(err)
	}
	after, _ := r.s.Head(r.ctx, r.kg.ID)
	if after.AppliedSeq != before.AppliedSeq || after.Open() {
		t.Fatalf("head after the abandon %+v, before %+v", after, before)
	}
	if o, l := r.count(graph.ReadPoint{Kind: graph.ReadApplied}); o != 5 || l != 4 {
		t.Errorf("%d objects and %d links after the abandon", o, l)
	}
	if err := r.a.Verify(r.ctx, r.kg.ID); err != nil {
		t.Fatal(err)
	}
}

// [REQ-248]
func TestARestoreRaisesTheGenerationAndCutsLaterRows(t *testing.T) {
	r := newRig(t, graph.Options{})
	r.apply(r.fx.World(1, 1, 1))
	r.apply(r.fx.World(1, 2, 3))
	if _, err := r.a.Restore(r.ctx, r.kg.ID, 1); err != nil {
		t.Fatal(err)
	}
	gens, _ := r.s.Generations(r.ctx, r.kg.ID)
	if len(gens) != 2 {
		t.Fatalf("generations %+v", gens)
	}
	if o, _ := r.count(graph.ReadPoint{Kind: graph.ReadLatest}); o != 3 {
		t.Errorf("%d objects after the restore", o)
	}
}
