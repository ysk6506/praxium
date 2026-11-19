package pgstore

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the writes of the new storage that only it has: the registry, the abandon and the restore in steps,
// the refusals, and what a write touches. Their names start with v2w.

type v2wRig struct {
	*v2rWorld
	ctx   context.Context
	fx    *graph.Fixture
	clock *kernel.ManualClock
	app   *graph.Applier
	lease graph.Lease
}

func v2wRigOf(t *testing.T, opts graph.Options) *v2wRig {
	t.Helper()
	w := v2rWorldWith(t, LeaseClockCaller)
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	r := &v2wRig{v2rWorld: w, ctx: context.Background(), fx: fx, clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))}
	opts.Semantic = 1
	if r.app, err = graph.NewApplier(w.st, fx.Reg, graph.NewTestKeyProvider("v2w"), r.clock, kernel.NewRandomIDs(kernel.NewSeededRng(11)), opts); err != nil {
		t.Fatal(err)
	}
	if r.lease, err = r.app.Acquire(r.ctx, w.kg.ID); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *v2wRig) v2wApply(t *testing.T, b graph.Batch) graph.Result {
	t.Helper()
	res, err := r.app.Apply(r.ctx, r.kg.ID, r.lease, b)
	noErr(t, "apply", err)
	return res
}

func (r *v2wRig) v2wCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := r.admin.QueryRowContext(r.ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (r *v2wRig) v2wTable(name string) string { return r.a.t(name) }

func TestV2wARetiredObjectComesBackWithTheSameNumberAndTheKeyStaysBound(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	srv := r.fx.ServerID("SN-000000")
	r.v2wApply(t, r.fx.World(1, 1, 2))
	var no, firstSeq int64
	var firstKey sql.NullString
	row := r.admin.QueryRow(`SELECT obj_no, seq_from, first_key::text FROM `+r.v2wTable("object")+` WHERE ref = $1`, srv.String())
	noErr(t, "registry", func() error { return row.Scan(&no, &firstSeq, &firstKey) }())
	if !firstKey.Valid {
		t.Fatal("a new object has no first key")
	}
	bound := r.v2wCount(t, `SELECT bound_seq FROM `+r.v2wTable("object_key")+` WHERE obj_no = $1`, no)
	if int64(bound) != firstSeq {
		t.Fatalf("the key was bound at %d, the object began at %d", bound, firstSeq)
	}
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00")
	r.v2wApply(t, graph.Batch{RetireObjects: []graph.ObjectID{srv}, RetireLinks: []graph.LinkID{r.fx.MountedInID(srv, rack)}})
	if retired := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("object")+` WHERE obj_no = $1 AND retired_seq IS NOT NULL`, no); retired != 1 {
		t.Fatal("a retired object is not marked retired in the registry")
	}
	r.v2wApply(t, graph.Batch{Objects: []graph.ObjectUpsert{r.fx.Server("SN-000000", "M2")}, Links: []graph.LinkUpsert{r.fx.MountedIn(srv, rack, 1)}})
	var again int64
	var retired sql.NullInt64
	noErr(t, "registry again", r.admin.QueryRow(`SELECT obj_no, retired_seq FROM `+r.v2wTable("object")+` WHERE ref = $1`, srv.String()).Scan(&again, &retired))
	if again != no || retired.Valid {
		t.Fatalf("the object came back as number %d (was %d), retired %v", again, no, retired)
	}
	if n := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("object_key")+` WHERE obj_no = $1`, no); n != 1 {
		t.Fatalf("the key of the object is bound %d times", n)
	}
	got, ok, err := r.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest}).Object(r.ctx, srv)
	if err != nil || !ok || got.Attrs[graph.AttrServerModel].Value != "M2" {
		t.Fatalf("the object that came back: %+v %v %v", got, ok, err)
	}
	if n := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("object")+` WHERE ref = $1`, srv.String()); n != 1 {
		t.Fatalf("%d registry rows for one ref", n)
	}
}

