package pgconn

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func oneConn(t *testing.T, cfg Config) *Pool {
	t.Helper()
	cfg.MaxOpenConns, cfg.MaxIdleConns = 1, 1
	return NewTestPoolWith(t, cfg)
}

func readText(ctx context.Context, p *Pool, role Role, ro bool, q string) string {
	var v string
	err := p.Tx(ctx, role, ro, func(tx *sql.Tx) error { return tx.QueryRowContext(ctx, q).Scan(&v) })
	if err != nil {
		return "error: " + err.Error()
	}
	return v
}

// [REQ-553]
func TestAServerOlderThan18IsRefusedAtTheStartWithTheReason(t *testing.T) {
	ctx := context.Background()
	wantCode(t, "17.5", CheckServerVersion(170005), CodeServerTooOld)
	wantCode(t, "9.6", CheckServerVersion(90624), CodeServerTooOld)
	noErr(t, "18.0", CheckServerVersion(180000))
	noErr(t, "19.1", CheckServerVersion(190001))
	if err := CheckServerVersion(170005); !strings.Contains(err.Error(), "version 17") || !strings.Contains(err.Error(), "18 or newer") {
		t.Fatalf("the reason is not told: %v", err)
	}
	dsn := NewTestDatabase(t)
	_, err := Open(ctx, Config{DSN: dsn, ReadVersion: func(context.Context, *Handle) (int, error) { return 170005, nil }})
	wantCode(t, "a server of 17", err, CodeServerTooOld)
	p, err := Open(ctx, Config{DSN: dsn, ReadVersion: func(context.Context, *Handle) (int, error) { return 180002, nil }})
	noErr(t, "a server of 18", err)
	noErr(t, "close", p.Close())
	_, err = Open(ctx, Config{DSN: dsn, ReadDSN: dsn + " application_name=reader", ReadVersion: func(_ context.Context, h *Handle) (int, error) {
		var n int
		if e := h.QueryRowContext(ctx, `SELECT 170000 WHERE current_setting('application_name') = 'reader' UNION ALL SELECT 180000`).Scan(&n); e != nil {
			return 0, e
		}
		return n, nil
	}})
	wantCode(t, "a read server of 17", err, CodeServerTooOld)
	_, err = Open(ctx, Config{DSN: dsn, ReadVersion: func(context.Context, *Handle) (int, error) { return 0, driver.ErrBadConn }})
	wantCode(t, "no answer about the version", err, kernel.CodeInternal)
	p = NewTestPool(t)
	num, err := readServerVersion(ctx, p.DB())
	noErr(t, "the real version", err)
	noErr(t, "the real server passes the gate", CheckServerVersion(num))
}

// [REQ-554]
func TestTheConnectionStringIsReadByTheCodeAndNotByTheEnvironment(t *testing.T) {
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	for name, bad := range map[string]string{
		"an unknown key":               dsn + " keepalives_idle=10",
		"a setting of the server":      dsn + " search_path=a",
		"a way to choose the server":   dsn + " target_session_attrs=any",
		"another way to choose":        dsn + " target_session_attrs=read-only",
		"not a connection string":      "postgres://user:hunter2@%zz/db",
		"a key without a value":        "host",
		"a wrong value of a known key": dsn + " sslmode=sometimes",
	} {
		_, err := Open(ctx, Config{DSN: bad})
		if err == nil {
			t.Errorf("%s: opened", name)
			continue
		}
		if kernel.CodeOf(err) != kernel.CodeInvalidValue || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: %v", name, err)
		}
	}
	p, err := Open(ctx, Config{DSN: dsn + " target_session_attrs=read-write"})
	noErr(t, "the value that is put in anyway", err)
	noErr(t, "close", p.Close())
	t.Setenv("PGHOST", "elsewhere.example.test")
	_, err = Open(ctx, Config{DSN: dsn})
	if kernel.CodeOf(err) != kernel.CodeInvalidValue || !strings.Contains(err.Error(), "PGHOST") {
		t.Fatalf("a variable of the driver was let in: %v", err)
	}
	if names := strings.Join(driverEnvNames(), " "); !strings.Contains(names, "PGSSLMODE") || !strings.Contains(names, "PGTARGETSESSIONATTRS") || strings.Contains(names, " set ") {
		t.Fatalf("the variables of the driver: %s", names)
	}
}

