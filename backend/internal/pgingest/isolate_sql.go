package pgingest

// The statements of the fifth step (ADR-0123). Every statement reads a range of the staged rows (the identifiers by the index of their
// bytes, the links by their number or their id) and, for each, what it needs by an index, one row at a time: the objects by their number,
// the identifiers the KG has by the value, the links of the KG at an end by the end and the type. A lookup is a scalar subquery or a
// lateral one with OFFSET 0, which the planner does not turn into a join that reads a whole table (it does that with an EXISTS, and a
// small table invites it): so the rows a unit reads go with the rows of its range, whatever the size of the KG, and not with the product
// of the rows that share an identifier or an end.

// isoIdentOrder is the order the identifiers of the run are read in: the bytes of the scheme, the value and the scope.
const isoIdentOrder = `scheme COLLATE "C", value COLLATE "C", scope COLLATE "C"`

// isoIdentRange is the identifier lines after the place ($1, $2, $3).
const isoIdentRange = "(" + isoIdentOrder + ") > ($1, $2, $3)"

// isoIdentEndSQL is the identifier of the line $4 lines after the place: a page is the identifiers up to it, each with all its lines.
const isoIdentEndSQL = "SELECT scheme, value, scope FROM {stid} WHERE " + isoIdentRange + " ORDER BY " + isoIdentOrder + ", obj_no OFFSET $4 LIMIT 1"

// isoHas is the condition that a table has a row of a condition, read by an index one row at a time.
func isoHas(table, cond string) string {
	return "((SELECT count(*) FROM " + table + " x WHERE " + cond + ") > 0)"
}

// isoStaged, isoAlive and isoRetire say of the object o that the run staged it (it makes or computes it), that the KG has it and it is not
// retired, and that the run retires it.
func isoStaged(o string) string { return isoHas("{stobj}", "x.obj_no = "+o) }
func isoAlive(o string) string  { return isoHas("{obj}", "x.obj_no = "+o+" AND x.retired_seq IS NULL") }
func isoRetire(o string) string { return isoHas("{objrun}", "x.obj_no = "+o+" AND x.why = {RETIRE}") }

// isoUntouched is the condition that the object o of the KG keeps what it has: the run does not compute it, it is not retired, and the run
// does not retire it (an object the computation left, calc_left, is one of these). isoMissing is that the object is not there after the
// run (the run does not stage it, and the KG does not have it alive), and isoRetiring that the run retires it.
func isoUntouched(o string) string {
	return "(NOT " + isoStaged(o) + " AND " + isoAlive(o) + " AND NOT " + isoRetire(o) + ")"
}
func isoMissing(o string) string  { return "(NOT " + isoStaged(o) + " AND NOT " + isoAlive(o) + ")" }
func isoRetiring(o string) string { return "(NOT " + isoStaged(o) + " AND " + isoRetire(o) + ")" }

// isoOf is a column of the object o: the run's row, or the KG's.
func isoOf(column, o, none string) string {
	return "COALESCE((SELECT x." + column + " FROM {stobj} x WHERE x.obj_no = " + o + "), (SELECT x." + column + " FROM {obj} x WHERE x.obj_no = " + o + "), " + none + ")"
}

// isoIdentRowsSQL is how many lines the identifier ($1, $2, $3) has.
const isoIdentRowsSQL = "SELECT count(*) FROM {stid} WHERE (" + isoIdentOrder + ") = ($1, $2, $3)"

// isoIdentKeysSQL is, for the identifiers of a page (the $4 lines after the place, up to the identifier $5..$7 for a page that is not the
// last), every object that has one that more than one object has: the objects whose staged identifiers have it (staged), and the objects
// that have it in the KG and keep it (holds: the object is untouched, or its staged identifiers have it too). An object that has it in the
// KG and is computed without it gives it up. The lines are read in the order of the index with a limit, so the planner reads the range by
// the index and no more (a range of a row of columns is one it cannot tell the size of, and a scheme is often all of the table).
func isoIdentKeysSQL(last bool) string {
	upTo := " WHERE (" + isoIdentOrder + ") <= ($5, $6, $7)"
	if last {
		upTo = ""
	}
	return "WITH k0 AS (SELECT i.scheme, i.value, i.scope, i.obj_no FROM {stid} i WHERE " + isoIdentRange + " ORDER BY " + isoIdentOrder + ", obj_no LIMIT $4), " +
		"k AS (SELECT * FROM k0" + upTo + "), " + isoIdentKeysRest
}

