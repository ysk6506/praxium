package pgstore

import (
	"context"
	"database/sql"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// resolve is for a write that moves the head. When its COMMIT got no answer (pgconn.CodeOutcomeUnknown) the write may
// or may not be in, so the head is read again from the primary server (the write pool, in a read-only transaction):
// a head that is the one the write set means it was committed, and the error is dropped; any other head means it was
// not, and the caller gets an error that says so and may try again (the applier tells a repeat by the head and the
// apply id, as it always does). When the head cannot be read either, the answer stays "unknown". Every other error
// passes unchanged.
func (sh *shard) resolve(ctx context.Context, kg graph.KGID, want graph.Head, err error) error {
	if kernel.CodeOf(err) != pgconn.CodeOutcomeUnknown {
		return err
	}
	var cur headRow
	rerr := sh.pool.TxOn(ctx, pgconn.WritePool, RoleApplier, true, func(tx *sql.Tx) error {
		var e error
		cur, e = loadHead(ctx, tx, kg, false)
		return e
	})
	if rerr != nil {
		return err
	}
	if sameHead(cur.head, want) {
		return nil
	}
	return kernel.Wrap(pgconn.CodeConnectionLost, err, "the head of the KG does not show this write, so it was not committed; try it again")
}

// sameHead compares what a write sets in the head: the numbers of the apply and of its chunks.
func sameHead(a, b graph.Head) bool {
	return a.AppliedSeq == b.AppliedSeq && a.ReadableThrough == b.ReadableThrough && a.IngestSeq == b.IngestSeq &&
		a.ApplyID == b.ApplyID && a.NextChunkNo == b.NextChunkNo && a.ChunkCount == b.ChunkCount && a.LastApplyID == b.LastApplyID
}
