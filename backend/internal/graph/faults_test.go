package graph

import (
	"context"
	"errors"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

var errInjected = errors.New("injected store failure")

// flaky fails the nth call that reaches the store (reads of a snapshot count
// too) and passes every other call on.
type flaky struct {
	*liveStore
	n, calls int
}

func (f *flaky) hit() error {
	f.calls++
	if f.calls == f.n {
		return errInjected
	}
	return nil
}

func (f *flaky) Head(ctx context.Context, kg KGID) (Head, error) {
	if err := f.hit(); err != nil {
		return Head{}, err
	}
	return f.liveStore.Head(ctx, kg)
}

func (f *flaky) OrderEvents(ctx context.Context, kg KGID, after int64, limit int) ([]OrderEvent, error) {
	if err := f.hit(); err != nil {
		return nil, err
	}
	return f.liveStore.OrderEvents(ctx, kg, after, limit)
}

func (f *flaky) Versions(ctx context.Context, kg KGID) ([]Version, error) {
	if err := f.hit(); err != nil {
		return nil, err
	}
	return f.liveStore.Versions(ctx, kg)
}

func (f *flaky) ApplyChunk(ctx context.Context, w ChunkWrite) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.liveStore.ApplyChunk(ctx, w)
}

func (f *flaky) Seal(ctx context.Context, w SealWrite) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.liveStore.Seal(ctx, w)
}

func (f *flaky) Snapshot(ctx context.Context, rp ReadPoint) (Snapshot, error) {
	if err := f.hit(); err != nil {
		return nil, err
	}
	s, err := f.liveStore.Snapshot(ctx, rp)
	return flakySnap{Snapshot: s, f: f}, err
}

type flakySnap struct {
	Snapshot
	f *flaky
}

func (s flakySnap) Object(ctx context.Context, id ObjectID) (ObjectRow, bool, error) {
	if err := s.f.hit(); err != nil {
		return ObjectRow{}, false, err
	}
	return s.Snapshot.Object(ctx, id)
}

func (s flakySnap) Link(ctx context.Context, id LinkID) (LinkRow, bool, error) {
	if err := s.f.hit(); err != nil {
		return LinkRow{}, false, err
	}
	return s.Snapshot.Link(ctx, id)
}

func (s flakySnap) ScanObjects(ctx context.Context, after ObjectID, limit int) ([]ObjectRow, error) {
	if err := s.f.hit(); err != nil {
		return nil, err
	}
	return s.Snapshot.ScanObjects(ctx, after, limit)
}

func (s flakySnap) ScanLinks(ctx context.Context, after LinkID, limit int) ([]LinkRow, error) {
	if err := s.f.hit(); err != nil {
		return nil, err
	}
	return s.Snapshot.ScanLinks(ctx, after, limit)
}

func (s flakySnap) LinksOf(ctx context.Context, id ObjectID, d Direction) ([]LinkRow, error) {
	if err := s.f.hit(); err != nil {
		return nil, err
	}
	return s.Snapshot.LinksOf(ctx, id, d)
}

func (s flakySnap) Volatile(ctx context.Context, id ObjectID, no int) (VolatileRow, bool, error) {
	if err := s.f.hit(); err != nil {
		return VolatileRow{}, false, err
	}
	return s.Snapshot.Volatile(ctx, id, no)
}

func (s flakySnap) Identifiers(ctx context.Context, scheme, value string) ([]IdentifierRecord, error) {
	if err := s.f.hit(); err != nil {
		return nil, err
	}
	return s.Snapshot.Identifiers(ctx, scheme, value)
}

// workload runs a few applies, a seal, a verify and a restore on the store.
// It returns the first error and whether the injected failure was the cause.
func workload(t *testing.T, f *flaky) error {
	t.Helper()
	ctx := context.Background()
	u := newUnit(t, nil, Options{ChunkRows: 3, VerifyOnSeal: true})
	f.liveStore = u.store
	a, err := NewApplier(f, u.fx.Reg, NewTestKeyProvider("unit"), kernel.NewManualClock(epoch), kernel.NewRandomIDs(kernel.NewSeededRng(5)), Options{ChunkRows: 3, Semantic: 1, VerifyOnSeal: true})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := a.Acquire(ctx, u.kg.ID)
	if err != nil {
		return err
	}
	scheme, _ := u.fx.Reg.IdentifierSchemeByName("redfish_system_id")
	one, rack := u.fx.ServerID("SN-000000"), u.fx.RackID(u.fx.SiteID("S00"), "R00")
	two := u.fx.ServerID("SN-000001")
	twoLink, err := NewLinkID(u.fx.linkID("MOUNTED_IN"), two, rack, "")
	if err != nil {
		t.Fatal(err)
	}
	steps := []Batch{
		u.fx.World(1, 2, 4),
		{Objects: []ObjectUpsert{u.fx.Site("S00", "Else")}, Links: []LinkUpsert{u.fx.MountedIn(one, rack, 7)},
			Volatile:    []VolatileWrite{u.fx.Power(one, "on", "2030-01-01T00:00:00Z")},
			Identifiers: []IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a"}}},
		{RetireObjects: []ObjectID{two}, RetireLinks: []LinkID{twoLink}, Volatile: []VolatileWrite{u.fx.Power(one, "off", "2030-01-02T00:00:00Z")},
			Identifiers: []IdentifierWrite{{Object: one, Scheme: scheme.ID, Value: "sys-1", Scope: "a", Close: true}}},
	}
	for _, b := range steps {
		if _, err := a.Apply(ctx, u.kg.ID, lease, b); err != nil {
			return err
		}
		if _, err := a.Seal(ctx, u.kg.ID, lease, SealSync); err != nil {
			return err
		}
	}
	if err := a.Verify(ctx, u.kg.ID); err != nil {
		return err
	}
	if _, err := a.Restore(ctx, u.kg.ID, 1); err != nil && kernel.CodeOf(err) != kernel.CodeUnsupported {
		return err
	}
	return a.Release(ctx, lease)
}

func TestEveryStoreFailureComesBackAsAnError(t *testing.T) {
	var clean flaky
	if err := workload(t, &clean); err != nil {
		t.Fatalf("clean run: %v", err)
	}
	total := clean.calls
	if total < 50 {
		t.Fatalf("only %d store calls in the workload", total)
	}
	failed := 0
	for n := 1; n <= total; n++ {
		f := &flaky{n: n}
		err := workload(t, f)
		if err == nil {
			continue
		}
		if !errors.Is(err, errInjected) {
			t.Fatalf("failure at call %d came back as %v", n, err)
		}
		failed++
	}
	if failed < total/2 {
		t.Errorf("only %d of %d injected failures surfaced", failed, total)
	}
}
