package ingest

import (
	"context"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The fifth step of the new ingestion flow, its second part (ADR-0127): the difference between what the run staged (after the isolation
// took out what the KG cannot have) and what the KG has, compared by number and by hash. A row of the difference is an object, a key, a link
// or an identifier that is added, changed or retired; what is the same makes no row. The rows are put in the order the apply writes them
// (the kind, the type, the id of the object or the link in the order of its bytes) and cut into chunks at the boundary of a type and at a
// number of rows, and the numbers of the order and of the chunks are stored, so the apply (graph.StagedApplier), the preview, the counts
// and the fingerprint all read the same rows. The step is SQL only: this file is the port a state machine drives it by.

// The phases of the step: the rows (compared, source by source), the plan of the order (from the counts), the order (the rows of a kind and
// a type, a range at a time) and the print (the digest of every chunk, one after another).
const (
	DiffPhaseRows  = "rows"
	DiffPhasePlan  = "plan"
	DiffPhaseOrder = "order"
	DiffPhasePrint = "print"
	DiffPhaseDone  = "done"
)

// DefaultDiffRows is how many rows of a source (or of the difference, in the order) a unit of the step reads.
const DefaultDiffRows = 2000

// DefaultDiffChunkRows is the most rows of the difference a chunk has: a chunk is one transaction of the apply (ADR-0099).
const DefaultDiffChunkRows = 10_000

// The kinds of a row of the difference and its changes, as the report names them.
const (
	DiffObject     = "object"
	DiffKey        = "key"
	DiffLink       = "link"
	DiffIdentifier = "identifier"

	DiffAdded   = "added"
	DiffChanged = "changed"
	DiffRetired = "retired"
)

// DiffStart is what begins the step: the rows a unit reads and the most rows of a chunk (0 is the default of each).
type DiffStart struct {
	Rows      int
	ChunkRows int
}

// Validate checks what the step cannot begin without.
func (s DiffStart) Validate() error {
	if s.Rows < 0 || s.ChunkRows < 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the size of a unit and of a chunk of the difference are not negative")
	}
	return nil
}

// DiffPlace is where the step stands.
type DiffPlace struct {
	Stage    RunStage
	Begun    bool
	Finished bool
	Phase    string
	Unit     int
}

// Done says whether every unit is done.
func (p DiffPlace) Done() bool { return p.Begun && p.Finished }

// DiffTally is how many rows of the difference a kind, a type and a change have: the count a judgment of a large loss reads (REQ-118). The
// rows are the ones after the isolation, which are the ones the apply writes.
type DiffTally struct {
	Kind   string `json:"kind"`
	TypeNo int    `json:"type_no"`
	Change string `json:"change"`
	Rows   int    `json:"rows"`
}

// DiffCounts is what the step made: the rows and the chunks of the difference, the fingerprint of the change (the chain of the digests of
// its chunks, the one the apply ends with; the chain of no chunk is 32 zero bytes) and the rows by kind, type and change.
type DiffCounts struct {
	Rows        int         `json:"rows"`
	Chunks      int         `json:"chunks"`
	Fingerprint kernel.Hash `json:"fingerprint"`
	Tally       []DiffTally `json:"tally"`
}

// Of is how many rows the tally has of a kind and a change, in every type.
func (c DiffCounts) Of(kind, change string) int {
	n := 0
	for _, t := range c.Tally {
		if t.Kind == kind && t.Change == change {
			n += t.Rows
		}
	}
	return n
}

// SortTally puts a tally in the order of the kinds as the apply writes them, then the type, then the change.
func SortTally(tally []DiffTally) {
	rank := func(v string, order ...string) int {
		for i, o := range order {
			if o == v {
				return i
			}
		}
		return len(order)
	}
	sort.Slice(tally, func(i, j int) bool {
		a, b := tally[i], tally[j]
		if ka, kb := rank(a.Kind, DiffObject, DiffKey, DiffLink, DiffIdentifier), rank(b.Kind, DiffObject, DiffKey, DiffLink, DiffIdentifier); ka != kb {
			return ka < kb
		}
		if a.TypeNo != b.TypeNo {
			return a.TypeNo < b.TypeNo
		}
		return rank(a.Change, DiffAdded, DiffChanged, DiffRetired) < rank(b.Change, DiffAdded, DiffChanged, DiffRetired)
	})
}

// Differ is the port of the step. Every call names the attempt of the lease, and a call whose attempt is not the run's any more is refused
// with CodeRunLeaseLost and changes nothing (reads too). A unit and the place that moves past it are one transaction, so a unit is done
// once or not at all. BeginDiff starts the step of a run whose isolation is done (a second begin is CodeRunStage); DiffStep does the unit
// the place says, and a place that is done is refused with CodeRunStage.
type Differ interface {
	BeginDiff(ctx context.Context, lease RunLease, start DiffStart) error
	DiffPlace(ctx context.Context, lease RunLease) (DiffPlace, error)
	DiffStep(ctx context.Context, lease RunLease) error
	DiffReport(ctx context.Context, lease RunLease) (DiffCounts, error)
}

// RunDiff does the step, or what is left of it: it begins it when it has not begun, and does the units from where the run is. The context
// is looked at between units.
func RunDiff(ctx context.Context, d Differ, lease RunLease, start DiffStart) (DiffCounts, error) {
	if err := start.Validate(); err != nil {
		return DiffCounts{}, err
	}
	place, err := d.DiffPlace(ctx, lease)
	if err != nil {
		return DiffCounts{}, err
	}
	if place.Stage != RunComputing {
		return DiffCounts{}, kernel.Errorf(CodeRunStage, "the run %d is %s: the difference is the work of a run that is computing", lease.No, place.Stage)
	}
	if !place.Begun {
		if err := d.BeginDiff(ctx, lease, start); err != nil {
			return DiffCounts{}, err
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return DiffCounts{}, err
		}
		if place, err = d.DiffPlace(ctx, lease); err != nil {
			return DiffCounts{}, err
		}
		if place.Done() {
			return d.DiffReport(ctx, lease)
		}
		if err := d.DiffStep(ctx, lease); err != nil {
			return DiffCounts{}, err
		}
	}
}