var isoIdentKeysRest = "v AS (SELECT DISTINCT scheme, value, scope FROM k), " +
	"h AS (SELECT v.scheme, v.value, v.scope, o.obj_no, " + isoUntouched("o.obj_no") + " AS keeps FROM v CROSS JOIN LATERAL (SELECT o.obj_no FROM {ident} o " +
	"WHERE o.scheme = v.scheme AND o.value = v.value AND o.scope = v.scope AND o.seq_to IS NULL OFFSET 0) o), " +
	"a AS (SELECT scheme, value, scope, obj_no, bool_or(staged) AS staged, bool_or(inkg) AS inkg, bool_or(keeps) AS keeps FROM (" +
	"SELECT scheme, value, scope, obj_no, true AS staged, false AS inkg, false AS keeps FROM k UNION ALL SELECT scheme, value, scope, obj_no, false, true, keeps FROM h) z " +
	"GROUP BY scheme, value, scope, obj_no), " +
	"b AS (SELECT scheme, value, scope, obj_no, staged, inkg AND (staged OR keeps) AS holds FROM a WHERE staged OR (inkg AND keeps)), " +
	"c AS (SELECT scheme, value, scope FROM b GROUP BY scheme, value, scope HAVING count(*) > 1) " +
	"SELECT b.scheme, b.value, b.scope, b.obj_no, " + isoOf("ref", "b.obj_no", "''") + ", b.staged, b.holds FROM b JOIN c ON c.scheme = b.scheme AND c.value = b.value AND c.scope = b.scope " +
	`ORDER BY b.scheme COLLATE "C", b.value COLLATE "C", b.scope COLLATE "C", b.obj_no`

// The statements that write the identifier lines that lost: the record of them, the lines taken out, the references of the claims of the
// run that are not solved any more, and the references of the claims that are kept (of the records of the run that are read again, whose
// claims the identification did not see) written as lost. Each problem is on the record of a reference (the reason is the one of the
// identification, identify_end.go).
const (
	isoIdentArgs   = "unnest($1::text::bigint[], $2::text::text[], $3::text::text[], $4::text::text[], $5::text::int[], $6::text::text[], $7::text::int[]) AS u(obj, scheme, value, scope, no, hash, why)"
	isoIdentReason = "'an identifier of the record already names another object'"

	// isoIdentLine is the staged line of each identifier that lost, found by the key of the table one line at a time.
	isoIdentLine = " CROSS JOIN LATERAL (SELECT s.ctid AS at FROM {stid} s WHERE s.obj_no = u.obj AND s.scheme = u.scheme AND s.value = u.value AND s.scope = u.scope OFFSET 0) s"

	isoIdentsSQL = "INSERT INTO {iso} (kind, entity_no, scheme, value, scope, ref, phase, why, reason) SELECT {KIDENT}, u.obj, u.scheme, u.value, u.scope, '', {PIDENT}, u.why, " +
		"'the identifier of the scheme ' || u.scheme || ' that the object has is kept by another object' || CASE WHEN u.why = {IKG} THEN ' of the KG' ELSE ' of the run' END || ', so the object is made without it' " +
		"FROM " + isoIdentArgs + isoIdentLine + " ON CONFLICT DO NOTHING"
	isoIdentTakeSQL = "DELETE FROM {stid} WHERE ctid = ANY(ARRAY(SELECT s.at FROM " + isoIdentArgs + isoIdentLine + "))"

	// isoClaimsOf is the references of the claims of the run of each line that lost: the claims of its object of the scheme and the value.
	isoClaimsOf    = " CROSS JOIN LATERAL (SELECT f.ctid AS at, f.rid, f.sub FROM {cref} f WHERE f.role = {CLAIM} AND f.target_no = u.obj AND f.def_no = u.no AND f.lookup_hash = decode(u.hash, 'hex') AND f.state = {FOUND} OFFSET 0) f"
	isoClaimRunSQL = "WITH x AS (UPDATE {cref} SET state = {LOST} WHERE ctid = ANY(ARRAY(SELECT f.at FROM " + isoIdentArgs + isoClaimsOf + ")) RETURNING rid, sub) " +
		"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, sub, '', '{KCONFLICT}', " + isoIdentReason + " FROM x"
)

