package pgingest

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// A round of the identification (ingestion-at-scale-identity.md 2.2): the keys whose parents are found become slots, the records that
// share a slot's key become groups (the smallest number of a group reaches all of it, a turn at a time), and every group gets an
// object. A round ends with the objects of the records that did not have a parent when it began, so the next round finds them.

// q writes the names of the tables and the numbers of the unit into a statement.
func (r *identRun) q(query string) string {
	pairs := []string{
		"{slot}", r.t(pgschema.TableCalcSlot), "{label}", r.t(pgschema.TableCalcLabel), "{bind}", r.t(pgschema.TableCalcBind),
		"{key}", r.t(pgschema.TableCalcKey), "{newkey}", r.t(pgschema.TableCalcNewKey), "{dropped}", r.t(pgschema.TableCalcDropped),
		"{newobj}", r.t(pgschema.TableCalcNewObj), "{ask}", r.t(pgschema.TableCalcAsk), "{ref}", r.t(pgschema.TableCalcRef),
		"{prob}", r.t(pgschema.TableCalcProblem), "{obj}", r.t(pgschema.TableObject), "{okey}", r.t(pgschema.TableObjectKey),
		"{rec}", r.t(pgschema.TableCalcRecord), "{claim}", r.t(pgschema.TableCalcClaim), "{ident}", r.t(pgschema.TableObjectIdentifier),
		"{R}", strconv.Itoa(r.st.Round), "{F}", strconv.Itoa(r.st.Turn), "{feeds}", r.feedSQL(),
	}
	return strings.NewReplacer(pairs...).Replace(query)
}

// lookupSQL is the object the index has for a key: the identification index, else a key bound in an earlier round of the run.
func lookupSQL(typ, ctx, sig string) string {
	return "COALESCE((SELECT o.obj_no FROM {okey} o WHERE o.type_no = " + typ + " AND o.ctx_no = " + ctx + " AND o.sig_hash = " + sig + "), " +
		"(SELECT n.obj_no FROM {newkey} n WHERE n.type_no = " + typ + " AND n.ctx_no = " + ctx + " AND n.sig_hash = " + sig + "))"
}

// chainSQL is the lateral joins that find the parents of a key (alias k, with anc_types and anc_sigs), from the farthest down to the
// nearest: the parent of the key is the object the nearest one finds, and it is NULL when one of them is not found. A key with no
// parent has the context 0. The columns d.n (how many parents) and ctx are the result.
func chainSQL() string {
	var b strings.Builder
	b.WriteString(" CROSS JOIN LATERAL (SELECT COALESCE(cardinality(k.anc_types), 0) AS n) d")
	for i := 4; i >= 1; i-- {
		ctx := "0"
		if i < 4 {
			ctx = "COALESCE(l" + strconv.Itoa(i+1) + ".o, 0)"
		}
		cond := "d.n >= " + strconv.Itoa(i)
		if i < 4 {
			cond += " AND (d.n < " + strconv.Itoa(i+1) + " OR l" + strconv.Itoa(i+1) + ".o IS NOT NULL)"
		}
		idx := strconv.Itoa(i)
		fmt.Fprintf(&b, " LEFT JOIN LATERAL (SELECT %s AS o WHERE %s) l%d ON true", lookupSQL("k.anc_types["+idx+"]", ctx, "k.anc_sigs["+idx+"]"), cond, i)
	}
	return b.String()
}

// ctxSQL is the context of the key that chainSQL found, NULL when it is not found.
const ctxSQL = "(CASE WHEN d.n = 0 THEN 0 WHEN d.n > 4 THEN NULL ELSE l1.o END)"

