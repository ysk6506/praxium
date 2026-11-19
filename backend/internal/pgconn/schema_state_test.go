package pgconn

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func sxExec(t *testing.T, p *Pool, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	noErr(t, query, p.Tx(ctx, RoleMigration, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	}))
}

// [REQ-632]
func TestABuildThatKnowsAnOlderStorageFormatRefusesToStartAndSaysWhy(t *testing.T) {
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	p, err := Open(ctx, Config{DSN: dsn})
	noErr(t, "open", err)
	var stored int
	noErr(t, "read", p.DB().QueryRowContext(ctx, `SELECT version FROM storage_format WHERE id = 1`).Scan(&stored))
	if stored != StorageFormat || StorageFormat != 1 {
		t.Fatalf("the database has the format %d and the build knows %d", stored, StorageFormat)
	}
	noErr(t, "same format", CheckStorageFormat(StorageFormat))
	noErr(t, "older format", CheckStorageFormat(StorageFormat-1))
	sxExec(t, p, `UPDATE storage_format SET version = $1`, StorageFormat+1)
	noErr(t, "close", p.Close())
	_, err = Open(ctx, Config{DSN: dsn})
	wantCode(t, "a newer format", err, CodeStorageFormatTooNew)
	if !strings.Contains(err.Error(), "storage format 2") || !strings.Contains(err.Error(), "as new as the database") {
		t.Fatalf("the reason is not told: %v", err)
	}
	raw, err := sql.Open("postgres", dsn)
	noErr(t, "raw", err)
	defer func() { _ = raw.Close() }()
	_, err = raw.ExecContext(ctx, `DELETE FROM storage_format`)
	noErr(t, "delete", err)
	_, err = Open(ctx, Config{DSN: dsn})
	wantCode(t, "no row", err, kernel.CodeInternal)
	_, err = raw.ExecContext(ctx, `INSERT INTO storage_format (id, version) VALUES (1, 1)`)
	noErr(t, "put back", err)
	p, err = Open(ctx, Config{DSN: dsn})
	noErr(t, "open again", err)
	noErr(t, "close again", p.Close())
}

func TestTheSharedTablesOfTheSchemaExecutorAreThere(t *testing.T) {
	ctx := context.Background()
	p := NewTestPool(t)
	var volatility string
	noErr(t, "function", p.DB().QueryRowContext(ctx, `SELECT provolatile::text FROM pg_proc WHERE proname = 'praxium_ts' AND pronamespace = 'public'::regnamespace`).Scan(&volatility))
	if volatility != "i" {
		t.Fatalf("praxium_ts is %q, not immutable", volatility)
	}
	var at time.Time
	noErr(t, "time", p.DB().QueryRowContext(ctx, `SELECT public.praxium_ts('2030-01-02T03:04:05Z')`).Scan(&at))
	if !at.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("the time is %v", at)
	}
	var first, second int64
	noErr(t, "sequence", p.DB().QueryRowContext(ctx, `SELECT nextval('kg_no_seq')`).Scan(&first))
	noErr(t, "sequence", p.DB().QueryRowContext(ctx, `SELECT nextval('kg_no_seq')`).Scan(&second))
	if first != 1 || second != 2 {
		t.Fatalf("the sequence gave %d and %d", first, second)
	}
	var n int
	noErr(t, "state", p.DB().QueryRowContext(ctx, `SELECT count(*) FROM kg_schema_state`).Scan(&n))
	if n != 0 {
		t.Fatalf("%d rows in a new database", n)
	}
	sxExec(t, p, `INSERT INTO kg_schema_state (kg_id, kg_no, generator_version, registry_hash, numbering_hash, search_hash, manifest, state, applied_at)
		VALUES ('kg-1', 1, 1, 'a', 'b', 'c', '{}', 'ok', now())`)
	err := p.Tx(ctx, RoleMigration, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO kg_schema_state (kg_id, kg_no, generator_version, registry_hash, numbering_hash, search_hash, manifest, state, applied_at)
			VALUES ('kg-2', 1, 1, 'a', 'b', 'c', '{}', 'ok', now())`)
		return err
	})
	if err == nil {
		t.Fatal("two KGs got one number")
	}
	err = p.Tx(ctx, RoleMigration, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE kg_schema_state SET state = 'half'`)
		return err
	})
	if err == nil {
		t.Fatal("a state that does not exist was taken")
	}
}

