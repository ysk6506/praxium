package pgingest

import (
	"context"
	"database/sql"
	"math"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The deciding of a round: every group has the object of its keys, or a new one. What a statement reads of the tables of the run is
// the pairs of its range of labels (ps) and the rows they have in the other tables, each by one probe of an index (ADR-0108).

// bindOf is the object a pair of ps (alias s) is bound to, if it is (the alias b has obj_no, and the join drops the pairs that are not).
func bindOf(pair, alias string) string {
	return probe("x.obj_no", "{bind}", alias, "x.rid = "+pair+".rid AND x.feed_no = "+pair+".feed_no")
}

// unboundOf says (hit is NULL) that a pair of ps is not bound to any object yet.
func unboundOf(pair, alias string) string {
	return absent("{bind}", alias, "x.rid = "+pair+".rid AND x.feed_no = "+pair+".feed_no")
}

// refUnresolved is the references that say that the identity of a record was left (the record and feed of p).
func refUnresolved(from string) string {
	return "INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT rid, " + strconv.Itoa(int(roleIdentity)) +
		", feed_no, 0, NULL, NULL, " + strconv.Itoa(int(pgschema.RefUnresolved)) + ", false FROM " + from
}

// groupsSQL are the groups that have no key the index knows, each with the key that names its object: the first of the model's keys
// of the type, then the smaller signature, and, when the records give the same key, the record whose dataset and natural key come
// first (the number of a record is the order the run met them in, which changes from one run to the next, so it names nothing). A
// key that has a context is the one of a record that gives the object a name, when there is one: a record whose cell for the name is
// empty would not make the object (a name is what an object is made with). It needs ps.
const groupsSQL = "g AS (SELECT label FROM ps GROUP BY label HAVING bool_and(found_no IS NULL)), " +
	"w AS (SELECT DISTINCT ON (ps.label) ps.label, ps.rid, ps.feed_no, ps.key_ord, ps.type_no, ps.ctx_no, k.cand_ref, (k.anc_types IS NOT NULL) AS has_ctx " +
	"FROM g JOIN ps ON ps.label = g.label" +
	" CROSS JOIN LATERAL (SELECT x.cand_ref, x.anc_types FROM {key} x WHERE x.rid = ps.rid AND x.feed_no = ps.feed_no AND x.key_ord = ps.key_ord LIMIT 1) k" +
	" JOIN kord ko ON ko.src_no = ps.src_no AND ko.feed_no = ps.feed_no AND ko.key_ord = ps.key_ord" +
	" JOIN feeds fe ON fe.src_no = ps.src_no AND fe.feed_no = ps.feed_no" +
	" CROSS JOIN LATERAL (SELECT x.dataset_no, x.key, x.fields FROM {rec} x WHERE x.rid = ps.rid LIMIT 1) wc " +
	"ORDER BY ps.label, (k.anc_types IS NOT NULL AND NOT (fe.name_col >= 0 AND btrim(COALESCE(wc.fields[fe.name_col + 1], '')) <> '')), ko.model_ord, ps.sig_hash, wc.dataset_no, wc.key COLLATE \"C\", ps.feed_no, ps.key_ord)"

// makeSQL gives numbers to the objects of ex (label, rid, feed_no, key_ord, type_no, ref, reg: the number the registry has for the
// ref, or NULL), gives every pair of the groups its object (mb), and writes the new objects to calc_newobj: the rows it changes are the
// new objects. It needs ps and ex.
const makeSQL = "numbered AS (SELECT label, row_number() OVER (ORDER BY ref) AS rn FROM ex WHERE reg IS NULL), " +
	"nums AS (SELECT n, row_number() OVER (ORDER BY n) AS rn FROM public.praxium_next_obj_nos((SELECT count(*) FROM numbered)::int) AS t(n)), " +
	"assigned AS (SELECT ex.label, ex.rid, ex.feed_no, ex.key_ord, ex.type_no, ex.ref, COALESCE(ex.reg, nums.n) AS obj_no, (ex.reg IS NULL) AS fresh FROM ex " +
	"LEFT JOIN numbered ON numbered.label = ex.label LEFT JOIN nums ON nums.rn = numbered.rn), " +
	"mb AS (INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) SELECT s.rid, s.feed_no, min(s.key_ord), a.obj_no, a.fresh, {R} FROM assigned a " +
	"JOIN ps s ON s.label = a.label GROUP BY s.rid, s.feed_no, a.obj_no, a.fresh RETURNING 1) " +
	"INSERT INTO {newobj} (obj_no, ref, type_no, rid, feed_no, round_no) SELECT obj_no, ref, type_no, rid, feed_no, {R} FROM assigned WHERE fresh"

// decide gives an object to every group of a range of labels.
func (r *identRun) decide(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	var end sql.NullInt64
	endQuery := r.q("SELECT label FROM {label} WHERE round_no = {R} AND iter = {F} AND label > $1 ORDER BY label OFFSET $2 LIMIT 1")
	r.saw(tx, "decide end", endQuery, st.Cur, st.Size)
	err := tx.QueryRowContext(ctx, endQuery, st.Cur, st.Size).Scan(&end)
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
	if st.AskLeft > 0 {
		st.Phase, st.Cur = phaseAsk, 0
		// the questions of the round are all written: the planner is given their number (as it is for the other tables the run fills,
		// analyze), so that the read of a page of them is not planned on a table it takes for empty, which reads every question and joins
		// a record to each when the round asks many (ADR-0121)
		return r.analyze(ctx, tx, pgschema.TableCalcAsk)
	}
	return r.nextRound(ctx, tx)
}

// unitStep is a statement of a unit and where the numbers it gives go: the rows it changes (or, for a statement that gives two numbers,
// the first and the second) are added to the counts of the run as it goes, so that nothing has to read the tables of the run again to
// tell how many there were.
type unitStep struct {
	what, query string
	to          []*int
	// two says that the statement gives two numbers, the first and the second of to (and not the rows it changes).
	two bool
}

// run does a step of a unit: the rows it changed, or the numbers it gave, are added to where they go.
func (r *identRun) run(ctx context.Context, tx *sql.Tx, s unitStep, args ...any) error {
	if s.two {
		a, b, err := r.scanTwo(ctx, tx, s.what, r.q(s.query), args...)
		if err != nil {
			return err
		}
		*s.to[0] += a
		*s.to[1] += b
		return nil
	}
	n, err := r.exec(ctx, tx, s.what, r.q(s.query), args...)
	if err != nil {
		return err
	}
	for _, to := range s.to {
		*to += n
	}
	return nil
}

// decideRange is the statements that decide the groups whose label is in the range.
func (r *identRun) decideRange(ctx context.Context, tx *sql.Tx, lo, hi int64) error {
	if r.st.Unsettled {
		// the pairs of the groups that are not known to be whole are left, and are told: one more run may find them whole (the count is the
		// report's, a line of a problem for each would be a line for every record of a chain)
		c := &r.st.Counts
		leave := unitStep{"leave the records of the groups that are not whole", "WITH " + unsettledSQL + "up AS (SELECT DISTINCT s.rid, s.feed_no FROM bad" +
			scan("x.pid", "{label}", "l", "x.round_no = {R} AND x.iter = {F} AND x.label = bad.label") +
			probe("x.rid, x.feed_no", "{slot}", "s", "x.round_no = {R} AND x.pid = l.pid") + ") " + refUnresolved("up"), []*int{&c.Unsettled, &c.Unresolved}, false}
		before := c.Unsettled
		if err := r.run(ctx, tx, leave, lo, hi); err != nil {
			return err
		}
		r.st.LeftKeys = r.st.LeftKeys || c.Unsettled > before
	}
	for _, s := range r.decideSteps(pairsSQL(r.st.Unsettled)) {
		if err := r.run(ctx, tx, s, lo, hi); err != nil {
			return err
		}
	}
	return nil
}

// decideSteps are the statements of decideRange, in the order they are done in (ps is what pairsSQL gives).
func (r *identRun) decideSteps(pl string) []unitStep {
	c := &r.st.Counts
	conflict := pgschema.ProblemIdentityConflict
	return []unitStep{
		// a record that has a key the index knows is the object of its first such key
		{"bind the records that are known", "WITH " + pl + " INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) " +
			"SELECT DISTINCT ON (s.pid) s.rid, s.feed_no, s.key_ord, s.found_no, false, {R} FROM ps s WHERE s.found_no IS NOT NULL ORDER BY s.pid, s.key_ord", nil, false},
		{"report the keys that stand for another object", "WITH " + pl + " INSERT INTO {prob} (rid, def_no, field, kind, reason) " +
			"SELECT s.rid, s.feed_no, '', '" + conflict + "', (count(DISTINCT s.found_no) - 1) || ' key(s) of the record stand for other objects than the one it is' " +
			"FROM ps s WHERE s.found_no IS NOT NULL GROUP BY s.rid, s.feed_no HAVING count(DISTINCT s.found_no) > 1", nil, false},
		// a group the index knows nothing of is a new object whose id is the candidate of its key
		{"make the new objects", "WITH {feeds}, " + pl + ", " + groupsSQL + ", ex AS (SELECT w.label, w.rid, w.feed_no, w.key_ord, w.type_no, w.cand_ref AS ref, o.obj_no AS reg " +
			"FROM w" + maybe("x.obj_no", "{obj}", "o", "x.ref = w.cand_ref") + " WHERE NOT w.has_ctx), " + makeSQL, []*int{&c.NewObjects}, false},
		// a group whose new object has a parent waits for Go to make its id
		{askStep, "WITH {feeds}, " + pl + ", " + groupsSQL + " INSERT INTO {ask} (round_no, label, rid, feed_no, key_ord, type_no, parent_no, parent_ref, answer) " +
			"SELECT {R}, w.label, w.rid, w.feed_no, w.key_ord, w.type_no, w.ctx_no, COALESCE(o.ref, n.ref), false FROM w" +
			maybe("x.ref", "{obj}", "o", "x.obj_no = w.ctx_no") + maybe("x.ref", "{newobj}", "n", "x.obj_no = w.ctx_no") + " WHERE w.has_ctx", []*int{&r.st.AskLeft}, false},
		// the other records of a group that has one object are that object
		{"bind the records of a group of one object", "WITH " + pl + ", gb AS (SELECT s.label, min(b.obj_no) AS obj, count(DISTINCT b.obj_no) AS c FROM ps s" +
			bindOf("s", "b") + " GROUP BY s.label) " +
			"INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) SELECT s.rid, s.feed_no, min(s.key_ord), gb.obj, false, {R} FROM ps s " +
			"JOIN gb ON gb.label = s.label AND gb.c = 1" + unboundOf("s", "hx") + " WHERE hx.hit IS NULL GROUP BY s.rid, s.feed_no, gb.obj", nil, false},
		// in a group of several objects a record is the object that the records it shares a key with are, when they are one: each key is
		// looked at once (how many objects the pairs that have it are bound to), and a pair takes what its keys say
		{"bind the records of a group of several objects", "WITH " + pl + ", gm AS (SELECT s.label FROM ps s" + bindOf("s", "b") +
			" GROUP BY s.label HAVING count(DISTINCT b.obj_no) > 1), " +
			"gk AS (SELECT DISTINCT s.type_no, s.ctx_no, s.sig_hash FROM ps s JOIN gm ON gm.label = s.label), " +
			"kt AS (SELECT gk.type_no, gk.ctx_no, gk.sig_hash, count(DISTINCT b.obj_no) AS c, min(b.obj_no) AS obj FROM gk" +
			scan("x.rid, x.feed_no", "{slot}", "t", "x.round_no = {R} AND x.type_no = gk.type_no AND x.ctx_no = gk.ctx_no AND x.sig_hash = gk.sig_hash") +
			bindOf("t", "b") + " GROUP BY gk.type_no, gk.ctx_no, gk.sig_hash) " +
			"INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) " +
			"SELECT s.rid, s.feed_no, min(s.key_ord), min(kt.obj), false, {R} FROM ps s JOIN gm ON gm.label = s.label" + unboundOf("s", "hx") +
			" JOIN kt ON kt.type_no = s.type_no AND kt.ctx_no = s.ctx_no AND kt.sig_hash = s.sig_hash WHERE hx.hit IS NULL " +
			"GROUP BY s.rid, s.feed_no HAVING max(kt.c) = 1 AND count(DISTINCT kt.obj) = 1", nil, false},
		{"bind the new keys", r.newKeysSQL(pl), []*int{&c.NewKeys, &c.Dropped}, true},
		{"leave the records that are not bound", r.unboundSQL(pl), []*int{&c.Unresolved}, false},
	}
}

// askStep is the statement that writes the questions for Go: what it writes is what the round waits for (counted as it is written, so
// that no statement has to read all the questions of the round to know how many are left).
const askStep = "ask for the new objects that have a parent"

// maybe is probe for a row that may not be there (the columns of the alias are NULL then).
func maybe(cols, table, alias, cond string) string {
	return " LEFT JOIN LATERAL (SELECT " + cols + " FROM " + table + " x WHERE " + cond + " LIMIT 1) " + alias + " ON true"
}

// newKeysSQL binds the keys the index did not know to the object of the records that have them, and drops a key that leads to two. It
// gives the number of the keys it bound and of the keys it dropped.
func (r *identRun) newKeysSQL(pl string) string {
	return "WITH " + pl + ", ks AS (SELECT s.type_no, s.ctx_no, s.sig_hash, count(DISTINCT b.obj_no) AS c, min(b.obj_no) AS obj FROM ps s" + bindOf("s", "b") +
		" WHERE s.found_no IS NULL GROUP BY s.type_no, s.ctx_no, s.sig_hash), " +
		"nk AS (INSERT INTO {newkey} (type_no, ctx_no, sig_hash, obj_no, round_no) SELECT ks.type_no, ks.ctx_no, ks.sig_hash, ks.obj, {R} FROM ks" +
		absent("{dropped}", "dd", "x.type_no = ks.type_no AND x.ctx_no = ks.ctx_no AND x.sig_hash = ks.sig_hash") +
		" WHERE ks.c = 1 AND dd.hit IS NULL ON CONFLICT DO NOTHING RETURNING 1), " +
		"dr AS (INSERT INTO {dropped} (type_no, ctx_no, sig_hash, round_no) SELECT type_no, ctx_no, sig_hash, {R} FROM ks WHERE c > 1 ON CONFLICT DO NOTHING RETURNING 1) " +
		"SELECT (SELECT count(*) FROM nk), (SELECT count(*) FROM dr)"
}

// unboundSQL leaves the records that were not bound as references that are not solved, each with its problem. The rows it changes are
// the problems, one for each record it leaves.
func (r *identRun) unboundSQL(pl string) string {
	return "WITH " + pl + ", u AS (SELECT DISTINCT s.rid, s.feed_no FROM ps s" + unboundOf("s", "hb") +
		absent("{ask}", "ha", "x.round_no = {R} AND x.label = s.label AND x.answer = false") + " WHERE hb.hit IS NULL AND ha.hit IS NULL), " +
		"r AS (" + refUnresolved("u") + " RETURNING 1) " +
		"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, feed_no, '', '" + pgschema.ProblemIdentityConflict +
		"', 'no object could be told for this record: every key it gives stands for more than one object' FROM u"
}
