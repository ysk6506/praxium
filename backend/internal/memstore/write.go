package memstore

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ApplyChunk implements graph.Writer. The whole call is atomic: the checks run
// before the first row moves, and with a journal the chunk is on stable storage
// before it is applied.
func (s *Store) ApplyChunk(_ context.Context, w graph.ChunkWrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(w.KG)
	if err != nil {
		return err
	}
	if err := st.guard(w.Generation, w.ExpectedSeq, w.Lease, w.Now); err != nil {
		return err
	}
	seq, err := st.checkChunk(w)
	if err != nil {
		return err
	}
	if s.j != nil {
		wire, err := toWireChunk(w)
		if err != nil {
			return err
		}
		if err := s.persist(kindChunk, wire); err != nil {
			return err
		}
	}
	st.commitChunk(w, seq)
	s.notify()
	s.maybeCheckpoint()
	return nil
}

// checkChunk runs the checks of a chunk that need the state only (not the lease) and returns its change number.
func (st *kgState) checkChunk(w graph.ChunkWrite) (int64, error) {
	seq := int64(0)
	if w.Batch != nil {
		seq = w.Batch.Seq
		if seq != st.head.AppliedSeq+1 || w.Head.AppliedSeq != seq {
			return 0, kernel.Errorf(graph.CodeHeadMoved, "chunk %d does not follow applied_seq %d", seq, st.head.AppliedSeq)
		}
	} else if w.Head.AppliedSeq != st.head.AppliedSeq {
		return 0, kernel.Errorf(graph.CodeHeadMoved, "a chunk without rows may not move applied_seq")
	}
	return seq, st.checkClosings(w, seq)
}

// commitChunk moves the rows and the head. The checks have passed.
func (st *kgState) commitChunk(w graph.ChunkWrite, seq int64) {
	st.applyRows(w, seq)
	st.head = w.Head.Clone()
	if w.Batch != nil && w.Batch.Attempt > st.attempt {
		st.attempt = w.Batch.Attempt
	}
	if w.Serve {
		st.kg.State = graph.StateServing
	}
	st.rebuild()
}

// checkClosings verifies that every row the chunk closes is open and is the
// one the applier planned against.
func (st *kgState) checkClosings(w graph.ChunkWrite, seq int64) error {
	for _, c := range w.Objects {
		if c.Old != nil && !openWithHash(st.objects[c.Old.ID], c.Old.ContentHash) {
			return kernel.Errorf(graph.CodeHeadMoved, "object %s is not the open row the chunk was planned against", c.Old.ID)
		}
		if c.New != nil && c.New.SeqFrom != seq {
			return kernel.Errorf(kernel.CodeInvalidValue, "object row %s opens at %d, the chunk is %d", c.New.ID, c.New.SeqFrom, seq)
		}
	}
	for _, c := range w.Links {
		if c.Old != nil && !linkOpenWithHash(st.links[c.Old.ID], c.Old.ContentHash) {
			return kernel.Errorf(graph.CodeHeadMoved, "link %s is not the open row the chunk was planned against", c.Old.ID)
		}
	}
	return nil
}

func openWithHash(chain []graph.ObjectRow, h kernel.Hash) bool {
	return len(chain) > 0 && chain[len(chain)-1].Open() && chain[len(chain)-1].ContentHash == h
}

func linkOpenWithHash(chain []graph.LinkRow, h kernel.Hash) bool {
	return len(chain) > 0 && chain[len(chain)-1].Open() && chain[len(chain)-1].ContentHash == h
}

