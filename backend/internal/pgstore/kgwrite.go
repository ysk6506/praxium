package pgstore

import (
	"context"
	"database/sql"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// ApplyChunk implements graph.Writer. One transaction in the schema of the KG: lock the head, verify (the work in
// the middle, the generation, the lease, the engine, the head), write every row, move the head. readable_through
// moves only because the head the applier computed says so. When the COMMIT gets no answer the head is read again
// and decides (resolve).
func (s *Store) ApplyChunk(ctx context.Context, w graph.ChunkWrite) error {
	a, err := s.access(ctx, w.KG)
	if err != nil {
		return err
	}
	if err := a.checked(); err != nil {
		return err
	}
	err = a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error { return s.applyChunk(ctx, tx, a, w) })
	if err = s.resolve(ctx, a, w.Head, err); err == nil && s.record != nil {
		s.record(w)
	}
	return err
}

func (s *Store) applyChunk(ctx context.Context, tx *sql.Tx, a *kgAccess, w graph.ChunkWrite) error {
	cur, err := s.kgGuard(ctx, tx, a, planned{w.KG, w.Generation, w.ExpectedSeq, w.Lease, w.Now})
	if err != nil {
		return err
	}
	seq := int64(0)
	if w.Batch != nil {
		seq = w.Batch.Seq
		if seq != cur.head.AppliedSeq+1 || w.Head.AppliedSeq != seq {
			return kernel.Errorf(graph.CodeHeadMoved, "chunk %d does not follow applied_seq %d", seq, cur.head.AppliedSeq)
		}
	} else if w.Head.AppliedSeq != cur.head.AppliedSeq {
		return kernel.Errorf(graph.CodeHeadMoved, "a chunk without rows may not move applied_seq")
	}
	if err := checkOpenings(w, seq); err != nil {
		return err
	}
	c := &kgChunk{writer: newWriter(ctx, tx, w.KG), a: a, seq: seq, reg: map[string]regEntry{}, linkNo: map[string]int64{}}
	if err := c.write(w); err != nil {
		return err
	}
	if err := saveKGHead(ctx, tx, a, w.Head); err != nil {
		return err
	}
	if w.Serve {
		_, err := tx.ExecContext(ctx, a.sub(`UPDATE @ SET state = 'serving' WHERE id = 1`, pgschema.TableHead))
		return dbErr(err, "serve the KG")
	}
	return nil
}

// kgChunk is what the writing of one chunk shares: the transaction, the change number, and the numbers of the objects
// and the links the chunk names (found once, by ref).
type kgChunk struct {
	*writer
	a      *kgAccess
	seq    int64
	reg    map[string]regEntry
	linkNo map[string]int64
}

// write puts every row of the chunk in, in the order that lets each one find what it needs: the registry first
// (every other row points at a number), then the objects and the links, then the records.
func (c *kgChunk) write(w graph.ChunkWrite) error {
	if err := c.resolve(w); err != nil {
		return err
	}
	c.registerObjects(w.Objects)
	c.objects(w.Objects)
	c.keys(w.Objects)
	c.links(w.Links)
	c.volatile(w.Volatile)
	c.light(w.Light)
	c.identifiers(w.Identifiers)
	c.events(w.Events)
	if w.Batch != nil {
		c.batch(*w.Batch)
		c.entries(w.Entries)
	}
	if w.Order != nil {
		c.order(*w.Order)
	}
	return c.err
}

// resolve reads the registry rows of every object the chunk names, in one read.
func (c *kgChunk) resolve(w graph.ChunkWrite) error {
	seen := map[string]bool{}
	add := func(id graph.ObjectID) {
		if !id.IsZero() {
			seen[id.String()] = true
		}
	}
	namedByRows(w, add)
	namedByRecords(w, add)
	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	rows, err := registryOf(c.ctx, c.tx, c.a, refs)
	for _, e := range rows {
		c.reg[e.ref] = e
	}
	return err
}

// namedByRows hands over the objects that the object, link and value rows of a chunk name.
func namedByRows(w graph.ChunkWrite, add func(graph.ObjectID)) {
	for _, o := range w.Objects {
		if o.Old != nil {
			add(o.Old.ID)
		}
		if o.New != nil {
			add(o.New.ID)
		}
		for _, p := range o.Key {
			for _, id := range p.Objects {
				add(id)
			}
		}
	}
	for _, l := range w.Links {
		for _, r := range []*graph.LinkRow{l.Old, l.New} {
			if r != nil {
				add(r.Src)
				add(r.Dst)
			}
		}
	}
	for _, v := range w.Volatile {
		add(v.Object)
	}
}

// namedByRecords hands over the objects that the light changes, identifiers and events of a chunk name.
func namedByRecords(w graph.ChunkWrite, add func(graph.ObjectID)) {
	for _, l := range w.Light {
		add(l.Object)
	}
	for _, i := range w.Identifiers {
		add(i.Record.Object)
	}
	for _, e := range w.Events {
		add(e.Retired)
		add(e.Into)
	}
}

// objNo is the number of an object the chunk names; an object that has none is a head that moved.
func (c *kgChunk) objNo(id graph.ObjectID) int64 {
	e, ok := c.reg[id.String()]
	if !ok {
		c.fail(kernel.Errorf(graph.CodeHeadMoved, "the object %s is not in the registry of the KG", id))
	}
	return e.objNo
}
