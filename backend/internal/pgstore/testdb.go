package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// TestDSNEnv names the environment variable that gives the DSN of the PostgreSQL server the tests use. It is
// set by scripts/with-postgres.sh (a throwaway server) and by the CI job (a service container).
const TestDSNEnv = "PRAXIUM_TEST_PG_DSN"

// TB is the part of testing.TB the test helpers need; a test passes its *testing.T.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	Cleanup(func())
}

// NewTestDatabase makes an empty database on the test server and returns its DSN. The database is dropped when
// the test ends. A missing TestDSNEnv fails the test: a test that needs PostgreSQL never passes by being skipped.
func NewTestDatabase(tb TB) string {
	tb.Helper()
	base, ok := kernel.LookupEnv(TestDSNEnv)
	if !ok || strings.TrimSpace(base) == "" {
		tb.Fatalf("%s is not set: run the tests through scripts/with-postgres.sh, or give the DSN of a test server (docs/harness-guide.md)", TestDSNEnv)
		return ""
	}
	db := "praxium_t_" + kernel.NewToken(8)
	admin, err := sql.Open("postgres", base)
	if err != nil {
		tb.Fatalf("cannot read %s as a PostgreSQL DSN (its text is not shown)", TestDSNEnv)
		return ""
	}
	tb.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(context.Background(), `CREATE DATABASE "`+db+`"`); err != nil {
		// A driver error may repeat the DSN, so only the server's own message is shown.
		var pe *pq.Error
		if errors.As(err, &pe) {
			tb.Fatalf("cannot create a test database: %s", pe.Message)
		} else {
			tb.Fatalf("cannot reach the test server given in %s (its text is not shown)", TestDSNEnv)
		}
		return ""
	}
	tb.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+db+`" WITH (FORCE)`); err != nil {
			tb.Logf("cannot drop the test database %s", db)
		}
	})
	return withDatabase(base, db)
}

// withDatabase is the DSN with another database name. A URL gets a new path; a key/value DSN gets a later
// dbname, which wins.
func withDatabase(dsn, name string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		if u, err := url.Parse(dsn); err == nil {
			u.Path = "/" + name
			return u.String()
		}
	}
	return dsn + " dbname=" + name
}

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
