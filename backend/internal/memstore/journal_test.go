package memstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// memJournal is a journal in memory: the records since the last snapshot, and the snapshot.
type memJournal struct {
	snap      []byte
	records   [][]byte
	closed    bool
	failAfter int // appends that work before one fails; -1 never
	failCkpt  bool
	failLoad  bool
	appended  int
}

func newMemJournal() *memJournal { return &memJournal{failAfter: -1} }

func (m *memJournal) Recover(load func(io.Reader) error, apply func([]byte) error) error {
	if m.snap != nil {
		if err := load(bytes.NewReader(m.snap)); err != nil {
			return err
		}
	}
	for _, r := range m.records {
		if err := apply(r); err != nil {
			return err
		}
	}
	return nil
}

func (m *memJournal) Append(rec []byte) (int64, error) {
	if m.failAfter >= 0 && m.appended >= m.failAfter {
		return 0, errors.New("disk full")
	}
	m.appended++
	m.records = append(m.records, append([]byte(nil), rec...))
	n := int64(0)
	for _, r := range m.records {
		n += int64(len(r))
	}
	return n, nil
}

func (m *memJournal) Checkpoint(write func(io.Writer) error) error {
	if m.failCkpt {
		return errors.New("cannot write the snapshot")
	}
	var b bytes.Buffer
	if err := write(&b); err != nil {
		return err
	}
	m.snap, m.records = b.Bytes(), nil
	return nil
}

func (m *memJournal) Close() error { m.closed = true; return nil }

// reopen opens a new store on what the journal holds.
func (m *memJournal) reopen(t *testing.T, opts Options) *Store {
	t.Helper()
	s, err := Open(&memJournal{snap: m.snap, records: m.records, failAfter: -1}, opts)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	return s
}

// jrig is a rig on a store with a journal.
type jrig struct {
	*rig
	j *memJournal
}

func newJRig(t *testing.T, opts Options, chunk int) *jrig {
	t.Helper()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	j := newMemJournal()
	s, err := Open(j, opts)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, ctx: context.Background(), s: s, fx: fx, clock: kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))}
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(11))
	if r.kg, err = graph.NewTestKG(r.ctx, r.s, fx.Reg, ids, r.clock, graph.KGSpec{Name: "main-kg", Role: graph.RoleProduction}); err != nil {
		t.Fatal(err)
	}
	if r.a, err = graph.NewApplier(r.s, fx.Reg, graph.NewTestKeyProvider("ms"), r.clock, ids, graph.Options{Semantic: 1, ChunkRows: chunk, VerifyOnSeal: true}); err != nil {
		t.Fatal(err)
	}
	if r.lease, err = r.a.Acquire(r.ctx, r.kg.ID); err != nil {
		t.Fatal(err)
	}
	return &jrig{rig: r, j: j}
}

