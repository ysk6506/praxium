package pgingest

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The last phases of the identification: the records that only find an object, the identifiers the objects are said to have, and the
// references and the objects of the run.

// lookup binds the records of a range whose feed does not make objects, or whose row a rule filters out, to the objects there are
// (an object of the index, or one this run made): they keep an object alive and make none (ingestion-at-scale-identity.md 2.2, rule 6).
func (r *identRun) lookup(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	if st.Cur >= st.MaxRid {
		st.Phase, st.Cur, st.CurKey, st.Cur2 = phaseClaims, 0, "", -1
		return nil
	}
	hi := st.Cur + int64(st.Size)
	none := strconv.Itoa(int(roleIdentity))
	query := "WITH {feeds}, ks AS (SELECT k.rid, k.feed_no, k.key_ord, k.held, f.creates, " + ctxSQL + " AS c, d.n, " +
		"(SELECT 1) AS x FROM {key} k JOIN feeds f ON f.feed_no = k.feed_no" + chainSQL() + " WHERE k.rid > $1 AND k.rid <= $2 AND (NOT f.creates OR k.held) " +
		"AND NOT EXISTS (SELECT 1 FROM {bind} b WHERE b.rid = k.rid AND b.feed_no = k.feed_no) " +
		"AND NOT EXISTS (SELECT 1 FROM {ref} z WHERE z.rid = k.rid AND z.def_no = k.feed_no AND z.role = " + none + ")), " +
		"fk AS (SELECT ks.*, " + lookupSQL("kk.type_no", "ks.c", "kk.sig_hash") + " AS found FROM ks JOIN {key} kk ON kk.rid = ks.rid AND kk.feed_no = ks.feed_no AND kk.key_ord = ks.key_ord WHERE ks.c IS NOT NULL), " +
		"b AS (SELECT DISTINCT ON (rid, feed_no) rid, feed_no, key_ord, found FROM fk WHERE found IS NOT NULL ORDER BY rid, feed_no, key_ord), " +
		"ib AS (INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) SELECT rid, feed_no, key_ord, found, false, 0 FROM b RETURNING 1), " +
		"u AS (SELECT rid, feed_no, bool_or(held) AS held, bool_or(creates) AS creates FROM ks GROUP BY rid, feed_no), " +
		"nb AS (SELECT u.* FROM u WHERE NOT EXISTS (SELECT 1 FROM b WHERE b.rid = u.rid AND b.feed_no = u.feed_no)), " +
		"ir AS (INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT rid, " + none + ", feed_no, 0, NULL, NULL, " +
		"CASE WHEN held THEN " + strconv.Itoa(int(pgschema.RefHeld)) + " ELSE " + strconv.Itoa(int(pgschema.RefUnresolved)) + " END, false FROM nb RETURNING 1) " +
		"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, feed_no, '', '" + pgschema.ProblemOrphanValue + "', 'no object of the type has this identity' FROM nb WHERE NOT held AND NOT creates"
	if _, err := exec(ctx, tx, "find the objects of the records that make none", r.q(query), st.Cur, hi); err != nil {
		return err
	}
	st.Cur = hi
	return nil
}

// claims decides, for a range of the identifiers (scheme and value) that the records say objects have, which object has each: the one
// that has it already and still says so, else the one whose id is the smallest. The others lose, and say so.
func (r *identRun) claims(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	var no sql.NullInt64
	var hash sql.NullString
	endQuery := r.q("SELECT scheme_no, encode(value_hash, 'hex') FROM {claim} WHERE (scheme_no, value_hash) > ($1, decode($2, 'hex')) " +
		"ORDER BY scheme_no, value_hash OFFSET $3 LIMIT 1")
	identSaw(tx, "claims end", endQuery, st.Cur2, st.CurKey, st.Size)
	err := tx.QueryRowContext(ctx, endQuery, st.Cur2, st.CurKey, st.Size).Scan(&no, &hash)
	last := isNoRows(err)
	if err != nil && !last {
		return dbErr(err, "find the end of the unit")
	}
	cond := "(c.scheme_no, c.value_hash) > ($1, decode($2, 'hex'))"
	args := []any{st.Cur2, st.CurKey}
	if !last {
		cond += " AND (c.scheme_no, c.value_hash) <= ($3, decode($4, 'hex'))"
		args = append(args, no.Int64, hash.String)
	}
	if _, err := exec(ctx, tx, "decide the identifiers", r.q(r.claimsSQL(cond)), args...); err != nil {
		return err
	}
	if last {
		st.Phase, st.Cur = phaseFinish, 0
		return nil
	}
	st.Cur2, st.CurKey = no.Int64, hash.String
	return nil
}

