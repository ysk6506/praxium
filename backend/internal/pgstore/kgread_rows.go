package pgstore

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// regEntry is what the registry says of an object: its number and its type, which never change.
type regEntry struct {
	ref    string
	objNo  int64
	typeNo int
}

// registry looks refs up in the registry; a ref that is not there is not in the answer.
func (sn *kgSnapshot) registry(ctx context.Context, tx *sql.Tx, refs []string) ([]regEntry, error) {
	return registryOf(ctx, tx, sn.a, refs)
}

func registryOf(ctx context.Context, tx *sql.Tx, a *kgAccess, refs []string) ([]regEntry, error) {
	return collect(ctx, tx, func(sc scanner) (regEntry, error) {
		var e regEntry
		err := sc.Scan(&e.ref, &e.objNo, &e.typeNo)
		return e, err
	}, "look objects up in the registry", `SELECT ref, obj_no, type_no FROM `+a.t(pgschema.TableObject)+` WHERE ref = ANY($1) ORDER BY ref`, pq.Array(refs))
}

// objectRowColumns are what a row of an object has in its table of the type and in the history: the ref and the
// registry are asked for apart.
const objectRowColumns = `obj_no, name, min_grade, doc, source_refs, content_hash, seq_from`

func scanKGObject(sc scanner) (int64, graph.ObjectRow, error) {
	var (
		r                 graph.ObjectRow
		objNo             int64
		grade             int16
		attrs, refs, hash []byte
	)
	if err := sc.Scan(&objNo, &r.Name, &grade, &attrs, &refs, &hash, &r.SeqFrom, &r.SeqTo); err != nil {
		return 0, r, err
	}
	var err error
	if r.MinGrade, err = gradeOf(grade); err != nil {
		return 0, r, err
	}
	if r.Attrs, err = decodeAttrs(attrs); err != nil {
		return 0, r, err
	}
	if r.SourceRefs, err = refsOf(refs); err != nil {
		return 0, r, err
	}
	r.ContentHash, err = hashOf(hash)
	return objNo, r, err
}

// objectRowsOfType reads the rows of some objects of one type that are valid in the state at seq: from the open rows
// alone when the state is the newest one, otherwise also from the history. The rows come with the object number.
func (sn *kgSnapshot) objectRowsOfType(ctx context.Context, tx *sql.Tx, typeNo int, objNos []int64, newest bool) (map[int64]graph.ObjectRow, error) {
	query := `SELECT ` + objectRowColumns + `, 0::bigint FROM ` + sn.a.typeTable(typeNo) + ` WHERE obj_no = ANY($1)`
	args := []any{pq.Array(objNos)}
	if !newest {
		query += ` AND seq_from <= $2 UNION ALL SELECT ` + objectRowColumns + `, seq_to FROM ` + sn.a.t(pgschema.TableObjectHist) +
			` WHERE obj_no = ANY($1) AND type_no = $3 AND seq_from <= $2 AND seq_to > $2`
		args = append(args, sn.info.Seq, typeNo)
	}
	out := map[int64]graph.ObjectRow{}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, dbErr(err, "read objects")
	}
	defer rows.Close()
	for rows.Next() {
		no, r, err := scanKGObject(rows)
		if err != nil {
			return nil, dbErr(err, "read an object")
		}
		r.TypeNo = typeNo
		out[no] = r
	}
	return out, dbErr(rows.Err(), "read objects")
}

// fetchObjects reads the rows of the objects of a registry answer that are valid in the state, in the order of
// the answer; an object that has no valid row (it was retired, or not there yet) is left out.
func (sn *kgSnapshot) fetchObjects(ctx context.Context, tx *sql.Tx, reg []regEntry, newest bool) ([]graph.ObjectRow, error) {
	found, err := sn.fetchRows(ctx, tx, reg, newest)
	if err != nil {
		return nil, err
	}
	var out []graph.ObjectRow
	for _, e := range reg {
		r, ok := found[e.objNo]
		if !ok {
			continue
		}
		id, err := parseObject(e.ref)
		if err != nil {
			return nil, err
		}
		r.ID = id
		out = append(out, r)
	}
	return out, nil
}

func (sn *kgSnapshot) Object(ctx context.Context, id graph.ObjectID) (graph.ObjectRow, bool, error) {
	got, err := sn.ObjectsByID(ctx, []graph.ObjectID{id})
	if err != nil {
		return graph.ObjectRow{}, false, err
	}
	row, ok := got[id]
	return row, ok, nil
}

// ObjectsByID implements graph.BulkReader: the objects that are live in the state, in one read.
func (sn *kgSnapshot) ObjectsByID(ctx context.Context, ids []graph.ObjectID) (map[graph.ObjectID]graph.ObjectRow, error) {
	out := map[graph.ObjectID]graph.ObjectRow{}
	if len(ids) == 0 {
		return out, nil
	}
	refs := make([]string, len(ids))
	for i, id := range ids {
		refs[i] = id.String()
	}
	err := sn.read(ctx, func(tx *sql.Tx, newest bool) error {
		reg, err := sn.registry(ctx, tx, refs)
		if err != nil {
			return err
		}
		rows, err := sn.fetchObjects(ctx, tx, reg, newest)
		for _, r := range rows {
			out[r.ID] = r
		}
		return err
	})
	return out, err
}

// ScanObjects lists up to limit rows after the id, in id order. The registry gives the candidates in the order of
// the refs; for the newest state those that are alive are the rows, and for another state a candidate is a row only
// if the tables say it was valid then, so more candidates are read until the page is full.
func (sn *kgSnapshot) ScanObjects(ctx context.Context, after graph.ObjectID, limit int) ([]graph.ObjectRow, error) {
	var out []graph.ObjectRow
	if limit <= 0 {
		return out, nil
	}
	err := sn.read(ctx, func(tx *sql.Tx, newest bool) error {
		cursor := afterText(after)
		for len(out) < limit {
			want := limit - len(out)
			reg, err := sn.candidates(ctx, tx, cursor, want, newest)
			if err != nil || len(reg) == 0 {
				return err
			}
			cursor = reg[len(reg)-1].ref
			rows, err := sn.fetchObjects(ctx, tx, reg, newest)
			if err != nil {
				return err
			}
			out = append(out, rows...)
			if len(reg) < want {
				return nil
			}
		}
		return nil
	})
	return out, err
}

// candidates are up to n registry entries after the ref that may be rows of the state.
func (sn *kgSnapshot) candidates(ctx context.Context, tx *sql.Tx, after string, n int, newest bool) ([]regEntry, error) {
	where := `seq_from <= $3`
	args := []any{after, n, sn.info.Seq}
	if newest {
		where, args = `retired_seq IS NULL`, args[:2]
	}
	return collect(ctx, tx, func(sc scanner) (regEntry, error) {
		var e regEntry
		err := sc.Scan(&e.ref, &e.objNo, &e.typeNo)
		return e, err
	}, "scan the registry", `SELECT ref, obj_no, type_no FROM `+sn.a.t(pgschema.TableObject)+` WHERE ref > $1 AND `+where+` ORDER BY ref LIMIT $2`, args...)
}
