package graph

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ScratchStore is the smallest store that the applier and the readers above
// the port can run on in their own tests (ADR-0027): one KG, the latest state
// only, no feeds. A unit above the port may not import an adapter, so it takes
// this one from the core; the conformance-checked reference store is
// adapter.memstore, and whatever needs history (reads at an older change
// number, restores, waiting) is tested against that one, in adapter.readcheck.
//
// It answers the read points it can: the latest state, the applied state, the
// readable_through change number and a version sealed at it. Any other read
// point is refused with CodeUnsupported.
type ScratchStore struct {
	mu       sync.Mutex
	made     bool
	kg       KG
	head     Head
	lease    *Lease
	attempt  int64
	objects  map[ObjectID]ObjectRow
	links    map[LinkID]LinkRow
	volatile map[scratchKey]VolatileRow
	idents   []IdentifierRecord
	events   []IdentityEvent
	order    []OrderEvent
	versions []Version
}

type scratchKey struct {
	object ObjectID
	attr   int
}

var _ Store = (*ScratchStore)(nil)

// NewScratchStore returns an empty store.
func NewScratchStore() *ScratchStore {
	return &ScratchStore{objects: map[ObjectID]ObjectRow{}, links: map[LinkID]LinkRow{}, volatile: map[scratchKey]VolatileRow{}}
}

func scratchUnsupported(what string) error {
	return kernel.Errorf(kernel.CodeUnsupported, "the scratch store does not do %s", what)
}

func (s *ScratchStore) own(id KGID) error {
	if !s.made || s.kg.ID != id {
		return kernel.Errorf(CodeKGNotFound, "no KG %q", id)
	}
	return nil
}

// CreateKG implements Catalog; the scratch store holds one KG.
func (s *ScratchStore) CreateKG(_ context.Context, kg KG) (KG, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.made {
		return KG{}, kernel.Errorf(kernel.CodeConflict, "the scratch store holds one KG")
	}
	if kg.ID.IsZero() || kg.Name == "" || !kg.Role.Valid() {
		return KG{}, kernel.Errorf(kernel.CodeInvalidValue, "a KG needs an id, a name and a role")
	}
	if kg.State == "" {
		kg.State = StateBuilding
	}
	kg.Generation = 1
	s.kg, s.head, s.made = kg, Head{KG: kg.ID}, true
	return kg, nil
}

// GetKG implements Catalog.
func (s *ScratchStore) GetKG(_ context.Context, ref string) (KG, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.made && (s.kg.ID.String() == ref || s.kg.Name == ref || (ref == MainAlias && s.kg.Role == RoleProduction)) {
		return s.kg, nil
	}
	return KG{}, kernel.Errorf(CodeKGNotFound, "no KG %q", ref)
}

// ListKGs implements Catalog.
func (s *ScratchStore) ListKGs(context.Context) ([]KG, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.made {
		return nil, nil
	}
	return []KG{s.kg}, nil
}

// Generations implements Catalog; there are no restores.
func (s *ScratchStore) Generations(context.Context, KGID) ([]GenerationEvent, error) {
	return nil, scratchUnsupported("restores")
}

// Restore implements Catalog and refuses.
func (s *ScratchStore) Restore(context.Context, KGID, int64, Head, time.Time) (KG, error) {
	return KG{}, scratchUnsupported("restores")
}

// AbandonApply implements Writer and refuses.
func (s *ScratchStore) AbandonApply(context.Context, AbandonWrite) error {
	return scratchUnsupported("abandoning a chunked apply")
}

// AcquireLease implements Writer.
func (s *ScratchStore) AcquireLease(_ context.Context, r LeaseRequest) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(r.KG); err != nil {
		return Lease{}, err
	}
	s.attempt++
	l := Lease{KG: r.KG, Owner: r.Owner, Until: r.Now.Add(r.TTL), Attempt: s.attempt, Semantic: r.Semantic}
	s.lease = &l
	return l, nil
}

