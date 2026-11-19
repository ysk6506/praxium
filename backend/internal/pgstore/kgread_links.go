package pgstore

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// qb collects the arguments of a statement and hands out their placeholders.
type qb struct{ args []any }

func (q *qb) arg(v any) string {
	q.args = append(q.args, v)
	return "$" + strconv.Itoa(len(q.args))
}

const kgLinkColumns = `l.link_no, l.ref AS ref, l.type_no, so.ref AS src_ref, de.ref AS dst_ref, l.discriminator, l.grade, l.basis, l.doc,
	l.source_refs, l.content_hash, l.seq_from, 0::bigint AS seq_to`

func scanKGLink(sc scanner) (int64, graph.LinkRow, error) {
	var (
		r                          graph.LinkRow
		linkNo                     int64
		id, src, dst, grade, basis string
		attrs, refs, hash          []byte
		typeNo                     int
	)
	if err := sc.Scan(&linkNo, &id, &typeNo, &src, &dst, &r.Discriminator, &grade, &basis, &attrs, &refs, &hash, &r.SeqFrom, &r.SeqTo); err != nil {
		return 0, r, err
	}
	var err error
	if r.ID, err = parseLink(id); err != nil {
		return 0, r, err
	}
	if r.Src, err = parseObject(src); err != nil {
		return 0, r, err
	}
	if r.Dst, err = parseObject(dst); err != nil {
		return 0, r, err
	}
	r.TypeNo, r.Grade, r.Basis = typeNo, model.Grade(grade), model.Basis(basis)
	if r.Attrs, err = decodeAttrs(attrs); err != nil {
		return 0, r, err
	}
	if r.SourceRefs, err = refsOf(refs); err != nil {
		return 0, r, err
	}
	r.ContentHash, err = hashOf(hash)
	return linkNo, r, err
}

// selectLinks reads the links that satisfy cond and are valid in the state, in the order of their ids, at most limit
// (0 is no limit). The newest state is read from the open rows alone; another one also from the history. The
// ends come back as ids, joined from the registry.
func (sn *kgSnapshot) selectLinks(ctx context.Context, tx *sql.Tx, newest bool, q *qb, cond string, limit int) ([]graph.LinkRow, error) {
	from := func(table string) string {
		return ` FROM ` + sn.a.t(table) + ` l JOIN ` + sn.a.t(pgschema.TableObject) + ` so ON so.obj_no = l.src JOIN ` +
			sn.a.t(pgschema.TableObject) + ` de ON de.obj_no = l.dst WHERE ` + cond
	}
	tail := ` ORDER BY ref`
	if limit > 0 {
		tail += ` LIMIT ` + q.arg(limit)
	}
	query := `SELECT ` + kgLinkColumns + from(pgschema.TableLink) + tail
	if !newest {
		seq := q.arg(sn.info.Seq)
		live := `(SELECT ` + kgLinkColumns + from(pgschema.TableLink) + ` AND l.seq_from <= ` + seq + tail + `)`
		hist := `(SELECT ` + kgLinkColumns + from(pgschema.TableLinkHist) + ` AND l.seq_from <= ` + seq + ` AND l.seq_to > ` + seq + tail + `)`
		query = `SELECT * FROM (` + live + ` UNION ALL ` + hist + `) u` + tail
	}
	return collectLinks(ctx, tx, query, q.args)
}

func collectLinks(ctx context.Context, tx *sql.Tx, query string, args []any) ([]graph.LinkRow, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, dbErr(err, "read links")
	}
	defer rows.Close()
	var out []graph.LinkRow
	for rows.Next() {
		_, r, err := scanKGLink(rows)
		if err != nil {
			return nil, dbErr(err, "read a link")
		}
		out = append(out, r)
	}
	return out, dbErr(rows.Err(), "read links")
}

func (sn *kgSnapshot) Link(ctx context.Context, id graph.LinkID) (graph.LinkRow, bool, error) {
	got, err := sn.LinksByID(ctx, []graph.LinkID{id})
	if err != nil {
		return graph.LinkRow{}, false, err
	}
	row, ok := got[id]
	return row, ok, nil
}

