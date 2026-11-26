package pgingest

import (
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The statements of the rules of the expansion (ingestion-at-scale-identity.md 3). Every one reads the records of its range (a number
// of records of the run, taken in the order of their numbers) and, from each, the rows of the permanent tables by an index: the key of
// a record is a prefix of the primary key of the references, and the keys of the ends, the claims and the links are the columns of the
// indexes of the references and of the links.
//
// What many records share (an end key, an object, an identifier) is read by one unit, whatever the number of units the records are in
// (ADR-0118): a statement writes the keys its records have to calc_xkey and reads the rows that a key has only for the keys it wrote
// itself (INSERT ... ON CONFLICT DO NOTHING RETURNING), so a key that an earlier unit wrote is not read again. A unit writes the keys and
// the rows it made of them in one transaction, so a key is read by the unit that wrote it, or the unit did not happen. The rule that
// goes through the objects it finds (old) has the objects of calc_object for that, and the ones that go through the objects of the
// run (retire_out and retire_in) have the objects as their unit.

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

// The kinds of key of calc_xkey: what a unit has read the rows of, by the statement that reads them (one each, so that a rule that is
// left out leaves its own keys unread and no other rule's).
const (
	xkKeyOld   int16 = 1 // an end key the records had: the records that read it
	xkKeyNew   int16 = 2 // an end key the records give: the records that read it
	xkLimitOut int16 = 3 // an object a record is bound to: its links that start at it
	xkLimitKey int16 = 4 // an end key a record gives for the end of a link: the objects it finds
	xkLimitIn  int16 = 5 // an object: its links that end at it
	xkClaim    int16 = 6 // an identifier a record claims: the objects that claim it
)

// expRange is the records of the run of the range of a unit.
const expRange = "r AS (SELECT rid, src_no, dataset_no, key FROM {rec} WHERE rid > $1 AND rid <= $2)"

// expOf reads the rows of a permanent table that a record of r has: the references of a role of its key (a prefix of the primary key).
func expOf(cols, role string) string {
	return " CROSS JOIN LATERAL (SELECT " + cols + " FROM {rref} x WHERE x.src_no = r.src_no AND x.dataset_no = r.dataset_no AND x.key = r.key AND x.role = " + role + " OFFSET 0) "
}

// expFresh is the keys of a kind that the statement is the first to have: the keys of from (the columns cols, a number and a hash)
// that no unit wrote before. It is the rows the key is read for, so that it is read once. The keys come back as key_no and key_hash.
func expFresh(name string, kind int16, cols, from string) string {
	return name + " AS (INSERT INTO {xkey} (" + pgschema.ColKind + ", " + pgschema.ColKeyNo + ", " + pgschema.ColKeyHash + ") SELECT " + expNum(kind) + ", " + cols +
		" FROM " + from + " ON CONFLICT DO NOTHING RETURNING " + pgschema.ColKeyNo + ", " + pgschema.ColKeyHash + ")"
}

// expAddRecords makes the records of the raw records that from gives (the alias is the raw record, where is what the rows must meet)
// records of the run again: they are read as they are kept. The ones that are in the run already are left out before the insert, so
// that the insert has no row to turn away (a row that the insert turns away has used a number of the records all the same, and the
// numbers of the records would then follow what was looked at and not what was made). ON CONFLICT is for the race that this does not see.
func expAddRecords(alias, from, where string) string {
	a := alias + "."
	return "INSERT INTO {rec} (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why) SELECT " + a + "src_no, " + a + "dataset_no, " + a + "key, " + a + "fields, " + a +
		"raw_hash, " + a + "decl_rev, " + a + "opened_seq, " + a + "fate, {WHY} FROM " + from + " WHERE " + where +
		" AND NOT EXISTS (SELECT 1 FROM {rec} c WHERE c.src_no = " + a + "src_no AND c.dataset_no = " + a + "dataset_no AND c.key = " + a + "key) ON CONFLICT (src_no, dataset_no, key) DO NOTHING"
}

// expAddProducers makes the records of the records named by a table of (src_no, dataset_no, key), the rows of the CTE pr, records of the run.
func expAddProducers() string {
	return expAddRecords("w", "pr JOIN {raw} w ON w.src_no = pr.src_no AND w.dataset_no = pr.dataset_no AND w.key = pr.key", "true")
}

// expProducers are the records that made the links of the CTE ls (their numbers): the references from the start of a link point at it.
const expProducers = "pr AS (SELECT DISTINCT p.src_no, p.dataset_no, p.key FROM ls CROSS JOIN LATERAL (SELECT y.src_no, y.dataset_no, y.key FROM {rref} y " +
	"WHERE y.target_no = ls.link_no AND y.role = 2 OFFSET 0) p)"

// expOldSQL is the first rule: the objects the records of the run were bound to before (the references of the identity of their keys).
// An object that no open record stands under now, and that a retirable reference points at, is a candidate for retirement: the judge of
// that is the one expression of expand_retire.go. An object that is in calc_object already (the identification put it there, or an earlier
// unit did) is decided and is left out before it is judged, so that the references an object has are read by the unit that decides it.
// It gives the number of the candidates and of the objects it added. A run whose identification left pairs makes no candidate.
var expOldSQL = "WITH {feeds}, " + expRange + ", old AS (SELECT DISTINCT x.target_no AS obj_no FROM r" + expOf("x.target_no", roleIdent) +
	"x WHERE x.target_no IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {objrun} d WHERE d.obj_no = x.target_no)), " +
	"j AS (SELECT o.obj_no, CASE WHEN {canretire} AND " + expRetirable("o.obj_no") + " THEN " + whyRetire + " ELSE " + whyOldRef + " END AS why FROM old o), " +
	"ins AS (INSERT INTO {objrun} (obj_no, why) SELECT obj_no, why FROM j ON CONFLICT DO NOTHING RETURNING why) " +
	"SELECT count(*) FILTER (WHERE why = " + whyRetire + "), count(*) FROM ins"

// expKeys is the records that read the link end keys of the CTE fresh (the number and the hash of each): the records whose ends point at
// a link of the key, from the start of a link.
const expKeys = "pr AS (SELECT DISTINCT f.src_no, f.dataset_no, f.key FROM fresh CROSS JOIN LATERAL (SELECT x.src_no, x.dataset_no, x.key FROM {rref} x " +
	"WHERE x.role = 2 AND x.def_no = fresh.key_no AND x.lookup_hash = fresh.key_hash OFFSET 0) f)"

// expKeyOldSQL is the end keys the records of the range had (the references of the ends of a link they were the end of).
var expKeyOldSQL = "WITH " + expRange + ", k AS (SELECT DISTINCT x.def_no, x.lookup_hash FROM r" + expOf("x.def_no, x.lookup_hash", roleTo) +
	"x WHERE x.lookup_hash IS NOT NULL), " + expFresh("fresh", xkKeyOld, "k.def_no, k.lookup_hash", "k") + ", " + expKeys + " " + expAddProducers()

// expKeyNewSQL is the end keys the records of the range give (what the receiving or the identification made of them).
var expKeyNewSQL = "WITH k AS (SELECT DISTINCT e.def_no, e.lookup_hash FROM {end} e WHERE e.rid > $1 AND e.rid <= $2 AND e.side = " + roleTo + "), " +
	expFresh("fresh", xkKeyNew, "k.def_no, k.lookup_hash", "k") + ", " + expKeys + " " + expAddProducers()

// expObjects is the objects the records of the range are bound to, before (the references) and now (the binding of the identification).
func expObjects() string {
	return "o AS (SELECT DISTINCT x.obj FROM r CROSS JOIN LATERAL (" +
		"SELECT y.target_no AS obj FROM {rref} y WHERE y.src_no = r.src_no AND y.dataset_no = r.dataset_no AND y.key = r.key AND y.role = " + roleIdent + " AND y.target_no IS NOT NULL " +
		"UNION SELECT b.obj_no FROM {bind} b WHERE b.rid = r.rid) x)"
}

// expLimitOutSQL is the records of the links that compete for the start of a link type that holds it to one: the links of the type that
// start at an object of the records of the range (link_src finds them by the object and the type), for the objects that no unit read.
var expLimitOutSQL = "WITH {cards}, " + expRange + ", " + expObjects() + ", " + expFresh("fresh", xkLimitOut, "o.obj, ''::bytea", "o") + ", " +
	"ls AS (SELECT l.link_no FROM fresh CROSS JOIN cards c CROSS JOIN LATERAL " +
	"(SELECT x.link_no FROM {link} x WHERE x.src = fresh.key_no AND x.type_no = c.type_no OFFSET 0) l WHERE c.from_one), " + expProducers + " " + expAddProducers()

// expLimitInSQL is the same for the end: the links of the type that end at an object of the records of the range, at the end the old
// links of the records had, or at the objects the end keys the records give find. A link whose end moves from one object to another
// frees the first and takes the second, and the links that compete for either are looked at. The end keys the records give (nk) are read
// once each (fk, the objects they find), and so are the objects (fo, their links), the ones the records have and the ones the keys find.
var expLimitInSQL = "WITH {cards}, " + expRange + ", " + expObjects() + ", " +
	"dd AS (SELECT DISTINCT l.dst AS obj FROM r" + expOf("x.target_no", roleFrom) + "x CROSS JOIN LATERAL (SELECT z.dst FROM {link} z WHERE z.link_no = x.target_no) l), " +
	"nk AS (SELECT DISTINCT e.def_no, e.lookup_hash FROM {end} e WHERE e.rid > $1 AND e.rid <= $2 AND e.side = " + roleFrom + "), " +
	expFresh("fk", xkLimitKey, "nk.def_no, nk.lookup_hash", "nk") + ", " +
	"nd AS (SELECT DISTINCT t.target_no AS obj FROM fk CROSS JOIN LATERAL (SELECT x.target_no FROM {rref} x WHERE x.role = " + roleTo +
	" AND x.def_no = fk.key_no AND x.lookup_hash = fk.key_hash AND x.target_no IS NOT NULL OFFSET 0) t), " +
	"y AS (SELECT obj FROM o UNION SELECT obj FROM dd UNION SELECT obj FROM nd), " + expFresh("fo", xkLimitIn, "y.obj, ''::bytea", "y") + ", " +
	"ls AS (SELECT l.link_no FROM fo CROSS JOIN cards c CROSS JOIN LATERAL (SELECT x.link_no FROM {link} x WHERE x.dst = fo.key_no AND x.type_no = c.type_no OFFSET 0) l WHERE c.to_one), " +
	expProducers + " " + expAddProducers()

// expClaimSQL is the objects that claim an identifier (scheme and value hash) that a record of the range claims, now or before: the
// claim that another object has for it is decided again, and so is the object that holds it. An identifier is read once.
var expClaimSQL = "WITH " + expRange + ", k AS (SELECT c.scheme_no AS def_no, c.value_hash AS lookup_hash FROM {claim} c WHERE c.rid > $1 AND c.rid <= $2 AND NOT c.held " +
	"UNION SELECT x.def_no, x.lookup_hash FROM r" + expOf("x.def_no, x.lookup_hash", roleClaim) + "x WHERE x.lookup_hash IS NOT NULL), " +
	expFresh("fresh", xkClaim, "k.def_no, k.lookup_hash", "k") + ", " +
	"o AS (SELECT DISTINCT f.target_no AS obj_no FROM fresh CROSS JOIN LATERAL (SELECT x.target_no FROM {rref} x WHERE x.role = " + roleClaim +
	" AND x.def_no = fresh.key_no AND x.lookup_hash = fresh.key_hash AND x.target_no IS NOT NULL OFFSET 0) f) " +
	"INSERT INTO {objrun} (obj_no, why) SELECT obj_no, " + whyClaim + " FROM o ON CONFLICT DO NOTHING"