// play does every kind of write the store takes.
func (r *jrig) play() {
	r.t.Helper()
	one, two := r.fx.ServerID("SN-000000"), r.fx.ServerID("SN-000001")
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00")
	scheme, _ := r.fx.Reg.IdentifierSchemeByName("redfish_system_id")
	r.apply(r.fx.World(1, 1, 2))
	if _, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealSync); err != nil {
		r.t.Fatal(err)
	}
	r.apply(graph.Batch{
		Objects:     []graph.ObjectUpsert{r.fx.Site("S00", "Elsewhere")},
		Links:       []graph.LinkUpsert{r.fx.MountedIn(one, rack, 9)},
		Volatile:    []graph.VolatileWrite{r.fx.Power(one, "off", "2030-01-02T00:00:00Z")},
		Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a"}},
	})
	r.apply(graph.Batch{RetireObjects: []graph.ObjectID{two}, RetireLinks: []graph.LinkID{r.fx.MountedInID(two, rack)},
		Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a", Close: true}}})
	run, err := r.a.Begin(r.ctx, r.kg.ID, r.lease, r.fx.World(1, 2, 3))
	if err != nil {
		r.t.Fatal(err)
	}
	if err := run.Next(r.ctx); err != nil {
		r.t.Fatal(err)
	}
	if err := r.a.Abandon(r.ctx, r.kg.ID, r.lease); err != nil {
		r.t.Fatal(err)
	}
	r.apply(r.fx.World(2, 1, 1))
	if _, err := r.a.Restore(r.ctx, r.kg.ID, 2); err != nil {
		r.t.Fatal(err)
	}
	r.apply(r.fx.World(1, 1, 3))
	if _, err := r.a.Seal(r.ctx, r.kg.ID, r.lease, graph.SealMerge); err != nil {
		r.t.Fatal(err)
	}
}

// look writes down what a reader sees of a store at every change number.
func look(t *testing.T, s *Store) string {
	t.Helper()
	ctx := context.Background()
	var b strings.Builder
	kgs, _ := s.ListKGs(ctx)
	for _, kg := range kgs {
		h, _ := s.Head(ctx, kg.ID)
		roll := fmt.Sprint(h.Rollback)
		if h.Rollback != nil {
			roll = fmt.Sprintf("%+v", *h.Rollback)
		}
		h.Rollback = nil
		vs, _ := s.Versions(ctx, kg.ID)
		gens, _ := s.Generations(ctx, kg.ID)
		ev, _ := s.OrderEvents(ctx, kg.ID, 0, 1<<20)
		ch, _ := s.AppliedChanges(ctx, kg.ID, 0, 1<<20)
		lc, _ := s.LightChanges(ctx, kg.ID, 0, 1<<20)
		fmt.Fprintf(&b, "%+v\n%+v %s\n%+v\n%+v\n%+v\n%+v\n%+v\n", kg, h, roll, vs, gens, ev, ch, lc)
		for seq := int64(0); seq <= h.AppliedSeq; seq++ {
			sn, err := s.Snapshot(ctx, graph.ReadPoint{KG: kg.ID, Kind: graph.ReadApplied, Seq: seq})
			if err != nil {
				t.Fatal(err)
			}
			if seq <= h.ReadableThrough {
				sn, _ = s.Snapshot(ctx, graph.ReadPoint{KG: kg.ID, Kind: graph.ReadSeq, Seq: seq})
			}
			objs, _ := sn.ScanObjects(ctx, graph.ObjectID{}, 1<<20)
			links, _ := sn.ScanLinks(ctx, graph.LinkID{}, 1<<20)
			fmt.Fprintf(&b, "%d %+v %+v\n", seq, objs, links)
			for _, o := range objs {
				v, ok, err := sn.Volatile(ctx, o.ID, graph.AttrServerPower)
				ids, _ := sn.(graph.IdentifierLister).IdentifiersOf(ctx, o.ID)
				ls, _ := sn.LinksOf(ctx, o.ID, graph.Both)
				fmt.Fprintf(&b, "%+v %v %v %+v %+v\n", v, ok, err, ids, ls)
			}
			hist, _ := sn.IdentifierHistory(ctx, "redfish_system_id", "sys-1")
			evs, _ := sn.IdentityEvents(ctx)
			fmt.Fprintf(&b, "%+v %+v\n", hist, evs)
		}
	}
	return b.String()
}

// A store with a journal that is opened again on what the journal holds is the same store, whether it is read from
// the log, from a snapshot taken at the end, or from a snapshot and the log after it.
// [REQ-271]
func TestAJournaledStoreComesBackAsItWas(t *testing.T) {
	for _, limit := range []int64{0, 1, 4000} {
		r := newJRig(t, Options{CheckpointBytes: limit}, 4)
		r.play()
		want := look(t, r.s)
		if got := look(t, r.j.reopen(t, Options{})); got != want {
			t.Fatalf("limit %d: the store read back from the journal differs", limit)
		}
		if err := r.s.Close(); err != nil {
			t.Fatal(err)
		}
		if got := look(t, r.j.reopen(t, Options{})); got != want {
			t.Fatalf("limit %d: the store read back after the closing snapshot differs", limit)
		}
	}
}

// A recovered store goes on: the lease, the applier and the hash all work on what came back.
// [REQ-271]
func TestARecoveredStoreTakesMoreWork(t *testing.T) {
	r := newJRig(t, Options{CheckpointBytes: 1}, 4)
	r.play()
	s := r.j.reopen(t, Options{})
	a, err := graph.NewApplier(s, r.fx.Reg, graph.NewTestKeyProvider("ms"), r.clock, kernel.NewRandomIDs(kernel.NewSeededRng(5)), graph.Options{Semantic: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(r.ctx, r.kg.ID); err != nil {
		t.Fatal(err)
	}
	lease, err := a.Acquire(r.ctx, r.kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Attempt <= 1 {
		t.Fatalf("attempt %d: attempt numbers must keep rising after a restart", lease.Attempt)
	}
	if _, err := a.Apply(r.ctx, r.kg.ID, lease, r.fx.World(3, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(r.ctx, r.kg.ID); err != nil {
		t.Fatal(err)
	}
}

// A journal that cannot take a record leaves the store as it was and shuts it to writes; reads go on.
// [REQ-272]
func TestAFailedAppendLeavesTheStoreAsItWasAndShutsItToWrites(t *testing.T) {
	r := newJRig(t, Options{}, 0)
	r.apply(r.fx.World(1, 1, 1))
	before := look(t, r.s)
	r.j.failAfter = r.j.appended
	_, err := r.a.Apply(r.ctx, r.kg.ID, r.lease, r.fx.World(1, 1, 3))
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("apply on a full disk: %v", err)
	}
	if look(t, r.s) != before {
		t.Fatalf("a write that was not made durable changed the store")
	}
	r.j.failAfter = -1
	if _, err := r.a.Apply(r.ctx, r.kg.ID, r.lease, r.fx.World(1, 1, 3)); err == nil || !strings.Contains(err.Error(), "no more writes") {
		t.Fatalf("a store whose journal failed took another write: %v", err)
	}
	if _, err := r.s.CreateKG(r.ctx, graph.KG{ID: r.kg.ID, Name: "other", Role: graph.RoleTest}); err == nil {
		t.Fatalf("a write got through")
	}
	if look(t, r.s) != before {
		t.Fatalf("reads changed")
	}
}

// A snapshot that cannot be written does not fail the write that asked for it; the error is kept to be seen.
// [REQ-272]
func TestAFailedSnapshotLeavesTheWritesWorking(t *testing.T) {
	r := newJRig(t, Options{CheckpointBytes: 1}, 0)
	r.j.failCkpt = true
	r.apply(r.fx.World(1, 1, 1))
	r.apply(r.fx.World(1, 1, 2))
	if r.s.CheckpointError() == nil {
		t.Fatalf("the failed snapshot was not kept")
	}
	if err := r.s.Checkpoint(); err == nil {
		t.Fatalf("an explicit snapshot that fails must say so")
	}
	r.j.failCkpt = false
	if err := r.s.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := r.s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.s.Close(); err != nil {
		t.Fatalf("a second close: %v", err)
	}
	if !r.j.closed {
		t.Fatalf("the journal was not closed")
	}
	if _, err := r.a.Apply(r.ctx, r.kg.ID, r.lease, r.fx.World(1, 1, 3)); err == nil {
		t.Fatalf("a closed store took a write")
	}
}

// A store without a journal does nothing at the new calls, and an Open that fails closes the journal.
// [REQ-271]
func TestAStoreWithoutAJournalIsUnchanged(t *testing.T) {
	s := New()
	if s.Close() != nil || s.Checkpoint() != nil || s.CheckpointError() != nil {
		t.Fatalf("a store without a journal reacted to a journal call")
	}
	bad := &memJournal{failAfter: -1, records: [][]byte{{9, 9}}}
	if _, err := Open(bad, Options{}); kernel.CodeOf(err) != kernel.CodeUnsupported || !bad.closed {
		t.Fatalf("open on a record of an unknown version: %v, closed %v", err, bad.closed)
	}
}

// Records and snapshots that cannot be read are refused with an error, never applied halfway.
// [REQ-273]
func TestRecordsAndSnapshotsThatMakeNoSenseAreRefused(t *testing.T) {
	good := newJRig(t, Options{}, 0)
	good.apply(good.fx.World(1, 1, 1))
	if _, err := good.a.Seal(good.ctx, good.kg.ID, good.lease, graph.SealSync); err != nil {
		t.Fatal(err)
	}
	recs := good.j.records
	cases := map[string][][]byte{
		"a record of another version":    {{recordVersion + 1, kindChunk}},
		"a record of a kind not known":   {{recordVersion, 99}},
		"a record that is not gob":       {{recordVersion, kindChunk, 1, 2, 3}},
		"an abandon that is not gob":     {{recordVersion, kindAbandon, 1, 2, 3}},
		"a seal that is not gob":         {{recordVersion, kindSeal, 1, 2, 3}},
		"a restore that is not gob":      {{recordVersion, kindRestore, 1, 2, 3}},
		"a create that is not gob":       {{recordVersion, kindCreateKG, 1, 2, 3}},
		"a chunk of a KG that is absent": {recs[1]},
		"a seal of a KG that is absent":  {sealRecord(t, good, "kg-absent")},
		"an abandon of a KG not there":   {abandonRecord(t, "kg-absent")},
		"a restore of a KG not there":    {restoreRecord(t, "kg-absent")},
		"a record that is cut short":     {{recordVersion}},
		"a create with a bad id":         {createRecord(t, "bad id!")},
		"an abandon with a bad id":       {abandonRecord(t, "bad id!")},
		"a restore with a bad id":        {restoreRecord(t, "bad id!")},
		"a seal with a bad id":           {sealRecord(t, good, "bad id!")},
		"the same KG created twice":      {recs[0], recs[0]},
		"a chunk that skips a number":    {recs[0], recs[2]},
		"a seal that does not follow":    {recs[0], recs[len(recs)-1]},
	}
	for name, in := range cases {
		if _, err := Open(&memJournal{records: in, failAfter: -1}, Options{}); err == nil {
			t.Errorf("%s: the store opened", name)
		}
	}
	for name, snap := range map[string][]byte{
		"a snapshot that is not gob":    {1, 2, 3},
		"an empty snapshot":             {},
		"a snapshot of another format":  snapBytes(t, snapPart{Kind: partHeader, Header: &snapHeader{Format: 7}}),
		"a snapshot without its header": snapBytes(t, snapPart{Kind: partEnd}),
		"a snapshot cut before its end": snapBytes(t, snapPart{Kind: partHeader, Header: &snapHeader{Format: snapFormat}}),
		"a part outside a KG":           snapBytes(t, snapPart{Kind: partHeader, Header: &snapHeader{Format: snapFormat}}, snapPart{Kind: partLight, Light: []wLight{{Seq: 1}}}),
		"a KG with a bad id":            snapBytes(t, snapPart{Kind: partHeader, Header: &snapHeader{Format: snapFormat}}, snapPart{Kind: partKG, KG: &snapKG{KG: wKG{ID: "bad id!"}}}),
		"an object with a bad id":       snapWithPart(t, snapPart{Kind: partObjects, Objects: []wObject{{ID: "bad id!"}}}),
		"an object with a bad value":    snapWithPart(t, snapPart{Kind: partObjects, Objects: []wObject{{ID: "obj_1", Attrs: map[int]wAttr{1: {Value: []byte{99}}}}}}),
		"a link with a bad id":          snapWithPart(t, snapPart{Kind: partLinks, Links: []wLink{{ID: "bad id!"}}}),
		"a link with a bad value":       snapWithPart(t, snapPart{Kind: partLinks, Links: []wLink{{ID: "lnk_1", Attrs: map[int]wAttr{1: {Value: []byte{99}}}}}}),
		"a volatile row with bad value": snapWithPart(t, snapPart{Kind: partVolatile, Volatile: []wVolatile{{Object: "obj_1", Value: []byte{99}}}}),
		"a volatile row with a bad id":  snapWithPart(t, snapPart{Kind: partVolatile, Volatile: []wVolatile{{Object: "bad id!", Value: []byte{0}}}}),
		"a version with a bad id":       snapWithPart(t, snapPart{Kind: partVersions, Versions: []wVersion{{ID: "bad id!"}}}),
		"an identifier with a bad id":   snapWithPart(t, snapPart{Kind: partIdents, Idents: []wIdentRec{{Object: "bad id!"}}}),
		"an event with a bad id":        snapWithPart(t, snapPart{Kind: partEvents, Events: []wEvent{{Retired: "bad id!"}}}),
		"a light change with a bad id":  snapWithPart(t, snapPart{Kind: partLight, Light: []wLight{{Object: "bad id!"}}}),
	} {
		if _, err := Open(&memJournal{snap: snap, failAfter: -1}, Options{}); err == nil {
			t.Errorf("%s: the store opened", name)
		}
	}
}