// slot makes the slots of a range of the records: the keys of the records that are not bound yet and whose feed makes objects, with
// the object the index has for each. A record is ready when all its keys have their parents, or, in a round that takes what it can,
// when one has.
func (r *identRun) slot(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	if st.Cur >= st.MaxRid || st.Round > st.Rounds {
		return r.endSlots()
	}
	hi := st.Cur + int64(st.Size)
	ready := "bool_and(c IS NOT NULL)"
	if st.Relaxed {
		ready = "bool_or(c IS NOT NULL)"
	}
	n, err := exec(ctx, tx, "make the slots", r.q("WITH {feeds}, ks AS (SELECT k.rid, k.feed_no, k.key_ord, k.type_no, k.sig_hash, "+ctxSQL+" AS c FROM {key} k "+
		"JOIN feeds f ON f.feed_no = k.feed_no AND f.creates"+chainSQL()+" WHERE NOT k.held AND k.rid > $1 AND k.rid <= $2 "+
		"AND NOT EXISTS (SELECT 1 FROM {bind} b WHERE b.rid = k.rid AND b.feed_no = k.feed_no) "+
		"AND NOT EXISTS (SELECT 1 FROM {ref} x WHERE x.rid = k.rid AND x.def_no = k.feed_no AND x.role = "+strconv.Itoa(int(roleIdentity))+")), "+
		"ok AS (SELECT rid, feed_no FROM ks GROUP BY rid, feed_no HAVING "+ready+"), "+
		"found AS (SELECT ks.*, "+lookupSQL("ks.type_no", "ks.c", "ks.sig_hash")+" AS f FROM ks JOIN ok USING (rid, feed_no) WHERE ks.c IS NOT NULL) "+
		"INSERT INTO {slot} (rid, feed_no, pid, key_ord, type_no, ctx_no, sig_hash, round_no, found_no) "+
		"SELECT rid, feed_no, rid * 1024 + feed_no, key_ord, type_no, c, sig_hash, {R}, f FROM found "+
		"WHERE f IS NOT NULL OR NOT EXISTS (SELECT 1 FROM {dropped} d WHERE d.type_no = found.type_no AND d.ctx_no = found.c AND d.sig_hash = found.sig_hash)"),
		st.Cur, hi)
	if err != nil {
		return err
	}
	st.RoundSlots += n
	st.Cur = hi
	return nil
}

// endSlots is the end of the slots of a round: a round that made none is tried again with what it can take, and then the rounds are
// over; a round that made some goes on to the groups.
func (r *identRun) endSlots() error {
	st := r.st
	switch {
	case st.Round > st.Rounds:
		return r.toLookup()
	case st.RoundSlots == 0 && !st.Relaxed:
		st.Relaxed, st.Cur = true, 0
	case st.RoundSlots == 0:
		return r.toLookup()
	default:
		st.Phase, st.Cur, st.Turn, st.Changed = phaseLabels, 0, 0, 0
		st.Counts.Rounds = st.Round
	}
	return nil
}

func (r *identRun) toLookup() error {
	st := r.st
	st.Phase, st.Cur, st.Turn = phaseLookup, 0, 0
	return nil
}