func (st *kgState) applyRows(w graph.ChunkWrite, seq int64) {
	for _, c := range w.Objects {
		if c.Old != nil {
			chain := st.objects[c.Old.ID]
			chain[len(chain)-1].SeqTo = seq
		}
		if c.New != nil {
			if _, known := st.objects[c.New.ID]; !known {
				st.dirty = true
			}
			st.objects[c.New.ID] = append(st.objects[c.New.ID], c.New.Clone())
		}
	}
	for _, c := range w.Links {
		if c.Old != nil {
			chain := st.links[c.Old.ID]
			chain[len(chain)-1].SeqTo = seq
		}
		if c.New != nil {
			st.addLink(c.New.Clone())
		}
	}
	for _, v := range w.Volatile {
		st.volatile[volKey{v.Object, v.AttrNo}] = v.Clone()
	}
	st.light = append(st.light, w.Light...)
	st.applyIdentifiers(w.Identifiers, seq)
	st.events = append(st.events, w.Events...)
	if w.Batch != nil {
		st.batches = append(st.batches, *w.Batch)
		st.entries[seq] = append([]graph.ChangeEntry(nil), w.Entries...)
	}
	if w.Order != nil {
		st.order = append(st.order, *w.Order)
	}
}

func (st *kgState) addLink(row graph.LinkRow) {
	if _, known := st.links[row.ID]; !known {
		st.dirty = true
	}
	st.links[row.ID] = append(st.links[row.ID], row)
	for _, end := range []graph.ObjectID{row.Src, row.Dst} {
		if st.byObject[end] == nil {
			st.byObject[end] = map[graph.LinkID]bool{}
		}
		st.byObject[end][row.ID] = true
	}
}

func (st *kgState) applyIdentifiers(changes []graph.IdentifierChange, seq int64) {
	for _, c := range changes {
		if st.identByValue == nil {
			st.reindexIdents()
		}
		if !c.Closes {
			st.idents = append(st.idents, c.Record)
			st.indexIdent(len(st.idents) - 1)
			continue
		}
		for _, i := range st.identByValue[identKey{c.Record.Scheme, c.Record.Value}] {
			r := &st.idents[i]
			if r.SeqTo == 0 && r.Object == c.Record.Object && r.Scheme == c.Record.Scheme && r.Value == c.Record.Value && r.Scope == c.Record.Scope {
				r.SeqTo = seq
			}
		}
	}
}

// AbandonApply implements graph.Writer: the chunks past readable_through go.
func (s *Store) AbandonApply(_ context.Context, w graph.AbandonWrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(w.KG)
	if err != nil {
		return err
	}
	if err := st.guard(w.Generation, w.ExpectedSeq, w.Lease, w.Now); err != nil {
		return err
	}
	if w.Head.AppliedSeq != st.head.ReadableThrough {
		return kernel.Errorf(kernel.CodeInvalidValue, "an abandoned apply goes back to readable_through %d", st.head.ReadableThrough)
	}
	if err := s.persist(kindAbandon, wAbandon{KG: w.KG.String(), Head: wireHead(w.Head)}); err != nil {
		return err
	}
	st.abandon(w.Head)
	s.maybeCheckpoint()
	return nil
}

func (st *kgState) abandon(head graph.Head) {
	st.truncate(st.head.ReadableThrough)
	st.head = head.Clone()
	st.rebuild()
}

// Seal implements graph.Writer.
func (s *Store) Seal(_ context.Context, w graph.SealWrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(w.KG)
	if err != nil {
		return err
	}
	if err := st.guard(w.Generation, w.ExpectedSeq, w.Lease, w.Now); err != nil {
		return err
	}
	if err := st.checkSeal(w.Version); err != nil {
		return err
	}
	if err := s.persist(kindSeal, wSeal{KG: w.KG.String(), Version: wireVersion(w.Version), Order: w.Order, Head: wireHead(w.Head)}); err != nil {
		return err
	}
	st.seal(w.Version, w.Order, w.Head)
	s.maybeCheckpoint()
	return nil
}

func (st *kgState) checkSeal(v graph.Version) error {
	if st.head.AppliedSeq != st.head.ReadableThrough || st.head.Open() {
		return kernel.Errorf(graph.CodeSealNotReady, "applied_seq %d, readable_through %d", st.head.AppliedSeq, st.head.ReadableThrough)
	}
	if v.No != len(st.versions)+1 || v.SealedSeq != st.head.ReadableThrough {
		return kernel.Errorf(kernel.CodeInvalidValue, "version %d at %d does not follow the %d sealed versions", v.No, v.SealedSeq, len(st.versions))
	}
	return nil
}

