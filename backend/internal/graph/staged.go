package graph

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// StagedApplier is an optional capability of a Store (ADR-0099): the apply of the new ingestion flow, which works from
// the difference the flow stored in the KG (ingest_diff and the staged rows it points at) and not from a batch in
// memory. A store that keeps such tables offers it by implementing this interface; the port's own methods stay as
// they are, and the row by row path (Applier.Apply, ApplyChunk) goes on beside it.
//
// The apply is made of the chunks of the stored difference, one change number and one transaction for each, and the
// head of the KG says how far it got (the apply id, the next chunk, the number of chunks), so a server that takes
// the work over from one that died goes on with the next chunk. Abandoning the apply is the abandon of the port
// (Applier.Abandon): it takes the change numbers back out one by one, and the staged apply leaves nothing for it to
// miss. The volatile values are a step of their own, after the apply, in intervals of the staged values.
//
// The caller is the state machine of a run. It asks StagedProgress where the apply stands (after a start, after a
// crash, after a COMMIT that got no answer), applies the chunk after the one it is at, and renews its lease between
// two chunks.
type StagedApplier interface {
	// StagedProgress says where the apply of a.ApplyID stands, from the head of the KG and the stored difference.
	StagedProgress(ctx context.Context, a StagedApply) (StagedProgress, error)
	// ApplyStagedChunk applies the chunk at from.NextChunk and returns where the apply stands after it. from is the
	// progress the caller last saw (StagedProgress or the result of the chunk before). A head that is not where from
	// says is CodeHeadMoved: the caller asks StagedProgress again.
	ApplyStagedChunk(ctx context.Context, a StagedApply, from StagedProgress) (StagedProgress, error)
	// ApplyStagedVolatile writes one interval of the staged volatile values after the apply is done, and says where
	// the next interval starts.
	ApplyStagedVolatile(ctx context.Context, v StagedVolatile) (StagedVolatileStep, error)
}

// StagedApply names an apply of a KG that works from its stored difference. The generation and the lease are the
// ones the caller holds; Now is the time the lease is judged by and the apply is recorded at.
type StagedApply struct {
	KG         KGID
	Generation int
	Lease      Lease
	Now        time.Time
	// ApplyID is the name of the apply, made from the number of the run. A retry of an apply that is done, or the
	// resumption of one that is half done, is told by it.
	ApplyID  string
	Kind     BatchKind
	SourceID string
	// Event and EventRef are the ordering event the apply is, written with its last chunk; an empty Event is
	// OrderSourceChange.
	Event    OrderKind
	EventRef string
}

// The states of a staged apply.
const (
	// StagedEmpty: the stored difference has no row. There is nothing to apply, and the head and the chain of the KG are
	// left as they are (kg-storage-apply.md 5): a run that changes nothing is in the record of runs only.
	StagedEmpty StagedState = "empty"
	// StagedPending: the difference has chunks and none is applied.
	StagedPending StagedState = "pending"
	// StagedOpen: some chunks are applied, the head names this apply and the next chunk.
	StagedOpen StagedState = "open"
	// StagedDone: the apply is complete: the head is closed and its last apply id is this one.
	StagedDone StagedState = "done"
)

// StagedState is where a staged apply stands.
type StagedState string

// StagedProgress is the position of a staged apply. Chain is the chain of the digests of the chunks before
// NextChunk (the zero hash before the first); once the apply is done it is the chain of the digests of all the
// chunks. ApplyID of the head and the chain are not stored beside each other: a server that
// takes over makes the chain again from the stored difference (StagedProgress), so every chunk is applied against the
// chain of the chunks before it.
type StagedProgress struct {
	State StagedState
	// NextChunk is the number of the chunk to apply; ChunkCount is how many there are. Chunks are numbered from 0.
	NextChunk, ChunkCount int
	// AppliedSeq is the last change number of the KG, the one the next chunk is planned against.
	AppliedSeq int64
	Chain      kernel.Hash
}

// Last reports whether the chunk at NextChunk is the last one.
func (p StagedProgress) Last() bool { return p.NextChunk+1 >= p.ChunkCount }

// StagedVolatileCursor is a position in the staged volatile values: they are in the order of the object number and
// then the attribute number, and the cursor is the last one that was seen. The zero cursor is before the first.
type StagedVolatileCursor struct {
	ObjNo  int64
	AttrNo int
}

// StagedVolatile asks for one interval of the staged volatile values: the Limit values after the cursor. It needs the
// apply to be done (the head is closed): the values are written after the read watermark has moved.
type StagedVolatile struct {
	KG         KGID
	Generation int
	Lease      Lease
	Now        time.Time
	// ApplyID is the name of the run's apply; the light change record of the interval is under it.
	ApplyID  string
	Kind     BatchKind
	SourceID string
	After    StagedVolatileCursor
	Limit    int
}

// StagedVolatileStep is what one interval did. Next is where the following interval starts. Done says the interval
// was the last: it held fewer values than the limit. Written is how many values differed from what was kept and
// were written; when it is 0 no change number was used. Seq is the change number of the interval's light change
// record, or 0.
type StagedVolatileStep struct {
	Next    StagedVolatileCursor
	Done    bool
	Written int
	Seq     int64
}
