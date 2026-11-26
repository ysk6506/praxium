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
	query := "WITH {feeds}, ks AS (SELECT k.rid, k.feed_no, k.key_ord, k.type_no, k.sig_hash, k.held, f.creates, " + ctxSQL + " AS c FROM {key} k" +
		probe("x.src_no", "{rec}", "rc", "x.rid = k.rid") + " JOIN feeds f ON f.src_no = rc.src_no AND f.feed_no = k.feed_no" + chainSQL() +
		absent("{bind}", "hb", "x.rid = k.rid AND x.feed_no = k.feed_no") + absent("{ref}", "hr", "x.rid = k.rid AND x.def_no = k.feed_no AND x.role = "+none) +
		" WHERE k.rid > $1 AND k.rid <= $2 AND (NOT f.creates OR k.held) AND hb.hit IS NULL AND hr.hit IS NULL), " +
		"fk AS (SELECT ks.*, CASE WHEN ks.c IS NOT NULL THEN " + lookupSQL("ks.type_no", "ks.c", "ks.sig_hash") + " END AS found FROM ks), " +
		"b AS (SELECT DISTINCT ON (rid, feed_no) rid, feed_no, key_ord, found FROM fk WHERE found IS NOT NULL ORDER BY rid, feed_no, key_ord), " +
		"ib AS (INSERT INTO {bind} (rid, feed_no, key_ord, obj_no, is_new, round_no) SELECT rid, feed_no, key_ord, found, false, 0 FROM b RETURNING 1), " +
		"nb AS (SELECT rid, feed_no, bool_or(held) AS held, bool_or(creates) AS creates FROM fk GROUP BY rid, feed_no HAVING bool_or(found IS NOT NULL) = false), " +
		"ir AS (INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT rid, " + none + ", feed_no, 0, NULL, NULL, " +
		"CASE WHEN held THEN " + strconv.Itoa(int(pgschema.RefHeld)) + " ELSE " + strconv.Itoa(int(pgschema.RefUnresolved)) + " END, false FROM nb RETURNING 1), " +
		"ip AS (INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, feed_no, '', '" + pgschema.ProblemOrphanValue + "', 'no object of the type has this identity' FROM nb WHERE NOT held AND NOT creates RETURNING 1) " +
		"SELECT count(*) FROM ir"
	n, err := r.scanInt(ctx, tx, "find the objects of the records that make none", r.q(query), st.Cur, hi)
	if err != nil {
		return err
	}
	st.Counts.Unresolved += int(n)
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
	r.saw(tx, "claims end", endQuery, st.Cur2, st.CurKey, st.Size)
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
	lost, err := r.exec(ctx, tx, "decide the identifiers", r.q(r.claimsSQL(cond)), args...)
	if err != nil {
		return err
	}
	st.Counts.Lost += lost
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
	return "WITH " + sch + ", cl AS (SELECT c.scheme_no, c.value_hash, c.value, c.rid, c.feed_no, b.obj_no FROM {claim} c" +
		probe("x.obj_no", "{bind}", "b", "x.rid = c.rid AND x.feed_no = c.feed_no") + " WHERE NOT c.held AND " + cond + "), " +
		"cand AS (SELECT scheme_no, value_hash, value, obj_no FROM cl GROUP BY scheme_no, value_hash, value, obj_no), " +
		"hold AS (SELECT cd.scheme_no, cd.value_hash, h.obj_no FROM (SELECT DISTINCT scheme_no, value_hash, value FROM cand) cd JOIN sch ON sch.no = cd.scheme_no " +
		"JOIN {ident} h ON h.scheme = sch.name AND h.value = cd.value AND h.seq_to IS NULL " +
		"WHERE EXISTS (SELECT 1 FROM cand x WHERE x.scheme_no = cd.scheme_no AND x.value_hash = cd.value_hash AND x.obj_no = h.obj_no) " +
		"OR EXISTS (SELECT 1 FROM " + r.t(pgschema.TableRecordRef) + " x WHERE x.role = " + strconv.Itoa(int(pgschema.RefRoleClaim)) + " AND x.def_no = cd.scheme_no AND x.lookup_hash = cd.value_hash " +
		"AND x.target_no = h.obj_no AND x.state = " + strconv.Itoa(int(pgschema.RefResolved)) + " AND NOT EXISTS (SELECT 1 FROM {rec} cr WHERE cr.src_no = x.src_no AND cr.dataset_no = x.dataset_no AND cr.key = x.key))), " +
		"win AS (SELECT DISTINCT ON (scheme_no, value_hash) scheme_no, value_hash, obj_no FROM (SELECT scheme_no, value_hash, obj_no, 0 AS ord, ''::text AS oref FROM hold " +
		"UNION ALL SELECT cd.scheme_no, cd.value_hash, cd.obj_no, 1, COALESCE(o.ref, n.ref) FROM cand cd" +
		maybe("x.ref", "{obj}", "o", "x.obj_no = cd.obj_no") + maybe("x.ref", "{newobj}", "n", "x.obj_no = cd.obj_no") + ") z " +
		"ORDER BY scheme_no, value_hash, ord, oref COLLATE \"C\"), " +
		"ins AS (INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT cl.rid, " + strconv.Itoa(int(pgschema.RefRoleClaim)) + ", cl.scheme_no, cl.feed_no, cl.value_hash, cl.obj_no, " +
		"CASE WHEN cl.obj_no = win.obj_no THEN " + strconv.Itoa(int(pgschema.RefResolved)) + " ELSE " + strconv.Itoa(int(pgschema.RefLostClaim)) + " END, false " +
		"FROM cl JOIN win ON win.scheme_no = cl.scheme_no AND win.value_hash = cl.value_hash RETURNING rid, sub, state) " +
		"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, sub, '', '" + pgschema.ProblemIdentityConflict + "', 'an identifier of the record already names another object' FROM ins WHERE state = " + strconv.Itoa(int(pgschema.RefLostClaim))
}