// labels does a range of the pairs of a turn of the search for the groups. Turn 0 gives every pair its own number as the label; a
// later turn gives it the smallest label of the pairs that have a key in common with it. The turns go on until none changes.
func (r *identRun) labels(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	var end sql.NullInt64
	err := tx.QueryRowContext(ctx, r.q("SELECT pid FROM {slot} WHERE round_no = {R} AND pid > $1 ORDER BY pid OFFSET $2 LIMIT 1"), st.Cur, st.Size).Scan(&end)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the unit")
	}
	hi := int64(math.MaxInt64)
	if !last {
		hi = end.Int64
	}
	if st.Turn == 0 {
		if _, err := exec(ctx, tx, "label the pairs", r.q("INSERT INTO {label} (round_no, iter, pid, label) SELECT {R}, 0, pid, pid FROM {slot} "+
			"WHERE round_no = {R} AND pid > $1 AND pid <= $2 GROUP BY pid"), st.Cur, hi); err != nil {
			return err
		}
	} else {
		changed, err := scanInt(ctx, tx, "label the pairs", r.q("WITH nl AS (SELECT s.pid, min(l2.label) AS label FROM {slot} s "+
			"JOIN {slot} s2 ON s2.round_no = s.round_no AND s2.type_no = s.type_no AND s2.ctx_no = s.ctx_no AND s2.sig_hash = s.sig_hash "+
			"JOIN {label} l2 ON l2.round_no = {R} AND l2.iter = $3 - 1 AND l2.pid = s2.pid "+
			"WHERE s.round_no = {R} AND s.pid > $1 AND s.pid <= $2 GROUP BY s.pid), "+
			"ins AS (INSERT INTO {label} (round_no, iter, pid, label) SELECT {R}, $3, pid, label FROM nl RETURNING pid, label) "+
			"SELECT count(*) FROM ins i JOIN {label} p ON p.round_no = {R} AND p.iter = $3 - 1 AND p.pid = i.pid WHERE p.label <> i.label"), st.Cur, hi, st.Turn)
		if err != nil {
			return err
		}
		st.Changed += int(changed)
	}
	if !last {
		st.Cur = hi
		return nil
	}
	st.Cur = 0
	switch {
	case st.Turn == 0 || (st.Changed > 0 && st.Turn < st.Turns):
		st.Turn, st.Changed = st.Turn+1, 0
	default:
		st.Unsettled = st.Changed > 0
		st.Phase, st.Cur = phaseDecide, 0
	}
	return nil
}

// pairsSQL is the pairs of the groups of a range of labels of the last turn of the round. Pairs whose label changed in the last turn
// (the search was stopped by its limit) are not in it.
func pairsSQL(unsettled bool) string {
	cond := ""
	if unsettled {
		cond = " AND NOT EXISTS (SELECT 1 FROM {label} p WHERE p.round_no = {R} AND p.iter = {F} - 1 AND p.pid = l.pid AND p.label <> l.label)"
	}
	return "pl AS (SELECT l.pid, l.label FROM {label} l WHERE l.round_no = {R} AND l.iter = {F} AND l.label > $1 AND l.label <= $2" + cond + ")"
}

// groupsSQL are the groups that have no key the index knows, each with the key that names its object: the first of the model's keys
// of the type (then the smaller signature). It needs pl.
const groupsSQL = "g AS (SELECT pl.label FROM pl JOIN {slot} s ON s.round_no = {R} AND s.pid = pl.pid GROUP BY pl.label HAVING bool_and(s.found_no IS NULL)), " +
	"w AS (SELECT DISTINCT ON (pl.label) pl.label, s.rid, s.feed_no, s.key_ord, s.type_no, s.ctx_no, k.cand_ref, (k.anc_types IS NOT NULL) AS has_ctx " +
	"FROM g JOIN pl ON pl.label = g.label JOIN {slot} s ON s.round_no = {R} AND s.pid = pl.pid " +
	"JOIN {key} k ON k.rid = s.rid AND k.feed_no = s.feed_no AND k.key_ord = s.key_ord " +
	"JOIN kord ko ON ko.feed_no = s.feed_no AND ko.key_ord = s.key_ord ORDER BY pl.label, ko.model_ord, s.sig_hash)"

// makeSQL gives numbers to the objects of ex (label, rid, feed_no, key_ord, type_no, ref, reg: the number the registry has for the
// ref, or NULL), writes the new ones to calc_newobj and gives every pair of the groups its object. It needs pl and ex.
const makeSQL = "numbered AS (SELECT label, row_number() OVER (ORDER BY ref) AS rn FROM ex WHERE reg IS NULL), " +
	"nums AS (SELECT n, row_number() OVER (ORDER BY n) AS rn FROM public.praxium_next_obj_nos((SELECT count(*) FROM numbered)::int) AS t(n)), " +
	"assigned AS (SELECT ex.label, ex.rid, ex.feed_no, ex.key_ord, ex.type_no, ex.ref, COALESCE(ex.reg, nums.n) AS obj_no, (ex.reg IS NULL) AS fresh FROM ex " +
	"LEFT JOIN numbered ON numbered.label = ex.label LEFT JOIN nums ON nums.rn = numbered.rn), " +
	"mk AS (INSERT INTO {newobj} (obj_no, ref, type_no, rid, feed_no, round_no) SELECT obj_no, ref, type_no, rid, feed_no, {R} FROM assigned WHERE fresh RETURNING obj_no) " +
	"INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) SELECT s.rid, s.feed_no, min(s.key_ord), a.obj_no, a.fresh, {R} FROM assigned a " +
	"JOIN pl ON pl.label = a.label JOIN {slot} s ON s.round_no = {R} AND s.pid = pl.pid GROUP BY s.rid, s.feed_no, a.obj_no, a.fresh"

