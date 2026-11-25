package ingest

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The second step of the new ingestion flow (ADR-0086, ADR-0102): what the received rows of a source make of the raw records the
// source kept. It is the first job of the computing. For each record it says what became of it: its key came twice in the batch,
// it is new, it changed, it is the same, it is not shown, it is shown again, or the time that it was waited for is up (for an
// absence and for a rule that filters it, each). The work is cut into buckets that are ranges of (dataset, key) (a key is always in
// one bucket and a bucket reads only the index range it covers, so the whole work costs what the rows cost), a bucket is one
// transaction, and a run that stops goes on from the bucket the run record names. This file
// is the port a state machine drives it by, and nothing here knows a database.

// DefaultRawDiffBucketRows is the number of the rows one bucket is sized for: it reads at most this many of the rows received, of
// the raw records kept and of the waits kept. A bucket must not take more than a third of a lease (20 seconds); the value is a
// starting value that a measurement of the unit sets (ADR-0102).
const DefaultRawDiffBucketRows = 100_000

// RawDiffDataset is what the comparison needs of a dataset that the run's receiving did not say: whether a record that the source
// no longer shows may be retired (the deletion rule of the dataset is absent_in_full).
type RawDiffDataset struct {
	Dataset string
	// Retire says that a record that complete reads do not show is counted absent and closed when its time is up, and that a
	// record a rule filters is let go of when its time is up (the deletion rule of the dataset is absent_in_full). Without it
	// (deletion none) an absence is not counted at all, as before, and the time of a record that a rule filters is counted but never
	// up: the record is not let go of.
	Retire bool
}

// RawDiffStart is what begins the comparison: the time of the run, which every wait is counted at (it is kept in the run, so a
// server that goes on after another one died counts at the same time), the datasets of the source with their rules, and the size
// of a bucket (0 is DefaultRawDiffBucketRows).
type RawDiffStart struct {
	Now        time.Time
	Datasets   []RawDiffDataset
	BucketRows int
}

// Validate checks what the comparison cannot begin without.
func (s RawDiffStart) Validate() error {
	switch {
	case s.Now.IsZero():
		return kernel.Errorf(kernel.CodeInvalidValue, "the comparison counts the waits at the time of the run, which is not given")
	case s.BucketRows < 0:
		return kernel.Errorf(kernel.CodeInvalidValue, "the size of a bucket is not negative, not %d", s.BucketRows)
	}
	seen := map[string]bool{}
	for _, d := range s.Datasets {
		if d.Dataset == "" || seen[d.Dataset] {
			return kernel.Errorf(kernel.CodeInvalidValue, "the datasets of the comparison are named, each once: %q", d.Dataset)
		}
		seen[d.Dataset] = true
	}
	return nil
}

// RawDiffPlan is what the comparison of a run counts each dataset as. The buckets are not cut before: each bucket finds where it
// ends by the index, so the work has as many buckets as the rows ask for.
type RawDiffPlan struct {
	Datasets []RawDiffCover
}

// RawDiffCover is what a dataset counts as in the comparison, which the receiving said (a dataset it did not give, or that gave
// no rows because its header changed, is incomplete) and the rule of the dataset.
type RawDiffCover struct {
	Dataset  string
	Coverage Coverage
	Retire   bool
}

// RawDiffPlace is where the comparison of a run stands: whether it has begun, whether the last bucket (the one that has no end)
// is done, and the bucket to do next. A bucket is done when the place has moved past it, so Next is the number of the buckets done.
// A comparison has begun only while the tables of the computation are the run's own (they are one set per KG, and another source
// of the KG that begins its computation empties them), so a run that comes back to find them another's has not begun.
type RawDiffPlace struct {
	Stage    RunStage
	Begun    bool
	Finished bool
	Next     int
}

// Done says whether every bucket is done.
func (p RawDiffPlace) Done() bool { return p.Begun && p.Finished }

// RawDiffCounts is what the comparison made of the records of a dataset (for a dataset that was not counted, all are 0). Added,
// Modified, Unchanged, Deleted and Unseen are the words of RawChanges, with the same meaning: Deleted is the records closed
// because their time was up, Unseen the records not shown that are still counted present. The others are new in this flow:
// Returned is a record that was waited for and is shown again, Duplicates the rows that a later row with the same key shadowed,
// Released the records a rule filters whose time is up (the first time), and Starting the records that the comparison hands to the
// next step.
type RawDiffCounts struct {
	Dataset    string `json:"dataset"`
	Added      int    `json:"added"`
	Modified   int    `json:"modified"`
	Unchanged  int    `json:"unchanged"`
	Deleted    int    `json:"deleted"`
	Unseen     int    `json:"unseen"`
	Returned   int    `json:"returned"`
	Duplicates int    `json:"duplicates"`
	Released   int    `json:"released"`
	Starting   int    `json:"starting"`
}

