package graph

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

type scratchWorld struct {
	store *ScratchStore
	a     *Applier
	kg    KG
	lease Lease
	fx    *Fixture
}

func newScratchWorld(t *testing.T) *scratchWorld {
	t.Helper()
	fx := mustFixture(t)
	ctx := context.Background()
	clock := kernel.NewManualClock(epoch)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(9))
	w := &scratchWorld{store: NewScratchStore(), fx: fx}
	var err error
	if w.kg, err = NewTestKG(ctx, w.store, fx.Reg, ids, clock, KGSpec{Name: "scratch-kg", Role: RoleProduction}); err != nil {
		t.Fatal(err)
	}
	if w.a, err = NewApplier(w.store, fx.Reg, NewTestKeyProvider("s"), clock, ids, Options{Semantic: 1}); err != nil {
		t.Fatal(err)
	}
	if w.lease, err = w.a.Acquire(ctx, w.kg.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = w.a.Apply(ctx, w.kg.ID, w.lease, fx.World(1, 2, 2)); err != nil {
		t.Fatal(err)
	}
	return w
}

// [REQ-259]
func TestScratchStoreServesWhatTheUnitsAboveItNeed(t *testing.T) {
	w := newScratchWorld(t)
	ctx := context.Background()
	kgs, err := w.store.ListKGs(ctx)
	if err != nil || len(kgs) != 1 || kgs[0].State != StateServing {
		t.Fatalf("KGs %v %v", kgs, err)
	}
	for _, ref := range []string{"main", "scratch-kg", w.kg.ID.String()} {
		if got, err := w.store.GetKG(ctx, ref); err != nil || got.ID != w.kg.ID {
			t.Errorf("GetKG(%s) = %v %v", ref, got.ID, err)
		}
	}
	head, err := w.store.Head(ctx, w.kg.ID)
	if err != nil || head.ReadableThrough == 0 {
		t.Fatalf("head %+v %v", head, err)
	}
	snap, err := w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Kind: ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	objs, err := snap.ScanObjects(ctx, ObjectID{}, 100)
	if err != nil || len(objs) != 1+2+4 {
		t.Fatalf("objects %d %v", len(objs), err)
	}
	if page, _ := snap.ScanObjects(ctx, ObjectID{}, 2); len(page) != 2 {
		t.Fatalf("a page holds %d", len(page))
	}
	links, err := snap.ScanLinks(ctx, LinkID{}, 100)
	if err != nil || len(links) != 2+4 {
		t.Fatalf("links %d %v", len(links), err)
	}
	if page, _ := snap.ScanLinks(ctx, LinkID{}, 1); len(page) != 1 {
		t.Fatalf("a link page holds %d", len(page))
	}
	site := w.fx.SiteID("S00")
	if in, _ := snap.LinksOf(ctx, site, In); len(in) != 2 {
		t.Fatalf("links into the site: %d", len(in))
	}
	if out, _ := snap.LinksOf(ctx, site, Out); len(out) != 0 {
		t.Fatalf("links out of the site: %d", len(out))
	}
	if row, ok, _ := snap.Object(ctx, site); !ok || row.ID != site {
		t.Fatalf("object %v %v", row.ID, ok)
	}
	if _, ok, _ := snap.Link(ctx, links[0].ID); !ok {
		t.Fatal("link missing")
	}
	server := w.fx.ServerID("SN-000000")
	if v, ok, err := snap.Volatile(ctx, server, AttrServerPower); err != nil || !ok || v.Value != "on" {
		t.Fatalf("volatile %v %v %v", v.Value, ok, err)
	}
	if ids, err := snap.Identifiers(ctx, "none", "x"); err != nil || len(ids) != 0 {
		t.Fatalf("identifiers %v %v", ids, err)
	}
	if hist, err := snap.IdentifierHistory(ctx, "none", "x"); err != nil || len(hist) != 0 {
		t.Fatalf("history %v %v", hist, err)
	}
	if lister, ok := snap.(IdentifierLister); !ok {
		t.Fatal("no IdentifierLister")
	} else if ids, err := lister.IdentifiersOf(ctx, server); err != nil || len(ids) != 0 {
		t.Fatalf("identifiers of %v %v", ids, err)
	}
	if ev, err := snap.IdentityEvents(ctx); err != nil || len(ev) != 0 {
		t.Fatalf("events %v %v", ev, err)
	}
}

