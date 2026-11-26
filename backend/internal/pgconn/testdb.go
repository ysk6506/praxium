package pgconn

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

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
	base, admin := openTestServer(tb)
	if admin == nil {
		return ""
	}
	return makeTestDatabase(tb, base, admin, "")
}

// openTestServer is the DSN of the test server and a handle on its maintenance database, closed when the test ends;
// a nil handle means the test has failed.
func openTestServer(tb TB) (string, *sql.DB) {
	tb.Helper()
	base, ok := kernel.LookupEnv(TestDSNEnv)
	if !ok || strings.TrimSpace(base) == "" {
		tb.Fatalf("%s is not set: run the tests through scripts/with-postgres.sh, or give the DSN of a test server (docs/harness-guide.md)", TestDSNEnv)
		return "", nil
	}
	admin, err := sql.Open("postgres", base)
	if err != nil {
		tb.Fatalf("cannot read %s as a PostgreSQL DSN (its text is not shown)", TestDSNEnv)
		return "", nil
	}
	tb.Cleanup(func() { _ = admin.Close() })
	return base, admin
}

// makeTestDatabase creates a database, a copy of template when that is not empty, and drops it when the test ends.
func makeTestDatabase(tb TB, base string, admin *sql.DB, template string) string {
	tb.Helper()
	db := "praxium_t_" + kernel.NewToken(8)
	create := `CREATE DATABASE "` + db + `"`
	if template != "" {
		create += ` TEMPLATE "` + template + `"`
	}
	_, err := admin.ExecContext(context.Background(), create)
	if err == nil {
		tb.Cleanup(func() {
			if _, err := admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+db+`" WITH (FORCE)`); err != nil {
				tb.Logf("cannot drop the test database %s", db)
			}
		})
		if template != "" {
			_, err = testQueryLines(context.Background(), WithDatabase(base, db), testRenewToken)
		}
	}
	if err != nil {
		tb.Fatalf("%s", testServerError("cannot create a test database", err))
		return ""
	}
	return WithDatabase(base, db)
}

// testRenewToken gives a copy of a template a token of the installation of its own (migration 0008): the token names the roles of
// the query schema, which belong to the cluster, so two databases with one token would share them (ADR-0113, ADR-0120). A template
// that the migrations did not run on has no token.
const testRenewToken = `DO $$ BEGIN
	IF to_regclass('public.install_token') IS NOT NULL THEN
		UPDATE public.install_token SET token = substr(replace(gen_random_uuid()::text, '-', ''), 1, 12);
	END IF;
END $$`

// testServerError says what went wrong with the test server. A driver error may repeat the DSN, so only the server's own
// message is shown.
func testServerError(what string, err error) string {
	var pe *pq.Error
	if errors.As(err, &pe) {
		return what + ": " + pe.Message
	}
	var ke *kernel.Error
	if errors.As(err, &ke) {
		return what + ": " + ke.Error()
	}
	return "cannot reach the test server given in " + TestDSNEnv + " (its text is not shown)"
}

// WithDatabase is the DSN with another database name. A URL gets a new path; a key/value DSN gets a later
// dbname, which wins.
func WithDatabase(dsn, name string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		if u, err := url.Parse(dsn); err == nil {
			u.Path = "/" + name
			return u.String()
		}
	}
	return dsn + " dbname=" + name
}

// NewTestPool opens a Pool on a new empty test database. The Pool is closed and the database dropped when the
// test ends.
func NewTestPool(tb TB) *Pool {
	tb.Helper()
	return NewTestPoolWith(tb, Config{})
}

// NewTestPoolWith is NewTestPool with the settings of cfg; its DSN is replaced by the test database's.
func NewTestPoolWith(tb TB, cfg Config) *Pool {
	tb.Helper()
	cfg.DSN = NewTestDatabase(tb)
	if cfg.DSN == "" {
		return nil
	}
	p, err := Open(context.Background(), cfg)
	if err != nil {
		tb.Fatalf("cannot open the test pool: %v", err)
		return nil
	}
	tb.Cleanup(func() { _ = p.Close() })
	return p
}

// testTemplatePrefix starts the name of every template database the tests make (ADR-0120).
const testTemplatePrefix = "praxium_tpl_"

// testTemplateForm is the way this file makes and names a template. It is in the hash of every template's name, so a
// change of the way never hands out a template made the old way.
const testTemplateForm = "template-form-1"

// testTemplates are the templates this process has made or found, by server and name; a test that asks again does not
// look on the server, and two tests of this process that ask at once make it once.
var testTemplates sync.Map // server DSN + NUL + name -> *testTemplate

type testTemplate struct {
	mu    sync.Mutex
	ready bool
}

// NewTestDatabaseFrom makes a database on the test server as a copy of a template and returns its DSN; the copy is
// dropped when the test ends (ADR-0120). key names everything that decides what the template holds: two calls with the
// same key get copies of one template, so a key that leaves out something that changes the template is a mistake of the
// caller. The first call on the server makes the template: build gets the DSN of a new empty database, leaves it as the
// tests need it and closes every connection it opened. It is made under an advisory lock of the maintenance database,
// so test processes that run side by side make it once, and it becomes the template, which no one can connect to, only
// once it is whole. The template is left on the server, which the tests throw away. A template that cannot be made, or
// copied, fails the test: there is no other way to the database.
func NewTestDatabaseFrom(tb TB, key string, build func(dsn string) error) string {
	tb.Helper()
	base, admin := openTestServer(tb)
	if admin == nil {
		return ""
	}
	name, err := ensureTestTemplate(context.Background(), base, admin, key, build)
	if err != nil {
		tb.Fatalf("%s", testServerError("cannot make the template database of the test", err))
		return ""
	}
	return makeTestDatabase(tb, base, admin, name)
}

