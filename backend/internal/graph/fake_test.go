package graph

import (
	"context"
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// liveStore is the smallest store the applier can run on in this unit's own
// tests: the latest state only, no history, no feeds. The full behaviour is
// the adapters' job and is checked by the conformance suite.
type liveStore struct {
	kg       KG
	head     Head
	lease    *Lease
	attempt  int64
	objects  map[ObjectID]ObjectRow
	links    map[LinkID]LinkRow
	volatile map[volKey]VolatileRow
	idents   []IdentifierRecord
	events   []IdentityEvent
	order    []OrderEvent
	versions []Version
	writes   []ChunkWrite
}

type volKey struct {
	o ObjectID
	a int
}

var _ Store = (*liveStore)(nil)

func newLiveStore() *liveStore {
	return &liveStore{objects: map[ObjectID]ObjectRow{}, links: map[LinkID]LinkRow{}, volatile: map[volKey]VolatileRow{}}
}

var errUnsupported = kernel.Errorf(kernel.CodeUnsupported, "the live store of the unit tests does not do this")

func (s *liveStore) CreateKG(_ context.Context, kg KG) (KG, error) {
	kg.Generation = 1
	s.kg, s.head = kg, Head{KG: kg.ID}
	return kg, nil
}
func (s *liveStore) GetKG(context.Context, string) (KG, error) { return s.kg, nil }
func (s *liveStore) ListKGs(context.Context) ([]KG, error)     { return []KG{s.kg}, nil }
func (s *liveStore) Generations(context.Context, KGID) ([]GenerationEvent, error) {
	return nil, errUnsupported
}
func (s *liveStore) Restore(context.Context, KGID, int64, Head, time.Time) (KG, error) {
	return KG{}, errUnsupported
}
func (s *liveStore) AbandonApply(context.Context, AbandonWrite) error { return errUnsupported }
func (s *liveStore) AppliedChanges(context.Context, KGID, int64, int) ([]AppliedChange, error) {
	return nil, errUnsupported
}
func (s *liveStore) LightChanges(context.Context, KGID, int64, int) ([]LightChange, error) {
	return nil, errUnsupported
}
func (s *liveStore) WaitReadable(context.Context, KGID, int64, time.Duration) (int64, error) {
	return 0, errUnsupported
}
func (s *liveStore) OrderEvents(context.Context, KGID, int64, int) ([]OrderEvent, error) {
	return s.order, nil
}

func (s *liveStore) AcquireLease(_ context.Context, r LeaseRequest) (Lease, error) {
	s.attempt++
	l := Lease{KG: r.KG, Owner: r.Owner, Until: r.Now.Add(r.TTL), Attempt: s.attempt, Semantic: r.Semantic}
	s.lease = &l
	return l, nil
}
func (s *liveStore) RenewLease(_ context.Context, l Lease, ttl time.Duration, now time.Time) (Lease, error) {
	l.Until = now.Add(ttl)
	s.lease = &l
	return l, nil
}
func (s *liveStore) ReleaseLease(context.Context, Lease) error { s.lease = nil; return nil }
func (s *liveStore) Head(context.Context, KGID) (Head, error)  { return s.head.Clone(), nil }
func (s *liveStore) Lease(context.Context, KGID) (Lease, bool, error) {
	if s.lease == nil {
		return Lease{}, false, nil
	}
	return *s.lease, true, nil
}
func (s *liveStore) Versions(context.Context, KGID) ([]Version, error) { return s.versions, nil }

func (s *liveStore) ApplyChunk(_ context.Context, w ChunkWrite) error {
	if w.ExpectedSeq != s.head.AppliedSeq {
		return kernel.Errorf(CodeHeadMoved, "head moved")
	}
	s.writes = append(s.writes, w)
	for _, c := range w.Objects {
		delete(s.objects, firstObject(c))
		if c.New != nil {
			s.objects[c.New.ID] = c.New.Clone()
		}
	}
	for _, c := range w.Links {
		if c.Old != nil {
			delete(s.links, c.Old.ID)
		}
		if c.New != nil {
			s.links[c.New.ID] = c.New.Clone()
		}
	}
	for _, v := range w.Volatile {
		s.volatile[volKey{v.Object, v.AttrNo}] = v.Clone()
	}
	for _, c := range w.Identifiers {
		if !c.Closes {
			s.idents = append(s.idents, c.Record)
		}
	}
	s.events = append(s.events, w.Events...)
	if w.Order != nil {
		s.order = append(s.order, *w.Order)
	}
	s.head = w.Head.Clone()
	if w.Serve {
		s.kg.State = StateServing
	}
	return nil
}

func firstObject(c ObjectChange) ObjectID {
	if c.Old != nil {
		return c.Old.ID
	}
	return ObjectID{}
}

func (s *liveStore) Seal(_ context.Context, w SealWrite) error {
	s.versions = append(s.versions, w.Version)
	s.order = append(s.order, w.Order)
	s.head = w.Head.Clone()
	return nil
}

func (s *liveStore) Snapshot(_ context.Context, rp ReadPoint) (Snapshot, error) {
	return liveSnap{s: s, seq: s.head.AppliedSeq}, nil
}

type liveSnap struct {
	s   *liveStore
	seq int64
}

func (v liveSnap) Info() SnapshotInfo {
	return SnapshotInfo{KG: v.s.kg.ID, Generation: 1, Seq: v.seq, Volatile: VolatileLatest}
}
func (v liveSnap) Object(_ context.Context, id ObjectID) (ObjectRow, bool, error) {
	r, ok := v.s.objects[id]
	return r.Clone(), ok, nil
}
func (v liveSnap) Link(_ context.Context, id LinkID) (LinkRow, bool, error) {
	r, ok := v.s.links[id]
	return r.Clone(), ok, nil
}
func (v liveSnap) ScanObjects(_ context.Context, after ObjectID, limit int) ([]ObjectRow, error) {
	var out []ObjectRow
	for _, r := range v.s.objects {
		if r.ID.String() > after.String() {
			out = append(out, r.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (v liveSnap) ScanLinks(_ context.Context, after LinkID, limit int) ([]LinkRow, error) {
	var out []LinkRow
	for _, r := range v.s.links {
		if r.ID.String() > after.String() {
			out = append(out, r.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (v liveSnap) LinksOf(_ context.Context, id ObjectID, dir Direction) ([]LinkRow, error) {
	var out []LinkRow
	for _, r := range v.s.links {
		if (dir != In && r.Src == id) || (dir != Out && r.Dst == id) {
			out = append(out, r.Clone())
		}
	}
	return out, nil
}
func (v liveSnap) Volatile(_ context.Context, id ObjectID, attr int) (VolatileRow, bool, error) {
	r, ok := v.s.volatile[volKey{id, attr}]
	return r.Clone(), ok, nil
}
func (v liveSnap) Identifiers(_ context.Context, scheme, value string) ([]IdentifierRecord, error) {
	var out []IdentifierRecord
	for _, r := range v.s.idents {
		if r.Scheme == scheme && r.Value == value {
			out = append(out, r)
		}
	}
	return out, nil
}
func (v liveSnap) IdentifierHistory(ctx context.Context, scheme, value string) ([]IdentifierRecord, error) {
	return v.Identifiers(ctx, scheme, value)
}
func (v liveSnap) IdentityEvents(context.Context) ([]IdentityEvent, error) { return v.s.events, nil }
