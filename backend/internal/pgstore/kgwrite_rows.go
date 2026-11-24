package pgstore

import (
	"sort"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// changeKindNo is the number a kind of change has in the change entries.
func changeKindNo(k graph.ChangeKind) (int16, error) {
	switch k {
	case graph.ChangeAdded:
		return pgschema.ChangeKindAdded, nil
	case graph.ChangeChanged:
		return pgschema.ChangeKindChanged, nil
	case graph.ChangeRetired:
		return pgschema.ChangeKindRetired, nil
	}
	return 0, kernel.Errorf(kernel.CodeInvalidValue, "unknown kind of change %q", k)
}

// changeKindOf is the kind of change a number in the change entries means.
func changeKindOf(n int16) (graph.ChangeKind, error) {
	switch n {
	case pgschema.ChangeKindAdded:
		return graph.ChangeAdded, nil
	case pgschema.ChangeKindChanged:
		return graph.ChangeChanged, nil
	case pgschema.ChangeKindRetired:
		return graph.ChangeRetired, nil
	}
	return "", kernel.Errorf(kernel.CodeInternal, "a stored change entry has the kind %d", n)
}

// kgVolatileKey names one volatile value of the chunk.
type kgVolatileKey struct {
	object graph.ObjectID
	attr   int
}

// volatile writes the current values into the columns of the table of values that has the attribute (the common one or the one
// of the type), and what is said about each into its entry of prov, merged into what is there; of two values for one key in a
// chunk the later one stays. It never touches the table of the type's rows: a change of values alone is not a change of the
// object.
func (c *kgChunk) volatile(rows []graph.VolatileRow) {
	last := map[kgVolatileKey]graph.VolatileRow{}
	for _, r := range rows {
		last[kgVolatileKey{r.Object, r.AttrNo}] = r
	}
	keys := make([]kgVolatileKey, 0, len(last))
	for k := range last {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := c.objNo(keys[i].object), c.objNo(keys[j].object)
		return a < b || (a == b && keys[i].attr < keys[j].attr)
	})
	items := make([]volatileItem, 0, len(keys))
	for _, k := range keys {
		r := last[k]
		e, ok := c.reg[k.object.String()]
		if !ok {
			c.fail(kernel.Errorf(graph.CodeHeadMoved, "the object %s is not in the registry of the KG", k.object))
			return
		}
		items = append(items, volatileItem{objNo: e.objNo, typeNo: e.typeNo, attrNo: k.attr, value: r.Value,
			doc: provDoc{Grade: string(r.Grade), Basis: string(r.Basis), Seen: r.SeenAt, Ref: r.Ref}})
	}
	c.writeItems(items)
}

// writeItems writes the volatile values, a column at a time.
func (c *kgChunk) writeItems(items []volatileItem) {
	groups, err := c.a.groupValues(items)
	if err != nil {
		c.fail(err)
		return
	}
	for _, g := range groups {
		c.writeValues(g.table, g.common, g.attrNo, g.kind, g.items)
	}
}

// light writes the lines of the light change line, the later of two lines for one key winning.
func (c *kgChunk) light(lines []graph.LightChange) {
	type key struct {
		seq  int64
		no   int64
		attr int
	}
	last := map[key]graph.LightChange{}
	for _, l := range lines {
		last[key{l.Seq, c.objNo(l.Object), l.AttrNo}] = l
	}
	keys := make([]key, 0, len(last))
	for k := range last {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		return a.seq < b.seq || (a.seq == b.seq && (a.no < b.no || (a.no == b.no && a.attr < b.attr)))
	})
	slices(len(keys), func(lo, hi int) {
		var seqs, nos, attrs []int64
		var marks []string
		for _, k := range keys[lo:hi] {
			seqs, nos, attrs, marks = append(seqs, k.seq), append(nos, k.no), append(attrs, int64(k.attr)), append(marks, last[k].ValueMark)
		}
		c.exec("write light changes", `INSERT INTO `+c.a.t(pgschema.TableLightChange)+` (change_seq, obj_no, attr_no, value_mark)
			SELECT u.change_seq, u.obj_no, u.attr_no, u.value_mark FROM unnest($1::bigint[], $2::bigint[], $3::bigint[], $4::text[])
				AS u(change_seq, obj_no, attr_no, value_mark)
			ON CONFLICT (change_seq, obj_no, attr_no) DO UPDATE SET value_mark = EXCLUDED.value_mark`,
			pq.Array(seqs), pq.Array(nos), pq.Array(attrs), pq.Array(marks))
	})
}

