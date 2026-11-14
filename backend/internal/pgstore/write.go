package pgstore

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// batchRows is the most rows one statement carries. A chunk can hold many more (the volatile values of a first
// load come with its last chunk), and a statement of that size would be a very large message.
const batchRows = 5000

// writer runs the statements of one transaction and keeps the first error: after it, every call does nothing,
// so a sequence of writes is written straight and its error is read once at the end.
type writer struct {
	ctx context.Context
	tx  *sql.Tx
	kg  string
	err error
}

func newWriter(ctx context.Context, tx *sql.Tx, kg graph.KGID) *writer {
	return &writer{ctx: ctx, tx: tx, kg: kg.String()}
}

// exec runs one statement and returns the rows it touched.
func (w *writer) exec(what, query string, args ...any) int64 {
	if w.err != nil {
		return 0
	}
	res, err := w.tx.ExecContext(w.ctx, query, args...)
	if err != nil {
		w.err = dbErr(err, what)
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// fail records an error that is not the database's.
func (w *writer) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

// slices calls fn for each run of at most batchRows rows of n.
func slices(n int, fn func(lo, hi int)) {
	for lo := 0; lo < n; lo += batchRows {
		fn(lo, min(lo+batchRows, n))
	}
}

const closeObjectsSQL = `WITH gone AS (
	DELETE FROM object_live o USING unnest($2::text[], $3::bytea[]) AS c(object_id, content_hash)
	WHERE o.kg_id = $1 AND o.object_id = c.object_id AND o.content_hash = c.content_hash
	RETURNING o.object_id, o.type_no, o.name, o.min_grade, o.attrs, o.source_refs, o.content_hash, o.seq_from)
INSERT INTO object_hist (kg_id, object_id, type_no, name, min_grade, attrs, source_refs, content_hash, seq_from, seq_to)
SELECT $1, object_id, type_no, name, min_grade, attrs, source_refs, content_hash, seq_from, $4 FROM gone`

const openObjectsSQL = `INSERT INTO object_live (kg_id, object_id, type_no, name, min_grade, attrs, source_refs, content_hash, seq_from)
SELECT $1, u.object_id, u.type_no, u.name, u.min_grade, u.attrs::jsonb, u.source_refs::jsonb, u.content_hash, u.seq_from
FROM unnest($2::text[], $3::bigint[], $4::text[], $5::text[], $6::text[], $7::text[], $8::bytea[], $9::bigint[])
	AS u(object_id, type_no, name, min_grade, attrs, source_refs, content_hash, seq_from)`

// objects closes the rows the chunk replaces or retires and opens the new ones. A row to close must be open with
// the content hash the chunk was planned against, or the head has moved.
func (w *writer) objects(changes []graph.ObjectChange, seq int64) {
	var oldIDs []string
	var oldHashes [][]byte
	var fresh []graph.ObjectRow
	for _, c := range changes {
		if c.Old != nil {
			oldIDs, oldHashes = append(oldIDs, c.Old.ID.String()), append(oldHashes, hashBytes(c.Old.ContentHash))
		}
		if c.New != nil {
			fresh = append(fresh, *c.New)
		}
	}
	slices(len(oldIDs), func(lo, hi int) {
		got := w.exec("close objects", closeObjectsSQL, w.kg, pq.Array(oldIDs[lo:hi]), pq.ByteaArray(oldHashes[lo:hi]), seq)
		if w.err == nil && got != int64(hi-lo) {
			w.fail(kernel.Errorf(graph.CodeHeadMoved, "%d of %d objects are not the open rows the chunk was planned against", int64(hi-lo)-got, hi-lo))
		}
	})
	slices(len(fresh), func(lo, hi int) { w.openObjects(fresh[lo:hi]) })
}

func (w *writer) openObjects(rows []graph.ObjectRow) {
	var a struct {
		ids, names, grades, attrs, refs []string
		types, from                     []int64
		hashes                          [][]byte
	}
	for _, r := range rows {
		attrs, err := encodeAttrs(r.Attrs)
		if err == nil {
			var refs []byte
			if refs, err = refsJSON(r.SourceRefs); err == nil {
				a.refs = append(a.refs, string(refs))
			}
		}
		if err != nil {
			w.fail(err)
			return
		}
		a.ids, a.names, a.grades, a.attrs = append(a.ids, r.ID.String()), append(a.names, r.Name), append(a.grades, string(r.MinGrade)), append(a.attrs, string(attrs))
		a.types, a.from, a.hashes = append(a.types, int64(r.TypeNo)), append(a.from, r.SeqFrom), append(a.hashes, hashBytes(r.ContentHash))
	}
	w.exec("open objects", openObjectsSQL, w.kg, pq.Array(a.ids), pq.Array(a.types), pq.Array(a.names), pq.Array(a.grades),
		pq.Array(a.attrs), pq.Array(a.refs), pq.ByteaArray(a.hashes), pq.Array(a.from))
}

const linkColumns = `link_id, link_type_no, src_object_id, dst_object_id, discriminator, grade, basis, attrs, source_refs, content_hash, seq_from`

const closeLinksSQL = `WITH gone AS (
	DELETE FROM link_live o USING unnest($2::text[], $3::bytea[]) AS c(link_id, content_hash)
	WHERE o.kg_id = $1 AND o.link_id = c.link_id AND o.content_hash = c.content_hash
	RETURNING o.link_id, o.link_type_no, o.src_object_id, o.dst_object_id, o.discriminator, o.grade, o.basis, o.attrs, o.source_refs, o.content_hash, o.seq_from)
INSERT INTO link_hist (kg_id, ` + linkColumns + `, seq_to)
SELECT $1, ` + linkColumns + `, $4 FROM gone`

const openLinksSQL = `INSERT INTO link_live (kg_id, ` + linkColumns + `)
SELECT $1, u.link_id, u.link_type_no, u.src, u.dst, u.discriminator, u.grade, u.basis, u.attrs::jsonb, u.source_refs::jsonb, u.content_hash, u.seq_from
FROM unnest($2::text[], $3::bigint[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::bytea[], $12::bigint[])
	AS u(link_id, link_type_no, src, dst, discriminator, grade, basis, attrs, source_refs, content_hash, seq_from)`

// links is objects for links.
func (w *writer) links(changes []graph.LinkChange, seq int64) {
	var oldIDs []string
	var oldHashes [][]byte
	var fresh []graph.LinkRow
	for _, c := range changes {
		if c.Old != nil {
			oldIDs, oldHashes = append(oldIDs, c.Old.ID.String()), append(oldHashes, hashBytes(c.Old.ContentHash))
		}
		if c.New != nil {
			fresh = append(fresh, *c.New)
		}
	}
	slices(len(oldIDs), func(lo, hi int) {
		got := w.exec("close links", closeLinksSQL, w.kg, pq.Array(oldIDs[lo:hi]), pq.ByteaArray(oldHashes[lo:hi]), seq)
		if w.err == nil && got != int64(hi-lo) {
			w.fail(kernel.Errorf(graph.CodeHeadMoved, "%d of %d links are not the open rows the chunk was planned against", int64(hi-lo)-got, hi-lo))
		}
	})
	slices(len(fresh), func(lo, hi int) { w.openLinks(fresh[lo:hi]) })
}

func (w *writer) openLinks(rows []graph.LinkRow) {
	var a struct {
		ids, srcs, dsts, discs, grades, bases, attrs, refs []string
		types, from                                        []int64
		hashes                                             [][]byte
	}
	for _, r := range rows {
		attrs, err := encodeAttrs(r.Attrs)
		if err == nil {
			var refs []byte
			if refs, err = refsJSON(r.SourceRefs); err == nil {
				a.refs = append(a.refs, string(refs))
			}
		}
		if err != nil {
			w.fail(err)
			return
		}
		a.ids, a.srcs, a.dsts, a.discs = append(a.ids, r.ID.String()), append(a.srcs, r.Src.String()), append(a.dsts, r.Dst.String()), append(a.discs, r.Discriminator)
		a.grades, a.bases, a.attrs = append(a.grades, string(r.Grade)), append(a.bases, string(r.Basis)), append(a.attrs, string(attrs))
		a.types, a.from, a.hashes = append(a.types, int64(r.TypeNo)), append(a.from, r.SeqFrom), append(a.hashes, hashBytes(r.ContentHash))
	}
	w.exec("open links", openLinksSQL, w.kg, pq.Array(a.ids), pq.Array(a.types), pq.Array(a.srcs), pq.Array(a.dsts), pq.Array(a.discs),
		pq.Array(a.grades), pq.Array(a.bases), pq.Array(a.attrs), pq.Array(a.refs), pq.ByteaArray(a.hashes), pq.Array(a.from))
}

const volatileSQL = `INSERT INTO attr_volatile (kg_id, object_id, attr_no, value, grade, basis, seen_at, ref)
SELECT $1, u.object_id, u.attr_no, u.value::jsonb, u.grade, u.basis, u.seen_at, u.ref
FROM unnest($2::text[], $3::bigint[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[]) AS u(object_id, attr_no, value, grade, basis, seen_at, ref)
ON CONFLICT (kg_id, object_id, attr_no) DO UPDATE SET value = EXCLUDED.value, grade = EXCLUDED.grade, basis = EXCLUDED.basis,
	seen_at = EXCLUDED.seen_at, ref = EXCLUDED.ref`

type volatileKey struct {
	object graph.ObjectID
	attr   int
}

// volatile writes the current values; of two values for one key in a chunk the later one stays, as when they
// are written one after the other.
func (w *writer) volatile(rows []graph.VolatileRow) {
	last := map[volatileKey]int{}
	for i, r := range rows {
		last[volatileKey{r.Object, r.AttrNo}] = i
	}
	var keep []graph.VolatileRow
	for i, r := range rows {
		if last[volatileKey{r.Object, r.AttrNo}] == i {
			keep = append(keep, r)
		}
	}
	slices(len(keep), func(lo, hi int) {
		var ids, values, grades, bases, seen, refs []string
		var nos []int64
		for _, r := range keep[lo:hi] {
			raw, err := encodeValue(r.Value)
			if err != nil {
				w.fail(err)
				return
			}
			ids, nos, values = append(ids, r.Object.String()), append(nos, int64(r.AttrNo)), append(values, string(raw))
			grades, bases, seen, refs = append(grades, string(r.Grade)), append(bases, string(r.Basis)), append(seen, r.SeenAt), append(refs, r.Ref)
		}
		w.exec("write volatile values", volatileSQL, w.kg, pq.Array(ids), pq.Array(nos), pq.Array(values), pq.Array(grades), pq.Array(bases), pq.Array(seen), pq.Array(refs))
	})
}

const lightSQL = `INSERT INTO light_change (kg_id, change_seq, object_id, attr_no, value_mark)
SELECT $1, u.change_seq, u.object_id, u.attr_no, u.value_mark
FROM unnest($2::bigint[], $3::text[], $4::bigint[], $5::text[]) AS u(change_seq, object_id, attr_no, value_mark)
ON CONFLICT (kg_id, change_seq, object_id, attr_no) DO UPDATE SET value_mark = EXCLUDED.value_mark`

type lightKey struct {
	seq    int64
	object graph.ObjectID
	attr   int
}

// light writes the lines of the light change line, the later of two lines for one key winning.
func (w *writer) light(lines []graph.LightChange) {
	last := map[lightKey]int{}
	for i, l := range lines {
		last[lightKey{l.Seq, l.Object, l.AttrNo}] = i
	}
	var keep []graph.LightChange
	for i, l := range lines {
		if last[lightKey{l.Seq, l.Object, l.AttrNo}] == i {
			keep = append(keep, l)
		}
	}
	slices(len(keep), func(lo, hi int) {
		var seqs, nos []int64
		var ids, marks []string
		for _, l := range keep[lo:hi] {
			seqs, ids, nos, marks = append(seqs, l.Seq), append(ids, l.Object.String()), append(nos, int64(l.AttrNo)), append(marks, l.ValueMark)
		}
		w.exec("write light changes", lightSQL, w.kg, pq.Array(seqs), pq.Array(ids), pq.Array(nos), pq.Array(marks))
	})
}

const entriesSQL = `INSERT INTO change_entry (kg_id, change_seq, entity_kind, entity_id, change_kind, old_hash, new_hash, ord)
SELECT $1, u.change_seq, u.entity_kind, u.entity_id, u.change_kind, NULLIF(u.old_hash, ''::bytea), NULLIF(u.new_hash, ''::bytea), u.ord
FROM unnest($2::bigint[], $3::text[], $4::text[], $5::text[], $6::bytea[], $7::bytea[], $8::bigint[])
	AS u(change_seq, entity_kind, entity_id, change_kind, old_hash, new_hash, ord)`

// entries writes the entries of the applied change in the order the applier gave them.
func (w *writer) entries(entries []graph.ChangeEntry) {
	slices(len(entries), func(lo, hi int) {
		var seqs, ords []int64
		var kinds, ids, changes []string
		var olds, news [][]byte
		for i, e := range entries[lo:hi] {
			seqs, kinds, ids, changes = append(seqs, e.Seq), append(kinds, e.Entity), append(ids, e.ID), append(changes, string(e.Change))
			olds, news, ords = append(olds, optionalHash(e.OldHash)), append(news, optionalHash(e.NewHash)), append(ords, int64(lo+i))
		}
		w.exec("write the entries", entriesSQL, w.kg, pq.Array(seqs), pq.Array(kinds), pq.Array(ids), pq.Array(changes),
			pq.ByteaArray(olds), pq.ByteaArray(news), pq.Array(ords))
	})
}

// optionalHash is the zero hash as no bytes (stored as null) and any other as its bytes.
func optionalHash(h kernel.Hash) []byte {
	if h == (kernel.Hash{}) {
		return []byte{}
	}
	return hashBytes(h)
}
