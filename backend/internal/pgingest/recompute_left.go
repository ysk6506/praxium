package pgingest

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The objects the computation of the objects does not make (ADR-0119). An object the KG has is left as it is: a calc_left row says that
// it was left and why, and the later steps leave an object that is not a candidate for retirement (calc_object.why 4) and has no row in
// stage_object as it is. An object the run would have made (calc_newobj) is not made, and what pointed at it is taken back in the same
// transaction as the page: the references of its records (identity and claims) become unresolved, with a problem for each record, and
// the rows that name it (calc_newobj, calc_newkey, calc_bind, calc_ask, calc_object) are deleted, so that nothing the run keeps names an
// object that is not there. An object that has a key whose context is an object that is taken back is taken back too, and so on.

// rcpLeftKinds is the kind of problem the records under an object that is taken back get, by the reason (the kinds are of the closed list;
// a mapping that puts too many records under one object is a conflict of the identity of the records).
var rcpLeftKinds = []struct {
	why  int16
	kind string
}{
	{pgschema.LeftUnreadable, pgschema.ProblemShapeChanged},
	{pgschema.LeftNameless, pgschema.ProblemMissingName},
	{pgschema.LeftTooMany, pgschema.ProblemIdentityConflict},
	{pgschema.LeftParent, pgschema.ProblemContextParent},
	{pgschema.LeftEmpty, pgschema.ProblemShapeChanged},
}

// rcpLeftWhys is the number a reason of the port has in calc_left.
var rcpLeftWhys = map[string]int16{
	ingest.LeftEmpty:      pgschema.LeftEmpty,
	ingest.LeftUnreadable: pgschema.LeftUnreadable,
	ingest.LeftNameless:   pgschema.LeftNameless,
	ingest.LeftTooMany:    pgschema.LeftTooMany,
}

const rcpParentReason = "the object this record would make is made under a parent object that was left, so it is not made"

// rcpLeft is a row of calc_left to write.
type rcpLeft struct {
	obj    int64
	why    int16
	lines  int
	reason string
}

// leftRows checks that what the page says it left agrees with the numbers it counts, and makes the rows of calc_left of it.
func (r *rcpRun) leftRows(page ingest.RecomputedPage) ([]rcpLeft, error) {
	var out []rcpLeft
	seen := map[int64]bool{}
	per := map[int16]int{}
	for _, l := range page.Left {
		row, err := r.leftRow(l)
		if err != nil {
			return nil, err
		}
		if seen[l.ObjNo] {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the page leaves the object %d twice", l.ObjNo)
		}
		seen[l.ObjNo] = true
		per[row.why]++
		out = append(out, row)
	}
	if empty, unreadable := per[pgschema.LeftEmpty], per[pgschema.LeftUnreadable]+per[pgschema.LeftNameless]; empty != len(page.Empty) || unreadable != len(page.Unreadable) || per[pgschema.LeftTooMany] != page.TooMany {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the page counts %d empty, %d unreadable and %d objects with too many lines and says it left %d, %d and %d",
			len(page.Empty), len(page.Unreadable), page.TooMany, empty, unreadable, per[pgschema.LeftTooMany])
	}
	return out, nil
}

// leftRow is the row of calc_left of an object the page left. The lines and the reason of one that has too many are the store's, which
// knows the limit.
func (r *rcpRun) leftRow(l ingest.RecomputedLeft) (rcpLeft, error) {
	why, ok := rcpLeftWhys[l.Why]
	if !ok {
		return rcpLeft{}, kernel.Errorf(kernel.CodeInvalidValue, "the page leaves the object %d for %q, which is not a reason", l.ObjNo, l.Why)
	}
	row := rcpLeft{obj: l.ObjNo, why: why, lines: l.Lines, reason: l.Reason}
	if why == pgschema.LeftTooMany {
		row.lines = r.st.Max + 1
		row.reason = fmt.Sprintf("more than %d records stand under the object: the identity key of the mapping puts too many records under one object", r.st.Max)
	}
	if row.reason == "" {
		return rcpLeft{}, kernel.Errorf(kernel.CodeInvalidValue, "the page leaves the object %d without a reason", l.ObjNo)
	}
	return row, nil
}

// the statements that take an object back (the objects are the array $1)
const (
	// rcpGoneSQL gives the objects the KG does not have among the given ones, and the objects that have a key whose context is one of them
	// that the run makes, and so on
	rcpGoneSQL = "WITH RECURSIVE g(obj_no) AS (SELECT u.obj FROM unnest($1::text::bigint[]) AS u(obj) WHERE NOT EXISTS (SELECT 1 FROM {obj} x WHERE x.obj_no = u.obj) " +
		"UNION SELECT k.obj_no FROM g JOIN {newkey} k ON k.ctx_no = g.obj_no JOIN {newobj} n ON n.obj_no = k.obj_no) SELECT obj_no FROM g ORDER BY obj_no"
	rcpLeftPutSQL = "INSERT INTO {left} (obj_no, why, lines, is_new, reason) SELECT u.obj, u.why, u.lines, EXISTS (SELECT 1 FROM {newobj} n WHERE n.obj_no = u.obj), u.reason " +
		"FROM unnest($1::text::bigint[], $2::text::int[], $3::text::int[], $4::text::text[]) AS u(obj, why, lines, reason)"
	rcpLeftProblemsSQL = "INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT f.rid, f.def_no, '', k.kind, l.reason FROM {cref} f JOIN {left} l ON l.obj_no = f.target_no " +
		"JOIN unnest($2::text::int[], $3::text::text[]) AS k(why, kind) ON k.why = l.why WHERE f.role = {IDENT} AND f.state = {FOUND} AND f.target_no = ANY($1::text::bigint[])"
	rcpUnresolveSQL = "UPDATE {cref} SET target_no = NULL, state = {UNRES}, retirable = false WHERE (role = {IDENT} OR role = {CLAIM}) AND target_no = ANY($1::text::bigint[])"
)

