package pgstore

import (
	"encoding/json"
	"sort"
	"strconv"

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

// volatile writes the current values into the table of values that has the attribute (the common one or the one of
// the type) as entries of prov, merged into what is there; of two values for one key in a chunk the later one stays.
// It never touches the table of the type's rows: a change of values alone is not a change of the object.
func (c *kgChunk) volatile(rows []graph.VolatileRow) {
	last := map[kgVolatileKey]graph.VolatileRow{}
	for _, r := range rows {
		last[kgVolatileKey{r.Object, r.AttrNo}] = r
	}
	provs := map[kgProvTarget]map[int64]map[string]provEntry{}
	for k, r := range last {
		t, e, err := c.provTarget(k)
		if err == nil {
			err = addProv(provs, t, e.objNo, k.attr, r)
		}
		if err != nil {
			c.fail(err)
			return
		}
	}
	targets := make([]kgProvTarget, 0, len(provs))
	for t := range provs {
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].table < targets[j].table || (targets[i].table == targets[j].table && targets[i].typeNo < targets[j].typeNo)
	})
	for _, t := range targets {
		c.writeProv(t.table, t.typeNo, t.table == pgschema.TableObjectState, provs[t])
	}
}

// kgProvTarget is one table of values: the common one or the one of a type.
type kgProvTarget struct {
	table  string
	typeNo int
}

// provTarget finds the table of values that has the attribute of the key, and the registry entry of its object.
func (c *kgChunk) provTarget(k kgVolatileKey) (kgProvTarget, regEntry, error) {
	e, ok := c.reg[k.object.String()]
	if !ok {
		return kgProvTarget{}, e, kernel.Errorf(graph.CodeHeadMoved, "the object %s is not in the registry of the KG", k.object)
	}
	if _, common := c.a.common[k.attr]; common {
		return kgProvTarget{pgschema.TableObjectState, e.typeNo}, e, nil
	}
	if _, own := c.a.state[e.typeNo][k.attr]; !own {
		return kgProvTarget{}, e, kernel.Errorf(kernel.CodeInvalidValue, "the attribute %d of %s has no table of values", k.attr, k.object)
	}
	return kgProvTarget{pgschema.StateTable(e.typeNo), e.typeNo}, e, nil
}

// addProv puts the prov entry of one value in the entries of its table.
func addProv(provs map[kgProvTarget]map[int64]map[string]provEntry, t kgProvTarget, objNo int64, attr int, r graph.VolatileRow) error {
	raw, err := encodeValue(r.Value)
	if err != nil {
		return err
	}
	if provs[t] == nil {
		provs[t] = map[int64]map[string]provEntry{}
	}
	if provs[t][objNo] == nil {
		provs[t][objNo] = map[string]provEntry{}
	}
	provs[t][objNo][strconv.Itoa(attr)] = provEntry{Value: json.RawMessage(raw), Grade: string(r.Grade), Basis: string(r.Basis), Seen: r.SeenAt, Ref: r.Ref}
	return nil
}

// writeProv upserts the prov entries of the objects of one table of values.
func (c *kgChunk) writeProv(table string, typeNo int, common bool, byObj map[int64]map[string]provEntry) {
	nos := make([]int64, 0, len(byObj))
	for no := range byObj {
		nos = append(nos, no)
	}
	sort.Slice(nos, func(i, j int) bool { return nos[i] < nos[j] })
	slices(len(nos), func(lo, hi int) {
		var docs []string
		var types []int64
		for _, no := range nos[lo:hi] {
			raw, err := marshal(byObj[no])
			if err != nil {
				c.fail(err)
				return
			}
			docs, types = append(docs, string(raw)), append(types, int64(typeNo))
		}
		if common {
			c.exec("write volatile values", `INSERT INTO `+c.a.t(table)+` (obj_no, type_no, prov)
				SELECT u.obj_no, u.type_no, u.prov::jsonb FROM unnest($1::bigint[], $2::bigint[], $3::text[]) AS u(obj_no, type_no, prov)
				ON CONFLICT (obj_no) DO UPDATE SET prov = `+c.a.t(table)+`.prov || EXCLUDED.prov`, pq.Array(nos[lo:hi]), pq.Array(types), pq.Array(docs))
			return
		}
		c.exec("write volatile values", `INSERT INTO `+c.a.t(table)+` (obj_no, prov)
			SELECT u.obj_no, u.prov::jsonb FROM unnest($1::bigint[], $2::text[]) AS u(obj_no, prov)
			ON CONFLICT (obj_no) DO UPDATE SET prov = `+c.a.t(table)+`.prov || EXCLUDED.prov`, pq.Array(nos[lo:hi]), pq.Array(docs))
	})
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
