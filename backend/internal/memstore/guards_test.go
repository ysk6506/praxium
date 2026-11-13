package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func wantCode(t *testing.T, what string, err error, code kernel.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: no error, wanted %s", what, code)
	}
	if got := kernel.CodeOf(err); got != code {
		t.Fatalf("%s: code %s (%v), wanted %s", what, got, err, code)
	}
}

// [REQ-132] [REQ-135]
func TestAnUnknownKGIsRefusedByEveryOperation(t *testing.T) {
	s, ctx := New(), context.Background()
	ghost, err := kernel.ParseID[graph.KGKind]("kg-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(0, 0)
	lease := graph.Lease{KG: ghost, Owner: "x", Attempt: 1}
	_, err = s.Generations(ctx, ghost)
	wantCode(t, "generations", err, graph.CodeKGNotFound)
	_, err = s.Head(ctx, ghost)
	wantCode(t, "head", err, graph.CodeKGNotFound)
	_, _, err = s.Lease(ctx, ghost)
	wantCode(t, "lease", err, graph.CodeKGNotFound)
	_, err = s.Versions(ctx, ghost)
	wantCode(t, "versions", err, graph.CodeKGNotFound)
	_, err = s.AcquireLease(ctx, graph.LeaseRequest{KG: ghost, Owner: "x", TTL: time.Minute, Now: now})
	wantCode(t, "acquire", err, graph.CodeKGNotFound)
	_, err = s.RenewLease(ctx, lease, time.Minute, now)
	wantCode(t, "renew", err, graph.CodeKGNotFound)
	wantCode(t, "release", s.ReleaseLease(ctx, lease), graph.CodeKGNotFound)
	wantCode(t, "chunk", s.ApplyChunk(ctx, graph.ChunkWrite{KG: ghost}), graph.CodeKGNotFound)
	wantCode(t, "abandon", s.AbandonApply(ctx, graph.AbandonWrite{KG: ghost}), graph.CodeKGNotFound)
	wantCode(t, "seal", s.Seal(ctx, graph.SealWrite{KG: ghost}), graph.CodeKGNotFound)
	_, err = s.Restore(ctx, ghost, 0, graph.Head{KG: ghost}, now)
	wantCode(t, "restore", err, graph.CodeKGNotFound)
	_, err = s.OrderEvents(ctx, ghost, 0, 10)
	wantCode(t, "order events", err, graph.CodeKGNotFound)
	_, err = s.AppliedChanges(ctx, ghost, 0, 10)
	wantCode(t, "applied changes", err, graph.CodeKGNotFound)
	_, err = s.LightChanges(ctx, ghost, 0, 10)
	wantCode(t, "light changes", err, graph.CodeKGNotFound)
	_, err = s.Snapshot(ctx, graph.ReadPoint{KG: ghost, Kind: graph.ReadLatest})
	wantCode(t, "snapshot", err, graph.CodeKGNotFound)
}

// [REQ-132] [REQ-135]
func TestTheCatalogAndLeaseRefuseBadInput(t *testing.T) {
	s, ctx := New(), context.Background()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(time.Unix(1000, 0))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(1))
	_, err = s.CreateKG(ctx, graph.KG{})
	wantCode(t, "empty kg", err, kernel.CodeInvalidValue)
	kg, err := graph.NewTestKG(ctx, s, fx.Reg, ids, clock, graph.KGSpec{Name: "one", Role: graph.RoleProduction})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateKG(ctx, kg)
	wantCode(t, "same id again", err, kernel.CodeConflict)
	_, err = s.GetKG(ctx, "nothing")
	wantCode(t, "get by unknown name", err, graph.CodeKGNotFound)
	if all, _ := s.ListKGs(ctx); len(all) != 1 {
		t.Fatalf("%d KGs listed", len(all))
	}
	now := clock.Now()
	_, err = s.AcquireLease(ctx, graph.LeaseRequest{KG: kg.ID, TTL: time.Minute, Now: now})
	wantCode(t, "lease without owner", err, kernel.CodeInvalidValue)
	l, err := s.AcquireLease(ctx, graph.LeaseRequest{KG: kg.ID, Owner: "a", Semantic: 2, TTL: time.Minute, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AcquireLease(ctx, graph.LeaseRequest{KG: kg.ID, Owner: "b", Semantic: 2, TTL: time.Minute, Now: now})
	wantCode(t, "a live lease of another", err, graph.CodeLeaseHeld)
	_, err = s.AcquireLease(ctx, graph.LeaseRequest{KG: kg.ID, Owner: "a", Semantic: 1, TTL: time.Minute, Now: now})
	wantCode(t, "an older engine", err, graph.CodeSemanticTooLow)
	_, err = s.RenewLease(ctx, l, time.Minute, now.Add(time.Hour))
	wantCode(t, "renew after expiry", err, graph.CodeLeaseLost)
	stale := l
	stale.Attempt++
	if err := s.ReleaseLease(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if _, held, _ := s.Lease(ctx, kg.ID); !held {
		t.Fatal("a stale attempt released the lease")
	}
	if err := s.ReleaseLease(ctx, l); err != nil {
		t.Fatal(err)
	}
	if _, held, _ := s.Lease(ctx, kg.ID); held {
		t.Fatal("the lease is still there after release")
	}
}

// [REQ-132] [REQ-135] [REQ-248]
func TestWritesAndOldReadersAreHeldToTheirGenerationLeaseAndHead(t *testing.T) {
	r := newRig(t, graph.Options{})
	r.apply(r.fx.World(1, 1, 1))
	sealed, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := r.s.Head(r.ctx, r.kg.ID)
	old, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	good := graph.ChunkWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: r.clock.Now(), Head: head}
	stale := good
	stale.Generation = 7
	wantCode(t, "wrong generation", r.s.ApplyChunk(r.ctx, stale), graph.CodeGenerationChanged)
	stale = good
	stale.ExpectedSeq = 99
	wantCode(t, "wrong head", r.s.ApplyChunk(r.ctx, stale), graph.CodeHeadMoved)
	stale = good
	stale.Lease.Attempt = 99
	wantCode(t, "wrong lease", r.s.ApplyChunk(r.ctx, stale), graph.CodeLeaseLost)
	stale = good
	stale.Lease.Semantic = 0
	r.s.kgs[r.kg.ID].head.AppliedSemanticVersion = 5
	wantCode(t, "engine below the applied", r.s.ApplyChunk(r.ctx, stale), graph.CodeSemanticTooLow)
	r.s.kgs[r.kg.ID].head.AppliedSemanticVersion = 0
	stale = good
	stale.Head.AppliedSeq = head.AppliedSeq + 3
	wantCode(t, "a chunk without rows that moves the head", r.s.ApplyChunk(r.ctx, stale), graph.CodeHeadMoved)
	stale = good
	stale.Batch = &graph.ChangeBatch{Seq: head.AppliedSeq + 5}
	wantCode(t, "a chunk that skips a number", r.s.ApplyChunk(r.ctx, stale), graph.CodeHeadMoved)

	ab := graph.AbandonWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: 99, Lease: r.lease, Now: r.clock.Now(), Head: head}
	wantCode(t, "abandon with the wrong head", r.s.AbandonApply(r.ctx, ab), graph.CodeHeadMoved)
	ab.ExpectedSeq = head.AppliedSeq
	ab.Head.AppliedSeq = head.AppliedSeq + 1
	wantCode(t, "abandon to a number that is not readable_through", r.s.AbandonApply(r.ctx, ab), kernel.CodeInvalidValue)

	sw := graph.SealWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: r.clock.Now(), Head: head, Version: sealed.Version}
	wantCode(t, "a version that does not follow", r.s.Seal(r.ctx, sw), kernel.CodeInvalidValue)
	sw.Generation = 2
	wantCode(t, "seal at the wrong generation", r.s.Seal(r.ctx, sw), graph.CodeGenerationChanged)

	if _, err := r.s.RenewLease(r.ctx, r.lease, time.Hour, r.clock.Now()); err != nil {
		t.Fatal(err)
	}
	wrong := r.lease
	wrong.Owner = "other"
	_, err = r.s.RenewLease(r.ctx, wrong, time.Hour, r.clock.Now())
	wantCode(t, "renew by another owner", err, graph.CodeLeaseLost)
	if _, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest, Generation: 9}); kernel.CodeOf(err) != graph.CodeGenerationChanged {
		t.Errorf("snapshot of a generation that is not there: %v", err)
	}
	if _, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadKind(99)}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("unknown read kind: %v", err)
	}
	if _, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadVersion, Version: sealed.Version.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := r.a.Restore(r.ctx, r.kg.ID, 1); err != nil {
		t.Fatal(err)
	}
	site := r.fx.SiteID("S00")
	if info := old.Info(); info.Generation != 1 {
		t.Fatalf("old snapshot info %+v", info)
	}
	_, _, err = old.Object(r.ctx, site)
	wantCode(t, "object of an old generation", err, graph.CodeGenerationChanged)
	_, _, err = old.Link(r.ctx, r.fx.MountedInID(r.fx.ServerID("SN-000000"), r.fx.RackID(site, "R00")))
	wantCode(t, "link of an old generation", err, graph.CodeGenerationChanged)
	_, err = old.ScanObjects(r.ctx, graph.ObjectID{}, 10)
	wantCode(t, "scan of an old generation", err, graph.CodeGenerationChanged)
	_, err = old.ScanLinks(r.ctx, graph.LinkID{}, 10)
	wantCode(t, "link scan of an old generation", err, graph.CodeGenerationChanged)
	_, err = old.LinksOf(r.ctx, site, graph.Both)
	wantCode(t, "links of an old generation", err, graph.CodeGenerationChanged)
	_, _, err = old.Volatile(r.ctx, site, graph.AttrServerPower)
	wantCode(t, "volatile of an old generation", err, graph.CodeGenerationChanged)
	_, err = old.Identifiers(r.ctx, "s", "v")
	wantCode(t, "identifiers of an old generation", err, graph.CodeGenerationChanged)
	_, err = old.IdentifierHistory(r.ctx, "s", "v")
	wantCode(t, "history of an old generation", err, graph.CodeGenerationChanged)
	_, err = old.IdentityEvents(r.ctx)
	wantCode(t, "events of an old generation", err, graph.CodeGenerationChanged)
}
