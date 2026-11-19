package pgstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// What the parts of the storage share: the way a statement is run in a transaction, the head and the lease as
// the writes read them, and the lock that serializes the creation of KGs.

// catalogLockKey is the key of the transaction-level lock that serializes the creation of KGs: the name of a KG
// and "one production KG" are rules across KGs, so they cannot be a unique index that starts with kg_id.
const catalogLockKey int64 = 0x7072617869756d32

type scanner interface{ Scan(dest ...any) error }

// headRow is the head of a KG with what a write checks beside it.
type headRow struct {
	generation int
	state      graph.State
	head       graph.Head
}

// sameHead compares what a write sets in the head: the numbers of the apply and of its chunks.
func sameHead(a, b graph.Head) bool {
	return a.AppliedSeq == b.AppliedSeq && a.ReadableThrough == b.ReadableThrough && a.IngestSeq == b.IngestSeq &&
		a.ApplyID == b.ApplyID && a.NextChunkNo == b.NextChunkNo && a.ChunkCount == b.ChunkCount && a.LastApplyID == b.LastApplyID
}

// lockMode says how a read of the lease row locks it: not at all (a reader), shared for a write that must see a
// lease that stays as it is until the write commits, exclusive for the calls that change the lease.
type lockMode string

const (
	lockNone      lockMode = ""
	lockShare     lockMode = " FOR SHARE"
	lockExclusive lockMode = " FOR UPDATE"
)

// planned is what a write names about the state it was planned against.
type planned struct {
	kg         graph.KGID
	generation int
	expected   int64
	lease      graph.Lease
	now        time.Time
}

// batchRows is the most rows one statement carries. A chunk can hold many more (the volatile values of a first
// load come with its last chunk), and a statement of that size would be a very large message.
const batchRows = 5000

// writer runs the statements of one transaction and keeps the first error: after it, every call does nothing,
// so a sequence of writes is written straight and its error is read once at the end.
type writer struct {
	ctx context.Context
	tx  *sql.Tx
	kg  string
	err error
}

func newWriter(ctx context.Context, tx *sql.Tx, kg graph.KGID) *writer {
	return &writer{ctx: ctx, tx: tx, kg: kg.String()}
}

// exec runs one statement and returns the rows it touched.
func (w *writer) exec(what, query string, args ...any) int64 {
	if w.err != nil {
		return 0
	}
	res, err := w.tx.ExecContext(w.ctx, query, args...)
	if err != nil {
		w.err = dbErr(err, what)
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// fail records an error that is not the database's.
func (w *writer) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

// slices calls fn for each run of at most batchRows rows of n.
func slices(n int, fn func(lo, hi int)) {
	for lo := 0; lo < n; lo += batchRows {
		fn(lo, min(lo+batchRows, n))
	}
}

// collect reads every row of a query with the scan function.
func collect[T any](ctx context.Context, tx *sql.Tx, scan func(scanner) (T, error), what, query string, args ...any) ([]T, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, dbErr(err, what)
	}
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, dbErr(err, what)
		}
		out = append(out, v)
	}
	return out, dbErr(rows.Err(), what)
}

// optionalHash is the zero hash as no bytes (stored as null) and any other as its bytes.
func optionalHash(h kernel.Hash) []byte {
	if h == (kernel.Hash{}) {
		return []byte{}
	}
	return hashBytes(h)
}