// testTemplateName is the name of the template of a key.
func testTemplateName(key string) string {
	sum := sha256.Sum256([]byte(testTemplateForm + "\n" + key))
	return testTemplatePrefix + hex.EncodeToString(sum[:12])
}

// testTemplateLock is the key of the advisory lock under which the template of that name is made.
func testTemplateLock(name string) int64 {
	sum := sha256.Sum256([]byte(name))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

func ensureTestTemplate(ctx context.Context, base string, admin *sql.DB, key string, build func(string) error) (string, error) {
	name := testTemplateName(key)
	v, _ := testTemplates.LoadOrStore(base+"\x00"+name, &testTemplate{})
	tpl := v.(*testTemplate)
	tpl.mu.Lock()
	defer tpl.mu.Unlock()
	if tpl.ready {
		return name, nil
	}
	if err := makeTestTemplate(ctx, base, admin, name, build); err != nil {
		return "", err
	}
	tpl.ready = true
	return name, nil
}

// makeTestTemplate makes the template unless another process has: it builds a database of another name and renames it
// to the template's only when it is whole, so a process that died while building leaves nothing that is found.
func makeTestTemplate(ctx context.Context, base string, admin *sql.DB, name string, build func(string) error) error {
	conn, err := admin.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	lock := testTemplateLock(name)
	_, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lock)
	var found bool
	if err == nil {
		defer func() { _, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lock) }()
		// a statement of its own, so its snapshot is taken after the lock: it sees what the process that let go of it made
		err = conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&found)
	}
	if err != nil || found {
		return err
	}
	work := name + "_b" + kernel.NewToken(4)
	_, err = conn.ExecContext(ctx, `CREATE DATABASE "`+work+`"`)
	if err == nil {
		err = buildTestTemplate(ctx, conn, base, work, build)
	}
	if err == nil {
		_, err = conn.ExecContext(ctx, `ALTER DATABASE "`+work+`" RENAME TO "`+name+`"`)
	}
	if err != nil {
		// a template cannot be dropped, so it stops being one first
		_, _ = conn.ExecContext(context.Background(), `ALTER DATABASE "`+work+`" WITH IS_TEMPLATE false`)
		_, _ = conn.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+work+`" WITH (FORCE)`)
	}
	return err
}

// buildTestTemplate lets build fill the database, refuses one that holds statistics of the planner (a copy would plan its
// statements otherwise than a database made the slow way), and closes it to connections.
func buildTestTemplate(ctx context.Context, conn *sql.Conn, base, work string, build func(string) error) error {
	if err := build(WithDatabase(base, work)); err != nil {
		return err
	}
	if err := refuseTestStatistics(ctx, WithDatabase(base, work)); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, `ALTER DATABASE "`+work+`" WITH IS_TEMPLATE true ALLOW_CONNECTIONS false`)
	return err
}

// refuseTestStatistics fails when a table or a statistics object of the database has statistics of the planner, which
// only ANALYZE (or the autovacuum's) makes.
func refuseTestStatistics(ctx context.Context, dsn string) error {
	rows, err := testQueryLines(ctx, dsn, `SELECT `+testStatisticsRows)
	if err == nil && rows[0] != "0" {
		err = kernel.Errorf(kernel.CodeConflict, "the template has %s rows of statistics of the planner", rows[0])
	}
	return err
}

// ParallelTB is the part of testing.T that TestInParallel needs.
type ParallelTB interface {
	Name() string
	Parallel()
}

// testsInParallel are the tests TestInParallel has made parallel.
var testsInParallel sync.Map // ParallelTB -> true

// TestInParallel lets a top-level test run beside the other parallel tests of its package, the first time a helper of
// the test asks (ADR-0120): every such test has a database of its own. A subtest is left as it is, as its parent may
// read what it did after it. A helper of a package calls it only for tests that share nothing else with the others (no
// environment, no lock table of the server they measure, no measure of what the planner does).
func TestInParallel(t ParallelTB) {
	if strings.Contains(t.Name(), "/") {
		return
	}
	if _, seen := testsInParallel.LoadOrStore(t, true); !seen {
		t.Parallel()
	}
}

// NewMigratedTestDatabase makes a database on the test server on which the migrations of this build have run (Open
// ran them) and returns its DSN; it is a copy of a template made once (NewTestDatabaseFrom). The database is dropped
// when the test ends.
func NewMigratedTestDatabase(tb TB) string {
	tb.Helper()
	return NewTestDatabaseFrom(tb, TestMigrationsKey(), func(dsn string) error {
		p, err := Open(context.Background(), Config{DSN: dsn})
		if err == nil {
			err = p.Close()
		}
		return err
	})
}

// TestMigrationsKey is the part of the key of a template that names the migrations of this build and the storage format
// (NewTestDatabaseFrom): every file's number, name and checksum. Migrations that cannot be read put their error in the
// key; Open fails with it when the template is made.
func TestMigrationsKey() string {
	steps, err := embedded()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("migrations (error %v), storage format %d", err, StorageFormat))
	for _, m := range steps {
		b.WriteString("\n" + strconv.Itoa(m.version) + " " + m.name + " " + m.checksum)
	}
	return b.String()
}