// decide gives an object to every group of a range of labels.
func (r *identRun) decide(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	var end sql.NullInt64
	err := tx.QueryRowContext(ctx, r.q("SELECT label FROM {label} WHERE round_no = {R} AND iter = {F} AND label > $1 ORDER BY label OFFSET $2 LIMIT 1"), st.Cur, st.Size).Scan(&end)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the unit")
	}
	hi := int64(math.MaxInt64)
	if !last {
		hi = end.Int64
	}
	if err := r.decideRange(ctx, tx, st.Cur, hi); err != nil {
		return err
	}
	if !last {
		st.Cur = hi
		return nil
	}
	left, err := scanInt(ctx, tx, "count the questions", r.q("SELECT count(*) FROM {ask} WHERE round_no = {R} AND NOT answer"))
	if err != nil {
		return err
	}
	st.AskLeft = int(left)
	if left > 0 {
		st.Phase = phaseAsk
		return nil
	}
	return r.nextRound(ctx, tx)
}

// decideRange is the statements that decide the groups whose label is in the range.
func (r *identRun) decideRange(ctx context.Context, tx *sql.Tx, lo, hi int64) error {
	pl := pairsSQL(r.st.Unsettled)
	steps := []struct{ what, query string }{
		// a record that has a key the index knows is the object of its first such key
		{"bind the records that are known", "WITH " + pl + " INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) " +
			"SELECT DISTINCT ON (s.pid) s.rid, s.feed_no, s.key_ord, s.found_no, false, {R} FROM {slot} s JOIN pl ON pl.pid = s.pid " +
			"WHERE s.round_no = {R} AND s.found_no IS NOT NULL ORDER BY s.pid, s.key_ord"},
		{"report the keys that stand for another object", "WITH " + pl + " INSERT INTO {prob} (rid, def_no, field, kind, reason) " +
			"SELECT s.rid, s.feed_no, '', '" + pgschema.ProblemIdentityConflict + "', (count(DISTINCT s.found_no) - 1) || ' key(s) of the record stand for other objects than the one it is' " +
			"FROM {slot} s JOIN pl ON pl.pid = s.pid WHERE s.round_no = {R} AND s.found_no IS NOT NULL GROUP BY s.rid, s.feed_no HAVING count(DISTINCT s.found_no) > 1"},
		// a group the index knows nothing of is a new object whose id is the candidate of its key
		{"make the new objects", "WITH {feeds}, " + pl + ", " + groupsSQL + ", ex AS (SELECT w.label, w.rid, w.feed_no, w.key_ord, w.type_no, w.cand_ref AS ref, o.obj_no AS reg " +
			"FROM w LEFT JOIN {obj} o ON o.ref = w.cand_ref WHERE NOT w.has_ctx), " + makeSQL},
		// a group whose new object has a parent waits for Go to make its id
		{"ask for the new objects that have a parent", "WITH {feeds}, " + pl + ", " + groupsSQL + " INSERT INTO {ask} (round_no, label, rid, feed_no, key_ord, type_no, parent_no, parent_ref, answer) " +
			"SELECT {R}, w.label, w.rid, w.feed_no, w.key_ord, w.type_no, w.ctx_no, COALESCE(o.ref, n.ref), false FROM w LEFT JOIN {obj} o ON o.obj_no = w.ctx_no " +
			"LEFT JOIN {newobj} n ON n.obj_no = w.ctx_no WHERE w.has_ctx"},
		// the other records of a group that has one object are that object
		{"bind the records of a group of one object", "WITH " + pl + ", gb AS (SELECT pl.label, min(b.obj_no) AS obj, count(DISTINCT b.obj_no) AS c FROM pl " +
			"JOIN {slot} s ON s.round_no = {R} AND s.pid = pl.pid JOIN {bind} b ON b.rid = s.rid AND b.feed_no = s.feed_no GROUP BY pl.label) " +
			"INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) SELECT s.rid, s.feed_no, min(s.key_ord), gb.obj, false, {R} FROM {slot} s " +
			"JOIN pl ON pl.pid = s.pid JOIN gb ON gb.label = pl.label AND gb.c = 1 WHERE s.round_no = {R} " +
			"AND NOT EXISTS (SELECT 1 FROM {bind} x WHERE x.rid = s.rid AND x.feed_no = s.feed_no) GROUP BY s.rid, s.feed_no, gb.obj"},
		// in a group of several objects a record is the object that the records it shares a key with are, when they are one
		{"bind the records of a group of several objects", "WITH " + pl + " INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) " +
			"SELECT s.rid, s.feed_no, min(s.key_ord), min(b.obj_no), false, {R} FROM {slot} s JOIN pl ON pl.pid = s.pid " +
			"JOIN {slot} s2 ON s2.round_no = {R} AND s2.type_no = s.type_no AND s2.ctx_no = s.ctx_no AND s2.sig_hash = s.sig_hash AND s2.pid <> s.pid " +
			"JOIN {bind} b ON b.rid = s2.rid AND b.feed_no = s2.feed_no WHERE s.round_no = {R} " +
			"AND NOT EXISTS (SELECT 1 FROM {bind} x WHERE x.rid = s.rid AND x.feed_no = s.feed_no) GROUP BY s.rid, s.feed_no HAVING count(DISTINCT b.obj_no) = 1"},
		{"bind the new keys", r.newKeysSQL(pl)},
		{"leave the records that are not bound", r.unboundSQL(pl)},
	}
	for _, s := range steps {
		if _, err := exec(ctx, tx, s.what, r.q(s.query), lo, hi); err != nil {
			return err
		}
	}
	return nil
}

