package filejournal

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
)

// countingJournal counts the records the journal answered, which is what "answered" means for the promise.
type countingJournal struct {
	*Journal
	acked int
}

func (c *countingJournal) Append(rec []byte) (int64, error) {
	n, err := c.Journal.Append(rec)
	if err == nil {
		c.acked++
	}
	return n, err
}

var _ memstore.Journal = (*countingJournal)(nil)

// openStore opens the store of a folder.
func openStore(d Disk, ckpt int64) (*memstore.Store, *countingJournal, error) {
	j, err := Open(d)
	if err != nil {
		return nil, nil, err
	}
	cj := &countingJournal{Journal: j}
	s, err := memstore.Open(cj, memstore.Options{CheckpointBytes: ckpt})
	return s, cj, err
}

// observed is a store that notes the whole state after every write that changed it.
type observed struct {
	*memstore.Store
	after func()
}

func (o observed) done(err error) error {
	if err == nil && o.after != nil {
		o.after()
	}
	return err
}

func (o observed) CreateKG(ctx context.Context, kg graph.KG) (graph.KG, error) {
	out, err := o.Store.CreateKG(ctx, kg)
	return out, o.done(err)
}

func (o observed) ApplyChunk(ctx context.Context, w graph.ChunkWrite) error {
	return o.done(o.Store.ApplyChunk(ctx, w))
}

func (o observed) AbandonApply(ctx context.Context, w graph.AbandonWrite) error {
	return o.done(o.Store.AbandonApply(ctx, w))
}

func (o observed) Seal(ctx context.Context, w graph.SealWrite) error {
	return o.done(o.Store.Seal(ctx, w))
}

func (o observed) Restore(ctx context.Context, id graph.KGID, to int64, h graph.Head, now time.Time) (graph.KG, error) {
	out, err := o.Store.Restore(ctx, id, to, h, now)
	return out, o.done(err)
}

// fingerprint writes down everything a reader can see of the store: every KG, its head, its versions, every state at
// every change number, the feeds, the identifiers and the volatile values. Two stores with the same fingerprint
// answer every read the same.
func fingerprint(ctx context.Context, s graph.Store) string { return fingerprintOf(ctx, s, false) }

// fingerprintLite is the fingerprint of the latest and the applied states only: much quicker, for the tests that
// compare thousands of stores.
func fingerprintLite(ctx context.Context, s graph.Store) string { return fingerprintOf(ctx, s, true) }

func fingerprintOf(ctx context.Context, s graph.Store, lite bool) string {
	var b strings.Builder
	kgs, _ := s.ListKGs(ctx)
	for _, kg := range kgs {
		fmt.Fprintf(&b, "KG %+v\n", kg)
		head, _ := s.Head(ctx, kg.ID)
		roll := "none"
		if head.Rollback != nil {
			roll = fmt.Sprintf("%+v", *head.Rollback)
		}
		head.Rollback = nil
		fmt.Fprintf(&b, "head %+v rollback %s\n", head, roll)
		vs, _ := s.Versions(ctx, kg.ID)
		fmt.Fprintf(&b, "versions %+v\n", vs)
		gens, _ := s.Generations(ctx, kg.ID)
		fmt.Fprintf(&b, "gens %+v\n", gens)
		ev, _ := s.OrderEvents(ctx, kg.ID, 0, 1<<20)
		fmt.Fprintf(&b, "order %+v\n", ev)
		ch, _ := s.AppliedChanges(ctx, kg.ID, 0, 1<<20)
		fmt.Fprintf(&b, "changes %+v\n", ch)
		lc, _ := s.LightChanges(ctx, kg.ID, 0, 1<<20)
		fmt.Fprintf(&b, "light %+v\n", lc)
		for seq := int64(0); seq <= head.AppliedSeq; seq++ {
			if lite && seq != head.ReadableThrough && seq != head.AppliedSeq {
				continue
			}
			kind := graph.ReadSeq
			if seq > head.ReadableThrough {
				kind = graph.ReadApplied
			}
			sn, err := s.Snapshot(ctx, graph.ReadPoint{KG: kg.ID, Kind: kind, Seq: seq})
			if err != nil {
				fmt.Fprintf(&b, "seq %d: %v\n", seq, err)
				continue
			}
			writeState(ctx, &b, sn, seq)
		}
	}
	return b.String()
}

