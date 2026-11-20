package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The comparison of the received rows with the raw records, on PostgreSQL (ingest.RawDiffer, ADR-0102). The run record keeps where
// it stands: the job and the number of the buckets done in calc_job and calc_part, and under "raw_diff" in the counts the time of the
// run, the size of a bucket, the key the next bucket begins at, whether the last bucket is done, what every dataset counts as and
// what each bucket made.

var _ ingest.RawDiffer = (*Store)(nil)

// rawDiffState is what the counts of a run keep of the comparison. The time is the one every wait is counted at; it is written when
// the comparison begins, so a server that goes on after another one died counts at the same time.
type rawDiffState struct {
	At       string                            `json:"at"`
	Size     int                               `json:"size"`
	Lo       *rawDiffKey                       `json:"lo"`
	Done     bool                              `json:"done"`
	Datasets []rawDiffDataset                  `json:"datasets"`
	Parts    map[string][]ingest.RawDiffCounts `json:"parts"`
}

// rawDiffKey is a place in the order of the comparison: a dataset and a key of it. The first bucket has none (it begins at the
// start) and the last bucket has no end.
type rawDiffKey struct {
	DatasetNo int64  `json:"dataset_no"`
	Key       string `json:"key"`
}

// rawDiffDataset is a dataset of the comparison: its number, what it counts as and whether a wait for it can end.
type rawDiffDataset struct {
	Dataset   string          `json:"dataset"`
	DatasetNo int64           `json:"dataset_no"`
	Coverage  ingest.Coverage `json:"coverage"`
	Retire    bool            `json:"retire"`
}

func (d rawDiffDataset) cover() ingest.RawDiffCover {
	return ingest.RawDiffCover{Dataset: d.Dataset, Coverage: d.Coverage, Retire: d.Retire}
}

// runRow is what a step of the comparison reads of the run it works for.
type runRow struct {
	kgID   string
	srcNo  int64
	stage  string
	job    int16
	part   int
	counts []byte
}

// readRun reads the run in the transaction and checks that the attempt is the lease's and that the run is computing.
func readRun(ctx context.Context, tx *sql.Tx, lease ingest.RunLease, lock bool) (runRow, error) {
	var r runRow
	query := "SELECT " + pgschema.ColKGID + ", " + pgschema.ColSrcNo + ", " + pgschema.ColStage + ", " + pgschema.ColCalcJob + ", " + pgschema.ColCalcPart + ", " +
		pgschema.ColCounts + ", " + pgschema.ColAttempt + " FROM " + pgschema.TableSyncRun + " WHERE " + pgschema.ColRunNo + " = $1"
	if lock {
		query += " FOR UPDATE"
	}
	var attempt int64
	err := tx.QueryRowContext(ctx, query, lease.No).Scan(&r.kgID, &r.srcNo, &r.stage, &r.job, &r.part, &r.counts, &attempt)
	switch {
	case isNoRows(err):
		return r, kernel.Errorf(ingest.CodeRunLeaseLost, "the run %d is not there any more", lease.No)
	case err != nil:
		return r, dbErr(err, "read the run")
	case attempt != lease.Attempt:
		return r, kernel.Errorf(ingest.CodeRunLeaseLost, "the run %d is held by attempt %d, not %d", lease.No, attempt, lease.Attempt)
	case ingest.RunStage(r.stage) != ingest.RunComputing:
		return r, kernel.Errorf(ingest.CodeRunStage, "the run %d is %s: the comparison is the work of a run that is computing", lease.No, r.stage)
	}
	return r, nil
}