// newKeysSQL binds the keys the index did not know to the object of the records that have them, and drops a key that leads to two.
func (r *identRun) newKeysSQL(pl string) string {
	return "WITH " + pl + ", ks AS (SELECT s.type_no, s.ctx_no, s.sig_hash, count(DISTINCT b.obj_no) AS c, min(b.obj_no) AS obj FROM {slot} s JOIN pl ON pl.pid = s.pid " +
		"JOIN {bind} b ON b.rid = s.rid AND b.feed_no = s.feed_no WHERE s.round_no = {R} AND s.found_no IS NULL GROUP BY s.type_no, s.ctx_no, s.sig_hash), " +
		"nk AS (INSERT INTO {newkey} (type_no, ctx_no, sig_hash, obj_no, round_no) SELECT type_no, ctx_no, sig_hash, obj, {R} FROM ks WHERE c = 1 " +
		"AND NOT EXISTS (SELECT 1 FROM {dropped} d WHERE d.type_no = ks.type_no AND d.ctx_no = ks.ctx_no AND d.sig_hash = ks.sig_hash) ON CONFLICT DO NOTHING RETURNING 1) " +
		"INSERT INTO {dropped} (type_no, ctx_no, sig_hash, round_no) SELECT type_no, ctx_no, sig_hash, {R} FROM ks WHERE c > 1 ON CONFLICT DO NOTHING"
}

