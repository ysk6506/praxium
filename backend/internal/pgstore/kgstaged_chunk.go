package pgstore

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// stagedChunk is one chunk of a staged apply in its transaction. Its statements are in the order of the kinds of the
// difference (ADR-0099): the objects (the registry, the type tables, the history, the retirement), the keys, the
// links (those of an object that is retired first), the identifiers. $1 of every statement is the chunk number
// and $2 or $3 the change number.
type stagedChunk struct {
	*writer
	a      *kgAccess
	ap     graph.StagedApply
	from   graph.StagedProgress
	cur    kgHeadRow
	seq    int64
	counts map[[3]int16]int
	hash   graph.HashState
	digest [32]byte
	types  map[string]map[int]string
}

func newStagedChunk(ctx context.Context, tx *sql.Tx, a *kgAccess, ap graph.StagedApply, from graph.StagedProgress, cur kgHeadRow) *stagedChunk {
	return &stagedChunk{writer: newWriter(ctx, tx, ap.KG), a: a, ap: ap, from: from, cur: cur, seq: cur.head.AppliedSeq + 1,
		hash: cur.head.Hash.Clone(), counts: map[[3]int16]int{}, types: map[string]map[int]string{}}
}

func (c *stagedChunk) q(tpl string) string { return c.a.stagedSQL(tpl) }

// n is the number of rows of the chunk of a kind and a change, in every type.
func (c *stagedChunk) n(kind, change int16) int {
	total := 0
	for k, v := range c.counts {
		if k[0] == kind && k[2] == change {
			total += v
		}
	}
	return total
}

// typesOf lists the types of the rows of a kind that the chunk has, in the order of the numbers.
func (c *stagedChunk) typesOf(kind int16) []int {
	seen := map[int]bool{}
	var out []int
	for k := range c.counts {
		if k[0] == kind && !seen[int(k[1])] {
			seen[int(k[1])] = true
			out = append(out, int(k[1]))
		}
	}
	sort.Ints(out)
	return out
}

func (c *stagedChunk) nIn(kind, typeNo, change int16) int {
	return c.counts[[3]int16{kind, typeNo, change}]
}

// expect fails the chunk when a statement touched other rows than the difference named: the state is not the one the
// difference was made against.
func (c *stagedChunk) expect(what string, got, want int) {
	if c.err == nil && got != want {
		c.fail(kernel.Errorf(graph.CodeHeadMoved, "%s: %d of %d rows are not what the chunk was planned against", what, want-got, want))
	}
}

func (c *stagedChunk) run() {
	c.countRows()
	if c.err != nil {
		return
	}
	c.registry()
	c.marks()
	for _, t := range c.typesOf(pgschema.DiffKindObject) {
		c.closeObjects(t)
		c.openObjects(t)
	}
	c.objectEntries()
	c.keys()
	c.links()
	c.identifiers()
	c.digestAndBatch()
}

// countRows reads what the chunk holds, by kind, type and change. A chunk that has no row is a difference that was
// cut wrong.
func (c *stagedChunk) countRows() {
	rows, err := c.tx.QueryContext(c.ctx, c.q(`SELECT {kind}, {type_no}, {change_kind}, count(*) FROM {ingest_diff} WHERE {chunk_no} = $1 GROUP BY 1, 2, 3`), c.from.NextChunk)
	if err != nil {
		c.fail(dbErr(err, "count the rows of the chunk"))
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind, typeNo, change int16
		var n int
		if err := rows.Scan(&kind, &typeNo, &change, &n); err != nil {
			c.fail(dbErr(err, "count the rows of the chunk"))
			return
		}
		c.counts[[3]int16{kind, typeNo, change}] = n
	}
	c.fail(dbErr(rows.Err(), "count the rows of the chunk"))
	if c.err == nil && len(c.counts) == 0 {
		c.fail(kernel.Errorf(kernel.CodeInvalidValue, "the difference has no row in chunk %d", c.from.NextChunk))
	}
}

