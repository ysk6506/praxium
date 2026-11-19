package pgstore_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
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

// dmgDamaged is damaged for a table of the storage schema of the KG.
func dmgDamaged(t *testing.T, admin *sql.DB, schema, table string, ops map[string]func() error) {
	t.Helper()
	ctx := context.Background()
	if _, err := admin.ExecContext(ctx, `ALTER TABLE `+pgschema.Qualified(schema, table)+` RENAME TO `+pgschema.QuoteIdent(table+"_away")); err != nil {
		t.Fatalf("cannot damage %s: %v", table, err)
	}
	defer func() {
		if _, err := admin.ExecContext(ctx, `ALTER TABLE `+pgschema.Qualified(schema, table+"_away")+` RENAME TO `+pgschema.QuoteIdent(table)); err != nil {
			t.Fatalf("cannot repair %s: %v", table, err)
		}
	}()
	for name, op := range ops {
		if err := op(); err == nil {
			t.Errorf("%s with %s missing: no error", name, table)
		}
	}
}

// dmgWorld is a KG with a few changes in it, on a store of the default model, and the connection of an administrator.
type dmgWorld struct {
	*rig
	admin  *sql.DB
	schema string
}

func dmgWorldOf(t *testing.T) *dmgWorld {
	t.Helper()
	dsn := pgstore.NewTestDatabase(t)
	st, err := pgstore.Open(context.Background(), pgstore.Config{DSN: dsn, LeaseClock: pgstore.LeaseClockCaller})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	r := newRigOn(t, st, graph.Options{ChunkRows: 7})
	var no int64
	if err := admin.QueryRow(`SELECT kg_no FROM kg_catalog WHERE kg_id = $1`, r.kg.ID.String()).Scan(&no); err != nil {
		t.Fatal(err)
	}
	return &dmgWorld{rig: r, admin: admin, schema: pgschema.SchemaName(no)}
}