func TestV2wARefThatCameBackWithAnotherTypeIsRefused(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	srv := r.fx.ServerID("SN-000000")
	r.v2wApply(t, r.fx.World(1, 1, 1))
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00")
	r.v2wApply(t, graph.Batch{RetireObjects: []graph.ObjectID{srv}, RetireLinks: []graph.LinkID{r.fx.MountedInID(srv, rack)}})
	head, err := r.st.Head(r.ctx, r.kg.ID)
	noErr(t, "head", err)
	next := head.Clone()
	next.AppliedSeq, next.ReadableThrough = head.AppliedSeq+1, head.ReadableThrough+1
	row := graph.ObjectRow{ID: srv, TypeNo: r.siteNo, Name: "x", MinGrade: "fact", SeqFrom: next.AppliedSeq}
	err = r.st.ApplyChunk(r.ctx, graph.ChunkWrite{KG: r.kg.ID, Generation: 1, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: r.clock.Now(),
		Batch:   &graph.ChangeBatch{Seq: next.AppliedSeq, Kind: graph.BatchSync, ApplyID: "x", ChunkNo: 0, ChunkCount: 1, AppliedAt: r.clock.Now()},
		Objects: []graph.ObjectChange{{New: &row}}, Head: next})
	wantCode(t, "another type", err, graph.CodeHeadMoved)
	if after, _ := r.st.Head(r.ctx, r.kg.ID); after.AppliedSeq != head.AppliedSeq {
		t.Fatal("a refused chunk moved the head")
	}
}

// v2wState is what a KG holds in the tables that an abandon or a restore changes, in a form that can be compared.
func v2wState(t *testing.T, r *v2wRig, withKeys bool) string {
	t.Helper()
	var out string
	for i, q := range []string{
		`SELECT coalesce(string_agg(ref || ':' || coalesce(retired_seq::text, '-') || ':' || seq_from, ',' ORDER BY ref), '') FROM ` + r.v2wTable("object") + ` WHERE retired_seq IS NULL`,
		`SELECT coalesce(string_agg(ref || ':' || seq_from, ',' ORDER BY ref, seq_from), '') FROM ` + r.v2wTable("link"),
		`SELECT coalesce(string_agg(ref || ':' || seq_from || ':' || seq_to, ',' ORDER BY ref, seq_from), '') FROM ` + r.v2wTable("link_hist"),
		`SELECT coalesce(string_agg(obj_no || ':' || bound_seq, ',' ORDER BY obj_no), '') FROM ` + r.v2wTable("object_key"),
		`SELECT coalesce(string_agg(change_seq::text, ',' ORDER BY change_seq), '') FROM ` + r.v2wTable("change_batch"),
		`SELECT count(*)::text FROM ` + r.v2wTable("change_entry"),
		`SELECT count(*)::text FROM ` + r.v2wTable("object_hist"),
	} {
		if i == 3 && !withKeys {
			continue
		}
		var s string
		noErr(t, "state", r.admin.QueryRow(q).Scan(&s))
		out += s + "|"
	}
	for _, typ := range []int{r.siteNo, r.serverNo} {
		var s string
		noErr(t, "rows", r.admin.QueryRow(`SELECT coalesce(string_agg(ref || ':' || seq_from, ',' ORDER BY ref), '') FROM `+r.a.typeTable(typ)).Scan(&s))
		out += s + "|"
	}
	return out
}

// v2wHalfDone leaves a chunked apply half done: the first chunks are in, the last is not.
func v2wHalfDone(t *testing.T, r *v2wRig) {
	t.Helper()
	run, err := r.app.Begin(r.ctx, r.kg.ID, r.lease, r.fx.World(2, 2, 3))
	noErr(t, "begin", err)
	for i := 0; i < 3 && !run.Done(); i++ {
		noErr(t, "chunk", run.Next(r.ctx))
	}
}

