package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The cleaning of a run, on PostgreSQL (ingest.Settler, ADR-0128). The record of the run keeps where it stands: the job in settle_job, the
// buckets of the job done in settle_part, and under "settle" in the counts the place in the job and what was done. A bucket, the move of
// the place past it and the lease that runs on are one transaction, so a bucket is done once or not at all, and the server that takes the
// run over goes on from the bucket the record names.

var _ ingest.Settler = (*Store)(nil)

// stlState is the place of the cleaning and what it counts.
type stlState struct {
	Rows  int                 `json:"rows"`
	Feeds []ingest.SettleFeed `json:"feeds"`
	// MaxRid is the last record of the run (the records are not added to after the computing), and Cur the last record, the last block of
	// a table or the last place of the waits that is done in the job.
	MaxRid int64 `json:"max_rid"`
	Cur    int64 `json:"cur"`
	// Phase is the part of a job that has two (the waits: the ones there were are deleted, then the run's are written; the references and
	// the problems: the records, then the problems the run computed).
	Phase  int    `json:"phase"`
	CurDS  int64  `json:"cur_ds"`
	CurKey string `json:"cur_key"`
	CurK   int16  `json:"cur_kind"`
	// Blocks is how many blocks the table a phase reads by its blocks has (it is not written to while the run cleans).
	Blocks int64               `json:"blocks"`
	Counts ingest.SettleCounts `json:"counts"`
}

// stlRun is what a bucket is done for: the KG and the source of the run, its number, and its record.
type stlRun struct {
	kgNo, srcNo, runNo int64
	defSet             []byte
	at                 string
	cover              []rawDiffDataset
	st                 *stlState
	seen               identObserver
}

func (r *stlRun) saw(tx *sql.Tx, what, query string, args ...any) {
	if r.seen != nil {
		r.seen(tx, what, query, args)
	}
}

// settleRow is what the cleaning reads of the run it works for.
type settleRow struct {
	kgID   string
	srcNo  int64
	job    int
	part   int
	counts []byte
	defSet []byte
}

