package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Capture is what the collection of one dataset gave: the bytes of the file, a hash of them and whether the read
// went to the end. An adapter makes it; the pipeline never opens a file itself (ingestion.md 3.2).
type Capture struct {
	Dataset string
	Data    []byte
	Hash    string
	// Complete is false when the read stopped short (a limit, an error): nothing of the capture is used then.
	Complete bool
	Reason   string
}

// Fetcher is the port of the source adapters. It has no method that writes to the source (ingestion.md 3.2). An
// error with the code not_found says the dataset was not collected this time, which is no failure: its raw records
// stay as they are.
type Fetcher interface {
	Fetch(ctx context.Context, source string, dataset Dataset) (Capture, error)
}

// Runner runs the pipeline of one source against one KG.
type Runner struct {
	Store   graph.Store
	Reg     *model.Registry
	Clock   kernel.Clock
	IDs     kernel.IDGenerator
	Keys    graph.KeyProvider
	Fetcher Fetcher
	States  StateStore
	// ChunkRows bounds the chunks of the batch; 0 means the default of the graph unit.
	ChunkRows int
}

// leaseTTL is how long a run keeps the lease of the KG: a run is short and nothing else writes to a KG that lives
// in one process.
const leaseTTL = 10 * time.Minute

// Run collects the datasets the fetcher gives, runs every stage and applies the result to the KG as one batch, then
// seals the version. The state of the source is saved only after the batch was applied. Running again with the same
// files changes nothing.
func (r Runner) Run(ctx context.Context, plan *Plan, kg graph.KGID) (Report, error) {
	st, err := r.States.Load(ctx, plan.Source.ID)
	if err != nil {
		return Report{}, err
	}
	st.Run++
	now := r.Clock.Now()
	rep := &Report{SourceID: plan.Source.ID, Run: st.Run}
	captured, err := r.collect(ctx, plan, st, rep, now)
	if err != nil {
		return *rep, err
	}
	d := newDerivation(plan, st, rep, now)
	d.materialize()
	if err := d.feeds(); err != nil {
		return *rep, err
	}
	if err := d.buildLinks(); err != nil {
		return *rep, err
	}
	d.checkCardinality()
	objects, err := d.objectsDone(seenText(now))
	if err != nil {
		return *rep, err
	}
	d.count(captured)
	batch, err := r.batch(ctx, d, st, kg, objects)
	if err != nil {
		return *rep, err
	}
	if err := r.apply(ctx, kg, batch, rep); err != nil {
		return *rep, err
	}
	return *rep, r.States.Save(ctx, plan.Source.ID, st)
}

func seenText(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// captured is the rows of each dataset this run received, by natural key, and the duplicates it set aside.
type captured struct {
	keys map[string][]string
	dups map[string]int
}

// collect is the capture, parse and raw record steps of every dataset.
func (r Runner) collect(ctx context.Context, plan *Plan, st *State, rep *Report, now time.Time) (captured, error) {
	cap := captured{keys: map[string][]string{}, dups: map[string]int{}}
	for _, ds := range plan.Source.Datasets {
		dr := rep.dataset(ds.ID)
		c, err := r.Fetcher.Fetch(ctx, plan.Source.ID, ds)
		if err != nil {
			if kernel.CodeOf(err) == kernel.CodeNotFound {
				continue
			}
			return cap, err
		}
		dr.Collected, dr.CaptureHash = true, c.Hash
		dr.SameCapture = st.dataset(ds.ID).LastCapture == c.Hash
		if !c.Complete {
			rep.problem(Problem{Kind: ProblemIncomplete, Dataset: ds.ID, Reason: c.Reason})
			continue
		}
		table := Parse(ds, c.Data)
		if len(table.Missing) > 0 {
			rep.problem(Problem{Kind: ProblemShapeChanged, Dataset: ds.ID, Reason: fmt.Sprintf("the file lacks the columns %v", table.Missing)})
			continue
		}
		if err := capture(captureInput{ds, table, st, rep, dr, &cap, now}); err != nil {
			return cap, err
		}
		st.dataset(ds.ID).LastCapture = c.Hash
	}
	return cap, nil
}

// captureInput is what the capture of one dataset works on.
type captureInput struct {
	ds  Dataset
	t   Table
	st  *State
	rep *Report
	dr  *DatasetReport
	cap *captured
	now time.Time
}

func capture(in captureInput) error {
	ds, t, st, rep, dr, cap, now := in.ds, in.t, in.st, in.rep, in.dr, in.cap, in.now
	for _, h := range t.Extra {
		rep.problem(Problem{Kind: ProblemExtraColumn, Dataset: ds.ID, Reason: "the header has a column no dataset column reads: " + h})
	}
	for _, u := range t.Unreadable {
		rep.problem(Problem{Kind: ProblemUnreadable, Dataset: ds.ID, Line: u.Line, Reason: u.Reason})
	}
	rows, dups := dedupe(t.Rows)
	for _, du := range dups {
		rep.problem(Problem{Kind: ProblemDuplicateKey, Dataset: ds.ID, Key: du.Key, Line: du.Line, Reason: "a later row has the same key and is used instead"})
	}
	dr.Applied, dr.Received, dr.Unreadable, dr.Read = true, t.Received(), len(t.Unreadable), len(t.Rows)
	cap.dups[ds.ID] = len(dups)
	for _, row := range rows {
		cap.keys[ds.ID] = append(cap.keys[ds.ID], row.Key)
	}
	ch, err := applyCapture(st.dataset(ds.ID), rows, ds.Deletion, st.Run, now)
	dr.Raw = ch
	return err
}

// count classifies the rows received this run (pipeline.md 7): used, a problem, filtered by a rule, or unused. A
// row that was set aside as a duplicate is a problem. The order of precedence is the design's.
func (d *derivation) count(c captured) {
	for i := range d.rep.Datasets {
		dr := &d.rep.Datasets[i]
		if !dr.Applied {
			continue
		}
		dr.Problem += c.dups[dr.Dataset]
		for _, k := range c.keys[dr.Dataset] {
			f := d.fates[dr.Dataset][k]
			switch {
			case f == nil:
				dr.Unused++
			case f.used:
				dr.Used++
				if f.problem {
					dr.UsedWithProblem++
				}
			case f.problem:
				dr.Problem++
			case f.filtered:
				dr.Filtered++
			default:
				dr.Unused++
			}
		}
	}
}
