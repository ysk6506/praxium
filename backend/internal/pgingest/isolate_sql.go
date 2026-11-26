package pgingest

// The statements of the fifth step (ADR-0123). Every statement reads a range of the staged rows (the identifiers by the index of their
// bytes, the links by their number or their id) and, for each, what it needs by an index (the identifiers the KG has by the value, the
// objects by their number, the links of the KG at an end by the end and the type), so the rows read go with the rows of the range and not
// with the product of the rows that share an identifier or an end.

// isoIdentOrder is the order the identifiers of the run are read in: the bytes of the scheme, the value and the scope.
const isoIdentOrder = `scheme COLLATE "C", value COLLATE "C", scope COLLATE "C"`

// isoIdentRange is the identifier lines after the place ($1, $2, $3).
const isoIdentRange = "(" + isoIdentOrder + ") > ($1, $2, $3)"

// isoIdentEndSQL is the identifier of the line $4 lines after the place: a page is the identifiers up to it, each with all its lines.
const isoIdentEndSQL = "SELECT scheme, value, scope FROM {stid} WHERE " + isoIdentRange + " ORDER BY " + isoIdentOrder + ", obj_no OFFSET $4 LIMIT 1"

// isoUntouched is the condition that the object o of the KG keeps what it has: the run does not compute it, it is not retired, and the run
// does not retire it (an object the computation left, calc_left, is one of these).
func isoUntouched(o string) string {
	return "(NOT EXISTS (SELECT 1 FROM {stobj} so WHERE so.obj_no = " + o + ") AND EXISTS (SELECT 1 FROM {obj} r WHERE r.obj_no = " + o + " AND r.retired_seq IS NULL) " +
		"AND NOT EXISTS (SELECT 1 FROM {objrun} c WHERE c.obj_no = " + o + " AND c.why = {RETIRE}))"
}

// isoIdentKeysSQL is, for the identifiers of a page ($4..$6 is the last one, or none when $7 is true), every object that has one that more
// than one object has: the objects whose staged identifiers have it (staged), and the objects that have it in the KG and keep it (holds:
// the object is untouched, or its staged identifiers have it too). An object that has it in the KG and is computed without it gives it up.
var isoIdentKeysSQL = "WITH k AS (SELECT i.scheme, i.value, i.scope, i.obj_no FROM {stid} i WHERE " + isoIdentRange + " AND ($7 OR (" + isoIdentOrder + ") <= ($4, $5, $6))), " +
	"v AS (SELECT DISTINCT scheme, value, scope FROM k), " +
	"h AS (SELECT v.scheme, v.value, v.scope, o.obj_no, " + isoUntouched("o.obj_no") + " AS keeps FROM v JOIN {ident} o ON o.scheme = v.scheme AND o.value = v.value " +
	"AND o.scope = v.scope AND o.seq_to IS NULL), " +
	"a AS (SELECT scheme, value, scope, obj_no, bool_or(staged) AS staged, bool_or(inkg) AS inkg, bool_or(keeps) AS keeps FROM (" +
	"SELECT scheme, value, scope, obj_no, true AS staged, false AS inkg, false AS keeps FROM k UNION ALL SELECT scheme, value, scope, obj_no, false, true, keeps FROM h) z " +
	"GROUP BY scheme, value, scope, obj_no), " +
	"b AS (SELECT scheme, value, scope, obj_no, staged, inkg AND (staged OR keeps) AS holds FROM a WHERE staged OR (inkg AND keeps)), " +
	"c AS (SELECT scheme, value, scope FROM b GROUP BY scheme, value, scope HAVING count(*) > 1) " +
	"SELECT b.scheme, b.value, b.scope, b.obj_no, COALESCE(so.ref, r.ref, ''), b.staged, b.holds FROM b JOIN c ON c.scheme = b.scheme AND c.value = b.value AND c.scope = b.scope " +
	"LEFT JOIN {stobj} so ON so.obj_no = b.obj_no LEFT JOIN {obj} r ON r.obj_no = b.obj_no " +
	`ORDER BY b.scheme COLLATE "C", b.value COLLATE "C", b.scope COLLATE "C", b.obj_no`