// readSettling reads the run in the transaction and checks that the attempt is the lease's and that the run is settling.
func readSettling(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (settleRow, error) {
	var r settleRow
	query := "SELECT " + pgschema.ColKGID + ", " + pgschema.ColSrcNo + ", " + pgschema.ColStage + ", " + pgschema.ColSettleJob + ", " + pgschema.ColSettlePart + ", " +
		pgschema.ColCounts + ", " + pgschema.ColDefSetHash + ", " + pgschema.ColAttempt + " FROM " + pgschema.TableSyncRun + " WHERE " + pgschema.ColRunNo + " = $1"
	if lock {
		query += " FOR UPDATE"
	}
	var (
		attempt int64
		stage   string
	)
	err := tx.QueryRowContext(ctx, query, lease.No).Scan(&r.kgID, &r.srcNo, &stage, &r.job, &r.part, &r.counts, &r.defSet, &attempt)
	switch {
	case isNoRows(err):
		return r, kernel.Errorf(ingest.CodeRunLeaseLost, "the run %d is not there any more", lease.No)
	case err != nil:
		return r, dbErr(err, "read the run")
	case attempt != lease.Attempt:
		return r, kernel.Errorf(ingest.CodeRunLeaseLost, "the run %d is held by attempt %d, not %d", lease.No, attempt, lease.Attempt)
	case ingest.RunStage(stage) != ingest.RunSettling:
		return r, kernel.Errorf(ingest.CodeRunStage, "the run %d is %s: the cleaning is the work of a run that is settling", lease.No, stage)
	}
	return r, nil
}

// stlCounts is what the cleaning reads of the counts of the run: its own place, and the time and the datasets of the comparison.
type stlCounts struct {
	Settle  *stlState    `json:"settle"`
	RawDiff rawDiffState `json:"raw_diff"`
	Isolate *isoState    `json:"isolate"`
}

func stlCountsOf(counts []byte) (stlCounts, error) {
	var in stlCounts
	if err := json.Unmarshal(counts, &in); err != nil {
		return in, kernel.Wrap(kernel.CodeInternal, err, "read the place of the cleaning")
	}
	return in, nil
}

// BeginSettle starts the cleaning: see ingest.Settler. The computing of the run must be done (its last step is), and the tables of the
// computation must be the run's.
func (s *Store) BeginSettle(ctx context.Context, lease ingest.RunLease, start ingest.SettleStart) error {
	if err := start.Validate(); err != nil {
		return err
	}
	return s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		run, err := readSettling(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		kgNo, err := kgNoOfRun(ctx, tx, run.kgID)
		if err != nil {
			return err
		}
		if err := ownsCalc(ctx, tx, kgNo, lease.No); err != nil {
			return err
		}
		in, err := stlCountsOf(run.counts)
		if err != nil {
			return err
		}
		switch {
		case run.job != 0:
			return kernel.Errorf(ingest.CodeRunStage, "the cleaning of the run %d has begun already", lease.No)
		case in.Isolate == nil || !in.Isolate.Finished:
			return kernel.Errorf(ingest.CodeRunStage, "the computing of the run %d is not done, so it cannot be cleaned", lease.No)
		}
		st := stlState{Rows: orInt(start.Rows, ingest.DefaultSettleRows), Feeds: start.Feeds}
		q := "SELECT COALESCE(max(" + pgschema.ColRid + "), 0) FROM " + pgschema.TableRef(kgNo, pgschema.TableCalcRecord)
		if err := tx.QueryRowContext(ctx, q).Scan(&st.MaxRid); err != nil {
			return dbErr(err, "read the records of the run")
		}
		return writeSettle(ctx, tx, lease, &st, ingest.SettleJobConfirm, 0)
	})
}

// writeSettle writes the place to the run and lets the lease run on. The attempt is the condition.
func writeSettle(ctx context.Context, tx *sql.Tx, l ingest.RunLease, st *stlState, job ingest.SettleJob, part int) error {
	text, _ := json.Marshal(map[string]any{"settle": st}) // a place of numbers and words cannot fail to be written as text
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColSettleJob+" = $3, "+pgschema.ColSettlePart+" = $4, "+pgschema.ColCounts+" = "+
		pgschema.ColCounts+" || $5::jsonb, "+pgschema.ColLeaseUntil+" = "+lease("$6")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+pgschema.ColStage+" = '"+
		string(ingest.RunSettling)+"'", l.No, l.Attempt, int(job), part, string(text), l.Term.Seconds())
	if err != nil {
		return dbErr(err, "write the place of the cleaning")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// SettlePlace says where the cleaning stands: see ingest.Settler.
func (s *Store) SettlePlace(ctx context.Context, lease ingest.RunLease) (ingest.SettlePlace, error) {
	place := ingest.SettlePlace{Stage: ingest.RunSettling}
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		run, err := readSettling(ctx, tx, lease, false)
		if err != nil {
			return err
		}
		place.Begun, place.Job, place.Part = run.job != 0, ingest.SettleJob(run.job), run.part
		return nil
	})
	return place, err
}

// SettleBucket does the bucket of the job the place says: see ingest.Settler.
func (s *Store) SettleBucket(ctx context.Context, lease ingest.RunLease, job ingest.SettleJob, part int) (ingest.SettleStep, error) {
	var out ingest.SettleStep
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		r, err := s.loadSettle(ctx, tx, lease, job, part)
		if err != nil {
			return err
		}
		done, err := r.bucket(ctx, tx, job, part)
		if err != nil {
			return err
		}
		r.st.Counts.Buckets++
		next := ingest.SettlePlace{Stage: ingest.RunSettling, Begun: true, Job: job, Part: part + 1}
		if done {
			next.Job, next.Part = job+1, 0
			r.st.Cur, r.st.Phase, r.st.Blocks, r.st.CurDS, r.st.CurKey, r.st.CurK = 0, 0, 0, 0, "", 0
		}
		if err := writeSettle(ctx, tx, lease, r.st, next.Job, next.Part); err != nil {
			return err
		}
		out.Next, out.Counts = next, r.st.Counts
		if job == ingest.SettleJobEnd {
			out.Over = true
			return endIn(ctx, tx, lease, ingest.RunEnd{Stage: ingest.RunSucceeded})
		}
		return nil
	})
	return out, err
}