func TestAStatementThatCannotRunInATransactionRunsOnItsOwnConnection(t *testing.T) {
	ctx := context.Background()
	p := NewTestPool(t)
	sxExec(t, p, `CREATE TABLE t_online (a int)`)
	sxExec(t, p, `INSERT INTO t_online SELECT g FROM generate_series(1, 100) g`)
	inTx := p.Tx(ctx, RoleMigration, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE INDEX CONCURRENTLY t_online_a ON t_online (a)`)
		return err
	})
	if inTx == nil {
		t.Fatal("CREATE INDEX CONCURRENTLY ran in a transaction")
	}
	noErr(t, "outside it", p.ExecOutsideTx(ctx, RoleMigration, `CREATE INDEX CONCURRENTLY IF NOT EXISTS t_online_a ON t_online (a)`))
	noErr(t, "again", p.ExecOutsideTx(ctx, RoleMigration, `CREATE INDEX CONCURRENTLY IF NOT EXISTS t_online_a ON t_online (a)`))
	var valid bool
	noErr(t, "valid", p.DB().QueryRowContext(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid = 't_online_a'::regclass`).Scan(&valid))
	if !valid {
		t.Fatal("the index is not valid")
	}
	err := p.ExecOutsideTx(ctx, RoleMigration, `CREATE INDEX CONCURRENTLY nope ON no_such_table (a)`)
	if err == nil || kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("a failure came back as %v", err)
	}
	noErr(t, "the connection is still good", p.ExecOutsideTx(ctx, RoleMigration, `DROP INDEX CONCURRENTLY IF EXISTS t_online_a`))
}

func TestAStatementOutsideATransactionIsHeldToTheStatementLimitOfItsRole(t *testing.T) {
	ctx := context.Background()
	p := NewTestPoolWith(t, Config{Limits: map[Role]Limits{RoleMigration: {Statement: 200 * time.Millisecond, LockWait: time.Second, IdleInTx: time.Second}}})
	err := p.ExecOutsideTx(ctx, RoleMigration, `SELECT pg_sleep(10)`)
	if err == nil {
		t.Fatal("the statement ran for its ten seconds: the limit of the role was not applied")
	}
	noErr(t, "the pool is still good", p.ExecOutsideTx(ctx, RoleMigration, `SELECT 1`))
	noErr(t, "close", p.Close())
	wantCode(t, "a closed pool", p.ExecOutsideTx(ctx, RoleMigration, `SELECT 1`), kernel.CodeInternal)
}

func TestAStepThatCannotGetItsLocksIsTriedAgainAndOtherFailuresAreNot(t *testing.T) {
	ctx := context.Background()
	calls := 0
	busy := &pq.Error{Code: "55P03"}
	err := RetryLockWaits(ctx, func() error {
		calls++
		if calls < 3 {
			return DBErr(busy, "a step")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("%d calls, %v", calls, err)
	}
	calls = 0
	other := RetryLockWaits(ctx, func() error { calls++; return kernel.Errorf(kernel.CodeConflict, "no") })
	if calls != 1 || kernel.CodeOf(other) != kernel.CodeConflict {
		t.Fatalf("%d calls, %v", calls, other)
	}
}

func TestTheOnlyFilesThatMayRunAStatementOffATransactionAreTheTestHelperAndTheOutsideRunner(t *testing.T) {
	src := "package a\nfunc h(conn interface{ ExecContext(string) }) { conn.ExecContext(\"CREATE INDEX CONCURRENTLY i ON t (a)\") }\n"
	got, err := SessionStateViolations(fstest.MapFS{
		"online_exec.go": &fstest.MapFile{Data: []byte(src)},
		"other.go":       &fstest.MapFile{Data: []byte(src)},
	})
	noErr(t, "scan", err)
	if len(got) != 1 || !strings.HasPrefix(got[0], "other.go:") {
		t.Fatalf("violations %v", got)
	}
}