func (r *identRun) claimsSQL(cond string) string {
	names := make([]string, 0, len(r.st.Schemes))
	for name := range r.st.Schemes {
		names = append(names, name)
	}
	sort.Strings(names)
	var nos []string
	for i, name := range names {
		nos = append(nos, strconv.Itoa(r.st.Schemes[name]))
		names[i] = `"` + strings.ReplaceAll(name, `"`, `\"`) + `"`
	}
	sch := "sch AS (SELECT name, no FROM unnest('{" + strings.Join(names, ",") + "}'::text[], '{" + strings.Join(nos, ",") + "}'::int[]) AS s(name, no))"
	return "WITH " + sch + ", cl AS (SELECT c.scheme_no, c.value_hash, c.value, c.rid, c.feed_no, b.obj_no FROM {claim} c JOIN {bind} b ON b.rid = c.rid AND b.feed_no = c.feed_no " +
		"WHERE NOT c.held AND " + cond + "), " +
		"cand AS (SELECT scheme_no, value_hash, value, obj_no FROM cl GROUP BY scheme_no, value_hash, value, obj_no), " +
		"hold AS (SELECT cd.scheme_no, cd.value_hash, h.obj_no FROM (SELECT DISTINCT scheme_no, value_hash, value FROM cand) cd JOIN sch ON sch.no = cd.scheme_no " +
		"JOIN {ident} h ON h.scheme = sch.name AND h.value = cd.value AND h.seq_to IS NULL " +
		"WHERE EXISTS (SELECT 1 FROM cand x WHERE x.scheme_no = cd.scheme_no AND x.value_hash = cd.value_hash AND x.obj_no = h.obj_no) " +
		"OR EXISTS (SELECT 1 FROM " + r.t(pgschema.TableRecordRef) + " x WHERE x.role = " + strconv.Itoa(int(pgschema.RefRoleClaim)) + " AND x.def_no = cd.scheme_no AND x.lookup_hash = cd.value_hash " +
		"AND x.target_no = h.obj_no AND x.state = " + strconv.Itoa(int(pgschema.RefResolved)) + " AND NOT EXISTS (SELECT 1 FROM {rec} cr WHERE cr.src_no = x.src_no AND cr.dataset_no = x.dataset_no AND cr.key = x.key))), " +
		"win AS (SELECT DISTINCT ON (scheme_no, value_hash) scheme_no, value_hash, obj_no FROM (SELECT scheme_no, value_hash, obj_no, 0 AS ord, ''::text AS oref FROM hold " +
		"UNION ALL SELECT cd.scheme_no, cd.value_hash, cd.obj_no, 1, COALESCE(o.ref, n.ref) FROM cand cd LEFT JOIN {obj} o ON o.obj_no = cd.obj_no LEFT JOIN {newobj} n ON n.obj_no = cd.obj_no) z " +
		"ORDER BY scheme_no, value_hash, ord, oref COLLATE \"C\"), " +
		"ins AS (INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT cl.rid, " + strconv.Itoa(int(pgschema.RefRoleClaim)) + ", cl.scheme_no, cl.feed_no, cl.value_hash, cl.obj_no, " +
		"CASE WHEN cl.obj_no = win.obj_no THEN " + strconv.Itoa(int(pgschema.RefResolved)) + " ELSE " + strconv.Itoa(int(pgschema.RefLostClaim)) + " END, false " +
		"FROM cl JOIN win ON win.scheme_no = cl.scheme_no AND win.value_hash = cl.value_hash RETURNING rid, sub, state) " +
		"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, sub, '', '" + pgschema.ProblemIdentityConflict + "', 'an identifier of the record already names another object' FROM ins WHERE state = " + strconv.Itoa(int(pgschema.RefLostClaim))
}