// the statements that delete what names an object that is taken back, in the order they are run (the answers are found by the id of the
// object, so they go before the objects)
var rcpTakeBackSQL = []struct{ what, query string }{
	{"take back the questions about a parent that was left", "DELETE FROM {ask} WHERE parent_no = ANY($1::text::bigint[]) OR ref IN (SELECT ref FROM {newobj} WHERE obj_no = ANY($1::text::bigint[]))"},
	{"take back the new objects", "DELETE FROM {newobj} WHERE obj_no = ANY($1::text::bigint[])"},
	{"take back the new keys", "DELETE FROM {newkey} WHERE obj_no = ANY($1::text::bigint[]) OR ctx_no = ANY($1::text::bigint[])"},
	{"take back the bindings", "DELETE FROM {bind} WHERE obj_no = ANY($1::text::bigint[])"},
	{"take back the objects of the run", "DELETE FROM {objrun} WHERE obj_no = ANY($1::text::bigint[])"},
}

// gone reads the objects that are taken back: the given ones the KG does not have, and the ones that stand under them.
func (r *rcpRun) gone(ctx context.Context, tx *sql.Tx, lefts []rcpLeft) ([]int64, error) {
	if len(lefts) == 0 {
		return nil, nil
	}
	nos := make([]int64, len(lefts))
	for i, l := range lefts {
		nos[i] = l.obj
	}
	query := r.q(rcpGoneSQL)
	arg := nums(nos)
	r.saw(tx, "find the objects that are taken back", query, arg)
	rows, err := tx.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, dbErr(err, "find the objects that are taken back")
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var no int64
		if err := rows.Scan(&no); err != nil {
			return nil, dbErr(err, "read an object that is taken back")
		}
		out = append(out, no)
	}
	return out, dbErr(rows.Err(), "find the objects that are taken back")
}

// leave writes calc_left for the objects the page left and, for the ones the run would have made (and the ones that stand under them),
// takes back what names them. It runs after the objects of the page are written, so that an object of the page that stands under one that
// is taken back is taken out of the rows that were just written.
func (r *rcpRun) leave(ctx context.Context, tx *sql.Tx, lefts []rcpLeft) error {
	if len(lefts) == 0 {
		return nil
	}
	gone, err := r.gone(ctx, tx, lefts)
	if err != nil {
		return err
	}
	rows := slices.Clone(lefts)
	for _, no := range gone {
		if !slices.ContainsFunc(lefts, func(l rcpLeft) bool { return l.obj == no }) {
			rows = append(rows, rcpLeft{obj: no, why: pgschema.LeftParent, reason: rcpParentReason})
		}
	}
	if err := r.putLeft(ctx, tx, rows); err != nil {
		return err
	}
	r.st.Counts.Isolated += len(gone)
	if len(gone) == 0 {
		return nil
	}
	return r.takeBack(ctx, tx, nums(gone))
}

// putLeft writes the rows of calc_left.
func (r *rcpRun) putLeft(ctx context.Context, tx *sql.Tx, rows []rcpLeft) error {
	var nos, whys, lines, reasons []string
	for _, l := range rows {
		nos, whys, lines, reasons = append(nos, strconv.FormatInt(l.obj, 10)), append(whys, strconv.Itoa(int(l.why))), append(lines, strconv.Itoa(l.lines)), append(reasons, l.reason)
	}
	n, err := r.exec(ctx, tx, "write the objects that were left", rcpLeftPutSQL, textArray(nos), textArray(whys), textArray(lines), textArray(reasons))
	if err != nil {
		return err
	}
	if n != len(rows) {
		return kernel.Errorf(kernel.CodeInternal, "%d objects were left and %d rows were written", len(rows), n)
	}
	return nil
}

// takeBack makes the references of the records under the objects unresolved (a problem for each), takes the rows of the objects out of
// what the page staged and deletes the rows that name them.
func (r *rcpRun) takeBack(ctx context.Context, tx *sql.Tx, gone string) error {
	var whys, kinds []string
	for _, k := range rcpLeftKinds {
		whys, kinds = append(whys, strconv.Itoa(int(k.why))), append(kinds, k.kind)
	}
	n, err := r.exec(ctx, tx, "write the problems of the records under the objects that were left", rcpLeftProblemsSQL, gone, textArray(whys), textArray(kinds))
	if err != nil {
		return err
	}
	r.st.Counts.Problems += n
	if _, err := r.exec(ctx, tx, "make the references to the objects that were left unresolved", rcpUnresolveSQL, gone); err != nil {
		return err
	}
	c := &r.st.Counts
	for _, s := range []struct {
		what, table string
		to          *int
	}{{"take the objects that were left out of the staged ones", "{stobj}", &c.Objects}, {"take their volatile values out", "{stvol}", &c.Volatile}, {"take their identifiers out", "{stid}", &c.Idents}} {
		n, err := r.exec(ctx, tx, s.what, "DELETE FROM "+s.table+" WHERE obj_no = ANY($1::text::bigint[])", gone)
		if err != nil {
			return err
		}
		*s.to -= n
	}
	for _, s := range rcpTakeBackSQL {
		if _, err := r.exec(ctx, tx, s.what, s.query, gone); err != nil {
			return err
		}
	}
	return nil
}
