package pgstore

import (
	"sort"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// registerObjects keeps the registry: an object that is new gets a number (or takes its own again when it comes
// back after it was retired: the number and the type of a ref never change, M8), a changed or returning object
// takes its name and its grade, a retired one is marked retired. A row of the registry is never deleted here.
func (c *kgChunk) registerObjects(changes []graph.ObjectChange) {
	var fresh, reopen []graph.ObjectChange
	var retired []int64
	for _, ch := range changes {
		switch {
		case ch.New == nil && ch.Old != nil:
			retired = append(retired, c.objNo(ch.Old.ID))
		case ch.Old != nil:
			reopen = append(reopen, ch)
		case c.has(ch.New.ID):
			if c.reg[ch.New.ID.String()].typeNo != ch.New.TypeNo {
				c.fail(kernel.Errorf(graph.CodeHeadMoved, "the object %s came back with another type", ch.New.ID))
			}
			reopen = append(reopen, ch)
		default:
			fresh = append(fresh, ch)
		}
	}
	c.insertObjects(fresh)
	c.reopenObjects(reopen)
	if len(retired) > 0 {
		c.exec("retire in the registry", c.a.sub(`UPDATE @ SET retired_seq = $1 WHERE obj_no = ANY($2)`, pgschema.TableObject), c.seq, pq.Array(retired))
	}
}

func (c *kgChunk) has(id graph.ObjectID) bool { _, ok := c.reg[id.String()]; return ok }

// allocate takes n numbers from the function that gives them (ADR-0089 decision 29), in ascending order.
func (c *kgChunk) allocate(fn string, n int) []int64 {
	if c.err != nil || n == 0 {
		return nil
	}
	rows, err := c.tx.QueryContext(c.ctx, `SELECT n FROM public.`+fn+`($1) AS t(n) ORDER BY n`, n)
	if err != nil {
		c.fail(dbErr(err, "take numbers"))
		return nil
	}
	defer rows.Close()
	out := make([]int64, 0, n)
	for rows.Next() {
		var no int64
		if err := rows.Scan(&no); err != nil {
			c.fail(dbErr(err, "take numbers"))
			return nil
		}
		out = append(out, no)
	}
	c.fail(dbErr(rows.Err(), "take numbers"))
	return out
}

// insertObjects gives the new objects their numbers in the order of their refs and writes their registry rows.
func (c *kgChunk) insertObjects(fresh []graph.ObjectChange) {
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].New.ID.String() < fresh[j].New.ID.String() })
	nos := c.allocate("praxium_next_obj_nos", len(fresh))
	if c.err != nil {
		return
	}
	var refs, names, keys []string
	var types, grades []int64
	for i, ch := range fresh {
		grade, err := gradeNo(ch.New.MinGrade)
		key, kerr := keyJSON(ch.Key)
		if err != nil || kerr != nil {
			c.fail(kernel.Wrap(kernel.CodeInvalidValue, firstErr(err, kerr), "an object cannot be put in the registry"))
			return
		}
		text, _ := key.(string)
		c.reg[ch.New.ID.String()] = regEntry{ref: ch.New.ID.String(), objNo: nos[i], typeNo: ch.New.TypeNo}
		refs, names, keys = append(refs, ch.New.ID.String()), append(names, ch.New.Name), append(keys, text)
		types, grades = append(types, int64(ch.New.TypeNo)), append(grades, int64(grade))
	}
	slices(len(refs), func(lo, hi int) {
		c.exec("write the registry", `INSERT INTO `+c.a.t(pgschema.TableObject)+` (obj_no, ref, type_no, name, min_grade, seq_from, retired_seq, first_key)
			SELECT u.obj_no, u.ref, u.type_no, u.name, u.min_grade, $1, NULL, NULLIF(u.first_key, '')::jsonb
			FROM unnest($2::bigint[], $3::text[], $4::bigint[], $5::text[], $6::bigint[], $7::text[]) AS u(obj_no, ref, type_no, name, min_grade, first_key)`,
			c.seq, pq.Array(nos[lo:hi]), pq.Array(refs[lo:hi]), pq.Array(types[lo:hi]), pq.Array(names[lo:hi]), pq.Array(grades[lo:hi]), pq.Array(keys[lo:hi]))
	})
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// reopenObjects writes the name and the grade of the objects that changed or came back, and makes them live.
func (c *kgChunk) reopenObjects(changes []graph.ObjectChange) {
	var nos, grades []int64
	var names []string
	for _, ch := range changes {
		grade, err := gradeNo(ch.New.MinGrade)
		if err != nil {
			c.fail(err)
			return
		}
		nos, names, grades = append(nos, c.objNo(ch.New.ID)), append(names, ch.New.Name), append(grades, int64(grade))
	}
	slices(len(nos), func(lo, hi int) {
		c.exec("update the registry", c.a.sub(`UPDATE @ o SET name = u.name, min_grade = u.min_grade, retired_seq = NULL
			FROM unnest($1::bigint[], $2::text[], $3::bigint[]) AS u(obj_no, name, min_grade) WHERE o.obj_no = u.obj_no`, pgschema.TableObject),
			pq.Array(nos[lo:hi]), pq.Array(names[lo:hi]), pq.Array(grades[lo:hi]))
	})
}

