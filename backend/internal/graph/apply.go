package graph

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// DefaultChunkRows is the default row limit of a chunk (a guess to be measured, defaults.md).
const DefaultChunkRows = 10000

// DefaultLeaseTTL is the default length of a lease.
const DefaultLeaseTTL = 30 * time.Second

// Options are the knobs of an Applier.
type Options struct {
	// ChunkRows is the most candidates one chunk holds; 0 means DefaultChunkRows.
	ChunkRows int
	// Semantic is the semantic version of the engine this applier runs.
	Semantic int
	// LeaseTTL is the length of a lease; 0 means DefaultLeaseTTL.
	LeaseTTL time.Duration
	// VerifyOnSeal recomputes the whole graph hash before every seal.
	VerifyOnSeal bool
}

// Applier is the one writer of a KG. It computes diffs, row hashes and the new
// head; the store only persists what it is handed.
type Applier struct {
	store Store
	reg   *model.Registry
	keys  KeyProvider
	clock kernel.Clock
	ids   kernel.IDGenerator
	opts  Options
	owner string
	v     *validator
}

// NewApplier builds an applier. Its lease owner name comes from the id generator.
func NewApplier(store Store, reg *model.Registry, keys KeyProvider, clock kernel.Clock, ids kernel.IDGenerator, opts Options) (*Applier, error) {
	if store == nil || reg == nil || keys == nil || clock == nil || ids == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "an applier needs a store, a registry, a key provider, a clock and an id generator")
	}
	if opts.ChunkRows <= 0 {
		opts.ChunkRows = DefaultChunkRows
	}
	if opts.LeaseTTL <= 0 {
		opts.LeaseTTL = DefaultLeaseTTL
	}
	return &Applier{store: store, reg: reg, keys: keys, clock: clock, ids: ids, opts: opts,
		owner: ids.Next("worker"), v: newValidator(reg)}, nil
}

// Acquire takes the lease of a KG for this applier.
func (a *Applier) Acquire(ctx context.Context, kg KGID) (Lease, error) {
	return a.store.AcquireLease(ctx, LeaseRequest{KG: kg, Owner: a.owner, Semantic: a.opts.Semantic, TTL: a.opts.LeaseTTL, Now: a.clock.Now()})
}

// Release gives the lease up.
func (a *Applier) Release(ctx context.Context, lease Lease) error {
	return a.store.ReleaseLease(ctx, lease)
}

// Result says what an apply did.
type Result struct {
	ApplyID string
	// Chunks is the number of chunks written (rows or not).
	Chunks int
	// FromSeq and ToSeq bound the change numbers used; both 0 when nothing changed.
	FromSeq, ToSeq int64
	// IngestSeq is the ordering event of the batch.
	IngestSeq int64
	// Duplicate is true when the apply had completed before (a retry of an ambiguous commit).
	Duplicate bool
}

// Changed reports whether the apply made any change.
func (r Result) Changed() bool { return r.ToSeq != 0 }

// Run is an apply in progress, one chunk at a time. Apply is the loop around it.
type Run struct {
	a      *Applier
	kg     KG
	lease  Lease
	plan   *plan
	chunk  int
	count  int
	ingest int64
	res    Result
	done   bool
}

// Apply applies a whole batch, chunk by chunk.
func (a *Applier) Apply(ctx context.Context, kg KGID, lease Lease, b Batch) (Result, error) {
	run, err := a.Begin(ctx, kg, lease, b)
	if err != nil {
		return Result{}, err
	}
	for !run.Done() {
		if err := run.Next(ctx); err != nil {
			return run.Result(), err
		}
	}
	return run.Result(), nil
}

// Begin validates a batch against the registry and the state and prepares its
// chunks. A batch whose ApplyID is the half-done apply of the head resumes it.
func (a *Applier) Begin(ctx context.Context, kg KGID, lease Lease, b Batch) (*Run, error) {
	kgRow, err := a.store.GetKG(ctx, kg.String())
	if err != nil {
		return nil, err
	}
	head, err := a.store.Head(ctx, kg)
	if err != nil {
		return nil, err
	}
	p, err := buildPlan(a.v, b)
	if err != nil {
		return nil, err
	}
	if b.ApplyID == "" {
		b.ApplyID = a.ids.Next("apply")
	}
	run := &Run{a: a, kg: kgRow, lease: lease, plan: p, count: p.chunkCount(a.opts.ChunkRows), ingest: head.IngestSeq + 1,
		res: Result{ApplyID: b.ApplyID}}
	if err := run.enter(ctx, head, b.ApplyID); err != nil {
		return nil, err
	}
	return run, nil
}

// enter decides how the run starts: a retry of a finished apply is done at
// once, a half-done apply of the same id resumes, and a new one is checked
// against the state.
func (r *Run) enter(ctx context.Context, head Head, applyID string) error {
	switch {
	case !head.Open() && head.LastApplyID == applyID:
		r.done = true
		r.res.Duplicate = true
	case head.Open() && head.ApplyID != applyID:
		return kernel.Errorf(CodeApplyInProgress, "apply %s is half done: resume it or abandon it", head.ApplyID)
	case head.Open():
		if head.ChunkCount != r.count {
			return invalid("the batch of apply %s cuts into %d chunks, the head says %d", applyID, r.count, head.ChunkCount)
		}
		r.chunk, r.ingest = head.NextChunkNo, head.IngestSeq+1
	default:
		snap, err := r.a.store.Snapshot(ctx, ReadPoint{KG: r.kg.ID, Kind: ReadApplied})
		if err != nil {
			return err
		}
		return checkState(ctx, snap, r.a.reg, r.a.v, r.plan)
	}
	return nil
}