// The statements that write the identifier lines that lost: the record of them, the lines taken out, the references of the claims of the
// run that are not solved any more, and the references of the claims that are kept (of the records of the run that are read again, whose
// claims the identification did not see) written as lost. Each problem is on the record of a reference (the reason is the one of the
// identification, ingest identify_end.go).
const (
	isoIdentArgs   = "unnest($1::text::bigint[], $2::text::text[], $3::text::text[], $4::text::text[], $5::text::int[], $6::text::text[], $7::text::int[]) AS u(obj, scheme, value, scope, no, hash, why)"
	isoIdentReason = "'an identifier of the record already names another object'"

	isoIdentsSQL = "INSERT INTO {iso} (kind, entity_no, scheme, value, scope, ref, phase, why, reason) SELECT {KIDENT}, u.obj, u.scheme, u.value, u.scope, '', {PIDENT}, u.why, " +
		"'the identifier of the scheme ' || u.scheme || ' that the object has is kept by another object' || CASE WHEN u.why = {IKG} THEN ' of the KG' ELSE ' of the run' END || ', so the object is made without it' " +
		"FROM " + isoIdentArgs + " JOIN {stid} s ON s.obj_no = u.obj AND s.scheme = u.scheme AND s.value = u.value AND s.scope = u.scope ON CONFLICT DO NOTHING"
	isoIdentTakeSQL = "DELETE FROM {stid} s USING " + isoIdentArgs + " WHERE s.obj_no = u.obj AND s.scheme = u.scheme AND s.value = u.value AND s.scope = u.scope"
	isoClaimRunSQL  = "WITH x AS (UPDATE {cref} f SET state = {LOST} FROM " + isoIdentArgs + " WHERE f.role = {CLAIM} AND f.target_no = u.obj AND f.def_no = u.no " +
		"AND f.lookup_hash = decode(u.hash, 'hex') AND f.state = {FOUND} RETURNING f.rid, f.sub) " +
		"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, sub, '', '{KCONFLICT}', " + isoIdentReason + " FROM x"
)

// isoClaimKeptSQL writes the references of the claims that are kept for the lines that lost as lost references of the records of the run.
var isoClaimKeptSQL = "WITH x AS (INSERT INTO {cref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT c.rid, {CLAIM}, s.def_no, s.sub, s.lookup_hash, s.target_no, {LOST}, false " +
	"FROM " + isoIdentArgs + " JOIN {rref} s ON s.role = {CLAIM} AND s.def_no = u.no AND s.lookup_hash = decode(u.hash, 'hex') AND s.target_no = u.obj AND s.state = {FOUND} " +
	"JOIN {rec} c ON c.src_no = s.src_no AND c.dataset_no = s.dataset_no AND c.key = s.key WHERE NOT " + expReplaced("s") +
	" AND NOT EXISTS (SELECT 1 FROM {cref} f WHERE f.rid = c.rid AND f.role = {CLAIM} AND f.def_no = s.def_no AND f.sub = s.sub) RETURNING rid, sub) " +
	"INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT rid, sub, '', '{KCONFLICT}', " + isoIdentReason + " FROM x"

// isoLinkExtentSQL is how many of the next $2 links after the number $1 there are, and the number of the last.
const isoLinkExtentSQL = "SELECT count(*), COALESCE(max(link_no), 0) FROM (SELECT link_no FROM {stlink} WHERE link_no > $1 ORDER BY link_no LIMIT $2) z"

// isoMissing is the condition that the object o is not there after the run (the run does not make or compute it, and the KG does not have
// it alive), and isoRetiring that the run retires it.
func isoMissing(o string) string {
	return "(NOT EXISTS (SELECT 1 FROM {stobj} so WHERE so.obj_no = " + o + ") AND NOT EXISTS (SELECT 1 FROM {obj} r WHERE r.obj_no = " + o + " AND r.retired_seq IS NULL))"
}

func isoRetiring(o string) string {
	return "(NOT EXISTS (SELECT 1 FROM {stobj} so WHERE so.obj_no = " + o + ") AND EXISTS (SELECT 1 FROM {objrun} c WHERE c.obj_no = " + o + " AND c.why = {RETIRE}))"
}

// isoEndsSQL takes the links of the range ($1, $2] whose end is not there (a problem) or retires (they go with the object): it writes them
// to calc_isolated and gives their numbers. An end that is not there is told first.
var isoEndsSQL = "WITH e AS (SELECT l.link_no, l.ref, " + isoMissing("l.src") + " OR " + isoMissing("l.dst") + " AS missing, " +
	isoRetiring("l.src") + " OR " + isoRetiring("l.dst") + " AS retiring FROM {stlink} l WHERE l.link_no > $1 AND l.link_no <= $2) " +
	"INSERT INTO {iso} (kind, entity_no, ref, phase, why, reason) SELECT {KLINK}, e.link_no, e.ref, {PENDS}, CASE WHEN e.missing THEN {MISSING} ELSE {RETIRES} END, " +
	"'the link ' || COALESCE(w.link_name, '') || CASE WHEN e.missing THEN ' ends at an object that the KG does not have and the run does not make, so it is not made' " +
	"ELSE ' ends at an object that retires, so it goes with it' END FROM e LEFT JOIN {win} w ON w.ref = e.ref WHERE e.missing OR e.retiring ON CONFLICT DO NOTHING RETURNING entity_no, why"

// The statements that take links out (the numbers $1, which calc_isolated has): the problem of the record whose end made the link (none
// for a link that goes with an object that retires, and none for a record a rule filtered out, which reports nothing), the reference of
// the record (not solved: the end is gone, or the place is another link's), and the staged link.
const (
	isoTakeProblemsSQL = "INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT w.rid, w.def_no, '', CASE WHEN i.why = {MISSING} THEN '{KMISSING}' ELSE '{KCARD}' END, i.reason " +
		"FROM {iso} i JOIN {win} w ON w.ref = i.ref WHERE i.kind = {KLINK} AND i.entity_no = ANY($1::text::bigint[]) AND i.why IN ({MISSING}, {CARD}) AND NOT w.held"
	isoTakeRefsSQL = "UPDATE {cref} x SET target_no = NULL, state = CASE WHEN w.held THEN {HELD} WHEN i.why = {CARD} THEN {LOST} ELSE {UNRES} END FROM {iso} i JOIN {win} w ON w.ref = i.ref " +
		"WHERE i.kind = {KLINK} AND i.entity_no = ANY($1::text::bigint[]) AND x.role = {FROM} AND x.target_no = i.entity_no AND x.rid = w.rid"
	isoTakeLinksSQL = "DELETE FROM {stlink} WHERE link_no = ANY($1::text::bigint[])"
)

