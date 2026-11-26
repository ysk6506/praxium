package pgingest

import "github.com/ysk6506/praxium/backend/internal/pgschema"

// The statements of the difference (ADR-0127). A statement of the rows phase reads a range of one source by its key ($1 < key <= $2) and,
// for each row of it, what the KG has by an index, one row at a time (a scalar subquery or a lateral one with OFFSET 0, as the isolation
// does: a join could be turned into one that reads a whole table of the KG). What differs is written to calc_diffrow with the kind, the type
// and the change of each row given back, which the unit counts. {T} is the table of the type $3.

// diffExtentSQL is how many of the next $2 rows of a source after the number $1 there are, and the number of the last: col is the number,
// from the table and cond what else a row of the source is.
func diffExtentSQL(col, from, cond string) string {
	return "SELECT count(*), COALESCE(max(x), 0) FROM (SELECT " + col + " AS x FROM " + from + " WHERE " + cond + col + " > $1 ORDER BY " + col + " LIMIT $2) z"
}

// diffReturning is what a statement that writes rows of the difference gives back.
const diffReturning = " RETURNING kind, type_no, change_kind"

// The objects the run staged: the types of a range, and the rows of a type that are new or whose content is not the one the KG has open.
const (
	diffStagedTypesSQL = "SELECT DISTINCT type_no FROM {stobj} WHERE obj_no > $1 AND obj_no <= $2 ORDER BY type_no"
	diffObjectsSQL     = "INSERT INTO {drow} (kind, type_no, ref, entity_no, change_kind, old_hash, new_hash) " +
		"SELECT {KO}, $3, d.ref, d.obj_no, CASE WHEN d.old IS NULL THEN {CA} ELSE {CC} END, d.old, d.content_hash FROM (" +
		"SELECT so.obj_no, so.ref, so.content_hash, (SELECT t.content_hash FROM {T} t WHERE t.obj_no = so.obj_no) AS old FROM {stobj} so " +
		"WHERE so.obj_no > $1 AND so.obj_no <= $2 AND so.type_no = $3) d WHERE d.old IS NULL OR d.old <> d.content_hash" + diffReturning
)

// diffIdentsSQL is the identifiers of the objects of a range the run staged: one the staged identifiers have and the KG has not open is
// added, one the KG has open and the staged identifiers have not is retired. A row names the identifier by its hash (IdentRowHash), which is
// the tie of the rows of one object too. The identifiers of an object the run does not stage (one it retires, one it left) stay as they are.
var diffIdentsSQL = "WITH o AS (SELECT obj_no, ref, type_no FROM {stobj} WHERE obj_no > $1 AND obj_no <= $2), " +
	"n AS (SELECT o.obj_no, o.ref, o.type_no, s.scheme, s.value, s.scope FROM o CROSS JOIN LATERAL (SELECT s.scheme, s.value, s.scope FROM {stid} s WHERE s.obj_no = o.obj_no OFFSET 0) s), " +
	"k AS (SELECT o.obj_no, o.ref, o.type_no, i.scheme, i.value, i.scope FROM o CROSS JOIN LATERAL (SELECT i.scheme, i.value, i.scope FROM {ident} i WHERE i.obj_no = o.obj_no AND i.seq_to IS NULL OFFSET 0) i), " +
	"c AS (SELECT a.*, {CA} AS ch FROM (SELECT * FROM n EXCEPT SELECT * FROM k) a UNION ALL SELECT b.*, {CR} FROM (SELECT * FROM k EXCEPT SELECT * FROM n) b), " +
	"h AS (SELECT c.*, " + pgschema.IdentRowHashExpr("c") + " AS h FROM c) " +
	"INSERT INTO {drow} (kind, type_no, ref, tie, entity_no, change_kind, old_hash, new_hash) " +
	"SELECT {KI}, type_no, ref, h, obj_no, ch, CASE WHEN ch = {CR} THEN h END, CASE WHEN ch = {CA} THEN h END FROM h" + diffReturning