// LinksByID implements graph.BulkReader: the links that are live in the state, in one read.
func (sn *kgSnapshot) LinksByID(ctx context.Context, ids []graph.LinkID) (map[graph.LinkID]graph.LinkRow, error) {
	out := map[graph.LinkID]graph.LinkRow{}
	if len(ids) == 0 {
		return out, nil
	}
	refs := make([]string, len(ids))
	for i, id := range ids {
		refs[i] = id.String()
	}
	err := sn.read(ctx, func(tx *sql.Tx, newest bool) error {
		q := &qb{}
		rows, err := sn.selectLinks(ctx, tx, newest, q, `l.ref = ANY(`+q.arg(pq.Array(refs))+`)`, 0)
		for _, r := range rows {
			out[r.ID] = r
		}
		return err
	})
	return out, err
}

func (sn *kgSnapshot) ScanLinks(ctx context.Context, after graph.LinkID, limit int) ([]graph.LinkRow, error) {
	var out []graph.LinkRow
	if limit <= 0 {
		return out, nil
	}
	err := sn.read(ctx, func(tx *sql.Tx, newest bool) error {
		q := &qb{}
		var err error
		out, err = sn.selectLinks(ctx, tx, newest, q, `l.ref > `+q.arg(afterText(after)), limit)
		return err
	})
	return out, err
}

func endCondition(dir graph.Direction, q *qb, nos []int64) (string, error) {
	set := q.arg(pq.Array(nos))
	switch dir {
	case graph.Out:
		return `l.src = ANY(` + set + `)`, nil
	case graph.In:
		return `l.dst = ANY(` + set + `)`, nil
	case graph.Both:
		return `(l.src = ANY(` + set + `) OR l.dst = ANY(` + set + `))`, nil
	}
	return "", kernel.Errorf(kernel.CodeInvalidValue, "unknown direction %d", dir)
}

func (sn *kgSnapshot) LinksOf(ctx context.Context, id graph.ObjectID, dir graph.Direction) ([]graph.LinkRow, error) {
	got, err := sn.LinksOfMany(ctx, []graph.ObjectID{id}, dir)
	return got[id], err
}

// LinksOfMany implements graph.BulkReader: the links at several objects in one read, each in link id order. A
// link between two of the objects is in the lists of both when the direction is Both.
func (sn *kgSnapshot) LinksOfMany(ctx context.Context, ids []graph.ObjectID, dir graph.Direction) (map[graph.ObjectID][]graph.LinkRow, error) {
	out := map[graph.ObjectID][]graph.LinkRow{}
	if len(ids) == 0 {
		return out, nil
	}
	refs := make([]string, len(ids))
	want := map[graph.ObjectID]bool{}
	for i, id := range ids {
		refs[i], want[id] = id.String(), true
	}
	err := sn.read(ctx, func(tx *sql.Tx, newest bool) error {
		rows, err := sn.linksAt(ctx, tx, newest, refs, dir)
		for _, r := range rows {
			addEnds(out, want, r, dir)
		}
		return err
	})
	return out, err
}

// linksAt reads the links that have an end, in the direction, at one of the objects.
func (sn *kgSnapshot) linksAt(ctx context.Context, tx *sql.Tx, newest bool, refs []string, dir graph.Direction) ([]graph.LinkRow, error) {
	reg, err := sn.registry(ctx, tx, refs)
	if err != nil || len(reg) == 0 {
		return nil, err
	}
	nos := make([]int64, len(reg))
	for i, e := range reg {
		nos[i] = e.objNo
	}
	q := &qb{}
	cond, err := endCondition(dir, q, nos)
	if err != nil {
		return nil, err
	}
	return sn.selectLinks(ctx, tx, newest, q, cond, 0)
}

// addEnds puts a link in the list of each of its ends that was asked for and that the direction means.
func addEnds(out map[graph.ObjectID][]graph.LinkRow, want map[graph.ObjectID]bool, r graph.LinkRow, dir graph.Direction) {
	if dir != graph.In && want[r.Src] {
		out[r.Src] = append(out[r.Src], r)
	}
	if dir != graph.Out && want[r.Dst] && (dir == graph.In || r.Dst != r.Src) {
		out[r.Dst] = append(out[r.Dst], r)
	}
}
