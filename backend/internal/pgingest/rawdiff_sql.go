package pgingest

import (
	"math"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The statement of one bucket of the comparison of the received rows with the raw records (ADR-0102). It is one statement of
// common table expressions that compare, and three inserts that write what they found, so every row is judged once and the three
// results cannot disagree. The names of the tables are those of adapter.pgschema; the columns are named in the text, and a
// test that runs the statement on a real schema holds them to the columns the tables have.
//
// The parameters: $1 the source, $2 and $3 the dataset and the key the bucket begins at, $4 and $5 those it ends before, $6 the time
// of the run, $7 the number of the run (the number the records it opens get), $8 to $10 the datasets (their numbers, what each
// counts as and whether it retires), $11 the seconds of the span of a wait and $12 the runs of a wait.
//
// The part of the work of every key is written in the comments of the statement.
const rawDiffTemplate = `
WITH cfg AS (
  -- the datasets of the source: what each counts as, whether a wait for it can end, and the revision of its declaration
  SELECT t.d AS dataset_no, t.c AS cover, t.r AS retire,
         (SELECT sd.decl_rev FROM {source_dataset} sd WHERE sd.dataset_no = t.d) AS decl_rev
  FROM unnest($8::int[], $9::text[], $10::bool[]) AS t(d, c, r)
),
w AS (
  -- the row of every key that is used (the last of the batch, by page and row), and how many rows had the key; a dataset that is
  -- left out whole gives none
  SELECT DISTINCT ON (s.dataset_no, s.key)
         s.dataset_no, s.key, s.page, s.row_no, s.filtered, s.change_kind, s.raw_hash, s.fields,
         count(*) OVER (PARTITION BY s.dataset_no, s.key) AS rows_n
  FROM {stage} s
  JOIN cfg ON cfg.dataset_no = s.dataset_no AND cfg.cover <> 'incomplete'
  WHERE s.row_no >= 0 AND {in_range_s}
  ORDER BY s.dataset_no, s.key, s.page DESC, s.row_no DESC
),
wt AS (
  -- what is waited for, by key: unseen is 0 and filtered is 1
  SELECT dataset_no, key,
         max(runs) FILTER (WHERE kind = {unseen}) AS u_runs,
         max(first_at) FILTER (WHERE kind = {unseen}) AS u_first,
         max(runs) FILTER (WHERE kind = {filtered}) AS f_runs,
         max(first_at) FILTER (WHERE kind = {filtered}) AS f_first,
         coalesce(bool_or(expired) FILTER (WHERE kind = {filtered}), false) AS f_expired
  FROM {wait}
  WHERE src_no = $1 AND {in_range}
  GROUP BY dataset_no, key
),
hd AS MATERIALIZED (
  -- the keys that have the problem of a repeated key from an earlier run
  SELECT DISTINCT dataset_no, key FROM {problem}
  WHERE src_no = $1 AND kind = 'duplicate_key' AND {in_range}
),
pres AS (
  -- a key that is shown: what it was waited for as, and what the wait for a rule that filters it makes of this run
  SELECT w.dataset_no, w.key, w.page, w.row_no, w.filtered, w.change_kind, w.raw_hash, w.fields, w.rows_n,
         cfg.retire, cfg.decl_rev,
         (wt.u_runs IS NOT NULL) AS was_unseen,
         (wt.f_runs IS NOT NULL) AS was_filtered,
         coalesce(wt.f_expired, false) AS was_expired,
         (hd.key IS NOT NULL) AS had_dup,
         CASE WHEN w.filtered THEN coalesce(wt.f_runs, 0) + 1 END AS f_runs,
         CASE WHEN w.filtered THEN coalesce(wt.f_first, date_trunc('second', $6::timestamptz)) END AS f_first
  FROM w
  JOIN cfg ON cfg.dataset_no = w.dataset_no
  LEFT JOIN wt ON wt.dataset_no = w.dataset_no AND wt.key = w.key
  LEFT JOIN hd ON hd.dataset_no = w.dataset_no AND hd.key = w.key
),
judged AS (
  -- why a shown key is a record of the run, if it is: new, changed, shown again, let go of, or its repeating began or ended
  SELECT p.*,
         (p.filtered AND p.retire AND p.f_runs >= $12 AND $6::timestamptz - p.f_first >= make_interval(secs => $11::float8)) AS f_over
  FROM pres p
),
why AS (
  SELECT f.*,
         CASE
           WHEN f.change_kind = {c_new} THEN {w_new}
           WHEN f.change_kind = {c_changed} THEN {w_changed}
           WHEN f.was_unseen OR (f.was_filtered AND NOT f.filtered) THEN {w_returned}
           WHEN f.f_over AND NOT f.was_expired THEN {w_released}
           WHEN (f.rows_n > 1) <> f.had_dup THEN {w_duplicates}
         END AS why
  FROM judged f
),
absent AS (
  -- a key that is kept and is not shown, in a dataset that was read whole and that retires: one more run of the wait
  SELECT r.dataset_no, r.key,
         coalesce(wt.u_runs, 0) + 1 AS runs,
         coalesce(wt.u_first, date_trunc('second', $6::timestamptz)) AS first_at
  FROM {raw} r
  JOIN cfg ON cfg.dataset_no = r.dataset_no AND cfg.cover = 'complete' AND cfg.retire
  LEFT JOIN wt ON wt.dataset_no = r.dataset_no AND wt.key = r.key
  WHERE r.src_no = $1 AND {in_range_r}
    AND NOT EXISTS (SELECT 1 FROM w WHERE w.dataset_no = r.dataset_no AND w.key = r.key)
),
absent2 AS (
  SELECT a.*, (a.runs >= $12 AND $6::timestamptz - a.first_at >= make_interval(secs => $11::float8)) AS expired
  FROM absent a
),
starts AS (
  -- the records the run goes on with: the shown ones that have a reason, and the ones that are closed
  SELECT dataset_no, key, fields, raw_hash, decl_rev, filtered, why, page, row_no, rows_n FROM why WHERE why IS NOT NULL
  UNION ALL
  SELECT dataset_no, key, NULL::text[], NULL::bytea, NULL::int, false, {w_closed}::smallint, NULL::int, NULL::int, 0 FROM absent2 WHERE expired
),
ins_rec AS (
  INSERT INTO {calc_record} (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why, page, row_no)
  SELECT $1::int, v.dataset_no, v.key, coalesce(v.fields, r.fields), coalesce(v.raw_hash, r.raw_hash),
         CASE WHEN v.why IN ({w_new}, {w_changed}) THEN v.decl_rev ELSE r.decl_rev END,
         CASE WHEN v.why IN ({w_new}, {w_changed}) THEN $7::bigint ELSE r.opened_seq END,
         CASE WHEN v.why IN ({w_new}, {w_changed}) THEN (CASE WHEN v.filtered THEN {fate_filtered} ELSE {fate_used} END) ELSE r.fate END,
         v.why,
         CASE WHEN v.why IN ({w_new}, {w_changed}) THEN v.page END,
         CASE WHEN v.why IN ({w_new}, {w_changed}) THEN v.row_no END
  FROM starts v
  LEFT JOIN LATERAL (
    SELECT k.fields, k.raw_hash, k.decl_rev, k.opened_seq, k.fate FROM {raw} k
    WHERE k.src_no = $1 AND k.dataset_no = v.dataset_no AND k.key = v.key AND v.why NOT IN ({w_new}, {w_changed})
  ) r ON true
  RETURNING rid, dataset_no, key
),
ins_prob AS (
  INSERT INTO {calc_problem} (rid, def_no, field, kind, reason)
  SELECT i.rid, NULL, '', 'duplicate_key', 'a later row has the same key and is used instead'
  FROM ins_rec i JOIN starts s ON s.dataset_no = i.dataset_no AND s.key = i.key
  WHERE s.rows_n > 1
  RETURNING 1
),
ins_wait AS (
  INSERT INTO {calc_wait} (src_no, dataset_no, key, kind, runs, first_at, expired)
  -- a shown key that a rule filters is waited for, and one that is not filtered is waited for no more
  SELECT $1, dataset_no, key, {filtered}, f_runs, f_first, f_over FROM why WHERE filtered
  UNION ALL
  -- a key that is not shown is waited for as absent
  SELECT $1, dataset_no, key, {unseen}, runs, first_at, expired FROM absent2
  UNION ALL
  -- what is not counted stays as it is: a dataset left out whole, a key that a read of what is seen does not show, and an absence
  -- in a dataset that never retires
  SELECT x.src_no, x.dataset_no, x.key, x.kind, x.runs, x.first_at, x.expired
  FROM {wait} x
  LEFT JOIN cfg ON cfg.dataset_no = x.dataset_no
  WHERE x.src_no = $1 AND {in_range_x}
    AND (coalesce(cfg.cover, 'incomplete') = 'incomplete'
         OR (cfg.cover = 'seen_only' AND NOT EXISTS (SELECT 1 FROM w WHERE w.dataset_no = x.dataset_no AND w.key = x.key))
         OR (cfg.cover = 'complete' AND NOT cfg.retire AND x.kind = {unseen}
             AND NOT EXISTS (SELECT 1 FROM w WHERE w.dataset_no = x.dataset_no AND w.key = x.key)))
  RETURNING 1
),
facts AS (
  SELECT dataset_no, (change_kind = {c_new}) AS added, (change_kind = {c_changed}) AS modified, (change_kind = {c_same}) AS unchanged,
         false AS deleted, false AS unseen, (was_unseen OR (was_filtered AND NOT filtered)) AS returned, rows_n - 1 AS dups,
         (f_over AND NOT was_expired) AS released, (why IS NOT NULL) AS starting
  FROM why
  UNION ALL
  SELECT dataset_no, false, false, false, expired, NOT expired, false, 0, false, expired FROM absent2
)
SELECT dataset_no,
       count(*) FILTER (WHERE added), count(*) FILTER (WHERE modified), count(*) FILTER (WHERE unchanged),
       count(*) FILTER (WHERE deleted), count(*) FILTER (WHERE unseen), count(*) FILTER (WHERE returned),
       coalesce(sum(dups), 0), count(*) FILTER (WHERE released), count(*) FILTER (WHERE starting)
FROM facts
GROUP BY dataset_no
`

// rangeTest is the condition that a row is in the range of the bucket: (dataset, key) is from $2 and $3 on and before $4 and $5.
// It is a comparison of the columns of the index of the table (the raw records by their key, the rows received by the index
// stage_record_key, the waits by their key), so a bucket reads the range of the index and nothing else. The table is named by the
// prefix of its columns.
func rangeTest(prefix string) string {
	row := "(" + prefix + "dataset_no, " + prefix + "key)"
	return "(" + row + " >= ($2::int, $3::text) AND " + row + " < ($4::int, $5::text))"
}

// The ends of the range of the first and of the last bucket are not a key: a first bucket begins before every dataset number and
// a last bucket ends after every one, so a key that is new is in a bucket as well. They are what the statement is given, and the
// run record keeps them as a null.
const (
	rangeStartDataset = -1
	rangeEndDataset   = math.MaxInt32
)

// endSQL is the statement that finds the end of a bucket from its start by the indexes of the three tables the bucket reads:
// the key that is $4 rows past the start in each, and the smallest of the three. With ">" in place of ">=" and 0 rows it is the
// key that comes after the start, which a bucket whose start is a key of more rows than the size of a bucket goes on by.
func endSQL(kgNo, srcNo int64, after bool) string {
	ref := func(table string) string { return pgschema.TableRef(kgNo, table) }
	op := ">="
	if after {
		op = ">"
	}
	from := func(table, where string) string {
		return "(SELECT dataset_no, key FROM " + table + " WHERE " + where + "(dataset_no, key) " + op + " ($2::int, $3::text) ORDER BY dataset_no, key OFFSET $4 LIMIT 1)"
	}
	return "SELECT dataset_no, key FROM (" +
		from(ref(pgschema.SourcePartition(pgschema.TableRawRecord, srcNo)), "src_no = $1 AND ") + " UNION ALL " +
		from(ref(pgschema.SourcePartition(pgschema.TableStageRecord, srcNo)), "row_no >= 0 AND ") + " UNION ALL " +
		from(ref(pgschema.TableRecordWait), "src_no = $1 AND ") + ") t ORDER BY dataset_no, key LIMIT 1"
}

// rawDiffSQL is the statement of a bucket of the comparison, for the source of a KG.
func rawDiffSQL(kgNo, srcNo int64) string {
	ref := func(table string) string { return pgschema.TableRef(kgNo, table) }
	num := func(v int16) string { return strconv.Itoa(int(v)) }
	return strings.NewReplacer(
		"{source_dataset}", pgschema.TableSourceDataset,
		"{stage}", ref(pgschema.SourcePartition(pgschema.TableStageRecord, srcNo)),
		"{raw}", ref(pgschema.SourcePartition(pgschema.TableRawRecord, srcNo)),
		"{wait}", ref(pgschema.TableRecordWait),
		"{problem}", ref(pgschema.TableRecordProblem),
		"{calc_record}", ref(pgschema.TableCalcRecord),
		"{calc_problem}", ref(pgschema.TableCalcProblem),
		"{calc_wait}", ref(pgschema.TableCalcWait),
		"{in_range_s}", rangeTest("s."),
		"{in_range_r}", rangeTest("r."),
		"{in_range_x}", rangeTest("x."),
		"{in_range}", rangeTest(""),
		"{unseen}", num(pgschema.WaitUnseen),
		"{filtered}", num(pgschema.WaitFiltered),
		"{c_new}", num(pgschema.ReceiveNew),
		"{c_changed}", num(pgschema.ReceiveChanged),
		"{c_same}", num(pgschema.ReceiveSame),
		"{w_new}", num(pgschema.CalcWhyNew),
		"{w_changed}", num(pgschema.CalcWhyChanged),
		"{w_returned}", num(pgschema.CalcWhyReturned),
		"{w_closed}", num(pgschema.CalcWhyClosed),
		"{w_released}", num(pgschema.CalcWhyReleased),
		"{w_duplicates}", num(pgschema.CalcWhyDuplicates),
		"{fate_used}", num(pgschema.FateUsed),
		"{fate_filtered}", num(pgschema.FateFiltered),
	).Replace(rawDiffTemplate)
}
