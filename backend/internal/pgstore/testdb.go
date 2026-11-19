package pgstore

import (
	"context"

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

// NewTestStore opens a Store on a new empty test database. The Store is closed and the database dropped when
// the test ends.
func NewTestStore(tb TB) *Store {
	tb.Helper()
	return NewTestStoreWith(tb, Config{})
}

// NewTestStoreWith is NewTestStore with the settings of cfg; its DSN is replaced by the test database's.
func NewTestStoreWith(tb TB, cfg Config) *Store {
	tb.Helper()
	cfg.DSN = NewTestDatabase(tb)
	if cfg.DSN == "" {
		return nil
	}
	st, err := Open(context.Background(), cfg)
	if err != nil {
		tb.Fatalf("cannot open the test store: %v", err)
		return nil
	}
	tb.Cleanup(func() { _ = st.Close() })
	return st
}