// loadSettle reads the place of the run in the transaction and checks that the bucket asked for is the one the place says. Until the
// staged tables are emptied (⑥) they must be the run's.
func (s *Store) loadSettle(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, job ingest.SettleJob, part int) (*stlRun, error) {
	run, err := readSettling(ctx, tx, lease, true)
	if err != nil {
		return nil, err
	}
	in, err := stlCountsOf(run.counts)
	if err != nil {
		return nil, err
	}
	switch {
	case run.job == 0 || in.Settle == nil:
		return nil, kernel.Errorf(ingest.CodeRunStage, "the cleaning of the run %d has not begun", lease.No)
	case ingest.SettleJob(run.job) != job || run.part != part:
		return nil, kernel.Errorf(ingest.CodeRunStage, "the cleaning of the run %d is at the bucket %d of the job %d, not at %d of %d", lease.No, run.part, run.job, part, job)
	}
	kgNo, err := kgNoOfRun(ctx, tx, run.kgID)
	if err != nil {
		return nil, err
	}
	if job < ingest.SettleJobAnalyze {
		if err := ownsCalc(ctx, tx, kgNo, lease.No); err != nil {
			return nil, err
		}
	}
	return &stlRun{kgNo: kgNo, srcNo: run.srcNo, runNo: lease.No, defSet: run.defSet, at: in.RawDiff.At, cover: in.RawDiff.Datasets, st: in.Settle, seen: s.stlSeen}, nil
}

// bucket does a bucket of a job and says whether the job is done.
func (r *stlRun) bucket(ctx context.Context, tx *sql.Tx, job ingest.SettleJob, part int) (bool, error) {
	switch job {
	case ingest.SettleJobConfirm, ingest.SettleJobEnd:
		// the confirmation is the caller's (ingest.RunSettle calls the Confirmer first); the end is written by the caller of bucket
		return true, nil
	case ingest.SettleJobRefs:
		return r.refsBucket(ctx, tx)
	case ingest.SettleJobRaw:
		return r.rawBucket(ctx, tx)
	case ingest.SettleJobWaits:
		return r.waitsBucket(ctx, tx)
	case ingest.SettleJobSource:
		return true, r.source(ctx, tx)
	case ingest.SettleJobTruncate:
		return true, r.truncate(ctx, tx)
	case ingest.SettleJobAnalyze:
		return r.analyze(ctx, tx, part)
	}
	return false, kernel.Errorf(kernel.CodeInternal, "the cleaning has no job %d", job)
}

// ridRange is the records of the next bucket: after the place and up to Rows more, and whether it is the last.
func (r *stlRun) ridRange() (lo, hi int64, last bool) {
	lo, hi = r.st.Cur, r.st.Cur+int64(r.st.Rows)
	if hi >= r.st.MaxRid {
		return lo, r.st.MaxRid, true
	}
	return lo, hi, false
}

func (r *stlRun) t(table string) string { return pgschema.TableRef(r.kgNo, table) }

// exec runs one statement and gives the rows it wrote.
func (r *stlRun) exec(ctx context.Context, tx *sql.Tx, what, query string, args ...any) (int, error) {
	q := r.q(query)
	r.saw(tx, what, q, args...)
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, dbErr(err, what)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func stlNum(v int16) string { return strconv.Itoa(int(v)) }