// registry keeps the registry of the objects: one that is new gets its row, one that is changed or comes back
// takes its name and grade and is live, one that is retired is marked retired. A row of the registry is never deleted.
func (c *stagedChunk) registry() {
	adds, changes, retires := c.n(pgschema.DiffKindObject, pgschema.ChangeKindAdded), c.n(pgschema.DiffKindObject, pgschema.ChangeKindChanged), c.n(pgschema.DiffKindObject, pgschema.ChangeKindRetired)
	if adds+changes+retires == 0 {
		return
	}
	updated := c.exec("update the registry", c.q(`UPDATE {object} o SET name = so.{name}, min_grade = so.{min_grade}, retired_seq = NULL
		FROM {ingest_diff} d JOIN {stage_object} so ON so.{obj_no} = d.{entity_no}
		WHERE d.{chunk_no} = $1 AND d.{kind} = {KO} AND d.{change_kind} IN ({CA}, {CC}) AND o.obj_no = so.{obj_no} AND o.type_no = so.{type_no}`), c.from.NextChunk)
	inserted := c.exec("register the new objects", c.q(`INSERT INTO {object} (obj_no, ref, type_no, name, min_grade, seq_from, retired_seq, first_key)
		SELECT so.{obj_no}, so.{ref}, so.{type_no}, so.{name}, so.{min_grade}, $2, NULL, so.{first_key}
		FROM {ingest_diff} d JOIN {stage_object} so ON so.{obj_no} = d.{entity_no}
		WHERE d.{chunk_no} = $1 AND d.{kind} = {KO} AND d.{change_kind} IN ({CA}, {CC})
			AND NOT EXISTS (SELECT 1 FROM {object} o WHERE o.obj_no = so.{obj_no})
		ORDER BY so.{obj_no}`), c.from.NextChunk, c.seq)
	c.expect("the registry of the objects that are added or changed", int(updated+inserted), adds+changes)
	retired := c.exec("retire in the registry", c.q(`UPDATE {object} o SET retired_seq = $2 FROM {ingest_diff} d
		WHERE d.{chunk_no} = $1 AND d.{kind} = {KO} AND d.{change_kind} = {CR} AND o.obj_no = d.{entity_no}`), c.from.NextChunk, c.seq)
	c.expect("the registry of the objects that are retired", int(retired), retires)
}

// marks keeps the mark of the objects that are held to retire for good (object_retire, ADR-0128): the objects of the chunk that a
// retirable reference of the run points at are marked, and then the objects the chunk retires lose their mark, in that order, so an object
// that the chunk both marks and retires has none, and one that comes back later by a feed that does not retire is not retired by a mark
// that stayed.
func (c *stagedChunk) marks() {
	if c.total(pgschema.DiffKindObject) == 0 {
		return
	}
	schema := c.a.rec.Schema()
	c.exec("mark the objects held to retire", pgschema.MarkRetirableChunkSQL(schema), c.from.NextChunk)
	if c.n(pgschema.DiffKindObject, pgschema.ChangeKindRetired) > 0 {
		c.exec("take the mark of the objects retired away", pgschema.UnmarkRetiredChunkSQL(schema), c.from.NextChunk)
	}
}

// hashed is the row hash of a row that went in or out of the table, with the type it is of.
type hashed struct {
	typeNo int
	hash   []byte
}