// The objects the run retires (calc_object.why 4, and not staged: isoRetiring of the isolation): the types of a range, and the rows of a
// type that the KG has open.
var (
	diffRetireTypesSQL = "SELECT DISTINCT (SELECT o.type_no FROM {obj} o WHERE o.obj_no = c.obj_no) FROM {objrun} c WHERE c.why = {RETIRE} AND c.obj_no > $1 AND c.obj_no <= $2"
	diffRetireSQL      = "INSERT INTO {drow} (kind, type_no, ref, entity_no, change_kind, old_hash) " +
		"SELECT {KO}, $3, t.ref, t.obj_no, {CR}, t.content_hash FROM {objrun} c CROSS JOIN LATERAL (SELECT t.obj_no, t.ref, t.content_hash FROM {T} t WHERE t.obj_no = c.obj_no OFFSET 0) t " +
		"WHERE c.why = {RETIRE} AND c.obj_no > $1 AND c.obj_no <= $2 AND (SELECT o.type_no FROM {obj} o WHERE o.obj_no = c.obj_no) = $3 AND NOT " + isoStaged("c.obj_no") + diffReturning
)

// diffCloseLinks closes the links of the KG in k (link_no) that the run did not stage and that do not stay (isoKGLinkStays, the condition
// the check of the cardinality counts the places of the KG by). A link may be found by more than one source, or twice by one: it is written
// once.
var diffCloseLinks = "INSERT INTO {drow} (kind, type_no, ref, entity_no, change_kind, old_hash) SELECT {KL}, l.type_no, l.ref, l.link_no, {CR}, l.content_hash " +
	"FROM (SELECT DISTINCT link_no FROM k) k CROSS JOIN LATERAL (SELECT l.* FROM {link} l WHERE l.link_no = k.link_no OFFSET 0) l " +
	"WHERE NOT " + isoHas("{stlink}", "x.link_no = l.link_no") + " AND NOT " + isoKGLinkStays("l") + " ON CONFLICT DO NOTHING" + diffReturning

// The links of the KG that may close: the ones at an object of a range the run retires, the ones a record of a range of the run made (a
// resolved reference of its From end), and the ones of a range the isolation took out.
var (
	diffRetireLinksSQL = "WITH k AS (SELECT k.link_no FROM {objrun} c CROSS JOIN LATERAL (SELECT k.link_no FROM {link} k WHERE k.src = c.obj_no " +
		"UNION SELECT k.link_no FROM {link} k WHERE k.dst = c.obj_no) k WHERE c.why = {RETIRE} AND c.obj_no > $1 AND c.obj_no <= $2 AND NOT " + isoStaged("c.obj_no") + ") " + diffCloseLinks
	diffRefLinksSQL = "WITH k AS (SELECT f.target_no AS link_no FROM {rec} c CROSS JOIN LATERAL (SELECT f.target_no FROM {rref} f WHERE f.src_no = c.src_no AND f.dataset_no = c.dataset_no " +
		"AND f.key = c.key AND f.role = {FROM} AND f.state = {FOUND} AND f.target_no IS NOT NULL OFFSET 0) f WHERE c.rid > $1 AND c.rid <= $2) " + diffCloseLinks
	diffIsoLinksSQL = "WITH k AS (SELECT entity_no AS link_no FROM {iso} WHERE kind = {KLINK} AND entity_no > $1 AND entity_no <= $2) " + diffCloseLinks
)

// diffLinksSQL is the links of a range the run staged that are new or whose content is not the one the KG has open.
const diffLinksSQL = "INSERT INTO {drow} (kind, type_no, ref, entity_no, change_kind, old_hash, new_hash) " +
	"SELECT {KL}, d.type_no, d.ref, d.link_no, CASE WHEN d.old IS NULL THEN {CA} ELSE {CC} END, d.old, d.content_hash FROM (" +
	"SELECT l.link_no, l.ref, l.type_no, l.content_hash, (SELECT k.content_hash FROM {link} k WHERE k.link_no = l.link_no) AS old FROM {stlink} l " +
	"WHERE l.link_no > $1 AND l.link_no <= $2) d WHERE d.old IS NULL OR d.old <> d.content_hash" + diffReturning

