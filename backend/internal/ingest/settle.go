package ingest

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The ninth step of the new ingestion flow, the cleaning (ADR-0128): after the apply and the volatile values, what the run computed is put
// in the permanent tables of the ingestion, the staged tables are emptied and the run ends. It is done in jobs, in the order below, and a
// job that has many rows is done a bucket at a time; a bucket and the place that moves past it are one transaction, and each bucket writes
// what the staged tables say over what is there, so a bucket that is done again gives the same. The confirmation of the graph (①) is the
// graph's work: the runner calls the Confirmer before the job is written as done, and sealing twice makes no second version.

// SettleJob is a job of the cleaning. The number is the one the record of the run keeps (sync_run.settle_job); 0 is a cleaning that has
// not begun. The jobs are named by what they write; the circled numbers are those of the step 9 of ingestion-at-scale.md 4.
type SettleJob int

// The jobs, in the order they are done. The references and the problems (④) go before the raw records (②), because the fate of a raw
// record is read from the lines the record has once they are written.
const (
	SettleJobConfirm  SettleJob = 1 // ① the graph is confirmed (a version is sealed)
	SettleJobRefs     SettleJob = 2 // ④ record_ref and record_problem
	SettleJobRaw      SettleJob = 3 // ② raw_record and the fate of the records
	SettleJobWaits    SettleJob = 4 // ③ record_wait
	SettleJobSource   SettleJob = 5 // ⑤ source_state and source_dataset
	SettleJobTruncate SettleJob = 6 // ⑥ the staged tables are emptied
	SettleJobAnalyze  SettleJob = 7 // ⑦ the statistics of the permanent tables
	SettleJobEnd      SettleJob = 8 // ⑧ the run ends as succeeded
)

// DefaultSettleRows is how many rows a bucket of the cleaning reads of the table it goes through (the records of the run, the waits).
const DefaultSettleRows = 10000

// SettleFeed is a feed of the plan of the run's source: its number, its id (the problems of the receiving name the feed by its id) and
// whether it makes objects (a cell of a feed that makes objects is the receiving's problem, one of a feed that only adds values the
// fourth step's).
type SettleFeed struct {
	Feed    int    `json:"feed"`
	ID      string `json:"id"`
	Creates bool   `json:"creates"`
}

// SettleFeedsOf is the feeds of a plan, by number.
func SettleFeedsOf(p *Plan) []SettleFeed {
	out := make([]SettleFeed, 0, len(p.feeds))
	for _, fp := range p.feeds {
		out = append(out, SettleFeed{Feed: fp.index, ID: fp.feed.ID, Creates: fp.feed.CreatesObjects})
	}
	return out
}

// SettleStart is what begins the cleaning: the rows of a bucket (0 is the default) and the feeds of the plan of the run's source.
type SettleStart struct {
	Rows  int
	Feeds []SettleFeed
}

// Validate checks what the cleaning cannot begin without.
func (s SettleStart) Validate() error {
	if s.Rows < 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the size of a bucket of the cleaning is not negative")
	}
	seen := map[int]bool{}
	ids := map[string]bool{}
	for _, f := range s.Feeds {
		if seen[f.Feed] || f.ID == "" || ids[f.ID] {
			return kernel.Errorf(kernel.CodeInvalidValue, "the feed %d (%q) of the cleaning is given twice or has no id", f.Feed, f.ID)
		}
		seen[f.Feed], ids[f.ID] = true, true
	}
	return nil
}

// SettlePlace is where the cleaning of a run stands: the job and the bucket of it to do next.
type SettlePlace struct {
	Stage RunStage
	Begun bool
	Job   SettleJob
	Part  int
}

// SettleCounts is what the cleaning did. Records are the records of the run that were written (Opened), whose fate alone was written
// (Fated) and that were closed (Closed); Waits the waits deleted and written; Refs and Problems the lines deleted and written; Received the
// problems of the receiving that were put to the records; Tables the tables measured; Buckets the buckets done.
type SettleCounts struct {
	Opened         int `json:"opened"`
	Fated          int `json:"fated"`
	Closed         int `json:"closed"`
	WaitsDeleted   int `json:"waits_deleted"`
	Waits          int `json:"waits"`
	RefsDeleted    int `json:"refs_deleted"`
	Refs           int `json:"refs"`
	ProblemsGone   int `json:"problems_deleted"`
	Problems       int `json:"problems"`
	Received       int `json:"received"`
	Tables         int `json:"tables"`
	Buckets        int `json:"buckets"`
	DatasetsMarked int `json:"datasets_marked"`
}