// [REQ-554]
func TestTheWritePoolWantsAServerThatTakesWritesAndTheReadPoolDoesNot(t *testing.T) {
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	admin := NewTestPool(t)
	name := dsn[strings.LastIndex(dsn, "dbname=")+len("dbname="):]
	// A server whose sessions are read-only by default is a server that does not take writes.
	noErr(t, "alter", admin.Tx(ctx, RoleMigration, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `ALTER DATABASE "`+name+`" SET default_transaction_read_only = on`)
		return err
	}))
	_, err := Open(ctx, Config{DSN: dsn, ConnectTimeout: 2 * time.Second})
	if err == nil {
		t.Fatal("the write pool took a server whose sessions are read-only")
	}
	p := &Pool{}
	rc, err := Config{DSN: dsn}.resolve()
	noErr(t, "resolve", err)
	p, err = build(rc)
	noErr(t, "build", err)
	t.Cleanup(func() { _ = p.Close() })
	noErr(t, "the read pool reaches it", p.read.PingContext(ctx))
	if err := p.db.PingContext(ctx); err == nil {
		t.Fatal("the write pool reached a server that is read-only")
	}
}

// [REQ-554]
func TestSeveralHostsAreTriedInOrderAndAReadConnectionStringHasItsOwnPool(t *testing.T) {
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	many := strings.Replace(dsn, "host=", "host=/no/such/dir,", 1)
	p, err := Open(ctx, Config{DSN: many, ConnectTimeout: 2 * time.Second})
	noErr(t, "the second host serves", err)
	noErr(t, "close", p.Close())
	r, err := Open(ctx, Config{DSN: dsn, ReadDSN: dsn + " application_name=reader"})
	noErr(t, "open", err)
	t.Cleanup(func() { _ = r.Close() })
	if got := readText(ctx, r, RoleAPIRead, true, `SHOW application_name`); got != "reader" {
		t.Errorf("a read went to %q", got)
	}
	if got := readText(ctx, r, RoleApplier, false, `SHOW application_name`); got == "reader" {
		t.Errorf("a write went to the read connection string: %q", got)
	}
	noErr(t, "the query pool", r.TxOn(ctx, QueryPool, RoleDryRun, true, func(tx *sql.Tx) error { return tx.QueryRowContext(ctx, `SELECT 1`).Scan(new(int)) }))
	if r.Generation("nowhere") != 0 {
		t.Error("a pool that is not there has a generation")
	}
	wantCode(t, "a pool that is not there", r.TxOn(ctx, "nowhere", RoleApplier, false, func(*sql.Tx) error { return nil }), kernel.CodeInternal)
}

