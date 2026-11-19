package pgconn

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ExecOutsideTx runs one statement that PostgreSQL does not let run inside a transaction, such as CREATE INDEX
// CONCURRENTLY, on one connection of the write pool. It is the only write that does not go through Tx, so it is
// kept to what has no other way, and it follows the rules of the others: it sets nothing on the session (the
// time limit of the role is the deadline of the context, which the driver turns into a cancel at the server), it
// gives the connection back as it took it, and it is safe to run twice (the caller writes the statement so,
// with IF NOT EXISTS or IF EXISTS). The pool is renewed and the code is given as Tx does when the failure says
// the server changed its role or the connection broke.
func (p *Pool) ExecOutsideTx(ctx context.Context, role Role, query string) error {
	if p.closed.Load() {
		return kernel.Errorf(kernel.CodeInternal, "the pool %q is not open", WritePool)
	}
	if limit := p.limits[role].Statement; limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	db, gen := p.db.current()
	conn, err := db.Conn(ctx)
	if err != nil {
		return settle(p.db, gen, Classify(err), err, "take a connection")
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, query); err != nil {
		return settle(p.db, gen, Classify(err), err, "run a statement outside a transaction")
	}
	return nil
}

// RetryLockWaits runs step again, waiting a little longer each time, while it fails for want of a lock within the
// lock timeout of its role. The migration runner uses the same rule, so several servers started together take
// turns; the executor of the schema of a KG does the same.
func RetryLockWaits(ctx context.Context, step func() error) error { return retryLocks(ctx, step) }