// The keys the run binds (calc_newkey) that the index of the KG does not have: the key $4 keys after the place ($1, $2, $3) ends a unit, and
// a key row names the key by its hash (KeyRowHash), which is the tie of the rows of one object too.
const diffKeyEndSQL = "SELECT type_no, ctx_no, sig_hash FROM {newkey} WHERE (type_no, ctx_no, sig_hash) > ($1, $2, $3) ORDER BY type_no, ctx_no, sig_hash OFFSET $4 LIMIT 1"

func diffKeysSQL(last bool) string {
	upTo := " AND (n.type_no, n.ctx_no, n.sig_hash) <= ($4, $5, $6)"
	if last {
		upTo = ""
	}
	h := pgschema.KeyRowHashExpr("n")
	return "INSERT INTO {drow} (kind, type_no, ref, tie, entity_no, change_kind, new_hash, ctx_no, sig_hash) SELECT {KK}, n.type_no, " +
		"COALESCE((SELECT so.ref FROM {stobj} so WHERE so.obj_no = n.obj_no), (SELECT o.ref FROM {obj} o WHERE o.obj_no = n.obj_no)), " + h + ", n.obj_no, {CA}, " + h + ", n.ctx_no, n.sig_hash " +
		"FROM {newkey} n WHERE (n.type_no, n.ctx_no, n.sig_hash) > ($1, $2, $3)" + upTo + " AND NOT " +
		isoHas("{okey}", "x.type_no = n.type_no AND x.ctx_no = n.ctx_no AND x.sig_hash = n.sig_hash") + diffReturning
}

// The order: the plan is written from the counts, and the rows of a kind and a type ($1, $2) after the place ($3, $4) are numbered in the
// order of their id and tie, $5 being how many of them are numbered already and $6 the most rows of a chunk; the row $5 rows after the place
// (diffEndSQL) ends a unit that is not the last ($7, $8). The first kind and type of the plan is the one after the place (-1, -1).
const (
	diffPlanSQL = "INSERT INTO {dplan} (kind, type_no, lines, ord, chunk_no) SELECT * FROM unnest($1::text::smallint[], $2::text::smallint[], $3::text::int[], $4::text::bigint[], $5::text::int[])"
	diffNextSQL = "SELECT kind, type_no, lines FROM {dplan} WHERE (kind, type_no) > ($1, $2) ORDER BY kind, type_no LIMIT 1"
	diffEndSQL  = "SELECT ref, tie FROM {drow} WHERE kind = $1 AND type_no = $2 AND (ref, tie) > ($3, $4) ORDER BY ref, tie OFFSET $5 LIMIT 1"
)

func diffOrderSQL(last bool) string {
	upTo := " AND (r.ref, r.tie) <= ($7, $8)"
	if last {
		upTo = ""
	}
	return "INSERT INTO {diff} (ord, chunk_no, kind, type_no, entity_no, ref, change_kind, old_hash, new_hash, ctx_no, sig_hash) " +
		"SELECT p.ord + r.o, p.chunk_no + ((r.o - 1) / $6)::int, r.kind, r.type_no, r.entity_no, r.ref, r.change_kind, r.old_hash, r.new_hash, r.ctx_no, r.sig_hash " +
		"FROM (SELECT r.*, $5::bigint + row_number() OVER (ORDER BY r.ref, r.tie) AS o FROM {drow} r WHERE r.kind = $1 AND r.type_no = $2 AND (r.ref, r.tie) > ($3, $4)" + upTo + ") r " +
		"CROSS JOIN (SELECT ord, chunk_no FROM {dplan} WHERE kind = $1 AND type_no = $2) p"
}
