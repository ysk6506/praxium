package pgstore

import (
	"context"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// TestDSNEnv names the environment variable that gives the DSN of the PostgreSQL server the tests use. It is
// set by scripts/with-postgres.sh (a throwaway server) and by the CI job (a service container).
const TestDSNEnv = pgconn.TestDSNEnv

// TB is the part of testing.TB the test helpers need; a test passes its *testing.T.
type TB = pgconn.TB

// NewTestDatabase makes an empty database on the test server and returns its DSN. The database is dropped when
// the test ends. A missing TestDSNEnv fails the test: a test that needs PostgreSQL never passes by being skipped.
func NewTestDatabase(tb TB) string {
	tb.Helper()
	return pgconn.NewTestDatabase(tb)
}

// withDatabase is the DSN with another database name (see pgconn.WithDatabase).
func withDatabase(dsn, name string) string { return pgconn.WithDatabase(dsn, name) }

// NewTestStore opens a Store on a new empty test database with the standard model (without the statistics
// objects). The lease runs out by the clock of the caller, as the tests move a clock of their own. The store is
// closed and the database dropped when the test ends.
func NewTestStore(tb TB) *Store { return NewTestStoreWith(tb, LeaseClockCaller) }

// NewTestStoreWith is NewTestStore with the clock that decides when a lease has run out (LeaseClockDB: the
// database's, the one a server uses).
func NewTestStoreWith(tb TB, clock LeaseClock) *Store {
	tb.Helper()
	dsn := NewTestDatabase(tb)
	if dsn == "" {
		return nil
	}
	models, err := StandardModels(false)
	if err != nil {
		tb.Fatalf("cannot load the standard model: %v", err)
		return nil
	}
	st, err := Open(context.Background(), Config{DSN: dsn, Models: models, LeaseClock: clock})
	if err != nil {
		tb.Fatalf("cannot open the test store: %v", err)
		return nil
	}
	tb.Cleanup(func() { _ = st.Close() })
	return st
}

// RecordChunks makes this Store hand every chunk that ApplyChunk wrote to fn once it is written, in the order they were
// written. The tests of the staged apply use it to take the rows the rows path wrote (ADR-0099). Only tests use it, and
// they set it before the calls that write.
func (s *Store) RecordChunks(fn func(graph.ChunkWrite)) { s.record = fn }

// StopWorkAfter makes an abandon or a restore of this Store stop with err once it has done n steps (n of at least 1),
// as a server that dies in the middle does: the head keeps the work and ResumeWork ends it. Only tests use it, and
// they set it before the call that does the work.
func (s *Store) StopWorkAfter(n int, err error) {
	steps := 0
	s.hook = func(string, int64) error {
		if steps++; steps >= n {
			return err
		}
		return nil
	}
}