// unboundPairs are the records of the groups of the range that no object was found for, and that no question waits for.
const unboundPairs = "SELECT DISTINCT s.rid, s.feed_no FROM {slot} s JOIN pl ON pl.pid = s.pid WHERE s.round_no = {R} " +
	"AND NOT EXISTS (SELECT 1 FROM {bind} x WHERE x.rid = s.rid AND x.feed_no = s.feed_no) " +
	"AND NOT EXISTS (SELECT 1 FROM {ask} a WHERE a.round_no = {R} AND a.label = pl.label AND NOT a.answer)"

// unboundSQL leaves the records that were not bound as references that are not solved, each with its problem.
func (r *identRun) unboundSQL(pl string) string {
	return "WITH " + pl + ", u AS (" + unboundPairs + "), r AS (INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) " +
		"SELECT rid, " + strconv.Itoa(int(roleIdentity)) + ", feed_no, 0, NULL, NULL, " + strconv.Itoa(int(pgschema.RefUnresolved)) + ", false FROM u RETURNING 1) " +
		"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, feed_no, '', '" + pgschema.ProblemIdentityConflict +
		"', 'no object could be told for this record: every key it gives stands for more than one object' FROM u"
}

// nextRound closes a round and begins the next one.
func (r *identRun) nextRound(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	st.Round, st.Cur, st.Turn, st.RoundSlots, st.Relaxed, st.Unsettled, st.AskLeft = st.Round+1, 0, 0, 0, false, false, 0
	st.Phase = phaseSlot
	if st.Round > st.Rounds {
		return r.toLookup()
	}
	return nil
}

// AskPage gives the new objects of the round that have a parent and no id yet: see ingest.Identifier.
func (s *Store) AskPage(ctx context.Context, lease ingest.RunLease) ([]ingest.AskRow, error) {
	var out []ingest.AskRow
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := loadIdent(ctx, tx, lease)
		if err != nil {
			return err
		}
		if r.st.Phase != phaseAsk {
			return kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is at %s, not at the new objects that have a parent", lease.No, r.st.Phase)
		}
		rows, err := tx.QueryContext(ctx, r.q("SELECT a.label, a.rid, c.src_no, d.dataset_id, c.key, c.fields, a.feed_no, a.key_ord, a.parent_ref FROM {ask} a "+
			"JOIN {rec} c ON c.rid = a.rid JOIN "+pgschema.TableSourceDataset+" d ON d.dataset_no = c.dataset_no WHERE a.round_no = {R} AND NOT a.answer "+
			"AND NOT EXISTS (SELECT 1 FROM {ask} b WHERE b.round_no = a.round_no AND b.label = a.label AND b.answer) ORDER BY a.label LIMIT $1"), r.st.Size)
		if err != nil {
			return dbErr(err, "read the questions")
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			a := ingest.AskRow{Round: r.st.Round}
			var cells string
			var parent sql.NullString
			if err := rows.Scan(&a.Label, &a.Rid, &a.SrcNo, &a.Dataset, &a.Key, &cells, &a.Feed, &a.Ord, &parent); err != nil {
				return dbErr(err, "read a question")
			}
			a.ParentRef = parent.String
			if a.Fields, err = parseTextArray(cells); err != nil {
				return err
			}
			out = append(out, a)
		}
		return dbErr(rows.Err(), "read the questions")
	})
	return out, err
}