func TestV2wAbandonTakesTheChunksOutOneNumberAtATimeAndLeavesNoTraceAndAnInterruptedOneIsEnded(t *testing.T) {
	r := v2wRigOf(t, graph.Options{ChunkRows: 7})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	before := v2wState(t, r, true)
	keys := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("object_key"))
	v2wHalfDone(t, r)
	if v2wState(t, r, true) == before {
		t.Fatal("the half done apply wrote nothing")
	}
	var steps []int64
	r.st.hook = func(stage string, seq int64) error {
		steps = append(steps, seq)
		if len(steps) == 2 {
			return kernel.Errorf(kernel.CodeInternal, "the server died")
		}
		return nil
	}
	err := r.app.Abandon(r.ctx, r.kg.ID, r.lease)
	wantCode(t, "an abandon that is cut", err, kernel.CodeInternal)
	r.st.hook = nil
	var work string
	noErr(t, "work", r.admin.QueryRow(`SELECT work FROM `+r.v2wTable("head")).Scan(&work))
	if work != workAbandoning {
		t.Fatalf("the head says %q after the cut", work)
	}
	err = r.app.Abandon(r.ctx, r.kg.ID, r.lease)
	if err != nil {
		t.Fatalf("the abandon is not ended when it is called again: %v", err)
	}
	if got := v2wState(t, r, true); got != before {
		t.Fatalf("the state after the abandon differs:\n%s\n%s", got, before)
	}
	if n := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("object_key")); n != keys {
		t.Fatalf("%d keys after the abandon, %d before", n, keys)
	}
	if len(steps) != 2 {
		t.Fatalf("the hook was called %d times", len(steps))
	}
	noErr(t, "resume of nothing", r.st.ResumeWork(r.ctx, r.kg.ID))
	again := v2wRigOf(t, graph.Options{ChunkRows: 7})
	again.v2wApply(t, again.fx.World(1, 1, 1))
	v2wHalfDone(t, again)
	noErr(t, "abandon without a cut", again.app.Abandon(again.ctx, again.kg.ID, again.lease))
	if v2wState(t, again, true) != before {
		t.Fatal("an abandon that was not cut ends in another state")
	}
}

func TestV2wRestoreClosesTheKGUntilItEndsAndAnInterruptedOneIsResumed(t *testing.T) {
	r := v2wRigOf(t, graph.Options{ChunkRows: 1000})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	mid := v2wState(t, r, false)
	keysMid := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("object_key"))
	r.v2wApply(t, r.fx.World(1, 2, 2))
	r.v2wApply(t, graph.Batch{Objects: []graph.ObjectUpsert{r.fx.Server("SN-000000", "M9")}})
	sn := r.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest})
	cur, err := r.st.Head(r.ctx, r.kg.ID)
	noErr(t, "head", err)
	r.st.hook = func(stage string, seq int64) error {
		_, snapErr := r.st.Snapshot(r.ctx, graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest})
		wantCode(t, "a read while the KG is restored", snapErr, CodeKGRestoring)
		wantCode(t, "a write while the KG is restored", r.st.ApplyChunk(r.ctx, graph.ChunkWrite{KG: r.kg.ID, Generation: 1, Lease: r.lease}), CodeKGRestoring)
		_, _, objErr := sn.Object(r.ctx, r.srv)
		wantCode(t, "a read of an old snapshot", objErr, CodeKGRestoring)
		return kernel.Errorf(kernel.CodeInternal, "the server died")
	}
	_, err = r.app.Restore(r.ctx, r.kg.ID, 1)
	wantCode(t, "a restore that is cut", err, kernel.CodeInternal)
	r.st.hook = nil
	var gen int
	noErr(t, "generation", r.admin.QueryRow(`SELECT generation FROM `+r.v2wTable("head")).Scan(&gen))
	if gen != 2 {
		t.Fatalf("the generation is %d after the first transaction", gen)
	}
	noErr(t, "resume", r.st.ResumeWork(r.ctx, r.kg.ID))
	got, err := r.st.Head(r.ctx, r.kg.ID)
	if err != nil || got.AppliedSeq != 1 || got.ReadableThrough != 1 || cur.AppliedSeq <= 1 {
		t.Fatalf("head after the restore %+v %v (was %+v)", got, err, cur)
	}
	if state := v2wState(t, r, false); state != mid {
		t.Fatalf("the restored state differs:\n%s\n%s", state, mid)
	}
	if n := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("object_key")); n < keysMid {
		t.Fatalf("the restore let go of keys: %d, %d before", n, keysMid)
	}
	if n := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("generation_event")+` WHERE generation = 2 AND restored_to_seq = 1`); n != 1 {
		t.Fatal("the restore is not kept as an event")
	}
	_, err = r.st.Restore(r.ctx, r.kg.ID, 5, got, r.clock.Now())
	wantCode(t, "a restore to a number that is not readable", err, graph.CodeNotReadable)
	v2rExec(t, r.admin, `UPDATE `+r.v2wTable("head")+` SET work = 'abandoning'`)
	_, err = r.st.Restore(r.ctx, r.kg.ID, 0, got, r.clock.Now())
	wantCode(t, "a restore while an abandon is half done", err, graph.CodeApplyInProgress)
}