// SettleStep is what a bucket did: where the cleaning stands after it, what the cleaning has done so far, and whether the run is over (the
// last job ended it).
type SettleStep struct {
	Next   SettlePlace
	Counts SettleCounts
	Over   bool
}

// Settler is the port of the cleaning. Every call names the attempt of the lease, and a call whose attempt is not the run's any more is
// refused with CodeRunLeaseLost and changes nothing (reads too). BeginSettle starts the cleaning of a run that is settling; SettleBucket
// does the bucket the place says and refuses with CodeRunStage another job or another bucket (one that was done already).
type Settler interface {
	BeginSettle(ctx context.Context, lease RunLease, start SettleStart) error
	SettlePlace(ctx context.Context, lease RunLease) (SettlePlace, error)
	SettleBucket(ctx context.Context, lease RunLease, job SettleJob, part int) (SettleStep, error)
}

// Confirmer confirms the graph the run applied to (① of the cleaning). It is called again when the cleaning is taken over before the job
// was written as done, so a second call must change nothing.
type Confirmer interface {
	Confirm(ctx context.Context) error
}

// SealConfirmer confirms by sealing the KG as a version (graph.SealSync), with the lease of the KG the apply held: what the old flow did
// after its apply. A seal of a graph that has not changed since the last version makes none.
type SealConfirmer struct {
	Applier *graph.Applier
	KG      graph.KGID
	Lease   graph.Lease
}

// Confirm seals the KG.
func (c SealConfirmer) Confirm(ctx context.Context) error {
	_, err := c.Applier.Seal(ctx, c.KG, c.Lease, graph.SealSync)
	return err
}

// RunSettle does the cleaning, or what is left of it: it begins it when it has not begun, and does the buckets from where the run is
// until the run is over. The context is looked at between buckets.
func RunSettle(ctx context.Context, d Settler, lease RunLease, start SettleStart, confirm Confirmer) (SettleCounts, error) {
	if err := start.Validate(); err != nil {
		return SettleCounts{}, err
	}
	if confirm == nil {
		return SettleCounts{}, kernel.Errorf(kernel.CodeInvalidValue, "the cleaning confirms the graph: it needs a confirmer")
	}
	place, err := settleBegun(ctx, d, lease, start)
	if err != nil {
		return SettleCounts{}, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return SettleCounts{}, err
		}
		if place.Job == SettleJobConfirm {
			if err := confirm.Confirm(ctx); err != nil {
				return SettleCounts{}, err
			}
		}
		step, err := d.SettleBucket(ctx, lease, place.Job, place.Part)
		if err != nil {
			return SettleCounts{}, err
		}
		if step.Over {
			return step.Counts, nil
		}
		place = step.Next
	}
}

// settleBegun is where the cleaning of a run that is settling stands, once it has begun: it begins it when it has not.
func settleBegun(ctx context.Context, d Settler, lease RunLease, start SettleStart) (SettlePlace, error) {
	place, err := d.SettlePlace(ctx, lease)
	if err != nil {
		return place, err
	}
	if place.Stage != RunSettling {
		return place, kernel.Errorf(CodeRunStage, "the run %d is %s: the cleaning is the work of a run that is settling", lease.No, place.Stage)
	}
	if place.Begun {
		return place, nil
	}
	if err := d.BeginSettle(ctx, lease, start); err != nil {
		return place, err
	}
	return d.SettlePlace(ctx, lease)
}

// RunState is what the record of a run says of where it stands, for the runner that drives the steps (I-7b): the stage, the job of the
// computing and its part, whether the tables of the computation are the run's, the job and the bucket of the cleaning, and which steps
// are done.
type RunState struct {
	Stage      RunStage
	CalcJob    int
	CalcPart   int
	OwnsCalc   bool
	SettleJob  SettleJob
	SettlePart int
	Done       RunStepsDone
}

// RunStepsDone says which steps of a run are done, as the counts of the run keep them. A step of the computing is done only while the
// tables of the computation are the run's (another source's run may have emptied them while this one waited for a confirmation).
type RunStepsDone struct {
	Receive   bool
	RawDiff   bool
	Identify  bool
	Expand    bool
	Recompute bool
	Links     bool
	Isolate   bool
}

// RunStateReader reads the state of a run by its number. It holds no lease: it is what a runner looks at to know what to do next.
type RunStateReader interface {
	RunState(ctx context.Context, no int64) (RunState, error)
}
