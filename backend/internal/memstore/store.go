package memstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

type volKey struct {
	object graph.ObjectID
	attr   int
}

// kgState is everything of one KG.
type kgState struct {
	kg       graph.KG
	head     graph.Head
	lease    *graph.Lease
	attempt  int64
	objects  map[graph.ObjectID][]graph.ObjectRow
	links    map[graph.LinkID][]graph.LinkRow
	byObject map[graph.ObjectID]map[graph.LinkID]bool
	volatile map[volKey]graph.VolatileRow
	light    []graph.LightChange
	batches  []graph.ChangeBatch
	entries  map[int64][]graph.ChangeEntry
	idents   []graph.IdentifierRecord
	events   []graph.IdentityEvent
	order    []graph.OrderEvent
	versions []graph.Version
	gens     []graph.GenerationEvent

	objIDs []graph.ObjectID
	lnkIDs []graph.LinkID
	dirty  bool
}

// Store is the in-memory store. The zero value is not usable: call New.
type Store struct {
	mu      sync.RWMutex
	kgs     map[graph.KGID]*kgState
	created []graph.KGID
	changed chan struct{}
}

var _ graph.Store = (*Store)(nil)

// New returns an empty store.
func New() *Store {
	return &Store{kgs: map[graph.KGID]*kgState{}, changed: make(chan struct{})}
}

// notify wakes the waiters of WaitReadable. The caller holds the write lock.
func (s *Store) notify() {
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *Store) state(id graph.KGID) (*kgState, error) {
	st, ok := s.kgs[id]
	if !ok {
		return nil, kernel.Errorf(graph.CodeKGNotFound, "no KG %q", id)
	}
	return st, nil
}

// rebuild refreshes the sorted id lists. Writers call it before they unlock,
// so readers under the read lock only read them.
func (st *kgState) rebuild() {
	if !st.dirty {
		return
	}
	st.objIDs = st.objIDs[:0]
	for id := range st.objects {
		st.objIDs = append(st.objIDs, id)
	}
	sort.Slice(st.objIDs, func(i, j int) bool { return st.objIDs[i].String() < st.objIDs[j].String() })
	st.lnkIDs = st.lnkIDs[:0]
	for id := range st.links {
		st.lnkIDs = append(st.lnkIDs, id)
	}
	sort.Slice(st.lnkIDs, func(i, j int) bool { return st.lnkIDs[i].String() < st.lnkIDs[j].String() })
	st.dirty = false
}

// CreateKG implements graph.Catalog.
func (s *Store) CreateKG(_ context.Context, kg graph.KG) (graph.KG, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kg.ID.IsZero() || kg.Name == "" || !kg.Role.Valid() {
		return graph.KG{}, kernel.Errorf(kernel.CodeInvalidValue, "a KG needs an id, a name and a role")
	}
	if kg.State == "" {
		kg.State = graph.StateBuilding
	}
	for _, other := range s.kgs {
		if other.kg.ID == kg.ID || other.kg.Name == kg.Name || kg.Name == graph.MainAlias ||
			(kg.Role == graph.RoleProduction && other.kg.Role == graph.RoleProduction) {
			return graph.KG{}, kernel.Errorf(kernel.CodeConflict, "KG %q conflicts with one that exists", kg.Name)
		}
	}
	kg.Generation = 1
	s.kgs[kg.ID] = &kgState{kg: kg, head: graph.Head{KG: kg.ID},
		objects: map[graph.ObjectID][]graph.ObjectRow{}, links: map[graph.LinkID][]graph.LinkRow{},
		byObject: map[graph.ObjectID]map[graph.LinkID]bool{}, volatile: map[volKey]graph.VolatileRow{},
		entries: map[int64][]graph.ChangeEntry{}, gens: []graph.GenerationEvent{{Generation: 1, At: kg.CreatedAt}}}
	s.created = append(s.created, kg.ID)
	return kg, nil
}

// GetKG implements graph.Catalog.
func (s *Store) GetKG(_ context.Context, ref string) (graph.KG, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range s.created {
		st := s.kgs[id]
		if st.kg.ID.String() == ref || st.kg.Name == ref || (ref == graph.MainAlias && st.kg.Role == graph.RoleProduction) {
			return st.kg, nil
		}
	}
	return graph.KG{}, kernel.Errorf(graph.CodeKGNotFound, "no KG %q", ref)
}

// ListKGs implements graph.Catalog.
func (s *Store) ListKGs(context.Context) ([]graph.KG, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]graph.KG, 0, len(s.created))
	for _, id := range s.created {
		out = append(out, s.kgs[id].kg)
	}
	return out, nil
}

// Generations implements graph.Catalog.
func (s *Store) Generations(_ context.Context, id graph.KGID) ([]graph.GenerationEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.state(id)
	if err != nil {
		return nil, err
	}
	return append([]graph.GenerationEvent(nil), st.gens...), nil
}

// Head implements graph.Reader.
func (s *Store) Head(_ context.Context, kg graph.KGID) (graph.Head, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.state(kg)
	if err != nil {
		return graph.Head{}, err
	}
	return st.head.Clone(), nil
}

// Lease implements graph.Reader.
func (s *Store) Lease(_ context.Context, kg graph.KGID) (graph.Lease, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.state(kg)
	if err != nil || st.lease == nil {
		return graph.Lease{}, false, err
	}
	return *st.lease, true, nil
}

// Versions implements graph.Reader.
func (s *Store) Versions(_ context.Context, kg graph.KGID) ([]graph.Version, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.state(kg)
	if err != nil {
		return nil, err
	}
	out := make([]graph.Version, len(st.versions))
	for i, v := range st.versions {
		out[i] = cloneVersion(v)
	}
	return out, nil
}

func cloneVersion(v graph.Version) graph.Version {
	parts := make(map[graph.PartKey]graph.Part, len(v.Parts))
	for k, p := range v.Parts {
		parts[k] = p
	}
	v.Parts = parts
	return v
}

// held checks the lease of a write: the owner and attempt are the store's, it
// has not expired at now, and its engine is not older than the applied one.
func (st *kgState) held(l graph.Lease, now time.Time) error {
	if st.lease == nil || st.lease.Owner != l.Owner || st.lease.Attempt != l.Attempt || !now.Before(st.lease.Until) {
		return kernel.Errorf(graph.CodeLeaseLost, "owner %s attempt %d does not hold the lease", l.Owner, l.Attempt)
	}
	if l.Semantic < st.head.AppliedSemanticVersion {
		return kernel.Errorf(graph.CodeSemanticTooLow, "engine %d is below the applied %d", l.Semantic, st.head.AppliedSemanticVersion)
	}
	return nil
}

// guard is the common check of every write.
func (st *kgState) guard(generation int, expected int64, l graph.Lease, now time.Time) error {
	if generation != st.kg.Generation {
		return kernel.Errorf(graph.CodeGenerationChanged, "generation %d, the KG is at %d", generation, st.kg.Generation)
	}
	if err := st.held(l, now); err != nil {
		return err
	}
	if expected != st.head.AppliedSeq {
		return kernel.Errorf(graph.CodeHeadMoved, "expected applied_seq %d, the head is at %d", expected, st.head.AppliedSeq)
	}
	return nil
}