// Done reports whether the apply is complete.
func (r *Run) Done() bool { return r.done }

// Result is what the apply has done so far.
func (r *Run) Result() Result { return r.res }

// Next applies the next chunk. An empty chunk before the last is skipped.
func (r *Run) Next(ctx context.Context) error {
	if r.done {
		return nil
	}
	a := r.a
	head, err := a.store.Head(ctx, r.kg.ID)
	if err != nil {
		return err
	}
	if err := r.ownsHead(head); err != nil {
		return err
	}
	snap, err := a.store.Snapshot(ctx, ReadPoint{KG: r.kg.ID, Kind: ReadApplied})
	if err != nil {
		return err
	}
	items, last := r.plan.chunkItems(r.chunk, a.opts.ChunkRows)
	cb := newChunkBuilder(a, r.kg.ID, snap, head.Hash)
	if err := cb.build(ctx, r.plan, items, last); err != nil {
		return err
	}
	if cb.empty() && !last {
		r.chunk++
		return nil
	}
	w, err := r.write(head, cb, last)
	if err != nil {
		return err
	}
	if err := a.store.ApplyChunk(ctx, w); err != nil {
		return err
	}
	r.record(w, last)
	return nil
}

// ownsHead fails when the head is no longer at the chunk this run is on.
func (r *Run) ownsHead(head Head) error {
	if (r.res.Chunks > 0 || head.Open()) && (head.ApplyID != r.res.ApplyID || head.NextChunkNo > r.chunk) {
		return kernel.Errorf(CodeHeadMoved, "the head no longer belongs to apply %s", r.res.ApplyID)
	}
	return nil
}

// record notes a written chunk in the result and moves to the next one.
func (r *Run) record(w ChunkWrite, last bool) {
	r.res.Chunks++
	if w.Batch != nil {
		r.res.ToSeq = w.Batch.Seq
		if r.res.FromSeq == 0 {
			r.res.FromSeq = w.Batch.Seq
		}
	}
	r.chunk++
	if last {
		r.done = true
		r.res.IngestSeq = r.ingest
	}
}

// write assembles the chunk write: the rows, the new head and, for the last
// chunk, the ordering event.
func (r *Run) write(head Head, cb *chunkBuilder, last bool) (ChunkWrite, error) {
	a := r.a
	now := a.clock.Now()
	nh := head.Clone()
	nh.Hash = cb.hash
	w := ChunkWrite{KG: r.kg.ID, Generation: r.kg.Generation, ExpectedSeq: head.AppliedSeq, Lease: r.lease, Now: now}
	if !cb.empty() {
		seq := head.AppliedSeq + 1
		cb.assign(seq)
		nh.AppliedSeq = seq
		w.Batch = &ChangeBatch{Seq: seq, Kind: r.plan.batch.Kind, SourceID: r.plan.batch.SourceID, ApplyID: r.res.ApplyID,
			ChunkNo: r.chunk, ChunkCount: r.count, IngestSeq: r.ingest, Attempt: r.lease.Attempt, AppliedAt: now,
			Objects: len(cb.objects), Links: len(cb.links)}
		w.Objects, w.Links, w.Volatile, w.Light, w.Identifiers, w.Events, w.Entries =
			cb.objects, cb.links, cb.volatile, cb.light, cb.idents, cb.events, cb.entries
	}
	if !last {
		if !head.Open() {
			start := head.Hash.Clone()
			nh.Rollback = &start
		}
		nh.ApplyID, nh.NextChunkNo, nh.ChunkCount = r.res.ApplyID, r.chunk+1, r.count
		w.Head = nh
		return w, nil
	}
	chain, err := kernel.ContentHash([]any{"praxium.input.v1", head.InputDigest.String(), r.ingest,
		string(r.plan.batch.Event), r.plan.batch.EventRef, r.plan.digest.String()})
	if err != nil {
		return w, err
	}
	w.Order = &OrderEvent{IngestSeq: r.ingest, Kind: r.plan.batch.Event, Ref: r.plan.batch.EventRef, Digest: chain, ChangeSeq: nh.AppliedSeq}
	nh.ReadableThrough, nh.IngestSeq, nh.InputDigest = nh.AppliedSeq, r.ingest, chain
	nh.ApplyID, nh.NextChunkNo, nh.ChunkCount, nh.Rollback, nh.LastApplyID = "", 0, 0, nil, r.res.ApplyID
	if r.lease.Semantic > nh.AppliedSemanticVersion {
		nh.AppliedSemanticVersion = r.lease.Semantic
	}
	w.Head = nh
	w.Serve = r.kg.State == StateBuilding
	return w, nil
}

// Abandon reverts a chunked apply that is half done: the chunks, which no
// reader has seen, are removed and their change numbers will be used again.
func (a *Applier) Abandon(ctx context.Context, kg KGID, lease Lease) error {
	kgRow, err := a.store.GetKG(ctx, kg.String())
	if err != nil {
		return err
	}
	head, err := a.store.Head(ctx, kg)
	if err != nil {
		return err
	}
	if !head.Open() || head.Rollback == nil {
		return nil
	}
	nh := head.Clone()
	nh.AppliedSeq, nh.Hash = head.ReadableThrough, head.Rollback.Clone()
	nh.ApplyID, nh.NextChunkNo, nh.ChunkCount, nh.Rollback = "", 0, 0, nil
	return a.store.AbandonApply(ctx, AbandonWrite{KG: kg, Generation: kgRow.Generation, ExpectedSeq: head.AppliedSeq,
		Lease: lease, Now: a.clock.Now(), Head: nh})
}