// hashRows runs a statement that returns (type number, row hash) for each row it wrote or closed.
func (c *stagedChunk) hashRows(what, query string, args ...any) []hashed {
	if c.err != nil {
		return nil
	}
	rows, err := c.tx.QueryContext(c.ctx, query, args...)
	if err != nil {
		c.fail(dbErr(err, what))
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []hashed
	for rows.Next() {
		var h hashed
		if err := rows.Scan(&h.typeNo, &h.hash); err != nil {
			c.fail(dbErr(err, what))
			return nil
		}
		out = append(out, h)
	}
	c.fail(dbErr(rows.Err(), what))
	return out
}

// typeID is the immutable id of a type by its number, the part of the hash state a row is in.
func (c *stagedChunk) typeID(entity string, typeNo int) string {
	m := c.types[entity]
	if m == nil {
		m = map[int]string{}
		c.types[entity] = m
		reg := c.a.model.Registry
		if entity == entityNameObject {
			for _, t := range reg.ObjectTypes() {
				m[t.TypeNo] = t.ID.String()
			}
		} else {
			for _, t := range reg.LinkTypes() {
				m[t.LinkTypeNo] = t.ID.String()
			}
		}
	}
	return m[typeNo]
}

// move counts rows in or out of the hash state of the head: the sum of the row hashes of the rows that went in and
// the rows that went out, by type. The row hash is the one the rows keep (made by Go when the row was staged).
func (c *stagedChunk) move(entity string, rows []hashed, remove bool) {
	for _, r := range rows {
		sum, err := sumFrom(r.hash)
		if err != nil {
			c.fail(err)
			return
		}
		key := graph.PartKey{Entity: entity, TypeID: c.typeID(entity, r.typeNo)}
		if remove {
			c.hash.Remove(key, sum)
		} else {
			c.hash.Add(key, sum)
		}
	}
}

// closeObjects closes the rows of a type that the chunk changes or retires: each moves to the history with the change
// number it was closed at. A row to close must be open with the content hash the difference names.
func (c *stagedChunk) closeObjects(typeNo int) {
	want := c.nIn(pgschema.DiffKindObject, int16(typeNo), pgschema.ChangeKindChanged) + c.nIn(pgschema.DiffKindObject, int16(typeNo), pgschema.ChangeKindRetired)
	if want == 0 {
		return
	}
	rows := c.hashRows("close objects", c.q(strings.ReplaceAll(`WITH gone AS (DELETE FROM {T} t USING {ingest_diff} d
			WHERE d.{chunk_no} = $1 AND d.{kind} = {KO} AND d.{type_no} = $2 AND d.{change_kind} IN ({CC}, {CR})
				AND t.obj_no = d.{entity_no} AND t.content_hash = d.{old_hash}
			RETURNING t.obj_no, t.name, t.min_grade, t.doc, t.source_refs, t.content_hash, t.row_hash, t.seq_from),
		hist AS (INSERT INTO {object_hist} (obj_no, type_no, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from, seq_to)
			SELECT obj_no, $2, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from, $3 FROM gone)
		SELECT $2::smallint, row_hash FROM gone`, "{T}", c.a.typeTable(typeNo))), c.from.NextChunk, typeNo, c.seq)
	c.expect("the objects to close", len(rows), want)
	c.move(entityNameObject, rows, true)
}

// openObjects opens the new rows of a type, in the order of the numbers, from the staged objects whose content hash is
// the one the difference names.
func (c *stagedChunk) openObjects(typeNo int) {
	want := c.nIn(pgschema.DiffKindObject, int16(typeNo), pgschema.ChangeKindAdded) + c.nIn(pgschema.DiffKindObject, int16(typeNo), pgschema.ChangeKindChanged)
	if want == 0 {
		return
	}
	rows := c.hashRows("open objects", c.q(strings.ReplaceAll(`INSERT INTO {T} (obj_no, ref, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from)
		SELECT so.{obj_no}, so.{ref}, so.{name}, so.{min_grade}, so.{doc}, so.{source_refs}, so.{content_hash}, so.{row_hash}, $3
		FROM {ingest_diff} d JOIN {stage_object} so ON so.{obj_no} = d.{entity_no} AND so.{content_hash} = d.{new_hash}
		WHERE d.{chunk_no} = $1 AND d.{kind} = {KO} AND d.{type_no} = $2 AND d.{change_kind} IN ({CA}, {CC})
		ORDER BY so.{obj_no}
		RETURNING $2::smallint, row_hash`, "{T}", c.a.typeTable(typeNo))), c.from.NextChunk, typeNo, c.seq)
	c.expect("the objects to open", len(rows), want)
	c.move(entityNameObject, rows, false)
}

// objectEntries writes the entries of the objects of the chunk, in the order of the difference. The entry of a row
// that is added or retired has one hash, and of a row that is changed two.
func (c *stagedChunk) objectEntries() {
	if c.total(pgschema.DiffKindObject) == 0 {
		return
	}
	c.exec("write the entries of the objects", c.q(`INSERT INTO {change_entry} (change_seq, entity_kind, entity_no, change_kind, old_hash, new_hash, ord)
		SELECT $2, {EO}, d.{entity_no}, d.{change_kind}, d.{old_hash}, d.{new_hash}, (row_number() OVER (ORDER BY d.{ord}))::int - 1
		FROM {ingest_diff} d WHERE d.{chunk_no} = $1 AND d.{kind} = {KO}`), c.from.NextChunk, c.seq)
}

func (c *stagedChunk) total(kind int16) int {
	n := 0
	for k, v := range c.counts {
		if k[0] == kind {
			n += v
		}
	}
	return n
}

// keys binds the keys of the difference to their objects (the identification index). A key that is bound stays with
// its object, so a key that is bound again changes nothing. The index has no text of the key: it is for diagnosis,
// and the difference does not carry it.
func (c *stagedChunk) keys() {
	if c.total(pgschema.DiffKindKey) == 0 {
		return
	}
	c.exec("bind the keys", c.q(`INSERT INTO {object_key} (type_no, ctx_no, sig_hash, obj_no, bound_seq, sig)
		SELECT d.{type_no}, d.{ctx_no}, d.{sig_hash}, d.{entity_no}, $2, NULL FROM {ingest_diff} d
		WHERE d.{chunk_no} = $1 AND d.{kind} = {KK} AND d.{change_kind} = {CA}
		ORDER BY d.{type_no}, d.{ctx_no}, d.{sig_hash}
		ON CONFLICT (type_no, ctx_no, sig_hash) DO NOTHING`), c.from.NextChunk, c.seq)
}

// identifiers closes the identifiers the difference withdraws and opens the new ones. A row of the difference names
// an identifier by the hash of its scheme, value and scope (pgschema.IdentRowHash), so the line has a fixed width.
func (c *stagedChunk) identifiers() {
	closes, opens := c.n(pgschema.DiffKindIdentifier, pgschema.ChangeKindRetired), c.n(pgschema.DiffKindIdentifier, pgschema.ChangeKindAdded)
	if closes > 0 {
		got := c.exec("close identifiers", c.q(`UPDATE {object_identifier} oi SET seq_to = $2 FROM {ingest_diff} d
			WHERE d.{chunk_no} = $1 AND d.{kind} = {KI} AND d.{change_kind} = {CR} AND oi.obj_no = d.{entity_no} AND oi.seq_to IS NULL
				AND `+pgschema.IdentRowHashExpr("oi")+` = d.{old_hash}`), c.from.NextChunk, c.seq)
		c.expect("the identifiers to close", int(got), closes)
	}
	if opens > 0 {
		got := c.exec("open identifiers", c.q(`INSERT INTO {object_identifier} (obj_no, scheme, value, scope, seq_from, seq_to)
			SELECT si.{obj_no}, si.{scheme}, si.{value}, si.{scope}, $2, NULL
			FROM {ingest_diff} d JOIN {stage_ident} si ON si.{obj_no} = d.{entity_no} AND `+pgschema.IdentRowHashExpr("si")+` = d.{new_hash}
			WHERE d.{chunk_no} = $1 AND d.{kind} = {KI} AND d.{change_kind} = {CA}`), c.from.NextChunk, c.seq)
		c.expect("the identifiers to open", int(got), opens)
	}
}
