package graph

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// bulkBoth is a snapshot that reads in bulk: it answers the bulk reads from the snapshot it holds. It is a type of
// its own and has no method of the port, so no existing test reaches it.
type bulkBoth struct{ Snapshot }

func (v bulkBoth) ObjectsByID(ctx context.Context, ids []ObjectID) (map[ObjectID]ObjectRow, error) {
	out := map[ObjectID]ObjectRow{}
	for _, id := range ids {
		if r, ok, _ := v.Object(ctx, id); ok {
			out[id] = r
		}
	}
	return out, nil
}

func (v bulkBoth) LinksByID(ctx context.Context, ids []LinkID) (map[LinkID]LinkRow, error) {
	out := map[LinkID]LinkRow{}
	for _, id := range ids {
		if r, ok, _ := v.Link(ctx, id); ok {
			out[id] = r
		}
	}
	return out, nil
}

func (v bulkBoth) LinksOfMany(ctx context.Context, ids []ObjectID, dir Direction) (map[ObjectID][]LinkRow, error) {
	out := map[ObjectID][]LinkRow{}
	for _, id := range ids {
		if rows, _ := v.LinksOf(ctx, id, dir); len(rows) > 0 {
			out[id] = rows
		}
	}
	return out, nil
}

// bulkPlain is a snapshot that cannot read in bulk: only the port.
type bulkPlain struct{ Snapshot }

func TestBulkReadsServeTheApplierAndTheCacheRemembersWhatIsNotThere(t *testing.T) {
	ctx := context.Background()
	u := newUnit(t, nil, Options{ChunkRows: 5})
	fx := u.fx
	site := fx.SiteID("S00")
	rack, srv := fx.RackID(site, "R00"), fx.ServerID("SN-000000")
	u.apply(fx.World(2, 2, 3))
	u.apply(Batch{Objects: []ObjectUpsert{fx.Server("S9", "m2")}, Links: []LinkUpsert{fx.MountedIn(fx.ServerID("S9"), rack, 3)},
		Volatile: []VolatileWrite{fx.Power(srv, "on", "2031-01-01T00:00:00Z")}})
	u.apply(Batch{RetireObjects: []ObjectID{fx.ServerID("S9"), srv},
		RetireLinks: []LinkID{fx.MountedInID(fx.ServerID("S9"), rack), fx.MountedInID(srv, rack)}})
	if err := u.a.Verify(ctx, u.kg.ID); err != nil {
		t.Fatal(err)
	}
	_, err := u.a.Apply(ctx, u.kg.ID, u.lease, Batch{RetireObjects: []ObjectID{rack}})
	wantCode(t, "a retirement that leaves links", err, CodeDanglingLink)

	snap, err := u.store.Snapshot(ctx, ReadPoint{KG: u.kg.ID, Kind: ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	cached := withBulk(bulkBoth{snap})
	if withBulk(cached) != cached || withBulk(bulkPlain{snap}) == cached {
		t.Fatal("withBulk wraps once, and only a snapshot that can bulk read")
	}
	if err := prefetchObjects(ctx, bulkPlain{snap}, []ObjectID{site}); err != nil {
		t.Fatal(err)
	}
	ghost := fx.ServerID("nothing")
	if err := prefetchObjects(ctx, cached, []ObjectID{site, ghost}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cached.Object(ctx, ghost); ok {
		t.Fatal("an object that is not there is there")
	}
	if got, ok, _ := cached.Object(ctx, site); !ok || got.ID != site {
		t.Fatal("the object that was read in bulk is not served")
	}
	if err := prefetchLinks(ctx, cached, []LinkID{fx.MountedInID(fx.ServerID("SN-000001"), fx.RackID(site, "R00"))}); err != nil {
		t.Fatal(err)
	}
	if err := prefetchLinksOf(ctx, cached, []ObjectID{rack, site}, Both); err != nil {
		t.Fatal(err)
	}
	if rows, err := cached.LinksOf(ctx, rack, Both); err != nil || len(rows) == 0 {
		t.Fatalf("the links of the rack: %v %v", rows, err)
	}
	if _, ok, _ := cached.Link(ctx, fx.MountedInID(srv, rack)); ok {
		t.Fatal("a link that was retired is there")
	}
	if got, ok, err := cached.Object(ctx, rack); err != nil || !ok || got.ID != rack {
		t.Fatalf("an object that was not read in bulk is read from the snapshot: %v %v", ok, err)
	}
	if err := prefetchLinks(ctx, cached, []LinkID{fx.MountedInID(srv, rack)}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cached.Link(ctx, fx.MountedInID(srv, rack)); ok {
		t.Fatal("a link that is not there is remembered as there")
	}
}

// A chunk and a state check on a snapshot that reads in bulk find what they find on one that reads one at a time.
func TestBulkSnapshotGivesTheSameChunkAndTheSameRefusals(t *testing.T) {
	ctx := context.Background()
	u := newUnit(t, nil, Options{ChunkRows: 50})
	fx := u.fx
	site := fx.SiteID("S00")
	rack, srv := fx.RackID(site, "R00"), fx.ServerID("SN-000000")
	u.apply(fx.World(1, 1, 2))
	batches := map[string]Batch{
		"adds and changes": {Objects: []ObjectUpsert{fx.Server("S9", "m2"), fx.Server("SN-000000", "m3")}, Links: []LinkUpsert{fx.MountedIn(fx.ServerID("S9"), rack, 5)},
			Volatile: []VolatileWrite{fx.Power(srv, "off", "2031-01-01T00:00:00Z")}},
		"retires": {RetireObjects: []ObjectID{srv}, RetireLinks: []LinkID{fx.MountedInID(srv, rack)}},
		"dangles": {RetireObjects: []ObjectID{rack}},
	}
	for name, b := range batches {
		p, err := buildPlan(u.a.v, b)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var errs [2]error
		var built [2]*chunkBuilder
		for i, wrap := range []func(Snapshot) Snapshot{func(s Snapshot) Snapshot { return s }, func(s Snapshot) Snapshot { return bulkBoth{s} }} {
			snap, err := u.store.Snapshot(ctx, ReadPoint{KG: u.kg.ID, Kind: ReadApplied})
			if err != nil {
				t.Fatal(err)
			}
			snap = wrap(snap)
			if errs[i] = checkState(ctx, snap, u.a.reg, u.a.v, p); errs[i] != nil {
				continue
			}
			built[i] = newChunkBuilder(u.a, u.kg.ID, snap, u.store.head.Hash)
			if errs[i] = built[i].build(ctx, p, p.items, true); errs[i] != nil {
				t.Fatalf("%s: %v", name, errs[i])
			}
		}
		if kernel.CodeOf(errs[0]) != kernel.CodeOf(errs[1]) {
			t.Fatalf("%s: %v and %v", name, errs[0], errs[1])
		}
		if built[0] != nil && (len(built[0].entries) != len(built[1].entries) || !built[0].hash.Equal(built[1].hash)) {
			t.Fatalf("%s: the chunks differ", name)
		}
	}
}

func TestBulkRowSumIsTheRowHashOfTheEntityTheIdAndTheContent(t *testing.T) {
	var content kernel.Hash
	content[0] = 7
	a, err := RowSum("object", "obj_1", content)
	if err != nil {
		t.Fatal(err)
	}
	same, _ := rowSum("object", "obj_1", content)
	other, _ := RowSum("link", "obj_1", content)
	if a != same || a == other {
		t.Fatal("the sum is the row hash of the entity, the id and the content")
	}
}