// [REQ-270]
func TestADamagedSchemaFailsEveryOperationInsteadOfAnsweringWrongly(t *testing.T) {
	ctx := context.Background()
	w := dmgWorldOf(t)
	r, admin := w.rig, w.admin
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
	siteType, _ := r.fx.Reg.ObjectTypeByName("Site")
	serverType, _ := r.fx.Reg.ObjectTypeByName("PhysicalServer")
	state := pgschema.StateTable(serverType.TypeNo)
	if _, ok := dmgHas(t, admin, w.schema, state); !ok {
		state = pgschema.TableObjectState
	}

	readObject := func() error { _, _, err := snap.Object(ctx, site); return err }
	readScan := func() error { _, err := snap.ScanObjects(ctx, graph.ObjectID{}, 5); return err }
	readVolatile := func() error { _, _, err := snap.Volatile(ctx, one, graph.AttrServerPower); return err }
	readApplied := func() error { _, err := r.s.AppliedChanges(ctx, r.kg.ID, 0, 5); return err }
	readVersions := func() error { _, err := r.s.Versions(ctx, r.kg.ID); return err }
	reads := []struct {
		table string
		ops   map[string]func() error
	}{
		{pgschema.TableObject, map[string]func() error{"object": readObject, "scan": readScan, "volatile": readVolatile}},
		{pgschema.TypeTable(siteType.TypeNo), map[string]func() error{"object": readObject, "scan": readScan}},
		{pgschema.TableLink, map[string]func() error{
			"link":    func() error { _, _, err := snap.Link(ctx, link); return err },
			"scan":    func() error { _, err := snap.ScanLinks(ctx, graph.LinkID{}, 5); return err },
			"linksof": func() error { _, err := snap.LinksOf(ctx, one, graph.Both); return err }}},
		{state, map[string]func() error{"volatile": readVolatile}},
		{pgschema.TableObjectIdentifier, map[string]func() error{
			"identifiers": func() error { _, err := snap.Identifiers(ctx, "s", "a"); return err },
			"of":          func() error { _, err := snap.(graph.IdentifierLister).IdentifiersOf(ctx, one); return err }}},
		{pgschema.TableIdentityEvent, map[string]func() error{"events": func() error { _, err := snap.IdentityEvents(ctx); return err }}},
		{pgschema.TableChangeBatch, map[string]func() error{"applied": readApplied}},
		{pgschema.TableChangeEntry, map[string]func() error{"applied": readApplied}},
		{pgschema.TableLightChange, map[string]func() error{"light": func() error { _, err := r.s.LightChanges(ctx, r.kg.ID, 0, 5); return err }}},
		{pgschema.TableIngestOrder, map[string]func() error{"order": func() error { _, err := r.s.OrderEvents(ctx, r.kg.ID, 0, 5); return err }}},
		{pgschema.TableVersion, map[string]func() error{"versions": readVersions}},
		{pgschema.TableVersionHashPart, map[string]func() error{"versions": readVersions}},
		{pgschema.TableGenerationEvent, map[string]func() error{"generations": func() error { _, err := r.s.Generations(ctx, r.kg.ID); return err }}},
		{pgschema.TableLease, map[string]func() error{"lease": func() error { _, _, err := r.s.Lease(ctx, r.kg.ID); return err }}},
		{pgschema.TableHead, map[string]func() error{"head": func() error { _, err := r.s.Head(ctx, r.kg.ID); return err }}},
	}
	for _, c := range reads {
		dmgDamaged(t, admin, w.schema, c.table, c.ops)
	}
	damaged(t, admin, "kg_catalog", map[string]func() error{"get": func() error { _, err := r.s.GetKG(ctx, "x"); return err }, "list": func() error { _, err := r.s.ListKGs(ctx); return err }})

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
		pgschema.TypeTable(rows[0].TypeNo): func(w *graph.ChunkWrite) { n := rows[0]; n.SeqFrom = next; w.Objects = []graph.ObjectChange{{New: &n}} },
		pgschema.TableObjectHist:           func(w *graph.ChunkWrite) { w.Objects = []graph.ObjectChange{{Old: &rows[0]}} },
		pgschema.TableLink:                 func(w *graph.ChunkWrite) { n := links[0]; n.SeqFrom = next; w.Links = []graph.LinkChange{{New: &n}} },
		pgschema.TableLinkHist:             func(w *graph.ChunkWrite) { w.Links = []graph.LinkChange{{Old: &links[0]}} },
		state: func(w *graph.ChunkWrite) {
			w.Volatile = []graph.VolatileRow{{Object: one, AttrNo: graph.AttrServerPower, Value: "on", Grade: model.GradeFact, Basis: model.BasisObserved, SeenAt: "2030-01-03T00:00:00Z"}}
		},
		pgschema.TableLightChange: func(w *graph.ChunkWrite) {
			w.Light = []graph.LightChange{{Seq: next, Object: one, AttrNo: graph.AttrServerPower}}
		},
		pgschema.TableObjectIdentifier: func(w *graph.ChunkWrite) {
			w.Identifiers = []graph.IdentifierChange{{Record: graph.IdentifierRecord{Object: one, Scheme: "s", Value: "v", SeqFrom: next}}}
		},
		pgschema.TableIdentityEvent: func(w *graph.ChunkWrite) {
			w.Events = []graph.IdentityEvent{{Seq: next, Kind: graph.EventMerge, Retired: one, Into: site}}
		},
		pgschema.TableChangeEntry: func(w *graph.ChunkWrite) {
			w.Objects = []graph.ObjectChange{{Old: &rows[0]}}
			w.Entries = []graph.ChangeEntry{{Seq: next, Entity: "object", ID: rows[0].ID.String(), Change: graph.ChangeRetired}}
		},
		pgschema.TableIngestOrder: func(w *graph.ChunkWrite) {
			w.Order = &graph.OrderEvent{IngestSeq: 99, Kind: graph.OrderClock, ChangeSeq: next}
		},
		pgschema.TableChangeBatch: func(*graph.ChunkWrite) {},
		pgschema.TableHead:        func(*graph.ChunkWrite) {},
	}
	for table, fill := range writes {
		w2 := chunk()
		fill(&w2)
		dmgDamaged(t, admin, w.schema, table, map[string]func() error{"chunk": func() error { return r.s.ApplyChunk(ctx, w2) }})
	}
	if after, _ := r.s.Head(ctx, r.kg.ID); after.AppliedSeq != head.AppliedSeq {
		t.Fatalf("a failed chunk left the head at %d", after.AppliedSeq)
	}
	dmgDamaged(t, admin, w.schema, pgschema.TableVersion, map[string]func() error{"seal": func() error {
		return r.s.Seal(ctx, graph.SealWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: r.clock.Now(), Head: head})
	}})
	dmgDamaged(t, admin, w.schema, pgschema.TableLease, map[string]func() error{"renew": func() error { _, err := r.s.RenewLease(ctx, r.lease, time.Hour, r.clock.Now()); return err },
		"release": func() error { return r.s.ReleaseLease(ctx, r.lease) }, "acquire": func() error {
			_, err := r.s.AcquireLease(ctx, graph.LeaseRequest{KG: r.kg.ID, Owner: "z", TTL: time.Second, Now: r.clock.Now()})
			return err
		}})

	// The abandon and the restore go back one change number at a time, so a table that is gone stops them in the
	// middle and leaves the work to be ended, never a state that looks whole. Each damage is tried in the order that
	// keeps the work as it was: the tables of a step first (a failed step is rolled back), the tables of the end last.
	// The work ends when the table is back.
	steps := []string{"object", "link", "object_hist", "link_hist", "change_batch", "change_entry", "light_change", "object_identifier", "identity_event"}
	for _, kind := range []string{"abandon", "restore"} {
		t.Run(kind, func(t *testing.T) {
			w := dmgWorldOf(t)
			dmgHalfDone(t, w)
			head, err := w.s.Head(w.ctx, w.kg.ID)
			if err != nil {
				t.Fatal(err)
			}
			run := func() error {
				if kind == "abandon" {
					return w.a.Abandon(w.ctx, w.kg.ID, w.lease)
				}
				_, err := w.s.Restore(w.ctx, w.kg.ID, 1, head, w.clock.Now())
				return err
			}
			tables := append([]string{"object_key"}, steps...)
			if kind == "restore" {
				tables = append([]string{"generation_event"}, append(steps, "ingest_order", "version_hash_part", "version")...)
			}
			for _, table := range tables {
				dmgDamaged(t, w.admin, w.schema, table, map[string]func() error{kind: run})
			}
			if err := w.s.ResumeWork(w.ctx, w.kg.ID); err != nil {
				t.Fatalf("the work is not ended after the repair: %v", err)
			}
			after, err := w.s.Head(w.ctx, w.kg.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := head.AppliedSeq
			if kind == "abandon" {
				want = head.ReadableThrough
			}
			if after.AppliedSeq != want {
				t.Fatalf("after the %s the head is at %d, wanted %d", kind, after.AppliedSeq, want)
			}
			if kg, err := w.s.GetKG(w.ctx, w.kg.ID.String()); err != nil || kg.Generation != map[string]int{"abandon": 1, "restore": 2}[kind] {
				t.Fatalf("after the %s the KG is %+v, %v", kind, kg, err)
			}
		})
	}
}

// dmgHas says whether the schema has the table.
func dmgHas(t *testing.T, admin *sql.DB, schema, table string) (string, bool) {
	t.Helper()
	var found sql.NullString
	if err := admin.QueryRow(`SELECT to_regclass($1)::text`, pgschema.Qualified(schema, table)).Scan(&found); err != nil {
		t.Fatal(err)
	}
	return found.String, found.Valid
}

// dmgHalfDone leaves a chunked apply half done on the world: its first chunks are in and the last is not.
func dmgHalfDone(t *testing.T, w *dmgWorld) {
	t.Helper()
	w.apply(w.fx.World(1, 1, 1))
	run, err := w.a.Begin(w.ctx, w.kg.ID, w.lease, w.fx.World(2, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3 && !run.Done(); i++ {
		if err := run.Next(w.ctx); err != nil {
			t.Fatal(err)
		}
	}
}