// finish ends the identification, a range of the records at a time: the records that no key could bind are references that are not
// solved, the records that are bound are references that are, and the objects they are bound to are the objects of the run. What it
// does adds up what the identification made, for the report.
func (r *identRun) finish(ctx context.Context, tx *sql.Tx) error {
	st := r.st
	if st.Cur >= st.MaxRid {
		st.Phase = phaseDone
		return nil
	}
	hi := st.Cur + int64(st.Size)
	id := strconv.Itoa(int(roleIdentity))
	steps := []unitStep{
		{"end the identification", "WITH {feeds}, p AS (SELECT k.rid, k.feed_no, bool_or(k.anc_types IS NOT NULL) AS ctx FROM {key} k" + probe("x.src_no", "{rec}", "rc", "x.rid = k.rid") +
			" JOIN feeds f ON f.src_no = rc.src_no AND f.feed_no = k.feed_no AND f.creates" +
			absent("{bind}", "hb", "x.rid = k.rid AND x.feed_no = k.feed_no") + absent("{ref}", "hr", "x.rid = k.rid AND x.def_no = k.feed_no AND x.role = "+id) +
			" WHERE NOT k.held AND k.rid > $1 AND k.rid <= $2 AND hb.hit IS NULL AND hr.hit IS NULL GROUP BY k.rid, k.feed_no), " +
			"r AS (" + refUnresolved("p") + " RETURNING 1) INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, feed_no, '', " +
			"CASE WHEN ctx THEN '" + pgschema.ProblemContextParent + "' ELSE '" + pgschema.ProblemIdentityConflict + "' END, " +
			"CASE WHEN ctx THEN 'the parent of this record is not found, so its key cannot be told' ELSE 'no key of this record could be bound to an object' END FROM p",
			[]*int{&st.Counts.Unresolved}, false},
		{"end the identification", "INSERT INTO {ref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT rid, " + id + ", feed_no, 0, NULL, obj_no, " +
			strconv.Itoa(int(pgschema.RefResolved)) + ", false FROM {bind} WHERE rid > $1 AND rid <= $2", []*int{&st.Counts.Bound}, false},
		{"end the identification", "INSERT INTO {objrun} (obj_no, why) SELECT DISTINCT obj_no, " + strconv.Itoa(int(pgschema.ObjectWhyIdentified)) +
			" FROM {bind} WHERE rid > $1 AND rid <= $2 ON CONFLICT DO NOTHING", nil, false},
	}
	for _, s := range steps {
		s.query = strings.ReplaceAll(s.query, "{objrun}", r.t(pgschema.TableCalcObject))
		if err := r.run(ctx, tx, s, st.Cur, hi); err != nil {
			return err
		}
	}
	st.Cur = hi
	return nil
}
