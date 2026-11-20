package ingest

import (
	"context"
	"sort"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The record of the runs of the new ingestion flow (ADR-0086, ADR-0098). A run is the ingestion of one source, from the
// receiving of its pages to the cleaning after the apply; its row says where it has got to, so that any server can take it
// over from there. The port below is what the receiving and the steps after it need and nothing more: opening a run, holding
// it by a lease, reading where the receiving stands, moving it to the next stage and ending it. It knows no database.

// RunStage is where a run stands. The first group is the work of a run, in the order it is done; the second is how a run
// ends, and a run in one of them is over.
type RunStage string

// The stages of a run (ingestion-at-scale.md 4).
const (
	RunReceiving            RunStage = "receiving"
	RunComputing            RunStage = "computing"
	RunAwaitingConfirmation RunStage = "awaiting_confirmation"
	RunApplying             RunStage = "applying"
	RunVolatile             RunStage = "volatile"
	RunSettling             RunStage = "settling"

	RunSucceeded RunStage = "succeeded"
	RunFailed    RunStage = "failed"
	RunCanceled  RunStage = "canceled"
	RunDiscarded RunStage = "discarded"
)

// RunStages lists the stages, the work first and then the ends.
func RunStages() []RunStage {
	return []RunStage{RunReceiving, RunComputing, RunAwaitingConfirmation, RunApplying, RunVolatile, RunSettling,
		RunSucceeded, RunFailed, RunCanceled, RunDiscarded}
}

// Over says whether a run in this stage has ended.
func (s RunStage) Over() bool {
	switch s {
	case RunSucceeded, RunFailed, RunCanceled, RunDiscarded:
		return true
	}
	return false
}

// Valid says whether the stage is one of the stages.
func (s RunStage) Valid() bool {
	for _, v := range RunStages() {
		if v == s {
			return true
		}
	}
	return false
}

// advances is the work from one stage to the next. A run that waits for a confirmation goes back to computing, because it
// computes again when it is confirmed (ingestion-at-scale.md 4, step 6). Ending is a different move: EndRun.
var advances = map[RunStage][]RunStage{
	RunReceiving:            {RunComputing},
	RunComputing:            {RunAwaitingConfirmation, RunApplying},
	RunAwaitingConfirmation: {RunComputing},
	RunApplying:             {RunVolatile},
	RunVolatile:             {RunSettling},
}

// CanAdvance says whether the work of a run goes from one stage to the other.
func CanAdvance(from, to RunStage) bool {
	for _, next := range advances[from] {
		if next == to {
			return true
		}
	}
	return false
}

// EndClass is why a run failed, in the three ways the record of runs and the screen show apart: a setting the person mends and
// does not try again, trouble that passes and may be tried again, or a fault of the product.
type EndClass string

// The classes of a failed run.
const (
	EndConfig    EndClass = "config"
	EndTransient EndClass = "transient"
	EndProduct   EndClass = "product"
)

// EndClasses lists the classes.
func EndClasses() []EndClass { return []EndClass{EndConfig, EndTransient, EndProduct} }

// The kinds of a run (only the full one is made now) and the ways an apply is started.
const (
	RunKindFull            = "full"
	RunKindIncremental     = "incremental"
	RunKindStream          = "stream"
	ApplyAutomatic         = "automatic"
	ApplyAfterConfirmation = "after_confirmation"
	TriggerManual          = "manual"
)

// RunOpen is what opens a run: the source of a KG, what starts it, and the server that does it for the term of a lease. The
// definition set is the hash of the definitions the run reads its rows by; a run that sees another set later is not the run it
// began as.
type RunOpen struct {
	KG            graph.KGID
	Source        string
	Kind          string
	Trigger       string
	ApplyMode     string
	DefinitionSet kernel.Hash
	Owner         string
	Term          time.Duration
}

// Validate checks what a run cannot be opened without.
func (o RunOpen) Validate() error {
	switch {
	case o.KG.IsZero() || o.Source == "":
		return kernel.Errorf(kernel.CodeInvalidValue, "a run is the run of a source of a KG: both must be named")
	case o.Owner == "":
		return kernel.Errorf(kernel.CodeInvalidValue, "a run is held by a server that names itself")
	case o.Term <= 0:
		return kernel.Errorf(kernel.CodeInvalidValue, "the term of a lease must be longer than nothing")
	}
	return nil
}

// RunLease is the hold of a server on a run: the run, the attempt that holds it and for how long a lease lasts. A server that
// woke up late holds an attempt that another server has taken over, and what it writes with it is refused.
type RunLease struct {
	No      int64
	Attempt int64
	Owner   string
	Term    time.Duration
}

// RunPlace is where the receiving of a run stands: the last page stored (of the dataset named, with the token to go on after it)
// and the stage of the run. A run that has stored no page has no dataset.
type RunPlace struct {
	Stage   RunStage
	Dataset string
	Page    int
	Token   []byte
	// Last says that the page stored was the last one of its dataset, so that going on is the next dataset.
	Last bool
}

// RunEnd is how a run ends: the stage it ends in and, for a failure, the class, a code and the words.
type RunEnd struct {
	Stage  RunStage
	Class  EndClass
	Code   string
	Reason string
}

// Validate checks that the end is an end, and that exactly a failure says why it failed by a class.
func (e RunEnd) Validate() error {
	if !e.Stage.Over() {
		return kernel.Errorf(kernel.CodeInvalidValue, "a run ends in succeeded, failed, canceled or discarded, not %q", e.Stage)
	}
	failed := e.Stage == RunFailed
	valid := false
	for _, c := range EndClasses() {
		valid = valid || c == e.Class
	}
	switch {
	case failed && !valid:
		return kernel.Errorf(kernel.CodeInvalidValue, "a run that failed says why by one of the classes config, transient and product, not %q", e.Class)
	case !failed && e.Class != "":
		return kernel.Errorf(kernel.CodeInvalidValue, "only a run that failed has a class, and this one is %s", e.Stage)
	}
	return nil
}

// RunLedger is the port of the record of runs. Every write that a lease holds names the attempt, and a write whose attempt
// is not the run's any more is refused with CodeRunLeaseLost and changes nothing.
//
// OpenRun makes a run of a source in the stage receiving, held by the caller; a source that has a run that is not over is
// refused with CodeRunOpen. AcquireRun takes over a run whose lease has run out (by the clock of the database) or is the
// caller's own, with the next attempt; a lease that is alive is CodeRunHeld. RenewRun extends a lease. ReceivePlace says where
// the receiving stands. AdvanceRun moves the work to the next stage, and refuses with CodeRunStage a run that is not in the
// stage it is moved from; moving to computing takes the gate of the KG, and a KG that has a run at work is CodeKGGateTaken, the
// run staying where it was. EndRun ends the run.
type RunLedger interface {
	OpenRun(ctx context.Context, open RunOpen) (RunLease, error)
	AcquireRun(ctx context.Context, no int64, owner string, term time.Duration) (RunLease, error)
	RenewRun(ctx context.Context, lease RunLease) error
	ReceivePlace(ctx context.Context, no int64) (RunPlace, error)
	AdvanceRun(ctx context.Context, lease RunLease, from, to RunStage) error
	EndRun(ctx context.Context, lease RunLease, end RunEnd) error
}

// The codes of the record of runs. A code is never renamed or reused.
const (
	// CodeRunOpen is a source that has a run that is not over: a source has one at a time.
	CodeRunOpen kernel.Code = "ingest_run_open"
	// CodeRunLeaseLost is a write with an attempt that is not the run's any more: another server took the run over.
	CodeRunLeaseLost kernel.Code = "ingest_run_lease_lost"
	// CodeRunHeld is a run whose lease is alive and not the caller's.
	CodeRunHeld kernel.Code = "ingest_run_held"
	// CodeRunStage is a run that is not in the stage the call needs.
	CodeRunStage kernel.Code = "ingest_run_stage"
	// CodeKGGateTaken is a KG that has another run computing, applying, writing volatile values or cleaning.
	CodeKGGateTaken kernel.Code = "ingest_kg_gate_taken"
)

var runCodeMeanings = map[kernel.Code]string{
	CodeRunOpen:      "the source has a run that is not over",
	CodeRunLeaseLost: "another server holds the run now: the attempt of this write is not the run's",
	CodeRunHeld:      "the lease of the run is alive and another server's",
	CodeRunStage:     "the run is not in the stage this call needs",
	CodeKGGateTaken:  "another run of the KG is computing, applying, writing volatile values or cleaning",
}

// RunCodes lists the codes of the record of runs, sorted.
func RunCodes() []kernel.Code {
	out := make([]kernel.Code, 0, len(runCodeMeanings))
	for c := range runCodeMeanings {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RunCodeMeaning is the one-line meaning of a code of the record of runs, or false.
func RunCodeMeaning(code kernel.Code) (string, bool) {
	m, ok := runCodeMeanings[code]
	return m, ok
}
