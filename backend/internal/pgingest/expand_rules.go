package pgingest

import (
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The statements of the rules of the expansion (ingestion-at-scale-identity.md 3). Every one reads the records of its range (a range of
// the numbers the records of the run have) and, from each, the rows of the permanent tables by an index: the key of a record is a prefix
// of the primary key of the references, and the keys of the ends, the claims and the links are the columns of the indexes of the
// references and of the links. What the records of a unit share (an object, an end key, an identifier, a link) is read once for the
// unit, so that a thousand records that share an object do not read what the object has a thousand times.

func expNum(n int16) string { return strconv.Itoa(int(n)) }

// The numbers of the roles of the references and of the reasons an object is in the run, as the text of a statement.
var (
	roleIdent  = expNum(pgschema.RefRoleIdentity)
	roleFrom   = expNum(pgschema.RefRoleLinkFrom)
	roleTo     = expNum(pgschema.RefRoleLinkTo)
	roleClaim  = expNum(pgschema.RefRoleClaim)
	whyIdent   = expNum(pgschema.ObjectWhyIdentified)
	whyOldRef  = expNum(pgschema.ObjectWhyOldRef)
	whyClaim   = expNum(pgschema.ObjectWhyClaim)
	whyRetire  = expNum(pgschema.ObjectWhyRetire)
	stateFound = expNum(pgschema.RefResolved)
)

// expRange is the records of the run of the range of a unit.
const expRange = "r AS (SELECT rid, src_no, dataset_no, key FROM {rec} WHERE rid > $1 AND rid <= $2)"

// expOf reads the rows of a permanent table that a record of r has: the references of a role of its key (a prefix of the primary key).
func expOf(cols, role string) string {
	return " CROSS JOIN LATERAL (SELECT " + cols + " FROM {rref} x WHERE x.src_no = r.src_no AND x.dataset_no = r.dataset_no AND x.key = r.key AND x.role = " + role + " OFFSET 0) "
}

// expAddRecords makes the records of the raw records that from gives (the alias is the raw record) records of the run again: they are
// read as they are kept, and the ones that are in the run already stay as they are.
func expAddRecords(alias, from string) string {
	a := alias + "."
	return "INSERT INTO {rec} (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why) SELECT " + a + "src_no, " + a + "dataset_no, " + a + "key, " + a + "fields, " + a +
		"raw_hash, " + a + "decl_rev, " + a + "opened_seq, " + a + "fate, {WHY} FROM " + from + " ON CONFLICT (src_no, dataset_no, key) DO NOTHING"
}

// expAddProducers makes the records of the records named by a table of (src_no, dataset_no, key), the rows of the CTE pr, records of the run.
func expAddProducers() string {
	return expAddRecords("w", "pr JOIN {raw} w ON w.src_no = pr.src_no AND w.dataset_no = pr.dataset_no AND w.key = pr.key")
}

// expProducers are the records that made the links of the CTE ls (their numbers): the references from the start of a link point at it.
const expProducers = "pr AS (SELECT DISTINCT p.src_no, p.dataset_no, p.key FROM ls CROSS JOIN LATERAL (SELECT y.src_no, y.dataset_no, y.key FROM {rref} y " +
	"WHERE y.target_no = ls.link_no AND y.role = 2 OFFSET 0) p)"

// expOldSQL is the first rule: the objects the records of the run were bound to before (the references of the identity of their keys).
// An object that no open record stands under now, and that a retirable reference points at, is a candidate for retirement: the judge of
// that is the one expression of expand_retire.go. It gives the number of the candidates and of the objects it added.
var expOldSQL = "WITH " + expRange + ", old AS (SELECT DISTINCT x.target_no AS obj_no FROM r" + expOf("x.target_no", roleIdent) + "x WHERE x.target_no IS NOT NULL), " +
	"j AS (SELECT o.obj_no, CASE WHEN " + expRetirable("o.obj_no") + " THEN " + whyRetire + " ELSE " + whyOldRef + " END AS why FROM old o), " +
	"ins AS (INSERT INTO {objrun} (obj_no, why) SELECT obj_no, why FROM j ON CONFLICT DO NOTHING RETURNING why) " +
	"SELECT count(*) FILTER (WHERE why = " + whyRetire + "), count(*) FROM ins"

// expKeys is the records that read the link end keys (def_no and lookup_hash of k) that a record of the range has or gives: the
// records whose ends point at a link of the key, from the start of a link, and each key is read once for the unit.
const expKeys = "pr AS (SELECT DISTINCT f.src_no, f.dataset_no, f.key FROM k CROSS JOIN LATERAL (SELECT x.src_no, x.dataset_no, x.key FROM {rref} x " +
	"WHERE x.role = 2 AND x.def_no = k.def_no AND x.lookup_hash = k.lookup_hash OFFSET 0) f)"

// expKeyOldSQL is the end keys the records of the range had (the references of the ends of a link they were the end of).
var expKeyOldSQL = "WITH " + expRange + ", k AS (SELECT DISTINCT x.def_no, x.lookup_hash FROM r" + expOf("x.def_no, x.lookup_hash", roleTo) +
	"x WHERE x.lookup_hash IS NOT NULL), " + expKeys + " " + expAddProducers()

// expKeyNewSQL is the end keys the records of the range give (what the receiving or the identification made of them).
var expKeyNewSQL = "WITH k AS (SELECT DISTINCT e.def_no, e.lookup_hash FROM {end} e WHERE e.rid > $1 AND e.rid <= $2 AND e.side = " + roleTo + "), " +
	expKeys + " " + expAddProducers()

// expObjects is the objects the records of the range are bound to, before (the references) and now (the binding of the identification).
func expObjects() string {
	return "o AS (SELECT DISTINCT x.obj FROM r CROSS JOIN LATERAL (" +
		"SELECT y.target_no AS obj FROM {rref} y WHERE y.src_no = r.src_no AND y.dataset_no = r.dataset_no AND y.key = r.key AND y.role = " + roleIdent + " AND y.target_no IS NOT NULL " +
		"UNION SELECT b.obj_no FROM {bind} b WHERE b.rid = r.rid) x)"
}

// expLimitOutSQL is the records of the links that compete for the start of a link type that holds it to one: the links of the type that
// start at an object of the records of the range (link_src finds them by the object and the type).
var expLimitOutSQL = "WITH {cards}, " + expRange + ", " + expObjects() + ", ls AS (SELECT l.link_no FROM o CROSS JOIN cards c CROSS JOIN LATERAL " +
	"(SELECT x.link_no FROM {link} x WHERE x.src = o.obj AND x.type_no = c.type_no OFFSET 0) l WHERE c.from_one), " + expProducers + " " + expAddProducers()

// expLimitInSQL is the same for the end: the links of the type that end at an object of the records of the range, at the end the old
// links of the records had, or at the objects the end keys the records give find. A link whose end moves from one object to another
// frees the first and takes the second, and the links that compete for either are looked at.
var expLimitInSQL = "WITH {cards}, " + expRange + ", " + expObjects() + ", " +
	"dd AS (SELECT DISTINCT l.dst AS obj FROM r" + expOf("x.target_no", roleFrom) + "x CROSS JOIN LATERAL (SELECT z.dst FROM {link} z WHERE z.link_no = x.target_no) l), " +
	"nk AS (SELECT DISTINCT e.def_no, e.lookup_hash FROM {end} e WHERE e.rid > $1 AND e.rid <= $2 AND e.side = " + roleFrom + "), " +
	"nd AS (SELECT DISTINCT t.target_no AS obj FROM nk CROSS JOIN LATERAL (SELECT x.target_no FROM {rref} x WHERE x.role = " + roleTo +
	" AND x.def_no = nk.def_no AND x.lookup_hash = nk.lookup_hash AND x.target_no IS NOT NULL OFFSET 0) t), " +
	"y AS (SELECT obj FROM o UNION SELECT obj FROM dd UNION SELECT obj FROM nd), " +
	"ls AS (SELECT l.link_no FROM y CROSS JOIN cards c CROSS JOIN LATERAL (SELECT x.link_no FROM {link} x WHERE x.dst = y.obj AND x.type_no = c.type_no OFFSET 0) l WHERE c.to_one), " +
	expProducers + " " + expAddProducers()

// expClaimSQL is the objects that claim an identifier (scheme and value hash) that a record of the range claims, now or before: the
// claim that another object has for it is decided again, and so is the object that holds it.
var expClaimSQL = "WITH " + expRange + ", k AS (SELECT c.scheme_no AS def_no, c.value_hash AS lookup_hash FROM {claim} c WHERE c.rid > $1 AND c.rid <= $2 AND NOT c.held " +
	"UNION SELECT x.def_no, x.lookup_hash FROM r" + expOf("x.def_no, x.lookup_hash", roleClaim) + "x WHERE x.lookup_hash IS NOT NULL), " +
	"o AS (SELECT DISTINCT f.target_no AS obj_no FROM k CROSS JOIN LATERAL (SELECT x.target_no FROM {rref} x WHERE x.role = " + roleClaim +
	" AND x.def_no = k.def_no AND x.lookup_hash = k.lookup_hash AND x.target_no IS NOT NULL OFFSET 0) f) " +
	"INSERT INTO {objrun} (obj_no, why) SELECT obj_no, " + whyClaim + " FROM o ON CONFLICT DO NOTHING"