// BeginRawDiff starts the comparison: see ingest.RawDiffer.
func (s *Store) BeginRawDiff(ctx context.Context, lease ingest.RunLease, start ingest.RawDiffStart) (ingest.RawDiffPlan, error) {
	if err := start.Validate(); err != nil {
		return ingest.RawDiffPlan{}, err
	}
	var plan ingest.RawDiffPlan
	err := s.tx(ctx, roleBulk, func(tx *sql.Tx) error {
		run, err := readRun(ctx, tx, lease, true)
		if err != nil {
			return err
		}
		kgNo, err := kgNoOfRun(ctx, tx, run.kgID)
		if err != nil {
			return err
		}
		datasets, err := rawDiffDatasets(ctx, tx, run, start)
		if err != nil {
			return err
		}
		size := start.BucketRows
		if size == 0 {
			size = ingest.DefaultRawDiffBucketRows
		}
		state := rawDiffState{At: start.Now.UTC().Format(time.RFC3339Nano), Size: size, Datasets: datasets, Parts: map[string][]ingest.RawDiffCounts{}}
		if err := s.startJob(ctx, tx, lease, kgNo, state); err != nil {
			return err
		}
		for _, d := range datasets {
			plan.Datasets = append(plan.Datasets, d.cover())
		}
		return nil
	})
	return plan, err
}

func kgNoOfRun(ctx context.Context, tx *sql.Tx, kgID string) (int64, error) {
	var no int64
	err := tx.QueryRowContext(ctx, "SELECT "+catalogKGNo+" FROM "+catalogTable+" WHERE "+catalogKGID+" = $1 FOR SHARE", kgID).Scan(&no)
	if err != nil {
		return 0, dbErr(err, "read the number of the KG")
	}
	return no, nil
}

// receivedCoverage is the coverage the receiving wrote to the counts of the run for each dataset it gave; a run whose counts have no
// coverage (written before the coverage was) is read by its complete flag.
type receivedCoverage struct {
	Dataset  string          `json:"dataset"`
	Coverage ingest.Coverage `json:"coverage"`
	Complete bool            `json:"complete"`
	NotRead  string          `json:"not_read"`
}

func (r receivedCoverage) coverage() ingest.Coverage {
	switch {
	case r.NotRead != "":
		return ingest.CoverageIncomplete
	case r.Coverage.Valid():
		return r.Coverage
	case r.Complete:
		return ingest.CoverageComplete
	}
	return ingest.CoverageIncomplete
}

// rawDiffDatasets says, for every dataset the source has, what the comparison counts it as: what the receiving said, and a dataset
// that the receiving did not give is incomplete. A dataset the start names that the source does not have is refused.
func rawDiffDatasets(ctx context.Context, tx *sql.Tx, run runRow, start ingest.RawDiffStart) ([]rawDiffDataset, error) {
	var counts struct {
		Receive struct {
			Datasets []receivedCoverage `json:"datasets"`
		} `json:"receive"`
	}
	if err := json.Unmarshal(run.counts, &counts); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "read the counts of the run")
	}
	got := map[string]ingest.Coverage{}
	for _, d := range counts.Receive.Datasets {
		got[d.Dataset] = d.coverage()
	}
	retire := map[string]bool{}
	for _, d := range start.Datasets {
		retire[d.Dataset] = d.Retire
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+pgschema.ColDatasetNo+", "+pgschema.ColDatasetID+" FROM "+pgschema.TableSourceDataset+
		" WHERE "+pgschema.ColSrcNo+" = $1 ORDER BY "+pgschema.ColDatasetNo, run.srcNo)
	if err != nil {
		return nil, dbErr(err, "read the datasets of the source")
	}
	defer func() { _ = rows.Close() }()
	var out []rawDiffDataset
	for rows.Next() {
		var d rawDiffDataset
		if err := rows.Scan(&d.DatasetNo, &d.Dataset); err != nil {
			return nil, dbErr(err, "read the datasets of the source")
		}
		cover, ok := got[d.Dataset]
		if !ok {
			cover = ingest.CoverageIncomplete
		}
		d.Coverage = cover
		d.Retire = retire[d.Dataset]
		delete(retire, d.Dataset)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr(err, "read the datasets of the source")
	}
	for name := range retire {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the dataset %q is not one the source was put to the KG with", name)
	}
	return out, nil
}

