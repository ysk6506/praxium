package pgstore

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// NewTestStoreThatLosesACommit opens a Store on a new test database. After arm() is called, the next transaction
// that moves a head has its backend ended by the server just before its COMMIT, so the COMMIT gets no answer.
func NewTestStoreThatLosesACommit(tb TB) (store *Store, arm func()) {
	tb.Helper()
	armed := new(atomic.Bool)
	var pool *pgconn.Pool
	hook := func(ctx context.Context, tx *sql.Tx) error {
		if !armed.Load() {
			return nil
		}
		var moved int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_xact_user_tables WHERE relname = 'head' AND n_tup_upd > 0`).Scan(&moved); err != nil || moved == 0 {
			return err
		}
		armed.Store(false)
		var pid int
		if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		return pool.TxOn(ctx, pgconn.ReadPool, RoleAPIRead, true, func(other *sql.Tx) error {
			if _, err := other.ExecContext(ctx, `SELECT pg_terminate_backend($1)`, pid); err != nil {
				return err
			}
			for i := 0; i < 500; i++ {
				var n int
				if err := other.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&n); err != nil || n == 0 {
					return err
				}
				time.Sleep(10 * time.Millisecond)
			}
			return nil
		})
	}
	pool = pgconn.NewTestPoolWith(tb, pgconn.Config{BeforeCommit: hook})
	models, err := StandardModels(false)
	if err != nil {
		tb.Fatalf("models: %v", err)
	}
	return &Store{rt: singleRouter{&shard{pool: pool}}, poll: defaultPollInterval, models: models, clock: LeaseClockCaller}, func() { armed.Store(true) }
}

// ResolveForTest runs the check of a write whose COMMIT got no answer.
func (s *Store) ResolveForTest(ctx context.Context, kg graph.KGID, want graph.Head, err error) error {
	a, aerr := s.access(ctx, kg)
	if aerr != nil {
		return err
	}
	return s.resolve(ctx, a, want, err)
}