// entries writes the entries of the applied change in the order the applier gave them.
func (c *kgChunk) entries(entries []graph.ChangeEntry) {
	slices(len(entries), func(lo, hi int) {
		var seqs, nos, ords []int64
		var kinds, changes []int64
		var olds, news [][]byte
		for i, e := range entries[lo:hi] {
			kind, no, err := c.entityOf(e)
			change, cerr := changeKindNo(e.Change)
			if err = firstErr(err, cerr); err != nil {
				c.fail(err)
				return
			}
			seqs, kinds, nos, changes = append(seqs, e.Seq), append(kinds, int64(kind)), append(nos, no), append(changes, int64(change))
			olds, news, ords = append(olds, optionalHash(e.OldHash)), append(news, optionalHash(e.NewHash)), append(ords, int64(lo+i))
		}
		c.exec("write the entries", `INSERT INTO `+c.a.t(pgschema.TableChangeEntry)+` (change_seq, entity_kind, entity_no, change_kind, old_hash, new_hash, ord)
			SELECT u.change_seq, u.entity_kind, u.entity_no, u.change_kind, NULLIF(u.old_hash, ''::bytea), NULLIF(u.new_hash, ''::bytea), u.ord
			FROM unnest($1::bigint[], $2::bigint[], $3::bigint[], $4::bigint[], $5::bytea[], $6::bytea[], $7::bigint[])
				AS u(change_seq, entity_kind, entity_no, change_kind, old_hash, new_hash, ord)`,
			pq.Array(seqs), pq.Array(kinds), pq.Array(nos), pq.Array(changes), pq.ByteaArray(olds), pq.ByteaArray(news), pq.Array(ords))
	})
}

// entityOf is the kind and the number of the entity an entry names.
func (c *kgChunk) entityOf(e graph.ChangeEntry) (int16, int64, error) {
	switch e.Entity {
	case entityNameObject:
		if r, ok := c.reg[e.ID]; ok {
			return pgschema.EntityKindObject, r.objNo, nil
		}
	case entityNameLink:
		if no, ok := c.linkNo[e.ID]; ok {
			return pgschema.EntityKindLink, no, nil
		}
	default:
		return 0, 0, kernel.Errorf(kernel.CodeInvalidValue, "unknown entity %q", e.Entity)
	}
	return 0, 0, kernel.Errorf(graph.CodeHeadMoved, "the entry names %s %s, which the chunk did not write", e.Entity, e.ID)
}

const kgBatchSQL = `INSERT INTO @ (change_seq, kind, source_id, apply_id, chunk_no, chunk_count, ingest_seq, attempt, applied_at, objects, links)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

func (c *kgChunk) batch(b graph.ChangeBatch) {
	c.exec("write the change batch", c.a.sub(kgBatchSQL, pgschema.TableChangeBatch), b.Seq, string(b.Kind), b.SourceID, b.ApplyID, b.ChunkNo, b.ChunkCount,
		b.IngestSeq, b.Attempt, pgTime(b.AppliedAt), b.Objects, b.Links)
}

func (c *kgChunk) order(e graph.OrderEvent) {
	c.exec("write the ordering event", c.a.sub(`INSERT INTO @ (ingest_seq, event_kind, ref, digest, change_seq) VALUES ($1, $2, $3, $4, $5)`, pgschema.TableIngestOrder),
		e.IngestSeq, string(e.Kind), e.Ref, hashBytes(e.Digest), e.ChangeSeq)
}