// [REQ-259]
func TestScratchStoreSealsAndRefusesWhatItCannotDo(t *testing.T) {
	w := newScratchWorld(t)
	ctx := context.Background()
	sealed, err := w.a.Seal(ctx, w.kg.ID, w.lease, SealSync)
	if err != nil || !sealed.Created {
		t.Fatalf("seal %v", err)
	}
	if vs, err := w.store.Versions(ctx, w.kg.ID); err != nil || len(vs) != 1 {
		t.Fatalf("versions %v %v", vs, err)
	}
	head, _ := w.store.Head(ctx, w.kg.ID)
	atVersion, err := w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Kind: ReadVersion, Version: sealed.Version.ID})
	if err != nil || atVersion.Info().Volatile != VolatileOmitted {
		t.Fatalf("version read %v", err)
	}
	if _, err := w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Kind: ReadSeq, Seq: head.ReadableThrough}); err != nil {
		t.Fatalf("read at the head: %v", err)
	}
	if _, err := w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Kind: ReadApplied}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	if _, _, err := atVersion.(scratchSnap).Volatile(ctx, w.fx.ServerID("SN-000000"), AttrServerPower); kernel.CodeOf(err) != CodeVolatileOmitted {
		t.Fatalf("a sealed read has no volatile values: %v", err)
	}
	refused := map[string]error{}
	_, refused["older seq"] = w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Kind: ReadSeq, Seq: head.ReadableThrough - 1})
	_, refused["ahead"] = w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Kind: ReadSeq, Seq: head.ReadableThrough + 1})
	_, refused["no version"] = w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Kind: ReadVersion, Version: VersionID{}})
	_, refused["generation"] = w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Generation: 7})
	_, refused["kind"] = w.store.Snapshot(ctx, ReadPoint{KG: w.kg.ID, Kind: ReadKind(9)})
	_, refused["kg"] = w.store.Snapshot(ctx, ReadPoint{KG: KGID{}})
	_, refused["restore"] = w.store.Restore(ctx, w.kg.ID, 1, head, time.Time{})
	_, refused["generations"] = w.store.Generations(ctx, w.kg.ID)
	refused["abandon"] = w.store.AbandonApply(ctx, AbandonWrite{})
	_, refused["applied feed"] = w.store.AppliedChanges(ctx, w.kg.ID, 0, 1)
	_, refused["light feed"] = w.store.LightChanges(ctx, w.kg.ID, 0, 1)
	_, refused["second KG"] = w.store.CreateKG(ctx, w.kg)
	_, refused["unnamed KG"] = NewScratchStore().CreateKG(ctx, KG{})
	_, refused["unknown ref"] = w.store.GetKG(ctx, "nothing")
	_, refused["wait"] = w.store.WaitReadable(ctx, w.kg.ID, head.ReadableThrough+5, time.Millisecond)
	want := map[string]kernel.Code{"older seq": kernel.CodeUnsupported, "ahead": CodeNotReadable, "no version": CodeKGNotFound,
		"generation": CodeGenerationChanged, "kind": kernel.CodeInvalidValue, "kg": CodeKGNotFound, "restore": kernel.CodeUnsupported,
		"generations": kernel.CodeUnsupported, "abandon": kernel.CodeUnsupported, "applied feed": kernel.CodeUnsupported,
		"light feed": kernel.CodeUnsupported, "second KG": kernel.CodeConflict, "unnamed KG": kernel.CodeInvalidValue,
		"unknown ref": CodeKGNotFound, "wait": CodeWaitTimeout}
	for name, code := range want {
		if kernel.CodeOf(refused[name]) != code {
			t.Errorf("%s: %v, want code %s", name, refused[name], code)
		}
	}
	if at, err := w.store.WaitReadable(ctx, w.kg.ID, head.ReadableThrough, 0); err != nil || at != head.ReadableThrough {
		t.Fatalf("wait for what is readable: %d %v", at, err)
	}
	if l, held, err := w.store.Lease(ctx, w.kg.ID); err != nil || !held {
		t.Fatalf("lease %v %v %v", l, held, err)
	}
	if _, err := w.store.RenewLease(ctx, w.lease, time.Minute, epoch); err != nil {
		t.Fatal(err)
	}
	if err := w.store.ReleaseLease(ctx, w.lease); err != nil {
		t.Fatal(err)
	}
	if _, held, _ := w.store.Lease(ctx, w.kg.ID); held {
		t.Fatal("the lease should be released")
	}
	if ev, err := w.store.OrderEvents(ctx, w.kg.ID, 0, 10); err != nil || len(ev) == 0 {
		t.Fatalf("order events %v %v", ev, err)
	}
	if _, err := w.store.OrderEvents(ctx, KGID{}, 0, 10); err == nil {
		t.Fatal("order events of no KG")
	}
	if err := w.store.ApplyChunk(ctx, ChunkWrite{KG: w.kg.ID, ExpectedSeq: 99}); kernel.CodeOf(err) != CodeHeadMoved {
		t.Fatalf("a stale write: %v", err)
	}
}