// isoClaimKeptSQL writes the references of the claims that are kept for the lines that lost as lost references of the records of the run.
var isoClaimKeptSQL = "WITH x AS (INSERT INTO {cref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT c.rid, {CLAIM}, s.def_no, s.sub, s.lookup_hash, s.target_no, {LOST}, false " +
	"FROM " + isoIdentArgs + " CROSS JOIN LATERAL (SELECT s.* FROM {rref} s WHERE s.role = {CLAIM} AND s.def_no = u.no AND s.lookup_hash = decode(u.hash, 'hex') AND s.target_no = u.obj AND s.state = {FOUND} OFFSET 0) s " +
	"CROSS JOIN LATERAL (SELECT c.rid FROM {rec} c WHERE c.src_no = s.src_no AND c.dataset_no = s.dataset_no AND c.key = s.key OFFSET 0) c WHERE NOT " + expReplaced("s") +
	" AND NOT " + isoHas("{cref}", "x.rid = c.rid AND x.role = {CLAIM} AND x.def_no = s.def_no AND x.sub = s.sub") + " RETURNING rid, sub) " +
	"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, sub, '', '{KCONFLICT}', " + isoIdentReason + " FROM x"

// isoLinkExtentSQL is how many of the next $2 links after the number $1 there are, and the number of the last.
const isoLinkExtentSQL = "SELECT count(*), COALESCE(max(link_no), 0) FROM (SELECT link_no FROM {stlink} WHERE link_no > $1 ORDER BY link_no LIMIT $2) z"

// isoEndsSQL takes the links of the range ($1, $2] whose end is not there (a problem) or retires (they go with the object): it writes them
// to calc_isolated and gives their numbers. An end that is not there is told first.
var isoEndsSQL = "WITH e AS (SELECT l.link_no, l.ref, " + isoMissing("l.src") + " OR " + isoMissing("l.dst") + " AS missing, " +
	isoRetiring("l.src") + " OR " + isoRetiring("l.dst") + " AS retiring FROM {stlink} l WHERE l.link_no > $1 AND l.link_no <= $2) " +
	"INSERT INTO {iso} (kind, entity_no, ref, phase, why, reason) SELECT {KLINK}, e.link_no, e.ref, {PENDS}, CASE WHEN e.missing THEN {MISSING} ELSE {RETIRES} END, " +
	"'the link ' || COALESCE((SELECT w.link_name FROM {win} w WHERE w.ref = e.ref), '') || CASE WHEN e.missing THEN ' ends at an object that the KG does not have and the run does not make, so it is not made' " +
	"ELSE ' ends at an object that retires, so it goes with it' END FROM e WHERE e.missing OR e.retiring ON CONFLICT DO NOTHING RETURNING entity_no, why"

// The statements that take links out (the numbers $1, which calc_isolated has): the problem of the record whose end made the link (none
// for a link that goes with an object that retires, and none for a record a rule filtered out, which reports nothing), the reference of
// the record (not solved: the end is gone, or the place is another link's), and the staged link.
const (
	isoTakenRows = "unnest($1::text::bigint[]) AS u(no) CROSS JOIN LATERAL (SELECT i.ref, i.why, i.reason FROM {iso} i WHERE i.kind = {KLINK} AND i.entity_no = u.no OFFSET 0) i " +
		"CROSS JOIN LATERAL (SELECT w.rid, w.def_no, w.held FROM {win} w WHERE w.ref = i.ref OFFSET 0) w"
	isoTakeProblemsSQL = "INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT w.rid, w.def_no, '', CASE WHEN i.why = {MISSING} THEN '{KMISSING}' ELSE '{KCARD}' END, i.reason " +
		"FROM " + isoTakenRows + " WHERE i.why IN ({MISSING}, {CARD}) AND NOT w.held"
	isoTakeRefsSQL = "WITH r AS (SELECT x.at, CASE WHEN w.held THEN {HELD} WHEN i.why = {CARD} THEN {LOST} ELSE {UNRES} END AS state FROM " + isoTakenRows +
		" CROSS JOIN LATERAL (SELECT x.ctid AS at FROM {cref} x WHERE x.role = {FROM} AND x.target_no = u.no AND x.rid = w.rid OFFSET 0) x) " +
		"UPDATE {cref} SET target_no = NULL, state = r.state FROM r WHERE {cref}.ctid = r.at"
	isoTakeLinksSQL = "DELETE FROM {stlink} WHERE link_no = ANY($1::text::bigint[])"
)