func (st *kgState) seal(v graph.Version, order graph.OrderEvent, head graph.Head) {
	st.versions = append(st.versions, cloneVersion(v))
	st.order = append(st.order, order)
	st.head = head.Clone()
}

// Restore implements graph.Catalog.
func (s *Store) Restore(_ context.Context, id graph.KGID, toSeq int64, head graph.Head, now time.Time) (graph.KG, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(id)
	if err != nil {
		return graph.KG{}, err
	}
	if toSeq < 0 || toSeq > st.head.ReadableThrough {
		return graph.KG{}, kernel.Errorf(graph.CodeNotReadable, "cannot restore to %d", toSeq)
	}
	if err := s.persist(kindRestore, wRestore{KG: id.String(), ToSeq: toSeq, Head: wireHead(head), Now: now}); err != nil {
		return graph.KG{}, err
	}
	st.restore(toSeq, head, now)
	s.notify()
	s.maybeCheckpoint()
	return st.kg, nil
}

func (st *kgState) restore(toSeq int64, head graph.Head, now time.Time) {
	st.truncate(toSeq)
	kept := st.order[:0]
	for _, e := range st.order {
		if e.ChangeSeq <= toSeq {
			kept = append(kept, e)
		}
	}
	st.order = kept
	versions := st.versions[:0]
	for _, v := range st.versions {
		if v.SealedSeq <= toSeq {
			versions = append(versions, v)
		}
	}
	st.versions = versions
	st.kg.Generation++
	st.gens = append(st.gens, graph.GenerationEvent{Generation: st.kg.Generation, RestoredTo: toSeq, At: now})
	st.head = head.Clone()
	st.rebuild()
}

// truncate removes every row that began after seq and reopens the rows that
// closed after it, as if nothing after seq had happened.
func (st *kgState) truncate(seq int64) {
	for id, chain := range st.objects {
		kept := chain[:0]
		for _, r := range chain {
			if r.SeqFrom > seq {
				continue
			}
			if r.SeqTo > seq {
				r.SeqTo = 0
			}
			kept = append(kept, r)
		}
		if len(kept) == 0 {
			delete(st.objects, id)
			st.dirty = true
			continue
		}
		st.objects[id] = kept
	}
	for id, chain := range st.links {
		kept := chain[:0]
		for _, r := range chain {
			if r.SeqFrom > seq {
				continue
			}
			if r.SeqTo > seq {
				r.SeqTo = 0
			}
			kept = append(kept, r)
		}
		if len(kept) == 0 {
			st.dropLink(id, chain)
			continue
		}
		st.links[id] = kept
	}
	st.truncateLogs(seq)
}

func (st *kgState) dropLink(id graph.LinkID, chain []graph.LinkRow) {
	for _, r := range chain {
		delete(st.byObject[r.Src], id)
		delete(st.byObject[r.Dst], id)
	}
	delete(st.links, id)
	st.dirty = true
}

func (st *kgState) truncateLogs(seq int64) {
	light := st.light[:0]
	for _, l := range st.light {
		if l.Seq <= seq {
			light = append(light, l)
		}
	}
	st.light = light
	batches := st.batches[:0]
	for _, b := range st.batches {
		if b.Seq <= seq {
			batches = append(batches, b)
		}
	}
	st.batches = batches
	for k := range st.entries {
		if k > seq {
			delete(st.entries, k)
		}
	}
	idents := st.idents[:0]
	for _, r := range st.idents {
		if r.SeqFrom > seq {
			continue
		}
		if r.SeqTo > seq {
			r.SeqTo = 0
		}
		idents = append(idents, r)
	}
	st.idents = idents
	st.reindexIdents()
	events := st.events[:0]
	for _, e := range st.events {
		if e.Seq <= seq {
			events = append(events, e)
		}
	}
	st.events = events
}