// Add adds the counts of another bucket of the same dataset.
func (c *RawDiffCounts) Add(o RawDiffCounts) {
	c.Added += o.Added
	c.Modified += o.Modified
	c.Unchanged += o.Unchanged
	c.Deleted += o.Deleted
	c.Unseen += o.Unseen
	c.Returned += o.Returned
	c.Duplicates += o.Duplicates
	c.Released += o.Released
	c.Starting += o.Starting
}

// RawDiffBucket is what one bucket did: its number, the counts of its datasets in the order of the plan, how long it took by
// the clock of the database (what the size of the buckets is held to), and whether it was the last (it has no end).
type RawDiffBucket struct {
	Part   int
	Counts []RawDiffCounts
	Took   time.Duration
	Last   bool
}

// RawDiffReport is what the comparison of a run made, added up over its buckets (Buckets is how many are done), a line for every
// dataset of the plan.
type RawDiffReport struct {
	Buckets  int
	Datasets []RawDiffCover
	Counts   []RawDiffCounts
}

// RawDiffer is the port of the comparison. Every write names the attempt of the lease, and a write whose attempt is not the run's
// any more is refused with CodeRunLeaseLost and changes nothing, as for RunLedger.
//
// BeginRawDiff starts the comparison of a run that is computing: it empties the tables of the computation of the KG (the
// comparison is the first job, so nothing in them is of use), reads from the run what the receiving said of each dataset, puts the
// run at the first bucket. DiffRawBucket does the bucket the run is at (a bucket that is not the one the run is at, or any bucket
// of a comparison that is done, is refused with CodeRunStage): it finds the end of the bucket by the index, compares the range, and
// in the same transaction moves the run to the next bucket and lets the lease run on, so a bucket is done once or not at all. RawDiffPlace says where the comparison stands, and RawDiffReport adds up what
// the buckets done have made.
type RawDiffer interface {
	BeginRawDiff(ctx context.Context, lease RunLease, start RawDiffStart) (RawDiffPlan, error)
	RawDiffPlace(ctx context.Context, no int64) (RawDiffPlace, error)
	DiffRawBucket(ctx context.Context, lease RunLease, part int) (RawDiffBucket, error)
	RawDiffReport(ctx context.Context, no int64) (RawDiffReport, error)
}

// RunRawDiff does the whole comparison of a run, or what is left of it: it begins it when it has not begun (a comparison whose
// tables another run emptied has not begun, so a run that waited for its turn and comes back does it again from its start, at the
// time in start), goes through the buckets from the one the run is at, and gives what was made. It may be called again for a run
// whose comparison is done and whose tables are its own, and then it only reports; it is not called for a run that is past the
// comparison in the later jobs of the computation (the state machine knows by the job of the run), for it would begin again and
// empty what they made. The context is looked at between buckets, so a canceled run stops
// after the bucket in hand and a server that takes the run over goes on from there. A run that is not computing is refused with
// CodeRunStage.
func RunRawDiff(ctx context.Context, d RawDiffer, lease RunLease, start RawDiffStart) (RawDiffReport, error) {
	if err := start.Validate(); err != nil {
		return RawDiffReport{}, err
	}
	place, err := d.RawDiffPlace(ctx, lease.No)
	if err != nil {
		return RawDiffReport{}, err
	}
	if place.Stage != RunComputing {
		return RawDiffReport{}, kernel.Errorf(CodeRunStage, "the run %d is %s: the comparison is the work of a run that is computing", lease.No, place.Stage)
	}
	if !place.Begun {
		if _, err := d.BeginRawDiff(ctx, lease, start); err != nil {
			return RawDiffReport{}, err
		}
		place = RawDiffPlace{Stage: RunComputing, Begun: true}
	}
	for part := place.Next; !place.Finished; part++ {
		if err := ctx.Err(); err != nil {
			return RawDiffReport{}, err
		}
		b, err := d.DiffRawBucket(ctx, lease, part)
		if err != nil {
			return RawDiffReport{}, err
		}
		place.Finished = b.Last
	}
	return d.RawDiffReport(ctx, lease.No)
}