func writeState(ctx context.Context, w io.Writer, sn graph.Snapshot, seq int64) {
	objs, _ := sn.ScanObjects(ctx, graph.ObjectID{}, 1<<20)
	links, _ := sn.ScanLinks(ctx, graph.LinkID{}, 1<<20)
	fmt.Fprintf(w, "seq %d objects %+v\nlinks %+v\n", seq, objs, links)
	for _, o := range objs {
		v, ok, err := sn.Volatile(ctx, o.ID, graph.AttrServerPower)
		fmt.Fprintf(w, "vol %s %+v %v %v\n", o.ID, v, ok, err)
		ids, _ := sn.(graph.IdentifierLister).IdentifiersOf(ctx, o.ID)
		fmt.Fprintf(w, "idents %+v\n", ids)
	}
	h, _ := sn.IdentifierHistory(ctx, "redfish_system_id", "sys-1")
	e, _ := sn.IdentityEvents(ctx)
	fmt.Fprintf(w, "history %+v events %+v\n", h, e)
}

// flow is the run of operations the tests of the store play: it creates a KG, applies in chunks, seals, changes and
// retires things, drops a half-done apply, and restores to an earlier number.
type flow struct {
	ctx   context.Context
	store graph.Store
}

func (f flow) play() error {
	reg, err := sharedFixture()
	if err != nil {
		return err
	}
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(7))
	kg, err := graph.NewTestKG(f.ctx, f.store, reg.Reg, ids, clock, graph.KGSpec{Name: "prod", Role: graph.RoleProduction})
	if err != nil {
		return err
	}
	a, err := graph.NewApplier(f.store, reg.Reg, graph.NewTestKeyProvider("fs"), clock, ids,
		graph.Options{ChunkRows: 4, Semantic: 1, VerifyOnSeal: true})
	if err != nil {
		return err
	}
	lease, err := a.Acquire(f.ctx, kg.ID)
	if err != nil {
		return err
	}
	scheme, _ := reg.Reg.IdentifierSchemeByName("redfish_system_id")
	one, two := reg.ServerID("SN-000000"), reg.ServerID("SN-000001")
	rack := reg.RackID(reg.SiteID("S00"), "R00")
	steps := []func() error{
		func() error { _, err := a.Apply(f.ctx, kg.ID, lease, reg.World(1, 1, 2)); return err },
		func() error { _, err := a.Seal(f.ctx, kg.ID, lease, graph.SealSync); return err },
		func() error {
			_, err := a.Apply(f.ctx, kg.ID, lease, graph.Batch{
				Objects:     []graph.ObjectUpsert{reg.Site("S00", "Elsewhere 1")},
				Links:       []graph.LinkUpsert{reg.MountedIn(one, rack, 9)},
				Volatile:    []graph.VolatileWrite{reg.Power(one, "off", "2030-01-02T00:00:00Z")},
				Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a"}},
			})
			return err
		},
		func() error {
			_, err := a.Apply(f.ctx, kg.ID, lease, graph.Batch{
				RetireObjects: []graph.ObjectID{two}, RetireLinks: []graph.LinkID{reg.MountedInID(two, rack)},
				Identifiers: []graph.IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a", Close: true}},
			})
			return err
		},
		func() error {
			run, err := a.Begin(f.ctx, kg.ID, lease, reg.World(1, 2, 3))
			if err != nil {
				return err
			}
			if err := run.Next(f.ctx); err != nil {
				return err
			}
			return a.Abandon(f.ctx, kg.ID, lease)
		},
		func() error { _, err := a.Apply(f.ctx, kg.ID, lease, reg.World(2, 1, 1)); return err },
		func() error { _, err := a.Restore(f.ctx, kg.ID, 2); return err },
		func() error { _, err := a.Apply(f.ctx, kg.ID, lease, reg.World(1, 1, 3)); return err },
		func() error { _, err := a.Seal(f.ctx, kg.ID, lease, graph.SealMerge); return err },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

var (
	fixtureOnce sync.Once
	fixture     *graph.Fixture
	fixtureErr  error
)

// sharedFixture loads the standard model once for all the tests: it is read only.
func sharedFixture() (*graph.Fixture, error) {
	fixtureOnce.Do(func() { fixture, fixtureErr = graph.NewFixture() })
	return fixture, fixtureErr
}