// objectPut is a row to close or to open with the number of its object.
type objectPut struct {
	no  int64
	row graph.ObjectRow
}

// objects closes the rows the chunk replaces or retires and opens the new ones, a type at a time, each in the order
// of the numbers (ADR-0089 decision 31). A row to close must be open with the content hash the chunk was planned
// against, or the head has moved.
func (c *kgChunk) objects(changes []graph.ObjectChange) {
	closes, opens := map[int][]objectPut{}, map[int][]objectPut{}
	var types []int
	for _, ch := range changes {
		if ch.Old != nil {
			e := c.reg[ch.Old.ID.String()]
			closes[e.typeNo] = append(closes[e.typeNo], objectPut{c.objNo(ch.Old.ID), *ch.Old})
		}
		if ch.New != nil {
			opens[ch.New.TypeNo] = append(opens[ch.New.TypeNo], objectPut{c.objNo(ch.New.ID), *ch.New})
		}
	}
	for t := range closes {
		types = append(types, t)
	}
	for t := range opens {
		if _, both := closes[t]; !both {
			types = append(types, t)
		}
	}
	sort.Ints(types)
	for _, t := range types {
		c.closeObjects(t, byNo(closes[t]))
		c.openObjects(t, byNo(opens[t]))
	}
}

func byNo(rows []objectPut) []objectPut {
	sort.Slice(rows, func(i, j int) bool { return rows[i].no < rows[j].no })
	return rows
}

func (c *kgChunk) closeObjects(typeNo int, rows []objectPut) {
	query := `WITH gone AS (DELETE FROM ` + c.a.typeTable(typeNo) + ` t USING unnest($1::bigint[], $2::bytea[]) AS c(obj_no, content_hash)
			WHERE t.obj_no = c.obj_no AND t.content_hash = c.content_hash
			RETURNING t.obj_no, t.name, t.min_grade, t.doc, t.source_refs, t.content_hash, t.row_hash, t.seq_from)
		INSERT INTO ` + c.a.t(pgschema.TableObjectHist) + ` (obj_no, type_no, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from, seq_to)
		SELECT obj_no, $3, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from, $4 FROM gone`
	slices(len(rows), func(lo, hi int) {
		var nos []int64
		var hashes [][]byte
		for _, r := range rows[lo:hi] {
			nos, hashes = append(nos, r.no), append(hashes, hashBytes(r.row.ContentHash))
		}
		got := c.exec("close objects", query, pq.Array(nos), pq.ByteaArray(hashes), typeNo, c.seq)
		if c.err == nil && got != int64(hi-lo) {
			c.fail(kernel.Errorf(graph.CodeHeadMoved, "%d of %d objects are not the open rows the chunk was planned against", int64(hi-lo)-got, hi-lo))
		}
	})
}