// finish ends the identification, a range of the records at a time: the records that no key could bind are references that are not
// solved, the records that are bound are references that are, and the objects they are bound to are the objects of the run.
func (r *identRun) finish(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	if st.Cur >= st.MaxRid {
		return r.count(ctx, tx)
	}
	hi := st.Cur + int64(st.Size)
	id := strconv.Itoa(int(roleIdentity))
	steps := []string{
		"WITH {feeds}, p AS (SELECT k.rid, k.feed_no, bool_or(k.anc_types IS NOT NULL) AS ctx FROM {key} k JOIN feeds f ON f.feed_no = k.feed_no AND f.creates WHERE NOT k.held AND k.rid > $1 AND k.rid <= $2 " +
			"AND NOT EXISTS (SELECT 1 FROM {bind} b WHERE b.rid = k.rid AND b.feed_no = k.feed_no) AND NOT EXISTS (SELECT 1 FROM {ref} z WHERE z.rid = k.rid AND z.def_no = k.feed_no AND z.role = " + id + ") " +
			"GROUP BY k.rid, k.feed_no), r AS (INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT rid, " + id + ", feed_no, 0, NULL, NULL, " +
			strconv.Itoa(int(pgschema.RefUnresolved)) + ", false FROM p RETURNING 1) INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, feed_no, '', " +
			"CASE WHEN ctx THEN '" + pgschema.ProblemContextParent + "' ELSE '" + pgschema.ProblemIdentityConflict + "' END, " +
			"CASE WHEN ctx THEN 'the parent of this record is not found, so its key cannot be told' ELSE 'no key of this record could be bound to an object' END FROM p",
		"INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT rid, " + id + ", feed_no, 0, NULL, obj_no, " +
			strconv.Itoa(int(pgschema.RefResolved)) + ", false FROM {bind} WHERE rid > $1 AND rid <= $2",
		"INSERT INTO {objrun} (obj_no, why) SELECT DISTINCT obj_no, " + strconv.Itoa(int(pgschema.ObjectWhyIdentified)) + " FROM {bind} WHERE rid > $1 AND rid <= $2 ON CONFLICT DO NOTHING",
	}
	for _, s := range steps {
		if _, err := exec(ctx, tx, "end the identification", strings.ReplaceAll(r.q(s), "{objrun}", r.t(pgschema.TableCalcObject)), st.Cur, hi); err != nil {
			return err
		}
	}
	st.Cur = hi
	return nil
}

// count adds up what the identification made, for the report, and ends it.
func (r *identRun) count(ctx context.Context, tx *sql.Tx) error {
	c := &r.st.Counts
	for _, f := range []struct {
		to    *int
		query string
	}{
		{&c.Bound, "SELECT count(*) FROM {bind}"},
		{&c.NewObjects, "SELECT count(*) FROM {newobj}"},
		{&c.NewKeys, "SELECT count(*) FROM {newkey}"},
		{&c.Dropped, "SELECT count(*) FROM {dropped}"},
		{&c.Unresolved, "SELECT count(*) FROM {ref} WHERE role = " + strconv.Itoa(int(roleIdentity)) + " AND state <> " + strconv.Itoa(int(pgschema.RefResolved))},
		{&c.Lost, "SELECT count(*) FROM {ref} WHERE role = " + strconv.Itoa(int(pgschema.RefRoleClaim)) + " AND state = " + strconv.Itoa(int(pgschema.RefLostClaim))},
	} {
		n, err := scanInt(ctx, tx, "count what the identification made", r.q(f.query))
		if err != nil {
			return err
		}
		*f.to = int(n)
	}
	r.st.Phase = phaseDone
	return nil
}
