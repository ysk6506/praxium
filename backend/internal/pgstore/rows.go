package pgstore

import (
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const objectColumns = `object_id, type_no, name, min_grade, attrs, source_refs, content_hash, seq_from`

// objectSelect is the objects valid at $3 whose id is above $2, in id order, at most $4. A row is valid in the
// live table when it began by then and in the history when it also ended after.
const objectSelect = `SELECT ` + objectColumns + `, 0::bigint AS seq_to FROM (
	(SELECT ` + objectColumns + ` FROM object_live WHERE kg_id = $1 AND object_id > $2 AND seq_from <= $3 ORDER BY object_id LIMIT $4)
	UNION ALL
	(SELECT ` + objectColumns + ` FROM object_hist WHERE kg_id = $1 AND object_id > $2 AND seq_from <= $3 AND seq_to > $3 ORDER BY object_id LIMIT $4)
) o ORDER BY object_id LIMIT $4`

func scanObject(sc scanner) (graph.ObjectRow, error) {
	var (
		r                 graph.ObjectRow
		id, grade         string
		attrs, refs, hash []byte
		typeNo            int
	)
	if err := sc.Scan(&id, &typeNo, &r.Name, &grade, &attrs, &refs, &hash, &r.SeqFrom, &r.SeqTo); err != nil {
		return r, err
	}
	var err error
	if r.ID, err = parseObject(id); err != nil {
		return r, err
	}
	r.TypeNo, r.MinGrade = typeNo, model.Grade(grade)
	if r.Attrs, err = decodeAttrs(attrs); err != nil {
		return r, err
	}
	if r.SourceRefs, err = refsOf(refs); err != nil {
		return r, err
	}
	r.ContentHash, err = hashOf(hash)
	return r, err
}

const linkSelectColumns = `link_id, link_type_no, src_object_id, dst_object_id, discriminator, grade, basis, attrs, source_refs, content_hash, seq_from`

// linkSelect is objectSelect for links with one more condition, on the ends, that may use $5 (an object id).
func linkSelect(where string) string {
	return `SELECT ` + linkSelectColumns + `, 0::bigint AS seq_to FROM (
	(SELECT ` + linkSelectColumns + ` FROM link_live WHERE kg_id = $1 AND link_id > $2 AND seq_from <= $3 AND ` + where + ` ORDER BY link_id LIMIT $4)
	UNION ALL
	(SELECT ` + linkSelectColumns + ` FROM link_hist WHERE kg_id = $1 AND link_id > $2 AND seq_from <= $3 AND seq_to > $3 AND ` + where + ` ORDER BY link_id LIMIT $4)
) l ORDER BY link_id LIMIT $4`
}

func scanLink(sc scanner) (graph.LinkRow, error) {
	var (
		r                          graph.LinkRow
		id, src, dst, grade, basis string
		attrs, refs, hash          []byte
		typeNo                     int
	)
	if err := sc.Scan(&id, &typeNo, &src, &dst, &r.Discriminator, &grade, &basis, &attrs, &refs, &hash, &r.SeqFrom, &r.SeqTo); err != nil {
		return r, err
	}
	var err error
	if r.ID, err = parseLink(id); err != nil {
		return r, err
	}
	if r.Src, err = parseObject(src); err != nil {
		return r, err
	}
	if r.Dst, err = parseObject(dst); err != nil {
		return r, err
	}
	r.TypeNo, r.Grade, r.Basis = typeNo, model.Grade(grade), model.Basis(basis)
	if r.Attrs, err = decodeAttrs(attrs); err != nil {
		return r, err
	}
	if r.SourceRefs, err = refsOf(refs); err != nil {
		return r, err
	}
	r.ContentHash, err = hashOf(hash)
	return r, err
}

func scanVolatile(sc scanner, id graph.ObjectID) (graph.VolatileRow, error) {
	var (
		r     = graph.VolatileRow{Object: id}
		value []byte
		g, b  string
	)
	if err := sc.Scan(&r.AttrNo, &value, &g, &b, &r.SeenAt, &r.Ref); err != nil {
		return r, err
	}
	var err error
	r.Grade, r.Basis = model.Grade(g), model.Basis(b)
	r.Value, err = decodeValue(value)
	return r, err
}

func scanIdentifier(sc scanner) (graph.IdentifierRecord, error) {
	var (
		r      graph.IdentifierRecord
		object string
		to     *int64
	)
	if err := sc.Scan(&object, &r.Scheme, &r.Value, &r.Scope, &r.SeqFrom, &to); err != nil {
		return r, err
	}
	var err error
	if r.Object, err = parseObject(object); err != nil {
		return r, err
	}
	if to != nil {
		r.SeqTo = *to
	}
	return r, nil
}

func scanEvent(sc scanner) (graph.IdentityEvent, error) {
	var (
		e                   graph.IdentityEvent
		kind, retired, into string
		links               []byte
	)
	if err := sc.Scan(&e.Seq, &kind, &retired, &into, &links); err != nil {
		return e, err
	}
	var err error
	e.Kind = graph.IdentityEventKind(kind)
	if e.Retired, err = parseObject(retired); err != nil {
		return e, err
	}
	if e.Into, err = parseObject(into); err != nil {
		return e, err
	}
	e.LinkMap, err = linkMapOf(links)
	return e, err
}
