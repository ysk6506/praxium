package pgingest

import (
	"strings"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The statements of the cleaning (ADR-0128). A bucket of records is a range of their numbers ($1, $2]; every statement reads the records of
// the range and, for each, what it needs by an index one record at a time (a lateral subquery with OFFSET 0, which the planner does not
// turn into a join that reads a whole table), so the rows a bucket reads go with the records of the bucket and not with the size of the
// tables. The tables the computation only inserted into and that have no index for the record (calc_problem, calc_wait) are read by a
// range of their blocks.

// q writes the names of the tables and the numbers into a statement.
func (r *stlRun) q(query string) string {
	isLink, isFeed := pgschema.ProblemDefIsLinkSQL("p.def_no"), pgschema.ProblemDefIsFeedSQL("p.def_no")
	return strings.NewReplacer(
		"{rec}", r.t(pgschema.TableCalcRecord), "{cref}", r.t(pgschema.TableCalcRef), "{cprob}", r.t(pgschema.TableCalcProblem), "{cend}", r.t(pgschema.TableCalcEnd),
		"{rref}", r.t(pgschema.TableRecordRef), "{rprob}", r.t(pgschema.TableRecordProblem), "{raw}", r.t(pgschema.TableRawRecord),
		"{rawp}", r.t(pgschema.SourcePartition(pgschema.TableRawRecord, r.srcNo)), "{wait}", r.t(pgschema.TableRecordWait), "{cwait}", r.t(pgschema.TableCalcWait),
		"{stobj}", r.t(pgschema.TableStageObject), "{stage}", r.t(pgschema.SourcePartition(pgschema.TableStageRecord, r.srcNo)), "{ds}", pgschema.TableSourceDataset,
		"{EXP}", stlNum(pgschema.CalcWhyExpanded), "{CHG}", stlNum(pgschema.CalcWhyChanged), "{NEW}", stlNum(pgschema.CalcWhyNew), "{CLOSED}", stlNum(pgschema.CalcWhyClosed),
		"{REL}", stlNum(pgschema.CalcWhyReleased), "{RET}", stlNum(pgschema.CalcWhyReturned), "{DUP}", stlNum(pgschema.CalcWhyDuplicates),
		"{IDENT}", stlNum(pgschema.RefRoleIdentity), "{FROM}", stlNum(pgschema.RefRoleLinkFrom), "{RES}", stlNum(pgschema.RefResolved),
		"{F_USED}", stlNum(pgschema.FateUsed), "{F_PROB}", stlNum(pgschema.FateProblem), "{F_FILT}", stlNum(pgschema.FateFiltered), "{F_UNUSED}", stlNum(pgschema.FateUnused),
		"{UNSEEN}", stlNum(pgschema.WaitUnseen), "{ISLINK}", isLink, "{ISFEED}", isFeed, "{ENDDEF}", pgschema.ProblemDefLinkSQL("e.def_no"),
		"{FEEDDEF}", pgschema.ProblemDefFeedSQL("f.feed_no"), "{REPLACEDKINDS}", stlReplacedKinds, "{OBJKINDS}", stlObjectKinds,
		"{DUPKIND}", pgschema.ProblemDuplicateKey, "{VALUEKIND}", pgschema.ProblemInvalidValue,
	).Replace(query)
}

// stlHas is the condition that a row of the tables is there, read by an index for the row of the statement at hand: a scalar subquery that
// stops at the first row, which the planner neither turns into a join nor reads as a hash of the whole table.
func stlHas(from, cond string) string {
	return "((SELECT true FROM " + from + " WHERE " + cond + " LIMIT 1) IS NOT NULL)"
}

// stlKinds is a list of kinds of problem as the text of an SQL list.
func stlKinds(kinds ...string) string { return "'" + strings.Join(kinds, "', '") + "'" }

var (
	// stlReplacedKinds are the kinds of problem of a feed that the steps after the receiving give again for every feed of a record they read
	// again (the identification, the fourth step's conflicts of the values and what it says of an object it left, the fifth step's claims).
	stlReplacedKinds = stlKinds(pgschema.ProblemSourceConflict, pgschema.ProblemIdentityConflict, pgschema.ProblemOrphanValue, pgschema.ProblemMissingName,
		pgschema.ProblemContextParent, pgschema.ProblemShapeChanged)
	// stlObjectKinds are the kinds of problem of a feed that the fourth step (the conflicts of the values) and the fifth (the claims) give
	// again for a record only because it stands under an object the run computed.
	stlObjectKinds = stlKinds(pgschema.ProblemSourceConflict, pgschema.ProblemIdentityConflict)
)

// stlRecs is the records of a bucket: whether the run read the record again and wrote its references anew (replaced: what the expansion
// calls so, expReplaced, with the records that are closed), and whether it was received now (new or changed).
const stlRecs = "r AS MATERIALIZED (SELECT c.rid, c.src_no, c.dataset_no, c.key, c.why, c.page, c.row_no, c.fate, " +
	"(c.why <> {EXP} AND (c.why IN ({CHG}, {NEW}, {CLOSED}, {REL}) OR c.decl_rev = d.decl_rev)) AS replaced, c.why IN ({CHG}, {NEW}) AS received " +
	"FROM {rec} c JOIN {ds} d ON d.dataset_no = c.dataset_no WHERE c.rid > $1 AND c.rid <= $2)"

// stlLinesOf is the lines of a permanent table (alias x) of the record r, read by the index of the record.
func stlLinesOf(table, cols string) string {
	return " CROSS JOIN LATERAL (SELECT x.ctid AS at, " + cols + " FROM " + table + " x WHERE x.src_no = r.src_no AND x.dataset_no = r.dataset_no AND x.key = r.key OFFSET 0) p"
}

// The statements of a bucket of the references and the problems (④), in the order they run. A problem line of a record is taken away when
// the run computed the pair of the record and the definition it is of (ADR-0128, the table of decision 4):
//   - every line of a record the run received (new or changed) or closed;
//   - the line of a repeated key (no definition) of a record the comparison judged again (shown again, let go of, repeating changed);
//   - a line of a link definition of a record the run read again whole (replaced), or of a link definition the record has an end of in
//     the run (the computation of the links reads every end of the records of the run);
//   - a line of a feed: of the kinds the later steps give for every feed of a record they read again whole (replaced); a cell of a feed
//     that only adds values of a record shown again; and a conflict of a value or a claim of a feed whose object the run computed.
//
// The lines of the receiving (missing_identity, the cells of a feed that makes objects) of a record that was not received now stay.
var (
	stlProblemsGoneSQL = "WITH " + stlRecs + " DELETE FROM {rprob} WHERE ctid = ANY(ARRAY(SELECT p.at FROM r" + stlLinesOf("{rprob}", "x.def_no, x.kind") +
		" WHERE r.received OR r.why = {CLOSED}" +
		" OR (p.def_no IS NULL AND p.kind = '{DUPKIND}' AND r.why IN ({RET}, {REL}, {DUP}))" +
		" OR ({ISLINK} AND (r.replaced OR " + stlHas("{cend} e", "e.rid = r.rid AND "+"{ENDDEF} = p.def_no") + "))" +
		" OR ({ISFEED} AND ((r.replaced AND p.kind IN ({REPLACEDKINDS}))" +
		" OR (p.kind = '{VALUEKIND}' AND r.why = {RET} AND NOT " + stlHas("unnest($3::text::int[], $4::text::bool[]) AS f(feed_no, creates)", "f.feed_no = p.def_no AND f.creates") + ")" +
		" OR (p.kind IN ({OBJKINDS}) AND " + stlHas("{rref} y JOIN {stobj} o ON o.obj_no = y.target_no", "y.src_no = r.src_no AND y.dataset_no = r.dataset_no "+
		"AND y.key = r.key AND y.role = {IDENT} AND y.def_no = p.def_no AND y.state = {RES}") + ")))))"

	// stlReceivedSQL writes the problems the receiving found of the records received now: the row of the record the comparison used (its
	// page and place), with the feed named by its id given its number.
	stlReceivedSQL = "WITH " + stlRecs + " INSERT INTO {rprob} (src_no, dataset_no, key, def_no, field, kind, reason) " +
		"SELECT r.src_no, r.dataset_no, r.key, {FEEDDEF}, COALESCE(j.v->>'field', ''), j.v->>'kind', j.v->>'reason' FROM r " +
		"CROSS JOIN LATERAL (SELECT s.problems FROM {stage} s WHERE s.dataset_no = r.dataset_no AND s.key = r.key AND s.page = r.page AND s.row_no = r.row_no OFFSET 0) s " +
		"CROSS JOIN LATERAL jsonb_array_elements(s.problems) AS j(v) LEFT JOIN unnest($3::text::int[], $4::text::text[]) AS f(feed_no, feed_id) ON f.feed_id = j.v->>'feed' " +
		"WHERE r.received AND r.src_no = $5 AND s.problems IS NOT NULL"

	// stlRefsGoneSQL takes away the references of a record the run read again whole or closed, and of the others the ones of a role and a
	// definition the run wrote again.
	stlRefsGoneSQL = "WITH " + stlRecs + " DELETE FROM {rref} WHERE ctid = ANY(ARRAY(SELECT p.at FROM r" + stlLinesOf("{rref}", "x.role, x.def_no") +
		" WHERE r.replaced OR " + stlHas("{cref} y", "y.rid = r.rid AND y.role = p.role AND y.def_no = p.def_no") + "))"

	// stlRefsSQL writes the references the run computed of the records that are open.
	stlRefsSQL = "WITH " + stlRecs + " INSERT INTO {rref} (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable) " +
		"SELECT r.src_no, r.dataset_no, r.key, y.role, y.def_no, y.sub, y.lookup_hash, y.target_no, y.state, y.retirable FROM r " +
		"CROSS JOIN LATERAL (SELECT y.* FROM {cref} y WHERE y.rid = r.rid OFFSET 0) y WHERE r.why <> {CLOSED}"

	// stlProblemsSQL writes the problems the run computed of the records that are open, a range of the blocks of calc_problem at a time.
	stlProblemsSQL = "INSERT INTO {rprob} (src_no, dataset_no, key, def_no, field, kind, reason) SELECT c.src_no, c.dataset_no, c.key, p.def_no, p.field, p.kind, p.reason " +
		"FROM {cprob} p CROSS JOIN LATERAL (SELECT c.src_no, c.dataset_no, c.key, c.why FROM {rec} c WHERE c.rid = p.rid OFFSET 0) c " +
		"WHERE p.ctid >= $1::tid AND p.ctid < $2::tid AND c.why <> {CLOSED}"
)

// The statements of a bucket of the raw records (②). The fate of a record that is open is read from the lines it has after ④ (the ones the
// run wrote and the ones it kept): a row a rule filters out is filtered whatever else it has (the receiving reads the filtered and the unused
// fates as a row that was filtered, and a row whose filter is the same is the same row), a record that has a solved reference of its object
// or of a link it made is used, one that has a problem is a problem, and the others are unused. A record that is closed is deleted, and one
// that was received now is written over with its cells.
var (
	stlFateOf = "CASE WHEN r.fate = {F_FILT} THEN {F_FILT} " +
		"WHEN " + stlHas("{rref} x", "x.src_no = r.src_no AND x.dataset_no = r.dataset_no AND x.key = r.key AND x.role IN ({IDENT}, {FROM}) AND x.state = {RES}") + " THEN {F_USED} " +
		"WHEN " + stlHas("{rprob} x", "x.src_no = r.src_no AND x.dataset_no = r.dataset_no AND x.key = r.key") + " THEN {F_PROB} ELSE {F_UNUSED} END"

	stlClosedSQL = "WITH " + stlRecs + " DELETE FROM {rawp} WHERE ctid = ANY(ARRAY(SELECT p.at FROM r" + stlLinesOf("{rawp}", "x.key") + " WHERE r.why = {CLOSED}))"

	stlOpenedSQL = "WITH " + stlRecs + " INSERT INTO {rawp} (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) " +
		"SELECT c.src_no, c.dataset_no, c.key, c.raw_hash, c.fields, c.decl_rev, c.opened_seq, " + stlFateOf + " FROM r " +
		"CROSS JOIN LATERAL (SELECT c.* FROM {rec} c WHERE c.rid = r.rid OFFSET 0) c WHERE r.received " +
		"ON CONFLICT (src_no, dataset_no, key) DO UPDATE SET raw_hash = excluded.raw_hash, fields = excluded.fields, decl_rev = excluded.decl_rev, " +
		"opened_seq = excluded.opened_seq, fate = excluded.fate"

	stlFatedSQL = "WITH " + stlRecs + ", f AS MATERIALIZED (SELECT r.src_no, r.dataset_no, r.key, " + stlFateOf + " AS fate FROM r WHERE NOT r.received AND r.why <> {CLOSED}) " +
		"UPDATE {raw} w SET fate = f.fate FROM f WHERE w.src_no = f.src_no AND w.dataset_no = f.dataset_no AND w.key = f.key AND w.fate <> f.fate"
)

// The statements of a bucket of the waits (③): the waits the source had are deleted a range of their key at a time, then the run's (the
// comparison wrote every wait of the source that stays, the ones it did not count among them) are written a range of the blocks of
// calc_wait at a time. A wait for the absence of a record that it closed is not written.
const (
	stlWaitsGoneSQL = "WITH d AS MATERIALIZED (SELECT ctid AS at, dataset_no, key, kind FROM {wait} WHERE src_no = $1 AND (dataset_no, key, kind) > ($2::int, $3::text, $4::smallint) " +
		"ORDER BY dataset_no, key, kind LIMIT $5), x AS (DELETE FROM {wait} WHERE ctid = ANY(ARRAY(SELECT at FROM d)) RETURNING 1) " +
		"SELECT n.c, l.dataset_no, l.key, l.kind FROM (SELECT count(*) AS c FROM x) n LEFT JOIN LATERAL (SELECT dataset_no, key, kind FROM d ORDER BY dataset_no DESC, key DESC, kind DESC LIMIT 1) l ON true"
	stlWaitsSQL = "INSERT INTO {wait} (src_no, dataset_no, key, kind, runs, first_at, expired) SELECT src_no, dataset_no, key, kind, runs, first_at, expired FROM {cwait} " +
		"WHERE ctid >= $1::tid AND ctid < $2::tid AND NOT (kind = {UNSEEN} AND expired)"
)

// stlBlocksSQL is how many blocks a table has.
const stlBlocksSQL = "SELECT pg_relation_size($1::regclass) / current_setting('block_size')::bigint"

// stlRowsPerBlock is how many rows a block of calc_problem or calc_wait is taken to have, to turn the rows of a bucket into its blocks.
const stlRowsPerBlock = 50
