package pgingest

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The jobs of the cleaning that have buckets (ADR-0128).

// feedArrays is the feeds of the plan of the run's source as the arrays a statement takes: the numbers, the ids and whether each makes objects.
func (r *stlRun) feedArrays() (nos, ids, creates string) {
	var n, i, c []string
	for _, f := range r.st.Feeds {
		n, i, c = append(n, strconv.Itoa(f.Feed)), append(i, f.ID), append(c, strconv.FormatBool(f.Creates))
	}
	return textArray(n), textArray(i), textArray(c)
}

// refsBucket does a bucket of the references and the problems (④): first the records a range at a time (the lines of the pairs the run
// computed are taken away, the problems of the receiving and the references the run computed are written), then the problems the run
// computed a range of the blocks of calc_problem at a time.
func (r *stlRun) refsBucket(ctx context.Context, tx *sql.Tx) (bool, error) {
	st := r.st
	if st.Phase == 1 {
		n, last, err := r.blocks(ctx, tx, "write the problems the run computed", stlProblemsSQL)
		st.Counts.Problems += n
		return last, err
	}
	lo, hi, last := r.ridRange()
	nos, ids, creates := r.feedArrays()
	steps := []struct {
		what, query string
		count       *int
		args        []any
	}{
		{"take away the problems of what the run computed", stlProblemsGoneSQL, &st.Counts.ProblemsGone, []any{lo, hi, nos, creates}},
		{"write the problems the receiving found", stlReceivedSQL, &st.Counts.Received, []any{lo, hi, nos, ids, r.srcNo}},
		{"take away the references the run wrote again", stlRefsGoneSQL, &st.Counts.RefsDeleted, []any{lo, hi}},
		{"write the references the run computed", stlRefsSQL, &st.Counts.Refs, []any{lo, hi}},
	}
	for _, s := range steps {
		n, err := r.exec(ctx, tx, s.what, s.query, s.args...)
		if err != nil {
			return false, err
		}
		*s.count += n
	}
	st.Cur = hi
	if last {
		st.Phase, st.Cur = 1, 0
		return false, r.measure(ctx, tx, pgschema.TableCalcProblem)
	}
	return false, nil
}

// measure keeps how many blocks the table the next phase reads by its blocks has.
func (r *stlRun) measure(ctx context.Context, tx *sql.Tx, table string) error {
	if err := tx.QueryRowContext(ctx, stlBlocksSQL, r.t(table)).Scan(&r.st.Blocks); err != nil {
		return dbErr(err, "read the size of "+table)
	}
	return nil
}

// blocks runs a statement on the next blocks of the table the phase reads ($1 and $2 are the first block and the block after the last) and
// says whether they were the last.
func (r *stlRun) blocks(ctx context.Context, tx *sql.Tx, what, query string) (int, bool, error) {
	st := r.st
	per := int64(st.Rows/stlRowsPerBlock + 1)
	lo, hi := st.Cur, st.Cur+per
	n, err := r.exec(ctx, tx, what, query, tidText(lo), tidText(hi))
	if err != nil {
		return 0, false, err
	}
	st.Cur = hi
	return n, hi >= st.Blocks, nil
}

// tidText is the first row of a block, as the text of a tid.
func tidText(block int64) string { return "(" + strconv.FormatInt(block, 10) + ",0)" }

// rawBucket does a bucket of the raw records (②): the records closed are deleted, the ones received now are written with their cells and
// fate, and the others take their fate. The records of the other sources (read again only) are in the table of their own source, which the
// statement that writes the fate finds by the key: it is a join of the few records of the bucket with the index, and it is held to that.
func (r *stlRun) rawBucket(ctx context.Context, tx *sql.Tx) (bool, error) {
	st := r.st
	lo, hi, last := r.ridRange()
	if _, err := tx.ExecContext(ctx, stlJoinSettingSQL); err != nil {
		return false, dbErr(err, "set the plan of the bucket")
	}
	for _, s := range []struct {
		what, query string
		count       *int
	}{
		{"delete the records that are closed", stlClosedSQL, &st.Counts.Closed},
		{"write the records received now", stlOpenedSQL, &st.Counts.Opened},
		{"write the fate of the other records", stlFatedSQL, &st.Counts.Fated},
	} {
		n, err := r.exec(ctx, tx, s.what, s.query, lo, hi)
		if err != nil {
			return false, err
		}
		*s.count += n
	}
	st.Cur = hi
	return last, nil
}

