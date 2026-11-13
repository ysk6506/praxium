package memstore

import (
	"context"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// snapshot is one state of a KG: the change number S it shows is fixed when
// it is taken, and every read takes the read lock for its own duration.
type snapshot struct {
	s    *Store
	st   *kgState
	info graph.SnapshotInfo
}

var _ graph.Snapshot = (*snapshot)(nil)

// Snapshot implements graph.Reader.
func (s *Store) Snapshot(_ context.Context, rp graph.ReadPoint) (graph.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.state(rp.KG)
	if err != nil {
		return nil, err
	}
	if rp.Generation != 0 && rp.Generation != st.kg.Generation {
		return nil, kernel.Errorf(graph.CodeGenerationChanged, "generation %d, the KG is at %d", rp.Generation, st.kg.Generation)
	}
	info := graph.SnapshotInfo{KG: rp.KG, Generation: st.kg.Generation, Volatile: graph.VolatileOmitted}
	switch rp.Kind {
	case graph.ReadLatest:
		info.Seq, info.Volatile = st.head.ReadableThrough, graph.VolatileLatest
	case graph.ReadApplied:
		info.Seq, info.Volatile = st.head.AppliedSeq, graph.VolatileLatest
	case graph.ReadSeq:
		if rp.Seq < 0 || rp.Seq > st.head.ReadableThrough {
			return nil, kernel.Errorf(graph.CodeNotReadable, "change number %d is not readable: readable_through is %d", rp.Seq, st.head.ReadableThrough)
		}
		info.Seq = rp.Seq
	case graph.ReadVersion:
		v, ok := st.version(rp.Version)
		if !ok {
			return nil, kernel.Errorf(graph.CodeKGNotFound, "no version %q", rp.Version)
		}
		info.Seq, info.Version = v.SealedSeq, v.ID
	default:
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "unknown read kind %d", rp.Kind)
	}
	return &snapshot{s: s, st: st, info: info}, nil
}

func (st *kgState) version(id graph.VersionID) (graph.Version, bool) {
	for _, v := range st.versions {
		if v.ID == id {
			return v, true
		}
	}
	return graph.Version{}, false
}

func (sn *snapshot) Info() graph.SnapshotInfo { return sn.info }

// check fails when the KG has moved to a newer generation since the snapshot was taken.
func (sn *snapshot) check() error {
	if sn.st.kg.Generation != sn.info.Generation {
		return kernel.Errorf(graph.CodeGenerationChanged, "the KG moved to generation %d", sn.st.kg.Generation)
	}
	return nil
}

func (sn *snapshot) Object(_ context.Context, id graph.ObjectID) (graph.ObjectRow, bool, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return graph.ObjectRow{}, false, err
	}
	row, ok := sn.objectAt(id)
	return row, ok, nil
}

func (sn *snapshot) objectAt(id graph.ObjectID) (graph.ObjectRow, bool) {
	chain := sn.st.objects[id]
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].ValidAt(sn.info.Seq) {
			return chain[i].Clone(), true
		}
	}
	return graph.ObjectRow{}, false
}

func (sn *snapshot) linkAt(id graph.LinkID) (graph.LinkRow, bool) {
	chain := sn.st.links[id]
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].ValidAt(sn.info.Seq) {
			return chain[i].Clone(), true
		}
	}
	return graph.LinkRow{}, false
}

func (sn *snapshot) Link(_ context.Context, id graph.LinkID) (graph.LinkRow, bool, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return graph.LinkRow{}, false, err
	}
	row, ok := sn.linkAt(id)
	return row, ok, nil
}

func (sn *snapshot) ScanObjects(_ context.Context, after graph.ObjectID, limit int) ([]graph.ObjectRow, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return nil, err
	}
	ids := sn.st.objIDs
	start := 0
	if !after.IsZero() {
		start = sort.Search(len(ids), func(i int) bool { return ids[i].String() > after.String() })
	}
	var out []graph.ObjectRow
	for _, id := range ids[start:] {
		if len(out) >= limit {
			break
		}
		if row, ok := sn.objectAt(id); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func (sn *snapshot) ScanLinks(_ context.Context, after graph.LinkID, limit int) ([]graph.LinkRow, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return nil, err
	}
	ids := sn.st.lnkIDs
	start := 0
	if !after.IsZero() {
		start = sort.Search(len(ids), func(i int) bool { return ids[i].String() > after.String() })
	}
	var out []graph.LinkRow
	for _, id := range ids[start:] {
		if len(out) >= limit {
			break
		}
		if row, ok := sn.linkAt(id); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func (sn *snapshot) LinksOf(_ context.Context, id graph.ObjectID, dir graph.Direction) ([]graph.LinkRow, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return nil, err
	}
	ids := make([]graph.LinkID, 0, len(sn.st.byObject[id]))
	for lid := range sn.st.byObject[id] {
		ids = append(ids, lid)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	var out []graph.LinkRow
	for _, lid := range ids {
		row, ok := sn.linkAt(lid)
		if !ok {
			continue
		}
		if (dir == graph.Out && row.Src == id) || (dir == graph.In && row.Dst == id) || dir == graph.Both {
			out = append(out, row)
		}
	}
	return out, nil
}

func (sn *snapshot) Volatile(_ context.Context, id graph.ObjectID, attrNo int) (graph.VolatileRow, bool, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return graph.VolatileRow{}, false, err
	}
	if sn.info.Volatile != graph.VolatileLatest {
		return graph.VolatileRow{}, false, kernel.Errorf(graph.CodeVolatileOmitted, "volatile values are only in latest reads")
	}
	row, ok := sn.st.volatile[volKey{id, attrNo}]
	if !ok {
		return graph.VolatileRow{}, false, nil
	}
	if _, live := sn.objectAt(id); !live {
		return graph.VolatileRow{}, false, nil
	}
	return row.Clone(), true, nil
}

func (sn *snapshot) Identifiers(_ context.Context, scheme, value string) ([]graph.IdentifierRecord, error) {
	return sn.identifiers(scheme, value, false)
}

func (sn *snapshot) IdentifierHistory(_ context.Context, scheme, value string) ([]graph.IdentifierRecord, error) {
	return sn.identifiers(scheme, value, true)
}

func (sn *snapshot) identifiers(scheme, value string, history bool) ([]graph.IdentifierRecord, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return nil, err
	}
	var out []graph.IdentifierRecord
	for _, r := range sn.st.idents {
		if r.Scheme != scheme || r.Value != value || r.SeqFrom > sn.info.Seq {
			continue
		}
		if history || r.SeqTo == 0 || sn.info.Seq < r.SeqTo {
			out = append(out, r)
		}
	}
	return out, nil
}

func (sn *snapshot) IdentityEvents(context.Context) ([]graph.IdentityEvent, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return nil, err
	}
	var out []graph.IdentityEvent
	for _, e := range sn.st.events {
		if e.Seq <= sn.info.Seq {
			out = append(out, e)
		}
	}
	return out, nil
}

var _ graph.IdentifierLister = (*snapshot)(nil)

// IdentifiersOf implements graph.IdentifierLister.
func (sn *snapshot) IdentifiersOf(_ context.Context, id graph.ObjectID) ([]graph.IdentifierRecord, error) {
	sn.s.mu.RLock()
	defer sn.s.mu.RUnlock()
	if err := sn.check(); err != nil {
		return nil, err
	}
	var out []graph.IdentifierRecord
	for _, r := range sn.st.idents {
		if r.Object == id && r.SeqFrom <= sn.info.Seq && (r.SeqTo == 0 || sn.info.Seq < r.SeqTo) {
			out = append(out, r)
		}
	}
	return out, nil
}