// RenewLease implements Writer.
func (s *ScratchStore) RenewLease(_ context.Context, l Lease, ttl time.Duration, now time.Time) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l.Until = now.Add(ttl)
	s.lease = &l
	return l, nil
}

// ReleaseLease implements Writer.
func (s *ScratchStore) ReleaseLease(context.Context, Lease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lease = nil
	return nil
}

// Head implements Reader.
func (s *ScratchStore) Head(_ context.Context, kg KGID) (Head, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(kg); err != nil {
		return Head{}, err
	}
	return s.head.Clone(), nil
}

// Lease implements Reader.
func (s *ScratchStore) Lease(_ context.Context, kg KGID) (Lease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(kg); err != nil || s.lease == nil {
		return Lease{}, false, err
	}
	return *s.lease, true, nil
}

// Versions implements Reader.
func (s *ScratchStore) Versions(_ context.Context, kg KGID) ([]Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(kg); err != nil {
		return nil, err
	}
	return append([]Version(nil), s.versions...), nil
}

// ApplyChunk implements Writer: the rows of the chunk replace the rows they close.
func (s *ScratchStore) ApplyChunk(_ context.Context, w ChunkWrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(w.KG); err != nil {
		return err
	}
	if w.ExpectedSeq != s.head.AppliedSeq {
		return kernel.Errorf(CodeHeadMoved, "expected applied_seq %d, the head is at %d", w.ExpectedSeq, s.head.AppliedSeq)
	}
	for _, c := range w.Objects {
		if c.Old != nil {
			delete(s.objects, c.Old.ID)
		}
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
		s.volatile[scratchKey{v.Object, v.AttrNo}] = v.Clone()
	}
	for _, c := range w.Identifiers {
		if !c.Closes {
			s.idents = append(s.idents, c.Record)
			continue
		}
		kept := s.idents[:0]
		for _, r := range s.idents {
			if r.Object != c.Record.Object || r.Scheme != c.Record.Scheme || r.Value != c.Record.Value {
				kept = append(kept, r)
			}
		}
		s.idents = kept
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

// Seal implements Writer.
func (s *ScratchStore) Seal(_ context.Context, w SealWrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(w.KG); err != nil {
		return err
	}
	s.versions = append(s.versions, w.Version)
	s.order = append(s.order, w.Order)
	s.head = w.Head.Clone()
	return nil
}

// AppliedChanges implements Feed and refuses.
func (s *ScratchStore) AppliedChanges(context.Context, KGID, int64, int) ([]AppliedChange, error) {
	return nil, scratchUnsupported("feeds")
}

// LightChanges implements Feed and refuses.
func (s *ScratchStore) LightChanges(context.Context, KGID, int64, int) ([]LightChange, error) {
	return nil, scratchUnsupported("feeds")
}

// OrderEvents implements Feed.
func (s *ScratchStore) OrderEvents(_ context.Context, kg KGID, after int64, limit int) ([]OrderEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(kg); err != nil {
		return nil, err
	}
	var out []OrderEvent
	for _, e := range s.order {
		if e.IngestSeq > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

// WaitReadable implements Feed without waiting: it answers at once, as a zero timeout would.
func (s *ScratchStore) WaitReadable(_ context.Context, kg KGID, minSeq int64, _ time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(kg); err != nil {
		return 0, err
	}
	if s.head.ReadableThrough < minSeq {
		return s.head.ReadableThrough, kernel.Errorf(CodeWaitTimeout, "readable_through is %d, below %d", s.head.ReadableThrough, minSeq)
	}
	return s.head.ReadableThrough, nil
}

// Snapshot implements Reader.
func (s *ScratchStore) Snapshot(_ context.Context, rp ReadPoint) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.own(rp.KG); err != nil {
		return nil, err
	}
	if rp.Generation != 0 && rp.Generation != s.kg.Generation {
		return nil, kernel.Errorf(CodeGenerationChanged, "generation %d, the KG is at %d", rp.Generation, s.kg.Generation)
	}
	info := SnapshotInfo{KG: rp.KG, Generation: s.kg.Generation, Seq: s.head.ReadableThrough, Volatile: VolatileLatest}
	switch rp.Kind {
	case ReadLatest:
	case ReadApplied:
		info.Seq = s.head.AppliedSeq
	case ReadSeq:
		if rp.Seq > s.head.ReadableThrough || rp.Seq < 0 {
			return nil, kernel.Errorf(CodeNotReadable, "change number %d is not readable: readable_through is %d", rp.Seq, s.head.ReadableThrough)
		}
		if rp.Seq != s.head.ReadableThrough {
			return nil, scratchUnsupported("reads at an older change number")
		}
		info.Volatile = VolatileOmitted
	case ReadVersion:
		v, ok := s.sealed(rp.Version)
		if !ok {
			return nil, kernel.Errorf(CodeKGNotFound, "no version %q", rp.Version)
		}
		if v.SealedSeq != s.head.ReadableThrough {
			return nil, scratchUnsupported("reads at an older version")
		}
		info.Seq, info.Version, info.Volatile = v.SealedSeq, v.ID, VolatileOmitted
	default:
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "unknown read kind %d", rp.Kind)
	}
	return scratchSnap{s: s, info: info}, nil
}

func (s *ScratchStore) sealed(id VersionID) (Version, bool) {
	for _, v := range s.versions {
		if v.ID == id {
			return v, true
		}
	}
	return Version{}, false
}

type scratchSnap struct {
	s    *ScratchStore
	info SnapshotInfo
}

func (v scratchSnap) Info() SnapshotInfo { return v.info }

func (v scratchSnap) Object(_ context.Context, id ObjectID) (ObjectRow, bool, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	r, ok := v.s.objects[id]
	return r.Clone(), ok, nil
}

func (v scratchSnap) Link(_ context.Context, id LinkID) (LinkRow, bool, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	r, ok := v.s.links[id]
	return r.Clone(), ok, nil
}

func (v scratchSnap) ScanObjects(_ context.Context, after ObjectID, limit int) ([]ObjectRow, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
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

func (v scratchSnap) ScanLinks(_ context.Context, after LinkID, limit int) ([]LinkRow, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
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

func (v scratchSnap) LinksOf(_ context.Context, id ObjectID, dir Direction) ([]LinkRow, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	var out []LinkRow
	for _, r := range v.s.links {
		if (dir != In && r.Src == id) || (dir != Out && r.Dst == id) {
			out = append(out, r.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (v scratchSnap) Volatile(_ context.Context, id ObjectID, attr int) (VolatileRow, bool, error) {
	if v.info.Volatile != VolatileLatest {
		return VolatileRow{}, false, kernel.Errorf(CodeVolatileOmitted, "volatile values are not part of this read point")
	}
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	r, ok := v.s.volatile[scratchKey{id, attr}]
	return r.Clone(), ok, nil
}

func (v scratchSnap) Identifiers(_ context.Context, scheme, value string) ([]IdentifierRecord, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	var out []IdentifierRecord
	for _, r := range v.s.idents {
		if r.Scheme == scheme && r.Value == value {
			out = append(out, r)
		}
	}
	return out, nil
}

func (v scratchSnap) IdentifierHistory(ctx context.Context, scheme, value string) ([]IdentifierRecord, error) {
	return v.Identifiers(ctx, scheme, value)
}

func (v scratchSnap) IdentityEvents(context.Context) ([]IdentityEvent, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	return append([]IdentityEvent(nil), v.s.events...), nil
}

// IdentifiersOf implements IdentifierLister.
func (v scratchSnap) IdentifiersOf(_ context.Context, id ObjectID) ([]IdentifierRecord, error) {
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	var out []IdentifierRecord
	for _, r := range v.s.idents {
		if r.Object == id {
			out = append(out, r)
		}
	}
	return out, nil
}