// stlJoinSettingSQL is set in the transaction of a bucket of the raw records: a join of the few records of the bucket with a table is a
// loop over the index, never a read of the whole table.
const stlJoinSettingSQL = "SET LOCAL enable_hashjoin = off; SET LOCAL enable_mergejoin = off"

// waitsBucket does a bucket of the waits (③).
func (r *stlRun) waitsBucket(ctx context.Context, tx *sql.Tx) (bool, error) {
	st := r.st
	if st.Phase == 1 {
		n, last, err := r.blocks(ctx, tx, "write the waits of the run", stlWaitsSQL)
		st.Counts.Waits += n
		return last, err
	}
	q := r.q(stlWaitsGoneSQL)
	args := []any{r.srcNo, st.CurDS, st.CurKey, st.CurK, st.Rows}
	r.saw(tx, "delete the waits the source had", q, args...)
	var (
		n   int
		ds  sql.NullInt64
		key sql.NullString
		k   sql.NullInt16
	)
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&n, &ds, &key, &k); err != nil {
		return false, dbErr(err, "delete the waits the source had")
	}
	st.Counts.WaitsDeleted += n
	if n < st.Rows {
		st.Phase, st.Cur = 1, 0
		return false, r.measure(ctx, tx, pgschema.TableCalcWait)
	}
	st.CurDS, st.CurKey, st.CurK = ds.Int64, key.String, k.Int16
	return false, nil
}

// source writes what the source keeps of the run (⑤): the definition set it applied, and the time of the run for every dataset the run
// read whole. The capture hash stays as it is: the receiving reads a source by pages and makes none.
func (r *stlRun) source(ctx context.Context, tx *sql.Tx) error {
	if _, err := r.exec(ctx, tx, "write the definition set the source applied", "UPDATE "+pgschema.TableSourceState+" SET applied_definition_set = $2 WHERE "+
		pgschema.ColSrcNo+" = $1", r.srcNo, r.defSet); err != nil {
		return err
	}
	var complete []string
	for _, d := range r.cover {
		if d.Coverage == ingest.CoverageComplete {
			complete = append(complete, strconv.FormatInt(d.DatasetNo, 10))
		}
	}
	n, err := r.exec(ctx, tx, "write the time of the datasets read whole", "UPDATE "+pgschema.TableSourceDataset+" SET captured_at = $2::timestamptz WHERE "+
		pgschema.ColSrcNo+" = $3 AND "+pgschema.ColDatasetNo+" = ANY($1::text::int[])", textArray(complete), r.at, r.srcNo)
	r.st.Counts.DatasetsMarked += n
	return err
}

// truncate empties the tables of the computation of the KG and the partitions the source was received into (⑥). The run holds the gate of
// the KG (no other run of the KG computes), and the partitions are the source's alone (a run of another source receives into its own).
func (r *stlRun) truncate(ctx context.Context, tx *sql.Tx) error {
	compute, err := pgschema.TruncateComputeSQL(r.kgNo)
	if err != nil {
		return err
	}
	receive, err := pgschema.TruncateReceiveSQL(r.kgNo, r.srcNo)
	if err != nil {
		return err
	}
	for _, q := range []string{compute, receive} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return dbErr(err, "empty the staged tables")
		}
	}
	return nil
}

// stlAnalyzed are the tables whose statistics the cleaning measures (⑦), one a bucket: the permanent tables it wrote, which the next run
// reads by the plans the planner chooses from them (the raw records of the source, the references, the problems and the waits).
func (r *stlRun) stlAnalyzed() []string {
	return []string{r.t(pgschema.SourcePartition(pgschema.TableRawRecord, r.srcNo)), r.t(pgschema.TableRecordRef), r.t(pgschema.TableRecordProblem), r.t(pgschema.TableRecordWait)}
}

// analyze measures the table of the bucket.
func (r *stlRun) analyze(ctx context.Context, tx *sql.Tx, part int) (bool, error) {
	tables := r.stlAnalyzed()
	if part >= len(tables) {
		return false, kernel.Errorf(kernel.CodeInternal, "the cleaning measures %d tables, not %d", len(tables), part+1)
	}
	if _, err := tx.ExecContext(ctx, "ANALYZE "+tables[part]); err != nil {
		return false, dbErr(err, "measure the tables the cleaning wrote")
	}
	r.st.Counts.Tables++
	return part == len(tables)-1, nil
}
