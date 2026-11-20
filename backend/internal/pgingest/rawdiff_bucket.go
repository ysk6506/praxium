package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// DiffRawBucket does one bucket of the comparison: see ingest.RawDiffer. The end of the bucket is found by the indexes of the
// tables it reads, and the bucket, the move of the run to the next one and the lease that runs on are one transaction, so a bucket
// that did not commit left nothing, and the server that does it again finds the run at the same bucket and the same start.
func (s *Store) DiffRawBucket(ctx context.Context, lease ingest.RunLease, part int) (ingest.RawDiffBucket, error) {
	out := ingest.RawDiffBucket{Part: part}
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		run, err := readRun(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		state, err := stateOf(run.counts)
		if err != nil {
			return err
		}
		switch {
		case run.job != pgschema.CalcJobRawDiff:
			return kernel.Errorf(ingest.CodeRunStage, "the comparison of the run %d has not begun", lease.No)
		case state.Done:
			return kernel.Errorf(ingest.CodeRunStage, "the comparison of the run %d is done", lease.No)
		case part != run.part:
			return kernel.Errorf(ingest.CodeRunStage, "the run %d is at bucket %d, not at %d", lease.No, run.part, part)
		}
		kgNo, err := kgNoOfRun(ctx, tx, run.kgID)
		if err != nil {
			return err
		}
		var began, ended time.Time
		if err := tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&began); err != nil {
			return dbErr(err, "read the clock of the database")
		}
		end, err := bucketEnd(ctx, tx, kgNo, run.srcNo, state)
		if err != nil {
			return err
		}
		if out.Counts, err = execBucket(ctx, tx, bucketRun{kgNo: kgNo, srcNo: run.srcNo, runNo: lease.No, state: state, end: end}); err != nil {
			return err
		}
		out.Last = end == nil
		if err := tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&ended); err != nil {
			return dbErr(err, "read the clock of the database")
		}
		out.Took = ended.Sub(began)
		return moveOn(ctx, tx, lease, part, end, out.Counts)
	})
	return out, err
}

// bucketRun is what a bucket is done for.
type bucketRun struct {
	kgNo, srcNo, runNo int64
	state              rawDiffState
	end                *rawDiffKey
}

// bucketEnd finds the key the bucket ends before, which is where the next one begins: the key that is Size rows past the start in
// the rows received, the raw records and the waits, the first of the three. Every table is read by its index, so it costs what a
// bucket has of rows. A bucket that cannot go as far (the tables have no more rows) is the last and has no end (nil).
func bucketEnd(ctx context.Context, tx *sql.Tx, kgNo, srcNo int64, state rawDiffState) (*rawDiffKey, error) {
	lo := rawDiffKey{DatasetNo: rangeStartDataset}
	if state.Lo != nil {
		lo = *state.Lo
	}
	find := func(after bool, size int) (*rawDiffKey, error) {
		var no sql.NullInt64
		var key sql.NullString
		err := tx.QueryRowContext(ctx, endSQL(kgNo, srcNo, after), srcNo, lo.DatasetNo, lo.Key, size).Scan(&no, &key)
		switch {
		case isNoRows(err):
			return nil, nil
		case err != nil:
			return nil, dbErr(err, "find the end of the bucket")
		}
		return &rawDiffKey{DatasetNo: no.Int64, Key: key.String}, nil
	}
	end, err := find(false, state.Size)
	if err != nil || end == nil || state.Lo == nil || *end != *state.Lo {
		return end, err
	}
	// A key with more rows than a bucket has would make a bucket that is empty: the bucket is that key alone.
	return find(true, 0)
}

// bucketArgs are the parameters of the statement of a bucket (see rawDiffTemplate).
func bucketArgs(b bucketRun) ([]any, error) {
	lo, hi := rawDiffKey{DatasetNo: rangeStartDataset}, rawDiffKey{DatasetNo: rangeEndDataset}
	if b.state.Lo != nil {
		lo = *b.state.Lo
	}
	if b.end != nil {
		hi = *b.end
	}
	var nos, covers, retires []string
	for _, d := range b.state.Datasets {
		nos = append(nos, strconv.FormatInt(d.DatasetNo, 10))
		covers = append(covers, string(d.Coverage))
		retires = append(retires, strconv.FormatBool(d.Retire))
	}
	at, err := time.Parse(time.RFC3339Nano, b.state.At)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "read the time of the comparison")
	}
	return []any{b.srcNo, lo.DatasetNo, lo.Key, hi.DatasetNo, hi.Key, at.UTC().Format(time.RFC3339Nano), b.runNo,
		"{" + strings.Join(nos, ",") + "}", "{" + strings.Join(covers, ",") + "}", "{" + strings.Join(retires, ",") + "}",
		ingest.UnseenSpan.Seconds(), ingest.UnseenRuns}, nil
}

// execBucket runs the statement of the bucket and gives the counts of every dataset of the state, in its order.
func execBucket(ctx context.Context, tx *sql.Tx, b bucketRun) ([]ingest.RawDiffCounts, error) {
	args, err := bucketArgs(b)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, rawDiffSQL(b.kgNo, b.srcNo), args...)
	if err != nil {
		return nil, dbErr(err, "compare the bucket with the raw records")
	}
	defer func() { _ = rows.Close() }()
	byNo := map[int64]*ingest.RawDiffCounts{}
	out := make([]ingest.RawDiffCounts, len(b.state.Datasets))
	for i, d := range b.state.Datasets {
		out[i].Dataset = d.Dataset
		byNo[d.DatasetNo] = &out[i]
	}
	for rows.Next() {
		var no int64
		var c ingest.RawDiffCounts
		if err := rows.Scan(&no, &c.Added, &c.Modified, &c.Unchanged, &c.Deleted, &c.Unseen, &c.Returned, &c.Duplicates, &c.Released, &c.Starting); err != nil {
			return nil, dbErr(err, "read what the bucket made")
		}
		if line, ok := byNo[no]; ok {
			line.Add(c)
		}
	}
	return out, dbErr(rows.Err(), "read what the bucket made")
}

// moveOn writes what the bucket made to the counts of the run, moves the place to the next bucket (it begins where this one ended,
// and there is none after the last) and lets the lease run on. The
// attempt is the condition, so a server that woke up late changes nothing (and its bucket rolls back with the transaction).
func moveOn(ctx context.Context, tx *sql.Tx, l ingest.RunLease, part int, end *rawDiffKey, counts []ingest.RawDiffCounts) error {
	text, err := json.Marshal(counts)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "write the counts of the bucket")
	}
	next, err := json.Marshal(end)
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "write the place of the next bucket")
	}
	set := "jsonb_set(jsonb_set(jsonb_set(" + pgschema.ColCounts + ", ARRAY['raw_diff', 'parts', $3::text], $4::jsonb, true), ARRAY['raw_diff', 'lo'], $5::jsonb, true), ARRAY['raw_diff', 'done'], $6::jsonb, true)"
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColCalcPart+" = "+pgschema.ColCalcPart+" + 1, "+pgschema.ColCounts+
		" = "+set+", "+pgschema.ColLeaseUntil+" = "+lease("$7")+", "+
		pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+string(ingest.RunComputing)+"'",
		l.No, l.Attempt, strconv.Itoa(part), string(text), string(next), strconv.FormatBool(end == nil), l.Term.Seconds())
	if err != nil {
		return dbErr(err, "move the run to the next bucket")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}