// isoLinkCols are the columns of a link at a place (the alias l): its number, id, type and ends, the type and the name of each end object.
var isoLinkCols = "l.link_no, l.ref, l.type_no, l.src, l.dst, " + isoOf("type_no", "l.src", "0") + ", " + isoOf("type_no", "l.dst", "0") + ", " +
	isoOf("name", "l.src", "''") + ", " + isoOf("name", "l.dst", "''")

// isoSeatLinksSQL is the next $2 links after the id $1 of the types $3, with whether the KG has each.
var isoSeatLinksSQL = "SELECT " + isoLinkCols + ", " + isoHas("{link}", "x.link_no = l.link_no") + " FROM {stlink} l " +
	"WHERE l.ref > $1 AND l.type_no = ANY($3::text::int[]) ORDER BY l.ref LIMIT $2"

// isoFixedSQL is the links the KG has at the starts ($1, $2: the types and the objects) and the ends ($3, $4) of the links of a page that
// stay: the ones the run staged again (kept) and the ones it does not touch (it does not stage them, did not take them out, does not
// retire an end of them, and a record the run does not read stands under them, or no record of an ingestion does: a link that was put by
// another way).
var isoFixedSQL = "WITH k AS (SELECT k.* FROM unnest($1::text::int[], $2::text::bigint[]) AS e(typ, obj) CROSS JOIN LATERAL (SELECT k.* FROM {link} k WHERE k.src = e.obj AND k.type_no = e.typ OFFSET 0) k " +
	"UNION SELECT k.* FROM unnest($3::text::int[], $4::text::bigint[]) AS e(typ, obj) CROSS JOIN LATERAL (SELECT k.* FROM {link} k WHERE k.dst = e.obj AND k.type_no = e.typ OFFSET 0) k), " +
	"s AS (SELECT k.*, " + isoHas("{stlink}", "x.link_no = k.link_no") + " AS kept FROM k) " +
	"SELECT " + isoLinkCols + ", l.kept FROM s l WHERE l.kept OR (NOT " + isoHas("{iso}", "x.kind = {KLINK} AND x.entity_no = l.link_no") +
	" AND NOT " + isoRetiring("l.src") + " AND NOT " + isoRetiring("l.dst") + " AND (" +
	"((SELECT count(*) FROM {rref} f WHERE f.role = {FROM} AND f.target_no = l.link_no AND f.state = {FOUND} AND (SELECT count(*) FROM {rec} c " +
	"WHERE c.src_no = f.src_no AND c.dataset_no = f.dataset_no AND c.key = f.key) = 0) > 0) OR NOT " +
	isoHas("{rref}", "x.role = {FROM} AND x.target_no = l.link_no AND x.state = {FOUND}") + ")) ORDER BY l.ref"

// isoTakenSQL is the places the links of earlier pages took at the starts ($1) and the ends ($2) of the links of a page.
const isoTakenSQL = "SELECT side, end_no, bind_id, link_no FROM {seat} WHERE (side = {OUT} AND end_no = ANY($1::text::bigint[])) OR (side = {IN} AND end_no = ANY($2::text::bigint[]))"

// isoSeatsSQL writes the places the links of a page took (the key of the table is the place: a place a link has already is refused), and
// isoLostSQL the links that lost (each a link the run staged).
const (
	isoSeatsSQL = "INSERT INTO {seat} (side, end_no, bind_id, link_no) SELECT u.side, u.obj, u.bind, u.no FROM unnest($1::text::int[], $2::text::bigint[], $3::text::text[], $4::text::bigint[]) AS u(side, obj, bind, no)"
	isoLostSQL  = "INSERT INTO {iso} (kind, entity_no, ref, phase, why, reason) SELECT {KLINK}, u.no, u.ref, {PCARD}, {CARD}, u.reason FROM " +
		"unnest($1::text::bigint[], $2::text::text[], $3::text::text[]) AS u(no, ref, reason) WHERE (SELECT l.ref FROM {stlink} l WHERE l.link_no = u.no) = u.ref RETURNING entity_no"
)
