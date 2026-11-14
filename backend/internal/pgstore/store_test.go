package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
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

// rig is an applier on a PostgreSQL store with the test fixture's registry.
type rig struct {
	t     *testing.T
	ctx   context.Context
	s     *pgstore.Store
	fx    *graph.Fixture
	a     *graph.Applier
	kg    graph.KG
	lease graph.Lease
	clock *kernel.ManualClock
}

func newRig(t *testing.T, opts graph.Options) *rig {
	t.Helper()
	return newRigOn(t, pgstore.NewTestStore(t), opts)
}

func newRigOn(t *testing.T, s *pgstore.Store, opts graph.Options) *rig {
	t.Helper()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, ctx: context.Background(), s: s, fx: fx, clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))}
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(11))
	if r.kg, err = graph.NewTestKG(r.ctx, r.s, fx.Reg, ids, r.clock, graph.KGSpec{Name: "main-kg", Role: graph.RoleProduction}); err != nil {
		t.Fatal(err)
	}
	opts.Semantic = 1
	if r.a, err = graph.NewApplier(r.s, fx.Reg, graph.NewTestKeyProvider("pg"), r.clock, ids, opts); err != nil {
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

func (r *rig) snap(rp graph.ReadPoint) graph.Snapshot {
	r.t.Helper()
	rp.KG = r.kg.ID
	s, err := r.s.Snapshot(r.ctx, rp)
	if err != nil {
		r.t.Fatalf("snapshot %+v: %v", rp, err)
	}
	return s
}

// [REQ-132] [REQ-135] [REQ-270]
func TestAnUnknownKGIsRefusedByEveryOperation(t *testing.T) {
	s, ctx := pgstore.NewTestStore(t), context.Background()
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
	_, err = s.WaitReadable(ctx, ghost, 1, time.Second)
	wantCode(t, "wait", err, graph.CodeKGNotFound)
}

// [REQ-132] [REQ-135] [REQ-270]
func TestTheCatalogRefusesKGsThatConflictAndListsByCreation(t *testing.T) {
	s, ctx := pgstore.NewTestStore(t), context.Background()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(time.Unix(1000, 0))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(1))
	_, err = s.CreateKG(ctx, graph.KG{})
	wantCode(t, "empty kg", err, kernel.CodeInvalidValue)
	mk := func(name string, role graph.Role) (graph.KG, error) {
		return graph.NewTestKG(ctx, s, fx.Reg, ids, clock, graph.KGSpec{Name: name, Role: role})
	}
	prod, err := mk("one", graph.RoleProduction)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateKG(ctx, prod)
	wantCode(t, "same id again", err, kernel.CodeConflict)
	_, err = mk("one", graph.RoleCandidate)
	wantCode(t, "same name", err, kernel.CodeConflict)
	_, err = mk("two", graph.RoleProduction)
	wantCode(t, "a second production KG", err, kernel.CodeConflict)
	_, err = mk(graph.MainAlias, graph.RoleCandidate)
	wantCode(t, "the name main", err, kernel.CodeConflict)
	cand, err := mk("cand", graph.RoleCandidate)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetKG(ctx, "nothing")
	wantCode(t, "get by unknown name", err, graph.CodeKGNotFound)
	for _, ref := range []string{prod.ID.String(), "one", graph.MainAlias} {
		if got, err := s.GetKG(ctx, ref); err != nil || got.ID != prod.ID {
			t.Fatalf("get %q: %+v %v", ref, got, err)
		}
	}
	all, err := s.ListKGs(ctx)
	if err != nil || len(all) != 2 || all[0].ID != prod.ID || all[1].ID != cand.ID {
		t.Fatalf("list: %+v %v", all, err)
	}
	if !cand.CreatedAt.Equal(all[1].CreatedAt) || all[1].State != graph.StateBuilding || all[1].Generation != 1 {
		t.Fatalf("a KG came back as %+v, made as %+v", all[1], cand)
	}
	gens, err := s.Generations(ctx, prod.ID)
	if err != nil || len(gens) != 1 || gens[0].Generation != 1 {
		t.Fatalf("generations: %+v %v", gens, err)
	}
}