// [REQ-555]
func TestAnAnswerThatSaysTheRoleChangedRenewsThePoolOnce(t *testing.T) {
	ctx := context.Background()
	p := oneConn(t, Config{})
	write := func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SET TRANSACTION READ ONLY`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `CREATE TABLE refused_write (a int)`)
		return err
	}
	g := p.Generation(WritePool)
	wantCode(t, "a write the server calls read-only", p.Tx(ctx, RoleApplier, false, write), CodeRoleChanged)
	if p.Generation(WritePool) != g+1 || p.Generation(ReadPool) != 0 {
		t.Fatalf("generations: write %d (was %d), read %d", p.Generation(WritePool), g, p.Generation(ReadPool))
	}
	noErr(t, "the next write", p.Tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TABLE accepted_write (a int)`)
		return err
	}))
	// The same refusal in a transaction that was opened read-only is the caller's doing: the server is fine.
	g = p.Generation(ReadPool)
	err := p.Tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TABLE written_by_a_reader (a int)`)
		return DBErr(err, "write")
	})
	wantCode(t, "a write in a read", err, kernel.CodeInternal)
	if p.Generation(ReadPool) != g {
		t.Error("a read-only transaction that tried to write renewed the pool")
	}
	// Callers that meet the same failure together renew once.
	h := p.DB()
	_, seen := h.current()
	if !h.renew(seen) || h.renew(seen) {
		t.Error("a failure seen by two callers renewed the pool twice")
	}
}

// [REQ-555]
func TestALostConnectionRenewsThePoolAndTheNextCallGoesOnANewConnection(t *testing.T) {
	ctx := context.Background()
	p := oneConn(t, Config{})
	g := p.Generation(WritePool)
	err := p.Tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `SELECT pg_terminate_backend(pg_backend_pid())`)
		return err
	})
	wantCode(t, "a session that ends itself", err, CodeConnectionLost)
	if p.Generation(WritePool) != g+1 {
		t.Fatalf("the write pool was not renewed")
	}
	if got := readText(ctx, p, RoleApplier, false, `SELECT 'alive'`); got != "alive" {
		t.Fatalf("the call after the loss: %s", got)
	}
	// A connection that the server ended while it sat idle is replaced by the pool without an error.
	var pid int
	noErr(t, "pid", p.Tx(ctx, RoleApplier, false, func(tx *sql.Tx) error { return tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid) }))
	var gone bool
	noErr(t, "end it", p.Tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&gone)
	}))
	waitGone(t, p, pid)
	if got := readText(ctx, p, RoleApplier, false, `SELECT 'again'`); got != "again" {
		t.Fatalf("the call after an idle connection was ended: %s", got)
	}
}

func waitGone(t *testing.T, p *Pool, pid int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 400; i++ {
		var n int
		noErr(t, "look", p.Tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&n)
		}))
		if n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d did not end", pid)
}

// [REQ-555] [REQ-557]
func TestACommitThatGetsNoAnswerIsUnknownAndNeverASuccessOrAFailure(t *testing.T) {
	ctx := context.Background()
	var p *Pool
	armed := new(atomic.Bool)
	hook := func(ctx context.Context, tx *sql.Tx) error {
		if !armed.CompareAndSwap(true, false) {
			return nil
		}
		var pid int
		if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		var ok bool
		if err := p.read.QueryRowContext(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&ok); err != nil {
			return err
		}
		waitGone(t, p, pid)
		return nil
	}
	p = oneConn(t, Config{BeforeCommit: hook})
	noErr(t, "table", p.Tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TABLE kept (a int)`)
		return err
	}))
	g := p.Generation(WritePool)
	armed.Store(true)
	err := p.Tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO kept VALUES (1)`)
		return err
	})
	armed.Store(false)
	wantCode(t, "a commit on a connection the server ended", err, CodeOutcomeUnknown)
	if p.Generation(WritePool) != g+1 {
		t.Error("the pool was not renewed after the unknown outcome")
	}
	if got := readText(ctx, p, RoleApplier, true, `SELECT count(*)::text FROM kept`); got != "0" {
		t.Errorf("rows in the table: %s (the server never saw the COMMIT)", got)
	}
	noErr(t, "the next write", p.Tx(ctx, RoleApplier, false, func(*sql.Tx) error { return nil }))
	hookFails := kernel.Errorf(kernel.CodeConflict, "no")
	failing := new(atomic.Bool)
	p2 := NewTestPoolWith(t, Config{BeforeCommit: func(context.Context, *sql.Tx) error {
		if failing.Load() {
			return hookFails
		}
		return nil
	}})
	failing.Store(true)
	if err := p2.Tx(ctx, RoleApplier, false, func(*sql.Tx) error { return nil }); err != hookFails {
		t.Errorf("the error of the hook: %v", err)
	}
}

// [REQ-555]
func TestAPoolThatIsClosedOrNotOpenedGivesAnError(t *testing.T) {
	p := NewTestPool(t)
	noErr(t, "close", p.Close())
	wantCode(t, "a closed pool", p.TxOn(context.Background(), ReadPool, RoleAPIRead, true, func(*sql.Tx) error { return nil }), kernel.CodeInternal)
	_, err := Open(context.Background(), Config{DSN: NewTestDatabase(t), ReadDSN: "host=/no/such/dir sslmode=disable connect_timeout=1"})
	if err == nil {
		t.Error("an unreachable read server was accepted")
	}
	_, err = Open(context.Background(), Config{DSN: NewTestDatabase(t), ReadDSN: "postgres://u:hunter2@%zz/db"})
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("an unreadable read connection string: %v", err)
	}
}

// [REQ-555]
func TestAPoolThatCannotConnectIsRenewedAndSaysTheConnectionIsLost(t *testing.T) {
	ctx := context.Background()
	rc, err := Config{DSN: "host=/no/such/dir user=u sslmode=disable", ConnectTimeout: time.Second}.resolve()
	noErr(t, "resolve", err)
	p, err := build(rc)
	noErr(t, "build", err)
	t.Cleanup(func() { _ = p.Close() })
	wantCode(t, "no server", p.Tx(ctx, RoleApplier, false, func(*sql.Tx) error { return nil }), CodeConnectionLost)
	if p.Generation(WritePool) != 1 {
		t.Errorf("generation %d", p.Generation(WritePool))
	}
	_, err = readServerVersion(ctx, p.DB())
	if err == nil {
		t.Error("a version from nowhere")
	}
}

// [REQ-555]
func TestAContextThatEndsBeforeTheCommitIsTheCallersAndNotUnknown(t *testing.T) {
	p := NewTestPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	err := p.Tx(ctx, RoleApplier, false, func(*sql.Tx) error { cancel(); return nil })
	if err == nil || kernel.CodeOf(err) == CodeOutcomeUnknown || kernel.CodeOf(err) == CodeConnectionLost {
		t.Fatalf("a commit after the context ended: %v", err)
	}
	if p.Generation(WritePool) != 0 {
		t.Error("the pool was renewed for an error of the caller")
	}
}