// isoSeatLinksSQL is the next $2 links after the id $1 of the types $3 that the KG has ($4) or not, with the type and the name of each end
// object (the run's, or the KG's).
const isoSeatLinksSQL = "SELECT l.link_no, l.ref, l.type_no, l.src, l.dst, COALESCE(a.type_no, ao.type_no, 0), COALESCE(b.type_no, bo.type_no, 0), " +
	"COALESCE(a.name, ao.name, ''), COALESCE(b.name, bo.name, '') FROM {stlink} l " +
	"LEFT JOIN {stobj} a ON a.obj_no = l.src LEFT JOIN {obj} ao ON ao.obj_no = l.src LEFT JOIN {stobj} b ON b.obj_no = l.dst LEFT JOIN {obj} bo ON bo.obj_no = l.dst " +
	"WHERE l.ref > $1 AND l.type_no = ANY($3::text::int[]) AND EXISTS (SELECT 1 FROM {link} k WHERE k.link_no = l.link_no) = $4 ORDER BY l.ref LIMIT $2"

// isoFixedSQL is the links the KG has at the starts ($1, $2: the types and the objects) and the ends ($3, $4) of the links of a page, that
// stay as they are: the run does not stage them, did not take them out, does not retire an end of them, and a record the run does not read
// stands under them (or no record of an ingestion does: a link that was put by another way). They keep their places.
var isoFixedSQL = "WITH k AS (SELECT k.* FROM unnest($1::text::int[], $2::text::bigint[]) AS e(typ, obj) JOIN {link} k ON k.src = e.obj AND k.type_no = e.typ " +
	"UNION SELECT k.* FROM unnest($3::text::int[], $4::text::bigint[]) AS e(typ, obj) JOIN {link} k ON k.dst = e.obj AND k.type_no = e.typ) " +
	"SELECT k.link_no, k.ref, k.type_no, k.src, k.dst, COALESCE(a.type_no, ao.type_no, 0), COALESCE(b.type_no, bo.type_no, 0), COALESCE(a.name, ao.name, ''), COALESCE(b.name, bo.name, '') FROM k " +
	"LEFT JOIN {stobj} a ON a.obj_no = k.src LEFT JOIN {obj} ao ON ao.obj_no = k.src LEFT JOIN {stobj} b ON b.obj_no = k.dst LEFT JOIN {obj} bo ON bo.obj_no = k.dst " +
	"WHERE NOT EXISTS (SELECT 1 FROM {stlink} s WHERE s.link_no = k.link_no) AND NOT EXISTS (SELECT 1 FROM {iso} i WHERE i.kind = {KLINK} AND i.entity_no = k.link_no) " +
	"AND NOT " + isoRetiring("k.src") + " AND NOT " + isoRetiring("k.dst") + " AND (" +
	"EXISTS (SELECT 1 FROM {rref} f WHERE f.role = {FROM} AND f.target_no = k.link_no AND f.state = {FOUND} AND NOT EXISTS (SELECT 1 FROM {rec} c WHERE c.src_no = f.src_no AND c.dataset_no = f.dataset_no AND c.key = f.key)) " +
	"OR NOT EXISTS (SELECT 1 FROM {rref} f WHERE f.role = {FROM} AND f.target_no = k.link_no AND f.state = {FOUND})) ORDER BY k.ref"

// isoTakenSQL is the places the links of earlier pages took at the starts ($1) and the ends ($2) of the links of a page.
const isoTakenSQL = "SELECT side, end_no, bind_id, link_no FROM {seat} WHERE (side = {OUT} AND end_no = ANY($1::text::bigint[])) OR (side = {IN} AND end_no = ANY($2::text::bigint[]))"

// isoSeatsSQL writes the places the links of a page took (the key of the table is the place: a place a link has already is refused), and
// isoLostSQL the links that lost (each a link the run staged).
const (
	isoSeatsSQL = "INSERT INTO {seat} (side, end_no, bind_id, link_no) SELECT u.side, u.obj, u.bind, u.no FROM unnest($1::text::int[], $2::text::bigint[], $3::text::text[], $4::text::bigint[]) AS u(side, obj, bind, no)"
	isoLostSQL  = "INSERT INTO {iso} (kind, entity_no, ref, phase, why, reason) SELECT {KLINK}, u.no, u.ref, {PCARD}, {CARD}, u.reason FROM " +
		"unnest($1::text::bigint[], $2::text::text[], $3::text::text[]) AS u(no, ref, reason) JOIN {stlink} l ON l.link_no = u.no AND l.ref = u.ref RETURNING entity_no"
)