func TestV2wAWriteIsRefusedWhenTheSchemaWasMadeFromAnotherModel(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	r.v2wApply(t, r.fx.World(1, 1, 1))
	std, err := StandardModels(false)
	noErr(t, "models", err)
	other, err := OpenV2(r.ctx, Config{DSN: r.dsn}, V2Options{Models: func(h kernel.Hash) (SchemaModel, error) {
		m, err := std(h)
		m.Search = pgschema.NewSearchSet()
		return m, err
	}})
	noErr(t, "open", err)
	defer other.Close()
	head, _ := r.st.Head(r.ctx, r.kg.ID)
	ops := v2dOps{
		"chunk": func() error {
			return other.ApplyChunk(r.ctx, graph.ChunkWrite{KG: r.kg.ID, Generation: 1, Lease: r.lease})
		},
		"abandon": func() error {
			return other.AbandonApply(r.ctx, graph.AbandonWrite{KG: r.kg.ID, Generation: 1, Lease: r.lease})
		},
		"seal":    func() error { return other.Seal(r.ctx, graph.SealWrite{KG: r.kg.ID, Generation: 1, Lease: r.lease}) },
		"restore": func() error { _, err := other.Restore(r.ctx, r.kg.ID, 0, head, r.clock.Now()); return err },
		"applied": func() error { _, err := other.AppliedChanges(r.ctx, r.kg.ID, 0, 5); return err },
		"light":   func() error { _, err := other.LightChanges(r.ctx, r.kg.ID, 0, 5); return err },
		"order":   func() error { _, err := other.OrderEvents(r.ctx, r.kg.ID, 0, 5); return err },
		"wait":    func() error { _, err := other.WaitReadable(r.ctx, r.kg.ID, 1, time.Second); return err },
	}
	names := make([]string, 0, len(ops))
	for n := range ops {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		wantCode(t, n, ops[n](), CodeSchemaMismatch)
	}
}