// [REQ-132] [REQ-135] [REQ-270]
func TestLeasesRefuseBadInputAndStaleHolders(t *testing.T) {
	s, ctx := pgstore.NewTestStore(t), context.Background()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(time.Unix(1000, 0))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(1))
	kg, err := graph.NewTestKG(ctx, s, fx.Reg, ids, clock, graph.KGSpec{Name: "one", Role: graph.RoleProduction})
	if err != nil {
		t.Fatal(err)
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
	got, held, err := s.Lease(ctx, kg.ID)
	if err != nil || !held || got.Owner != "a" || got.Attempt != 1 || got.Semantic != 2 || !got.Until.Equal(l.Until) {
		t.Fatalf("lease %+v %v %v, given %+v", got, held, err, l)
	}
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
	again, err := s.AcquireLease(ctx, graph.LeaseRequest{KG: kg.ID, Owner: "b", Semantic: 2, TTL: time.Minute, Now: now})
	if err != nil || again.Attempt != 2 {
		t.Fatalf("a lease taken after a release: %+v %v (the attempt only goes up)", again, err)
	}
}

// [REQ-132] [REQ-135] [REQ-248] [REQ-270]
func TestWritesAndOldReadersAreHeldToTheirGenerationLeaseAndHead(t *testing.T) {
	r := newRig(t, graph.Options{})
	r.apply(r.fx.World(1, 1, 1))
	sealed, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := r.s.Head(r.ctx, r.kg.ID)
	old := r.snap(graph.ReadPoint{Kind: graph.ReadLatest})
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
	stale.Now = r.clock.Now().Add(1000 * time.Hour)
	wantCode(t, "an expired lease", r.s.ApplyChunk(r.ctx, stale), graph.CodeLeaseLost)
	stale = good
	stale.Lease.Semantic = 0
	wantCode(t, "engine below the applied", r.s.ApplyChunk(r.ctx, stale), graph.CodeSemanticTooLow)
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
	low := r.lease
	low.Semantic = 0
	_, err = r.s.RenewLease(r.ctx, low, time.Hour, r.clock.Now())
	wantCode(t, "renew by an older engine", err, graph.CodeSemanticTooLow)
	checkSnapshotErrors(t, r, sealed.Version.ID)

	if _, err := r.a.Restore(r.ctx, r.kg.ID, 1); err != nil {
		t.Fatal(err)
	}
	wantCode(t, "restore past readable_through", func() error {
		_, err := r.s.Restore(r.ctx, r.kg.ID, 99, head, r.clock.Now())
		return err
	}(), graph.CodeNotReadable)
	checkOldReader(t, r, old)
}

func checkSnapshotErrors(t *testing.T, r *rig, version graph.VersionID) {
	t.Helper()
	rp := graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest}
	rp.Generation = 9
	_, err := r.s.Snapshot(r.ctx, rp)
	wantCode(t, "snapshot of a generation that is not there", err, graph.CodeGenerationChanged)
	_, err = r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadKind(99)})
	wantCode(t, "unknown read kind", err, kernel.CodeInvalidValue)
	_, err = r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadSeq, Seq: 999})
	wantCode(t, "a number past readable_through", err, graph.CodeNotReadable)
	missing, err := kernel.ParseID[graph.VersionKind]("kv_00000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadVersion, Version: missing})
	wantCode(t, "a version that is not there", err, graph.CodeKGNotFound)
	snap, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadVersion, Version: version})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = snap.Volatile(r.ctx, r.fx.SiteID("S00"), graph.AttrServerPower)
	wantCode(t, "volatile of a sealed version", err, graph.CodeVolatileOmitted)
	if _, err := r.s.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadApplied}); err != nil {
		t.Fatal(err)
	}
	if _, err := snap.LinksOf(r.ctx, r.fx.SiteID("S00"), graph.Direction(9)); err == nil {
		t.Fatal("an unknown direction was answered")
	}
}

