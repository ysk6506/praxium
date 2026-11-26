package pgingest

import (
	"context"
	"database/sql"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The rows phase of the difference (ADR-0127): each source of what can change is read a range at a time by its key, and what differs from
// the KG is written to calc_diffrow.

// diffSource is a source of the rows phase: its number, its table and what else a row of it is, and what a unit does with a range of it.
type diffSource struct {
	col, from, cond string
	do              func(r *diffRun, ctx context.Context, tx *sql.Tx, lo, hi int64) error
}

// diffSources are the sources read by a number (the keys are read by their own place).
var diffSources = map[string]diffSource{
	diffPartObjects: {"obj_no", "{stobj}", "", (*diffRun).objects},
	diffPartRetire:  {"obj_no", "{objrun}", "why = {RETIRE} AND ", (*diffRun).retire},
	diffPartLinks:   {"link_no", "{stlink}", "", (*diffRun).links},
	diffPartRefs:    {"rid", "{rec}", "", (*diffRun).refLinks},
	diffPartIso:     {"entity_no", "{iso}", "kind = {KLINK} AND ", (*diffRun).isoLinks},
}

// rowsUnit does the unit the place of the rows phase says: the next range of its source. A range shorter than a unit is the last of the
// source, and the place goes on to the next one.
func (r *diffRun) rowsUnit(ctx context.Context, tx *sql.Tx) error {
	if r.st.Part == diffPartKeys {
		return r.keys(ctx, tx)
	}
	src := diffSources[r.st.Part]
	var n int
	var hi int64
	q := r.q(diffExtentSQL(src.col, src.from, src.cond))
	r.saw(tx, "find the end of the range of "+r.st.Part, q, r.st.Cur, r.st.Size)
	if err := tx.QueryRowContext(ctx, q, r.st.Cur, r.st.Size).Scan(&n, &hi); err != nil {
		return dbErr(err, "find the end of the range of "+r.st.Part)
	}
	if n > 0 {
		if err := src.do(r, ctx, tx, r.st.Cur, hi); err != nil {
			return err
		}
	}
	if n < r.st.Size {
		r.nextPart()
	} else {
		r.st.Cur = hi
	}
	return nil
}

// nextPart moves the place to the start of the next source.
func (r *diffRun) nextPart() {
	parts := diffParts()
	r.st.Part, r.st.Cur = parts[slices.Index(parts, r.st.Part)+1], 0
}

// typesOf reads the types of the rows of a range a statement gives (a row without a type is not one of the source).
func (r *diffRun) typesOf(ctx context.Context, tx *sql.Tx, what, query string, lo, hi int64) ([]int, error) {
	var out []int
	err := r.query(ctx, tx, what, query, func(rows *sql.Rows) error {
		var t sql.NullInt64
		if err := rows.Scan(&t); err != nil {
			return err
		}
		if t.Valid {
			out = append(out, int(t.Int64))
		}
		return nil
	}, lo, hi)
	slices.Sort(out)
	return out, err
}

// ofType is a statement of the rows of a type, which reads the table of the type.
func (r *diffRun) ofType(query string, typeNo int) string {
	return strings.ReplaceAll(query, "{T}", pgschema.TableRef(r.kgNo, pgschema.TypeTable(typeNo)))
}

// objects writes the objects of a range the run staged that are new or changed, a type at a time, and their identifiers that changed.
func (r *diffRun) objects(ctx context.Context, tx *sql.Tx, lo, hi int64) error {
	types, err := r.typesOf(ctx, tx, "read the types of the staged objects", diffStagedTypesSQL, lo, hi)
	if err != nil {
		return err
	}
	for _, t := range types {
		if err := r.written(ctx, tx, "compare the staged objects", r.ofType(diffObjectsSQL, t), lo, hi, t); err != nil {
			return err
		}
	}
	return r.written(ctx, tx, "compare the identifiers of the staged objects", diffIdentsSQL, lo, hi)
}

// retire writes the objects of a range the run retires that the KG has open, a type at a time, and closes the links at them.
func (r *diffRun) retire(ctx context.Context, tx *sql.Tx, lo, hi int64) error {
	types, err := r.typesOf(ctx, tx, "read the types of the objects that retire", diffRetireTypesSQL, lo, hi)
	if err != nil {
		return err
	}
	for _, t := range types {
		if err := r.written(ctx, tx, "retire the objects", r.ofType(diffRetireSQL, t), lo, hi, t); err != nil {
			return err
		}
	}
	return r.written(ctx, tx, "close the links of the objects that retire", diffRetireLinksSQL, lo, hi)
}

// links writes the links of a range the run staged that are new or changed.
func (r *diffRun) links(ctx context.Context, tx *sql.Tx, lo, hi int64) error {
	return r.written(ctx, tx, "compare the staged links", diffLinksSQL, lo, hi)
}

// refLinks closes the links of the KG that the records of a range of the run made and that neither the run staged again nor stay.
func (r *diffRun) refLinks(ctx context.Context, tx *sql.Tx, lo, hi int64) error {
	return r.written(ctx, tx, "close the links the records of the run made", diffRefLinksSQL, lo, hi)
}

// isoLinks closes the links of the KG of a range the isolation took out.
func (r *diffRun) isoLinks(ctx context.Context, tx *sql.Tx, lo, hi int64) error {
	return r.written(ctx, tx, "close the links the isolation took out", diffIsoLinksSQL, lo, hi)
}

// keys writes the keys the run binds that the index of the KG does not have, a range of them at a time in the order of the index of
// calc_newkey. It is the last source: after its last range the plan comes.
func (r *diffRun) keys(ctx context.Context, tx *sql.Tx) error {
	cur := r.st.Key
	sig, err := hex.DecodeString(cur.Sig)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "read the place in the keys")
	}
	var end diffKeyCur
	var endSig []byte
	q := r.q(diffKeyEndSQL)
	r.saw(tx, "find the end of the range of the keys", q, cur.TypeNo, cur.CtxNo, sig, r.st.Size-1)
	err = tx.QueryRowContext(ctx, q, cur.TypeNo, cur.CtxNo, sig, r.st.Size-1).Scan(&end.TypeNo, &end.CtxNo, &endSig)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the range of the keys")
	}
	args := []any{cur.TypeNo, cur.CtxNo, sig}
	if !last {
		args = append(args, end.TypeNo, end.CtxNo, endSig)
	}
	if err := r.written(ctx, tx, "compare the keys the run binds", diffKeysSQL(last), args...); err != nil {
		return err
	}
	if last {
		r.st.Phase = ingest.DiffPhasePlan
		return nil
	}
	end.Sig = hex.EncodeToString(endSig)
	r.st.Key = end
	return nil
}
