package pgstore_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// [REQ-270]
func TestAClosedPoolFailsEveryOperationWithAnError(t *testing.T) {
	s, ctx := pgstore.NewTestStore(t), context.Background()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	kg, _ := kernel.ParseID[graph.KGKind]("kg-0123456789abcdef")
	now := time.Unix(0, 0)
	lease := graph.Lease{KG: kg, Owner: "x", Attempt: 1}
	ops := map[string]func() error{
		"create":  func() error { _, err := s.CreateKG(ctx, graph.KG{ID: kg, Name: "n", Role: graph.RoleTest}); return err },
		"get":     func() error { _, err := s.GetKG(ctx, "n"); return err },
		"list":    func() error { _, err := s.ListKGs(ctx); return err },
		"gens":    func() error { _, err := s.Generations(ctx, kg); return err },
		"restore": func() error { _, err := s.Restore(ctx, kg, 0, graph.Head{}, now); return err },
		"acquire": func() error {
			_, err := s.AcquireLease(ctx, graph.LeaseRequest{KG: kg, Owner: "x", TTL: time.Second, Now: now})
			return err
		},
		"renew":    func() error { _, err := s.RenewLease(ctx, lease, time.Second, now); return err },
		"release":  func() error { return s.ReleaseLease(ctx, lease) },
		"chunk":    func() error { return s.ApplyChunk(ctx, graph.ChunkWrite{KG: kg}) },
		"abandon":  func() error { return s.AbandonApply(ctx, graph.AbandonWrite{KG: kg}) },
		"seal":     func() error { return s.Seal(ctx, graph.SealWrite{KG: kg}) },
		"head":     func() error { _, err := s.Head(ctx, kg); return err },
		"lease":    func() error { _, _, err := s.Lease(ctx, kg); return err },
		"versions": func() error { _, err := s.Versions(ctx, kg); return err },
		"snapshot": func() error { _, err := s.Snapshot(ctx, graph.ReadPoint{KG: kg}); return err },
		"applied":  func() error { _, err := s.AppliedChanges(ctx, kg, 0, 1); return err },
		"light":    func() error { _, err := s.LightChanges(ctx, kg, 0, 1); return err },
		"order":    func() error { _, err := s.OrderEvents(ctx, kg, 0, 1); return err },
		"wait":     func() error { _, err := s.WaitReadable(ctx, kg, 1, time.Second); return err },
	}
	for name, op := range ops {
		if err := op(); err == nil || kernel.CodeOf(err) == graph.CodeKGNotFound {
			t.Errorf("%s on a closed pool: %v", name, err)
		}
	}
}

// damaged runs ops with a table renamed away, so that every statement that touches it fails, and then puts it
// back. Every op must fail with an error and none may answer wrongly.
func damaged(t *testing.T, admin *sql.DB, table string, ops map[string]func() error) {
	t.Helper()
	ctx := context.Background()
	if _, err := admin.ExecContext(ctx, `ALTER TABLE `+table+` RENAME TO `+table+`_away`); err != nil {
		t.Fatalf("cannot damage %s: %v", table, err)
	}
	defer func() {
		if _, err := admin.ExecContext(ctx, `ALTER TABLE `+table+`_away RENAME TO `+table); err != nil {
			t.Fatalf("cannot repair %s: %v", table, err)
		}
	}()
	for name, op := range ops {
		if err := op(); err == nil {
			t.Errorf("%s with %s missing: no error", name, table)
		}
	}
}