func TestV2wAChangeOfValuesAloneDoesNotTouchTheRowsOfTheObjectAndTheRowsAreWrittenInOrder(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	r.v2wApply(t, r.fx.World(2, 2, 3))
	srv := r.fx.ServerID("SN-000000")
	xmins := func() string {
		var s string
		noErr(t, "xmin", r.admin.QueryRow(`SELECT string_agg(xmin::text, ',' ORDER BY obj_no) FROM `+r.a.typeTable(r.serverNo)).Scan(&s))
		return s
	}
	before := xmins()
	r.v2wApply(t, graph.Batch{Volatile: []graph.VolatileWrite{r.fx.Power(srv, "off", "2031-01-01T00:00:00Z")}})
	if after := xmins(); after != before {
		t.Fatalf("a change of values rewrote rows of the type:\n%s\n%s", before, after)
	}
	vol, ok, err := r.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest}).Volatile(r.ctx, srv, graph.AttrServerPower)
	if err != nil || !ok || vol.Value != "off" {
		t.Fatalf("the value: %+v %v %v", vol, ok, err)
	}
	if n := r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable("light_change")); n == 0 {
		t.Fatal("no light change line for a change of values")
	}
	for _, typ := range []int{r.siteNo, r.serverNo} {
		var inOrder bool
		noErr(t, "order", r.admin.QueryRow(`SELECT bool_and(obj_no >= prev) FROM (SELECT obj_no, coalesce(lag(obj_no) OVER (ORDER BY ctid), 0) AS prev FROM `+r.a.typeTable(typ)+`) x`).Scan(&inOrder))
		if !inOrder {
			t.Fatalf("the rows of type %d were not written in the order of their numbers", typ)
		}
	}
	var linksInOrder bool
	noErr(t, "links", r.admin.QueryRow(`SELECT bool_and(src >= prev) FROM (SELECT src, coalesce(lag(src) OVER (ORDER BY ctid), 0) AS prev FROM `+r.v2wTable("link")+`) x`).Scan(&linksInOrder))
	if !linksInOrder {
		t.Fatal("the links were not written in the order of their src")
	}
}

func TestV2wTheSettingsOfTheTablesAreInForce(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	want := map[string]string{
		pgschema.TableHead:             "fillfactor=50",
		pgschema.TableLease:            "fillfactor=50",
		pgschema.TableObjectState:      "fillfactor=70",
		pgschema.TypeTable(r.serverNo): "fillfactor=85",
	}
	for table, option := range want {
		var got sql.NullString
		err := r.admin.QueryRow(`SELECT array_to_string(c.reloptions, ',') FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = $2`,
			r.a.rec.Schema(), table).Scan(&got)
		noErr(t, "options", err)
		if !strings.Contains(got.String, option) {
			t.Errorf("%s has the options %q, wanted %s", table, got.String, option)
		}
	}
	for _, table := range []string{pgschema.TableObjectHist, pgschema.TableLinkHist} {
		var key string
		noErr(t, "key", r.admin.QueryRow(`SELECT pg_get_indexdef(i.indexrelid) FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND i.indisprimary`, r.a.rec.Schema(), table).Scan(&key))
		if !strings.Contains(key, "(seq_to,") {
			t.Errorf("the key of %s does not start with the change number: %s", table, key)
		}
	}
}

func TestV2wBulkReadsGiveWhatReadsOneAtATimeGive(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	r.v2wApply(t, r.fx.World(2, 2, 2))
	sn := r.v2rSnap(t, graph.ReadPoint{Kind: graph.ReadLatest})
	bulk := sn.(graph.BulkReader)
	objs, err := sn.ScanObjects(r.ctx, graph.ObjectID{}, 1000)
	noErr(t, "scan", err)
	var ids []graph.ObjectID
	for _, o := range objs {
		ids = append(ids, o.ID)
	}
	got, err := bulk.ObjectsByID(r.ctx, ids)
	noErr(t, "bulk objects", err)
	for _, o := range objs {
		one, ok, err := sn.Object(r.ctx, o.ID)
		if err != nil || !ok || one.ContentHash != got[o.ID].ContentHash || one.Name != got[o.ID].Name {
			t.Fatalf("object %s: %v %v", o.ID, ok, err)
		}
	}
	for _, dir := range []graph.Direction{graph.Out, graph.In, graph.Both} {
		many, err := bulk.LinksOfMany(r.ctx, ids, dir)
		noErr(t, "bulk links", err)
		for _, id := range ids {
			one, err := sn.LinksOf(r.ctx, id, dir)
			noErr(t, "links of", err)
			if len(one) != len(many[id]) {
				t.Fatalf("%s %d: %d links one at a time, %d in bulk", id, dir, len(one), len(many[id]))
			}
			for i := range one {
				if one[i].ID != many[id][i].ID {
					t.Fatalf("the links of %s differ in order", id)
				}
			}
		}
	}
}