// startJob empties the tables of the computation and writes the state of the comparison to the run, with the place at the first bucket.
func (s *Store) startJob(ctx context.Context, tx *sql.Tx, l ingest.RunLease, kgNo int64, state rawDiffState) error {
	truncate, err := pgschema.TruncateComputeSQL(kgNo)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, truncate); err != nil {
		return dbErr(err, "empty the tables of the computation")
	}
	text, err := json.Marshal(map[string]any{"raw_diff": state})
	if err != nil {
		return kernel.Wrap(kernel.CodeInternal, err, "write the counts of the comparison")
	}
	res, err := tx.ExecContext(ctx, "UPDATE "+pgschema.TableSyncRun+" SET "+pgschema.ColCalcJob+" = $3, "+pgschema.ColCalcPart+" = 0, "+pgschema.ColCounts+" = "+
		pgschema.ColCounts+" || $4::jsonb, "+pgschema.ColLeaseUntil+" = "+lease("$5")+", "+pgschema.ColUpdatedAt+" = now() WHERE "+guard()+" AND "+
		pgschema.ColStage+" = '"+string(ingest.RunComputing)+"'", l.No, l.Attempt, pgschema.CalcJobRawDiff, string(text), l.Term.Seconds())
	if err != nil {
		return dbErr(err, "write the place of the comparison")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return miss(ctx, tx, l)
	}
	return nil
}

// RawDiffPlace says where the comparison stands: see ingest.RawDiffer.
func (s *Store) RawDiffPlace(ctx context.Context, no int64) (ingest.RawDiffPlace, error) {
	var (
		place  ingest.RawDiffPlace
		stage  string
		job    int16
		counts []byte
	)
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT "+pgschema.ColStage+", "+pgschema.ColCalcJob+", "+pgschema.ColCalcPart+", "+pgschema.ColCounts+" FROM "+
			pgschema.TableSyncRun+" WHERE "+pgschema.ColRunNo+" = $1", no).Scan(&stage, &job, &place.Next, &counts)
	})
	if isNoRows(err) {
		return place, kernel.Errorf(kernel.CodeNotFound, "no run %d", no)
	}
	if err != nil {
		return place, dbErr(err, "read where the comparison stands")
	}
	place.Stage = ingest.RunStage(stage)
	if job != pgschema.CalcJobRawDiff {
		return ingest.RawDiffPlace{Stage: place.Stage}, nil
	}
	state, err := stateOf(counts)
	if err != nil {
		return place, err
	}
	place.Begun, place.Finished = true, state.Done
	return place, nil
}

func stateOf(counts []byte) (rawDiffState, error) {
	var in struct {
		RawDiff rawDiffState `json:"raw_diff"`
	}
	if err := json.Unmarshal(counts, &in); err != nil {
		return rawDiffState{}, kernel.Wrap(kernel.CodeInternal, err, "read the counts of the comparison")
	}
	return in.RawDiff, nil
}

// RawDiffReport adds up what the buckets that are done made: see ingest.RawDiffer.
func (s *Store) RawDiffReport(ctx context.Context, no int64) (ingest.RawDiffReport, error) {
	var counts []byte
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT "+pgschema.ColCounts+" FROM "+pgschema.TableSyncRun+" WHERE "+pgschema.ColRunNo+" = $1", no).Scan(&counts)
	})
	if isNoRows(err) {
		return ingest.RawDiffReport{}, kernel.Errorf(kernel.CodeNotFound, "no run %d", no)
	}
	if err != nil {
		return ingest.RawDiffReport{}, dbErr(err, "read the counts of the comparison")
	}
	state, err := stateOf(counts)
	if err != nil {
		return ingest.RawDiffReport{}, err
	}
	rep := ingest.RawDiffReport{Buckets: len(state.Parts)}
	for i, d := range state.Datasets {
		rep.Datasets = append(rep.Datasets, d.cover())
		total := ingest.RawDiffCounts{Dataset: d.Dataset}
		for part := 0; part < len(state.Parts); part++ {
			if lines := state.Parts[strconv.Itoa(part)]; i < len(lines) {
				total.Add(lines[i])
			}
		}
		rep.Counts = append(rep.Counts, total)
	}
	return rep, nil
}