func checkOldReader(t *testing.T, r *rig, old graph.Snapshot) {
	t.Helper()
	site := r.fx.SiteID("S00")
	if info := old.Info(); info.Generation != 1 {
		t.Fatalf("old snapshot info %+v", info)
	}
	_, _, err := old.Object(r.ctx, site)
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
	_, err = old.(graph.IdentifierLister).IdentifiersOf(r.ctx, site)
	wantCode(t, "identifiers of an object, old generation", err, graph.CodeGenerationChanged)
}

// [REQ-251] [REQ-270]
func TestIdentifiersOfListsTheLiveIdentifiersOfOneObject(t *testing.T) {
	r := newRig(t, graph.Options{})
	r.apply(r.fx.World(1, 1, 2))
	scheme, _ := r.fx.Reg.IdentifierSchemeByName("redfish_system_id")
	one, other := r.fx.ServerID("SN-000000"), r.fx.ServerID("SN-000001")
	r.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{
		{Object: one, Scheme: scheme.ID, Value: "a", Scope: "x"}, {Object: other, Scheme: scheme.ID, Value: "b", Scope: "x"}}})
	list := func() []graph.IdentifierRecord {
		lister, ok := r.snap(graph.ReadPoint{Kind: graph.ReadLatest}).(graph.IdentifierLister)
		if !ok {
			t.Fatal("the PostgreSQL snapshot must list identifiers by object")
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
}

// [REQ-132] [REQ-249] [REQ-270]
func TestRowsThatDoNotMatchTheHeadAreRefusedAndLeaveNothingBehind(t *testing.T) {
	r := newRig(t, graph.Options{})
	r.apply(r.fx.World(1, 1, 2))
	head, err := r.s.Head(r.ctx, r.kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	snap := r.snap(graph.ReadPoint{Kind: graph.ReadLatest})
	rows, err := snap.ScanObjects(r.ctx, graph.ObjectID{}, 1)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	links, err := snap.ScanLinks(r.ctx, graph.LinkID{}, 1)
	if err != nil || len(links) != 1 {
		t.Fatal(links, err)
	}
	next := head.AppliedSeq + 1
	write := func() graph.ChunkWrite {
		h := head.Clone()
		h.AppliedSeq, h.ReadableThrough = next, next
		return graph.ChunkWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: r.clock.Now(),
			Batch: &graph.ChangeBatch{Seq: next, Kind: graph.BatchSync, AppliedAt: r.clock.Now()}, Head: h}
	}
	moved := rows[0]
	moved.ContentHash = kernel.Hash{9}
	w := write()
	w.Objects = []graph.ObjectChange{{Old: &moved}}
	wantCode(t, "closing an object with another hash", r.s.ApplyChunk(r.ctx, w), graph.CodeHeadMoved)
	movedLink := links[0]
	movedLink.ContentHash = kernel.Hash{9}
	w = write()
	w.Links = []graph.LinkChange{{Old: &movedLink}}
	wantCode(t, "closing a link with another hash", r.s.ApplyChunk(r.ctx, w), graph.CodeHeadMoved)
	late := rows[0]
	late.SeqFrom = next + 4
	w = write()
	w.Objects = []graph.ObjectChange{{New: &late}}
	wantCode(t, "opening an object at another number", r.s.ApplyChunk(r.ctx, w), kernel.CodeInvalidValue)
	after, err := r.s.Head(r.ctx, r.kg.ID)
	if err != nil || after.AppliedSeq != head.AppliedSeq {
		t.Fatalf("a refused chunk moved the head: %+v %v", after, err)
	}
}

// [REQ-132] [REQ-249] [REQ-270]
func TestAChunkLargerThanOneStatementIsWrittenWhole(t *testing.T) {
	r := newRig(t, graph.Options{ChunkRows: 20000})
	r.apply(r.fx.World(1, 1, 5200))
	snap := r.snap(graph.ReadPoint{Kind: graph.ReadLatest})
	objects := 0
	var after graph.ObjectID
	for {
		page, err := snap.ScanObjects(r.ctx, after, 2000)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		objects, after = objects+len(page), page[len(page)-1].ID
	}
	if objects != 5202 {
		t.Fatalf("%d objects read back, wanted 5202", objects)
	}
	if n, err := snap.ScanObjects(r.ctx, graph.ObjectID{}, 0); err != nil || n != nil {
		t.Fatalf("a scan of no rows: %v %v", n, err)
	}
	if n, err := snap.ScanLinks(r.ctx, graph.LinkID{}, -1); err != nil || n != nil {
		t.Fatalf("a link scan of no rows: %v %v", n, err)
	}
}

// [REQ-135] [REQ-249] [REQ-270]
func TestFeedsStopAtReadableThroughAndWaitsEndWhenTheyShould(t *testing.T) {
	r := newRig(t, graph.Options{})
	r.apply(r.fx.World(1, 1, 1))
	if got, err := r.s.AppliedChanges(r.ctx, r.kg.ID, 0, 0); err != nil || got != nil {
		t.Fatalf("a feed with no room: %v %v", got, err)
	}
	if got, err := r.s.LightChanges(r.ctx, r.kg.ID, 0, 0); err != nil || got != nil {
		t.Fatalf("a light feed with no room: %v %v", got, err)
	}
	if got, err := r.s.OrderEvents(r.ctx, r.kg.ID, 0, 0); err != nil || got != nil {
		t.Fatalf("an order feed with no room: %v %v", got, err)
	}
	head, _ := r.s.Head(r.ctx, r.kg.ID)
	if got, err := r.s.WaitReadable(r.ctx, r.kg.ID, head.ReadableThrough, 0); err != nil || got != head.ReadableThrough {
		t.Fatalf("a wait that is already over: %d %v", got, err)
	}
	_, err := r.s.WaitReadable(r.ctx, r.kg.ID, head.ReadableThrough+1, 120*time.Millisecond)
	wantCode(t, "a wait for a number that does not come", err, graph.CodeWaitTimeout)
	cancelled, cancel := context.WithTimeout(r.ctx, 80*time.Millisecond)
	defer cancel()
	_, err = r.s.WaitReadable(cancelled, r.kg.ID, head.ReadableThrough+1, time.Minute)
	wantCode(t, "a cancelled wait", err, kernel.CodeInternal)
}

// [REQ-135] [REQ-270]
func TestVersionsCarryTheirPartsAndRestoreDropsTheLaterOnes(t *testing.T) {
	r := newRig(t, graph.Options{})
	r.apply(r.fx.World(1, 1, 1))
	first, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil {
		t.Fatal(err)
	}
	r.apply(graph.Batch{Objects: []graph.ObjectUpsert{r.fx.Site("S00", "Elsewhere")}})
	second, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := r.s.Versions(r.ctx, r.kg.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions %+v %v", versions, err)
	}
	if versions[0].ID != first.Version.ID || versions[1].Prev != first.Version.ID || versions[1].ID != second.Version.ID ||
		versions[1].GraphHash != second.Version.GraphHash || len(versions[1].Parts) == 0 || versions[1].Parts == nil {
		t.Fatalf("versions came back as %+v, sealed as %+v", versions, second.Version)
	}
	for k, p := range second.Version.Parts {
		if versions[1].Parts[k] != p {
			t.Fatalf("part %v is %v, sealed as %v", k, versions[1].Parts[k], p)
		}
	}
	if _, err := r.a.Restore(r.ctx, r.kg.ID, first.Version.SealedSeq); err != nil {
		t.Fatal(err)
	}
	versions, err = r.s.Versions(r.ctx, r.kg.ID)
	if err != nil || len(versions) != 1 || versions[0].ID != first.Version.ID {
		t.Fatalf("versions after restore: %+v %v", versions, err)
	}
	gens, err := r.s.Generations(r.ctx, r.kg.ID)
	if err != nil || len(gens) != 2 || gens[1].RestoredTo != first.Version.SealedSeq {
		t.Fatalf("generations %+v %v", gens, err)
	}
}