func (c *kgChunk) openObjects(typeNo int, rows []objectPut) {
	slices(len(rows), func(lo, hi int) {
		var a struct {
			refs, names, docs, srcs []string
			nos, grades, from       []int64
			content, sums           [][]byte
		}
		for _, p := range rows[lo:hi] {
			r := p.row
			doc, err := encodeAttrs(r.Attrs)
			refs, rerr := refsJSON(r.SourceRefs)
			grade, gerr := gradeNo(r.MinGrade)
			sum, serr := graph.RowSum(entityNameObject, r.ID.String(), r.ContentHash)
			if err = firstErr(err, rerr, gerr, serr); err != nil {
				c.fail(err)
				return
			}
			a.nos, a.refs, a.names, a.grades = append(a.nos, p.no), append(a.refs, r.ID.String()), append(a.names, r.Name), append(a.grades, int64(grade))
			a.docs, a.srcs, a.content, a.sums = append(a.docs, string(doc)), append(a.srcs, string(refs)), append(a.content, hashBytes(r.ContentHash)), append(a.sums, sumBytes(sum))
			a.from = append(a.from, r.SeqFrom)
		}
		c.exec("open objects", `INSERT INTO `+c.a.typeTable(typeNo)+` (obj_no, ref, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from)
			SELECT u.obj_no, u.ref, u.name, u.min_grade, u.doc::jsonb, u.source_refs::jsonb, u.content_hash, u.row_hash, u.seq_from
			FROM unnest($1::bigint[], $2::text[], $3::text[], $4::bigint[], $5::text[], $6::text[], $7::bytea[], $8::bytea[], $9::bigint[])
				AS u(obj_no, ref, name, min_grade, doc, source_refs, content_hash, row_hash, seq_from)`,
			pq.Array(a.nos), pq.Array(a.refs), pq.Array(a.names), pq.Array(a.grades), pq.Array(a.docs), pq.Array(a.srcs),
			pq.ByteaArray(a.content), pq.ByteaArray(a.sums), pq.Array(a.from))
	})
}

// keys binds the identity keys of the objects that are new to their numbers (the identification index). A key that
// is bound stays with its object, so a key that is bound again (the object came back) changes nothing.
func (c *kgChunk) keys(changes []graph.ObjectChange) {
	var types, ctxs, nos []int64
	var hashes [][]byte
	var sigs []string
	for _, ch := range changes {
		if ch.Old != nil || ch.New == nil {
			continue
		}
		ctxRef, hash, sig, ok, err := keySignature(ch.Key)
		if err != nil {
			c.fail(err)
			return
		}
		if !ok {
			continue
		}
		ctxNo := int64(0)
		if ctxRef != "" {
			ctxNo = c.reg[ctxRef].objNo
		}
		types, ctxs, nos = append(types, int64(ch.New.TypeNo)), append(ctxs, ctxNo), append(nos, c.objNo(ch.New.ID))
		hashes, sigs = append(hashes, hash), append(sigs, sig)
	}
	slices(len(nos), func(lo, hi int) {
		c.exec("bind the keys", `INSERT INTO `+c.a.t(pgschema.TableObjectKey)+` (type_no, ctx_no, sig_hash, obj_no, bound_seq, sig)
			SELECT u.type_no, u.ctx_no, u.sig_hash, u.obj_no, $1, u.sig FROM unnest($2::bigint[], $3::bigint[], $4::bytea[], $5::bigint[], $6::text[])
				AS u(type_no, ctx_no, sig_hash, obj_no, sig) ON CONFLICT (type_no, ctx_no, sig_hash) DO NOTHING`,
			c.seq, pq.Array(types[lo:hi]), pq.Array(ctxs[lo:hi]), pq.ByteaArray(hashes[lo:hi]), pq.Array(nos[lo:hi]), pq.Array(sigs[lo:hi]))
	})
}