// PutAnswers takes the ids Go made, makes the objects of the groups and binds their records and keys: see ingest.Identifier.
func (s *Store) PutAnswers(ctx context.Context, lease ingest.RunLease, answers []ingest.Answer) error {
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := loadIdent(ctx, tx, lease)
		if err != nil {
			return err
		}
		st := r.st
		if st.Phase != phaseAsk {
			return kernel.Errorf(ingest.CodeRunStage, "the identification of the run %d is at %s, not at the new objects that have a parent", lease.No, st.Phase)
		}
		if _, err := tx.ExecContext(ctx, "SET LOCAL enable_nestloop = off"); err != nil {
			return dbErr(err, "set the plan of the unit")
		}
		var labels, refs, missing []string
		for _, a := range answers {
			if a.Round != st.Round {
				return kernel.Errorf(kernel.CodeInvalidValue, "an answer is of the round %d and the run is at %d", a.Round, st.Round)
			}
			labels, refs, missing = append(labels, strconv.FormatInt(a.Label, 10)), append(refs, a.Ref), append(missing, strconv.FormatBool(a.Missing))
		}
		if len(answers) > 0 {
			if err := r.settleAnswers(ctx, tx, textArray(labels), textArray(refs), textArray(missing)); err != nil {
				return err
			}
		}
		left, err := scanInt(ctx, tx, "count the questions", r.q("SELECT count(*) FROM {ask} a WHERE a.round_no = {R} AND NOT a.answer "+
			"AND NOT EXISTS (SELECT 1 FROM {ask} b WHERE b.round_no = a.round_no AND b.label = a.label AND b.answer)"))
		if err != nil {
			return err
		}
		st.AskLeft = int(left)
		if left == 0 {
			if err := r.nextRound(ctx, tx); err != nil {
				return err
			}
		}
		return writeIdent(ctx, tx, lease, st, false)
	})
}

// settleAnswers writes the answers and does for the groups they name what decide does for the groups it makes ids of itself. The
// parameters are the labels, the ids and whether each has no name, as the text of arrays.
func (r *identRun) settleAnswers(ctx context.Context, tx *sql.Tx, labels, refs, missing string) error {
	const un = "unnest($1::text::bigint[], $2::text::text[], $3::text::boolean[]) AS u(label, ref, missing)"
	pl := "pl AS (SELECT l.pid, l.label FROM {label} l WHERE l.round_no = {R} AND l.iter = {F} AND l.label = ANY($1::text::bigint[]))"
	answered := "answers AS (SELECT a.label, a.rid, a.feed_no, a.key_ord, a.type_no, u.ref, u.missing FROM {ask} a JOIN " + un +
		" ON u.label = a.label WHERE a.round_no = {R} AND NOT a.answer)"
	steps := []struct {
		what, query string
		args        []any
	}{
		{"write the answers", "WITH " + answered + " INSERT INTO {ask} (round_no, label, rid, feed_no, key_ord, type_no, ref, missing, answer) " +
			"SELECT {R}, label, rid, feed_no, key_ord, type_no, CASE WHEN missing THEN NULL ELSE ref END, missing, true FROM answers", []any{labels, refs, missing}},
		{"make the objects under a parent", "WITH " + pl + ", " + answered + ", ex AS (SELECT a.label, a.rid, a.feed_no, a.key_ord, a.type_no, a.ref, o.obj_no AS reg " +
			"FROM answers a LEFT JOIN {obj} o ON o.ref = a.ref WHERE NOT a.missing), " + makeSQL, []any{labels, refs, missing}},
		{"bind the new keys", r.newKeysSQL(pl), []any{labels}},
		{"leave the records of the objects with no name", "WITH " + pl + ", p AS (SELECT DISTINCT s.rid, s.feed_no FROM {slot} s JOIN pl ON pl.pid = s.pid " +
			"JOIN {ask} a ON a.round_no = {R} AND a.label = pl.label AND a.answer AND a.missing WHERE s.round_no = {R}), " +
			"r AS (INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT rid, " + strconv.Itoa(int(roleIdentity)) + ", feed_no, 0, NULL, NULL, " +
			strconv.Itoa(int(pgschema.RefUnresolved)) + ", false FROM p RETURNING 1) INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, feed_no, '', '" + pgschema.ProblemMissingName +
			"', 'the object this record would make has no name: the attribute name has no value and the key has no part but its context' FROM p", []any{labels}},
	}
	for _, s := range steps {
		if _, err := exec(ctx, tx, s.what, r.q(s.query), s.args...); err != nil {
			return err
		}
	}
	return nil
}