// [REQ-270]
func TestADamagedSchemaFailsEveryOperationInsteadOfAnsweringWrongly(t *testing.T) {
	ctx := context.Background()
	dsn := pgstore.NewTestDatabase(t)
	st, err := pgstore.Open(ctx, pgstore.Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	r := newRigOn(t, st, graph.Options{})
	scheme, _ := r.fx.Reg.IdentifierSchemeByName("redfish_system_id")
	one := r.fx.ServerID("SN-000000")
	r.apply(r.fx.World(1, 1, 2))
	r.apply(graph.Batch{Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "a", Scope: "x"}},
		Volatile: []graph.VolatileWrite{r.fx.Power(one, "off", "2030-01-02T00:00:00Z")}})
	if _, err := r.a.Seal(ctx, r.kg.ID, r.lease, graph.SealSync); err != nil {
		t.Fatal(err)
	}
	snap := r.snap(graph.ReadPoint{Kind: graph.ReadLatest})
	site := r.fx.SiteID("S00")
	link := r.fx.MountedInID(one, r.fx.RackID(site, "R00"))
	head, _ := r.s.Head(ctx, r.kg.ID)

	reads := map[string]map[string]func() error{
		"object_live": {
			"object":   func() error { _, _, err := snap.Object(ctx, site); return err },
			"scan":     func() error { _, err := snap.ScanObjects(ctx, graph.ObjectID{}, 5); return err },
			"volatile": func() error { _, _, err := snap.Volatile(ctx, one, graph.AttrServerPower); return err },
		},
		"link_live": {
			"link":    func() error { _, _, err := snap.Link(ctx, link); return err },
			"scan":    func() error { _, err := snap.ScanLinks(ctx, graph.LinkID{}, 5); return err },
			"linksof": func() error { _, err := snap.LinksOf(ctx, one, graph.Both); return err },
		},
		"attr_volatile":        {"volatile": func() error { _, _, err := snap.Volatile(ctx, one, graph.AttrServerPower); return err }},
		"object_identifier":    {"identifiers": func() error { _, err := snap.Identifiers(ctx, "s", "a"); return err }, "of": func() error { _, err := snap.(graph.IdentifierLister).IdentifiersOf(ctx, one); return err }},
		"identity_event":       {"events": func() error { _, err := snap.IdentityEvents(ctx); return err }},
		"change_batch":         {"applied": func() error { _, err := r.s.AppliedChanges(ctx, r.kg.ID, 0, 5); return err }},
		"change_entry":         {"applied": func() error { _, err := r.s.AppliedChanges(ctx, r.kg.ID, 0, 5); return err }},
		"light_change":         {"light": func() error { _, err := r.s.LightChanges(ctx, r.kg.ID, 0, 5); return err }},
		"ingest_order":         {"order": func() error { _, err := r.s.OrderEvents(ctx, r.kg.ID, 0, 5); return err }},
		"kg_version":           {"versions": func() error { _, err := r.s.Versions(ctx, r.kg.ID); return err }},
		"kg_version_hash_part": {"versions": func() error { _, err := r.s.Versions(ctx, r.kg.ID); return err }},
		"generation_event":     {"generations": func() error { _, err := r.s.Generations(ctx, r.kg.ID); return err }},
		"kg_apply_lease":       {"lease": func() error { _, _, err := r.s.Lease(ctx, r.kg.ID); return err }},
		"kg_head":              {"head": func() error { _, err := r.s.Head(ctx, r.kg.ID); return err }},
	}
	for table, ops := range reads {
		damaged(t, admin, table, ops)
	}

	next := head.AppliedSeq + 1
	chunk := func() graph.ChunkWrite {
		h := head.Clone()
		h.AppliedSeq, h.ReadableThrough = next, next
		return graph.ChunkWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: r.clock.Now(),
			Batch: &graph.ChangeBatch{Seq: next, Kind: graph.BatchSync, AppliedAt: r.clock.Now()}, Head: h}
	}
	rows, _ := snap.ScanObjects(ctx, graph.ObjectID{}, 1)
	links, _ := snap.ScanLinks(ctx, graph.LinkID{}, 1)
	writes := map[string]func(*graph.ChunkWrite){
		"object_live":   func(w *graph.ChunkWrite) { n := rows[0]; n.SeqFrom = next; w.Objects = []graph.ObjectChange{{New: &n}} },
		"object_hist":   func(w *graph.ChunkWrite) { w.Objects = []graph.ObjectChange{{Old: &rows[0]}} },
		"link_live":     func(w *graph.ChunkWrite) { n := links[0]; n.SeqFrom = next; w.Links = []graph.LinkChange{{New: &n}} },
		"link_hist":     func(w *graph.ChunkWrite) { w.Links = []graph.LinkChange{{Old: &links[0]}} },
		"attr_volatile": func(w *graph.ChunkWrite) { w.Volatile = []graph.VolatileRow{{Object: one, AttrNo: 1, Value: "v"}} },
		"light_change":  func(w *graph.ChunkWrite) { w.Light = []graph.LightChange{{Seq: next, Object: one, AttrNo: 1}} },
		"object_identifier": func(w *graph.ChunkWrite) {
			w.Identifiers = []graph.IdentifierChange{{Record: graph.IdentifierRecord{Object: one, Scheme: "s", Value: "v", SeqFrom: next}}}
		},
		"identity_event": func(w *graph.ChunkWrite) {
			w.Events = []graph.IdentityEvent{{Seq: next, Kind: graph.EventMerge, Retired: one, Into: site}}
		},
		"change_entry": func(w *graph.ChunkWrite) {
			w.Entries = []graph.ChangeEntry{{Seq: next, Entity: "object", ID: "x", Change: graph.ChangeAdded}}
		},
		"ingest_order": func(w *graph.ChunkWrite) {
			w.Order = &graph.OrderEvent{IngestSeq: 99, Kind: graph.OrderClock, ChangeSeq: next}
		},
		"change_batch": func(*graph.ChunkWrite) {},
		"kg_head":      func(*graph.ChunkWrite) {},
	}
	for table, fill := range writes {
		w := chunk()
		fill(&w)
		damaged(t, admin, table, map[string]func() error{"chunk": func() error { return r.s.ApplyChunk(ctx, w) }})
	}
	if after, _ := r.s.Head(ctx, r.kg.ID); after.AppliedSeq != head.AppliedSeq {
		t.Fatalf("a failed chunk left the head at %d", after.AppliedSeq)
	}

	abandon := graph.AbandonWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: r.clock.Now(), Head: head}
	for _, table := range []string{"object_live", "object_hist", "link_live", "link_hist", "light_change", "change_entry", "change_batch",
		"object_identifier", "identity_event", "ingest_order", "kg_version_hash_part", "kg_version", "generation_event"} {
		ops := map[string]func() error{"abandon": func() error { return r.s.AbandonApply(ctx, abandon) },
			"restore": func() error {
				_, err := r.s.Restore(ctx, r.kg.ID, head.ReadableThrough, head, r.clock.Now())
				return err
			}}
		if table == "ingest_order" || table == "kg_version" || table == "kg_version_hash_part" || table == "generation_event" {
			delete(ops, "abandon")
		}
		damaged(t, admin, table, ops)
	}
	seal := graph.SealWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: r.clock.Now(), Head: head}
	damaged(t, admin, "kg_version", map[string]func() error{"seal": func() error { return r.s.Seal(ctx, seal) }})
	damaged(t, admin, "kg_apply_lease", map[string]func() error{"renew": func() error { _, err := r.s.RenewLease(ctx, r.lease, time.Hour, r.clock.Now()); return err },
		"release": func() error { return r.s.ReleaseLease(ctx, r.lease) }, "acquire": func() error {
			_, err := r.s.AcquireLease(ctx, graph.LeaseRequest{KG: r.kg.ID, Owner: "z", TTL: time.Second, Now: r.clock.Now()})
			return err
		}})
	damaged(t, admin, "kg", map[string]func() error{"get": func() error { _, err := r.s.GetKG(ctx, "x"); return err }, "list": func() error { _, err := r.s.ListKGs(ctx); return err }})
}
