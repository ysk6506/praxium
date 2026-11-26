package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// RunState reads where a run stands (ingest.RunStateReader, ADR-0128): the columns of its record and, from its counts, which steps are done.

var _ ingest.RunStateReader = (*Store)(nil)

// runStateCounts is what the state reads of the counts of a run: whether each step wrote that it is done.
type runStateCounts struct {
	Receive json.RawMessage `json:"receive"`
	RawDiff struct {
		Done bool `json:"done"`
	} `json:"raw_diff"`
	Identify struct {
		Phase string `json:"phase"`
	} `json:"identify"`
	Expand struct {
		Rule string `json:"rule"`
	} `json:"expand"`
	Recompute stlFinished `json:"recompute"`
	Links     stlFinished `json:"recompute_links"`
	Isolate   stlFinished `json:"isolate"`
}

// stlFinished is the mark of a step that says it is finished.
type stlFinished struct {
	Finished bool `json:"finished"`
}

// RunState reads the state of a run: see ingest.RunStateReader.
func (s *Store) RunState(ctx context.Context, no int64) (ingest.RunState, error) {
	var (
		out    ingest.RunState
		stage  string
		kgID   string
		counts []byte
		job    int
	)
	err := s.tx(ctx, roleApplier, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT "+pgschema.ColStage+", "+pgschema.ColKGID+", "+pgschema.ColCalcJob+", "+pgschema.ColCalcPart+", "+pgschema.ColSettleJob+", "+
			pgschema.ColSettlePart+", "+pgschema.ColCounts+" FROM "+pgschema.TableSyncRun+" WHERE "+pgschema.ColRunNo+" = $1", no).
			Scan(&stage, &kgID, &out.CalcJob, &out.CalcPart, &job, &out.SettlePart, &counts)
		if err != nil {
			return err
		}
		var kgNo int64
		if err := tx.QueryRowContext(ctx, "SELECT "+catalogKGNo+" FROM "+catalogTable+" WHERE "+catalogKGID+" = $1", kgID).Scan(&kgNo); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM "+pgschema.TableRef(kgNo, pgschema.TableCalcOwner)+" WHERE "+pgschema.ColRunNo+" = $1)", no).Scan(&out.OwnsCalc)
	})
	if isNoRows(err) {
		return out, kernel.Errorf(kernel.CodeNotFound, "no run %d", no)
	}
	if err != nil {
		return out, dbErr(err, "read the state of the run")
	}
	out.Stage, out.SettleJob = ingest.RunStage(stage), ingest.SettleJob(job)
	var in runStateCounts
	if err := json.Unmarshal(counts, &in); err != nil {
		return out, kernel.Wrap(kernel.CodeInternal, err, "read the counts of the run")
	}
	out.Done = in.done(out.OwnsCalc)
	return out, nil
}

// done says which steps the counts say are done; the steps of the computing only while the tables of the computation are the run's.
func (in runStateCounts) done(owns bool) ingest.RunStepsDone {
	d := ingest.RunStepsDone{Receive: len(in.Receive) > 0}
	if !owns {
		return d
	}
	d.RawDiff, d.Identify, d.Expand = in.RawDiff.Done, in.Identify.Phase == phaseDone, in.Expand.Rule == ruleDone
	d.Recompute, d.Links, d.Isolate = in.Recompute.Finished, in.Links.Finished, in.Isolate.Finished
	return d
}
