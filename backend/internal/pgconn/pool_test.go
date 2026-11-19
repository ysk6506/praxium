package pgconn

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-270]
func TestAPoolRefusesWhatItCannotOpenAndNeverShowsTheDSN(t *testing.T) {
	ctx := context.Background()
	for name, dsn := range map[string]string{
		"unreadable":  "postgres://user:hunter2@%zz/db",
		"unreachable": "host=/nonexistent-socket-dir user=u password=hunter2 sslmode=disable connect_timeout=2",
	} {
		_, err := Open(ctx, Config{DSN: dsn})
		if err == nil {
			t.Fatalf("%s: opened", name)
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("%s: the DSN is in %q", name, err)
		}
	}
	_, err := Open(ctx, Config{})
	wantCode(t, "empty DSN", err, kernel.CodeInvalidValue)
}

// [REQ-270]
func TestAPoolWillNotOpenOnADatabaseWhoseMigrationWasEdited(t *testing.T) {
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	raw, err := sql.Open("postgres", dsn)
	noErr(t, "open", err)
	defer func() { _ = raw.Close() }()
	_, err = raw.ExecContext(ctx, `CREATE TABLE schema_migrations (version int PRIMARY KEY, name text NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`)
	noErr(t, "create", err)
	_, err = raw.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, checksum) VALUES (1, 'kg_storage', 'not the checksum of the file')`)
	noErr(t, "insert", err)
	_, err = Open(ctx, Config{DSN: dsn})
	wantCode(t, "an edited migration", err, kernel.CodeConflict)
}

// [REQ-270]
func TestAPoolRunsTransactionsAndClosesItsConnections(t *testing.T) {
	ctx := context.Background()
	pool := NewTestPool(t)
	noErr(t, "ping", pool.DB().PingContext(ctx))
	var one int
	noErr(t, "a reading transaction", pool.Tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
	}))
	if one != 1 {
		t.Fatalf("read %d", one)
	}
	own := kernel.Errorf(kernel.CodeConflict, "mine")
	if err := pool.Tx(ctx, RoleApplier, false, func(*sql.Tx) error { return own }); err != own {
		t.Errorf("the error of the function changed: %v", err)
	}
	err := pool.Tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TABLE written_by_a_reader (id int)`)
		return DBErr(err, "write")
	})
	wantCode(t, "a write in a reading transaction", err, kernel.CodeInternal)
	noErr(t, "close", pool.Close())
	err = pool.Tx(ctx, RoleApplier, false, func(*sql.Tx) error { return nil })
	wantCode(t, "a closed pool", err, kernel.CodeInternal)
}

// [REQ-270]
func TestDatabaseErrorsAreGivenTheCodesOfThisBackend(t *testing.T) {
	if DBErr(nil, "x") != nil {
		t.Error("nil became an error")
	}
	for state, want := range map[pq.ErrorCode]kernel.Code{"23505": kernel.CodeConflict, "55P03": kernel.CodeInternal, "57014": kernel.CodeInternal, "25P03": kernel.CodeInternal, "42P01": kernel.CodeInternal} {
		wantCode(t, string(state), DBErr(&pq.Error{Code: state}, "x"), want)
	}
	if err := DBErr(&pq.Error{Code: "25P03"}, "x"); !strings.Contains(err.Error(), "time limit") {
		t.Errorf("a time limit is not named: %v", err)
	}
	wantCode(t, "a plain error", DBErr(errors.New("broken"), "x"), kernel.CodeInternal)
	own := kernel.Errorf(kernel.CodeNotFound, "gone")
	if DBErr(own, "x") != own {
		t.Error("an error with a code changed")
	}
}

// [REQ-270]
func TestTestDatabasesNeedTheServerAndAreMadeOnIt(t *testing.T) {
	t.Setenv(TestDSNEnv, "")
	f := &failRecorder{}
	if got := NewTestDatabase(f); got != "" || !f.failed || !strings.Contains(f.message, "with-postgres.sh") {
		t.Fatalf("without the variable: %q failed=%v %q", got, f.failed, f.message)
	}
	if NewTestPool(&failRecorder{}) != nil {
		t.Error("a pool without a server")
	}
	if got := WithDatabase("postgres://u:p@h:5432/one?sslmode=disable", "two"); got != "postgres://u:p@h:5432/two?sslmode=disable" {
		t.Errorf("url form: %s", got)
	}
	if got := WithDatabase("host=/s user=u", "two"); got != "host=/s user=u dbname=two" {
		t.Errorf("key form: %s", got)
	}
	t.Setenv(TestDSNEnv, "postgres://user:hunter2@%zz/db")
	f = &failRecorder{}
	NewTestDatabase(f)
	if !f.failed || strings.Contains(f.message, "hunter2") {
		t.Errorf("an unreadable DSN: failed=%v %q", f.failed, f.message)
	}
	t.Setenv(TestDSNEnv, "host=/nonexistent-socket-dir user=u sslmode=disable connect_timeout=2")
	f = &failRecorder{}
	NewTestDatabase(f)
	if !f.failed {
		t.Error("an unreachable server was accepted")
	}
}

// [REQ-270]
func TestATestServerThatRefusesToMakeADatabaseIsReportedByItsOwnWords(t *testing.T) {
	base := NewTestDatabase(t)
	t.Setenv(TestDSNEnv, base+" user=no_such_role_"+kernel.NewToken(4)+" password=hunter2")
	f := &failRecorder{}
	NewTestDatabase(f)
	if !f.failed || !strings.Contains(f.message, "cannot create a test database") || strings.Contains(f.message, "hunter2") {
		t.Errorf("a refused server: failed=%v %q", f.failed, f.message)
	}
}

// [REQ-270]
func TestAPoolOfALimitedRoleStopsASlowStatementAndSaysSo(t *testing.T) {
	ctx := context.Background()
	pool := NewTestPoolWith(t, Config{Limits: map[Role]Limits{RoleDryRun: {Statement: 100 * time.Millisecond, LockWait: time.Second, IdleInTx: time.Minute}}})
	err := pool.Tx(ctx, RoleDryRun, true, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `SELECT pg_sleep(5)`)
		return DBErr(err, "sleep")
	})
	if err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Fatalf("a slow statement: %v", err)
	}
}
