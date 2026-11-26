package pgconn

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The tests of the templates of the test databases (ADR-0120). Every helper they add starts with tpl, so that none of them meets a
// name of another test file of this folder.

// tplKey is a key no other test uses, so the template is made by this test.
func tplKey(t *testing.T) string {
	return "pgconn template test " + t.Name() + " " + kernel.NewToken(6)
}

// tplServer is the DSN of the test server and a handle on its maintenance database; the template of key is dropped when the test
// ends, to give its room back.
func tplServer(t *testing.T, key string) (string, *sql.DB) {
	t.Helper()
	base, admin := openTestServer(t)
	if admin == nil {
		t.FailNow()
	}
	name := testTemplateName(key)
	t.Cleanup(func() {
		testTemplates.Delete(base + "\x00" + name)
		_, _ = admin.ExecContext(context.Background(), `ALTER DATABASE "`+name+`" WITH IS_TEMPLATE false`)
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})
	return base, admin
}

func tplInt(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func tplOpen(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", dsn)
	noErr(t, "open", err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// tplBuild makes a table with two rows and a sequence that has given two numbers.
func tplBuild(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(context.Background(), `CREATE TABLE tpl_thing (id bigserial PRIMARY KEY, v text NOT NULL); INSERT INTO tpl_thing (v) VALUES ('a'), ('b')`)
	return err
}

// tplLeft is how many databases of the template's name, made or being built, the server has.
func tplLeft(t *testing.T, admin *sql.DB, key string) int64 {
	t.Helper()
	return tplInt(t, admin, `SELECT count(*) FROM pg_database WHERE starts_with(datname, $1)`, testTemplateName(key))
}

// [REQ-270]
func TestTplATemplateIsMadeOnceAndEveryCopyIsADatabaseOfItsOwnWithWhatTheTemplateHas(t *testing.T) {
	key := tplKey(t)
	_, admin := tplServer(t, key)
	builds := 0
	build := func(dsn string) error { builds++; return tplBuild(dsn) }
	a, b := tplOpen(t, NewTestDatabaseFrom(t, key, build)), tplOpen(t, NewTestDatabaseFrom(t, key, build))
	if builds != 1 {
		t.Fatalf("the template was made %d times", builds)
	}
	for name, db := range map[string]*sql.DB{"a": a, "b": b} {
		if n := tplInt(t, db, `SELECT count(*) FROM tpl_thing`); n != 2 {
			t.Errorf("copy %s has %d rows", name, n)
		}
		if n := tplInt(t, db, `SELECT nextval('tpl_thing_id_seq')`); n != 3 {
			t.Errorf("the sequence of copy %s gave %d, not the number after the template's", name, n)
		}
	}
	_, err := a.ExecContext(context.Background(), `INSERT INTO tpl_thing (v) VALUES ('only in a')`)
	noErr(t, "write to a", err)
	if n := tplInt(t, b, `SELECT count(*) FROM tpl_thing`); n != 2 {
		t.Errorf("a write to one copy is in the other: %d rows", n)
	}
	var template, connect bool
	noErr(t, "the template", admin.QueryRowContext(context.Background(), `SELECT datistemplate, datallowconn FROM pg_database WHERE datname = $1`,
		testTemplateName(key)).Scan(&template, &connect))
	if !template || connect {
		t.Errorf("the template: is a template %v, takes connections %v", template, connect)
	}
	if n := tplLeft(t, admin, key); n != 1 {
		t.Errorf("%d databases of the template's name, not the template alone", n)
	}
	// a process that has not seen the template finds it on the server and does not make it again
	base, _ := kernel.LookupEnv(TestDSNEnv)
	testTemplates.Delete(base + "\x00" + testTemplateName(key))
	c := tplOpen(t, NewTestDatabaseFrom(t, key, func(string) error { t.Error("the template was made again"); return nil }))
	if n := tplInt(t, c, `SELECT count(*) FROM tpl_thing`); n != 2 {
		t.Errorf("the copy of the template found on the server has %d rows", n)
	}
	if testTemplateName(key) == testTemplateName(key+" ") || !strings.HasPrefix(testTemplateName(key), testTemplatePrefix) || len(testTemplateName(key))+10 > 63 {
		t.Errorf("the name of a template: %s", testTemplateName(key))
	}
}

// [REQ-270]
func TestTplTestsOfOneProcessThatAskForATemplateAtOnceMakeItOnce(t *testing.T) {
	key := tplKey(t)
	base, admin := tplServer(t, key)
	var mu sync.Mutex
	builds := 0
	build := func(dsn string) error {
		mu.Lock()
		builds++
		mu.Unlock()
		return tplBuild(dsn)
	}
	errs := make([]error, 4)
	names := make([]string, 4)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names[i], errs[i] = ensureTestTemplate(context.Background(), base, admin, key, build)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		noErr(t, "ask", err)
		if names[i] != testTemplateName(key) {
			t.Errorf("asked for %s, got %s", testTemplateName(key), names[i])
		}
	}
	if builds != 1 {
		t.Errorf("the template was made %d times", builds)
	}
}

// [REQ-270]
func TestTplAProcessWaitsForTheOneThatMakesTheTemplateAndThenCopiesWhatItMade(t *testing.T) {
	ctx := context.Background()
	key := tplKey(t)
	base, admin := tplServer(t, key)
	name := testTemplateName(key)
	other, err := admin.Conn(ctx)
	noErr(t, "the other process", err)
	defer func() { _ = other.Close() }()
	lock := uint64(testTemplateLock(name))
	_, err = other.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, int64(lock))
	noErr(t, "the lock of the other process", err)
	done := make(chan error, 1)
	go func() {
		_, err := ensureTestTemplate(ctx, base, admin, key, func(string) error { return errors.New("made while another process made it") })
		done <- err
	}()
	tplWaiting(t, admin, lock)
	// the other process makes the template, as a process of this build does, and lets go of the lock
	_, err = other.ExecContext(ctx, `CREATE DATABASE "`+name+`" IS_TEMPLATE true ALLOW_CONNECTIONS false`)
	noErr(t, "the template of the other process", err)
	_, err = other.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, int64(lock))
	noErr(t, "unlock", err)
	noErr(t, "the waiting process", <-done)
	if makeTestDatabase(t, base, admin, name) == "" {
		t.Error("no copy of the template the other process made")
	}
}

// tplWaiting waits until a session waits for the advisory lock of that key.
func tplWaiting(t *testing.T, admin *sql.DB, lock uint64) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		if tplInt(t, admin, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND classid = $1 AND objid = $2`,
			int64(lock>>32), int64(lock&0xffffffff)) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no session waits for the lock")
}

// [REQ-270]
func TestTplAProcessThatStopsWaitingOrCannotReachTheServerFailsAndMakesNothing(t *testing.T) {
	key := tplKey(t)
	base, admin := tplServer(t, key)
	other, err := admin.Conn(context.Background())
	noErr(t, "the other process", err)
	defer func() { _ = other.Close() }()
	lock := uint64(testTemplateLock(testTemplateName(key)))
	_, err = other.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, int64(lock))
	noErr(t, "the lock of the other process", err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ensureTestTemplate(ctx, base, admin, key, tplBuild)
		done <- err
	}()
	tplWaiting(t, admin, lock)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a process that stopped waiting for the lock went on")
	}
	_, err = other.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, int64(lock))
	noErr(t, "unlock", err)
	if n := tplLeft(t, admin, key); n != 0 {
		t.Errorf("%d databases of a template nobody made", n)
	}
	closed, err := sql.Open("postgres", base)
	noErr(t, "open", err)
	noErr(t, "close", closed.Close())
	if _, err := ensureTestTemplate(context.Background(), base, closed, key, tplBuild); err == nil {
		t.Error("a template was made through a closed handle")
	}
	if _, err := testQueryLines(context.Background(), base, `SELECT NULL::text`); err == nil {
		t.Error("a line that is not text was read")
	}
}

// [REQ-270]
func TestTplATemplateThatCannotBeMadeFailsTheTestAndLeavesNothingBehind(t *testing.T) {
	key := tplKey(t)
	_, admin := tplServer(t, key)
	f := &failRecorder{}
	broken := func(dsn string) error {
		if err := tplBuild(dsn); err != nil {
			return err
		}
		return kernel.Errorf(kernel.CodeConflict, "broken on purpose")
	}
	if got := NewTestDatabaseFrom(f, key, broken); got != "" || !f.failed || !strings.Contains(f.message, "cannot make the template database of the test") ||
		!strings.Contains(f.message, "broken on purpose") {
		t.Fatalf("a build that fails: %q failed=%v %q", got, f.failed, f.message)
	}
	if n := tplLeft(t, admin, key); n != 0 {
		t.Errorf("%d databases of the failed template are left", n)
	}
	// planner statistics in the template are refused: a copy would plan otherwise than a database made the slow way
	f = &failRecorder{}
	analyzed := func(dsn string) error {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		_, err = db.ExecContext(context.Background(), `CREATE TABLE tpl_many AS SELECT g AS n FROM generate_series(1, 100) g; ANALYZE tpl_many`)
		return err
	}
	if got := NewTestDatabaseFrom(f, key, analyzed); got != "" || !f.failed || !strings.Contains(f.message, "statistics of the planner") {
		t.Fatalf("a template with statistics: %q failed=%v %q", got, f.failed, f.message)
	}
	if n := tplLeft(t, admin, key); n != 0 {
		t.Errorf("%d databases of the refused template are left", n)
	}
	// the next test that asks makes it
	if n := tplInt(t, tplOpen(t, NewTestDatabaseFrom(t, key, tplBuild)), `SELECT count(*) FROM tpl_thing`); n != 2 {
		t.Errorf("the template made after the failures gives %d rows", n)
	}
}

// [REQ-270]
func TestTplACopyThatTheServerRefusesFailsTheTestAndIsNotMadeAnotherWay(t *testing.T) {
	key := tplKey(t)
	_, admin := tplServer(t, key)
	NewTestDatabaseFrom(t, key, tplBuild)
	// the template goes away behind the back of the process, which thinks it is there
	_, err := admin.ExecContext(context.Background(), `ALTER DATABASE "`+testTemplateName(key)+`" WITH IS_TEMPLATE false`)
	noErr(t, "unmark", err)
	_, err = admin.ExecContext(context.Background(), `DROP DATABASE "`+testTemplateName(key)+`"`)
	noErr(t, "drop", err)
	f := &failRecorder{}
	if got := NewTestDatabaseFrom(f, key, func(string) error { t.Error("made another way"); return nil }); got != "" || !f.failed ||
		!strings.Contains(f.message, "cannot create a test database") {
		t.Fatalf("a refused copy: %q failed=%v %q", got, f.failed, f.message)
	}
}

// [REQ-270]
func TestTplATemplateNeedsTheTestServerAsATestDatabaseDoes(t *testing.T) {
	t.Setenv(TestDSNEnv, "")
	f := &failRecorder{}
	if got := NewTestDatabaseFrom(f, "any", tplBuild); got != "" || !f.failed || !strings.Contains(f.message, "with-postgres.sh") {
		t.Fatalf("without the variable: %q failed=%v %q", got, f.failed, f.message)
	}
	f = &failRecorder{}
	if got := NewMigratedTestDatabase(f); got != "" || !f.failed {
		t.Fatalf("a migrated database without the variable: %q failed=%v", got, f.failed)
	}
	t.Setenv(TestDSNEnv, "host=/nonexistent-socket-dir user=u password=hunter2 sslmode=disable connect_timeout=2")
	f = &failRecorder{}
	NewTestDatabaseFrom(f, "any", tplBuild)
	if !f.failed || strings.Contains(f.message, "hunter2") {
		t.Errorf("an unreachable server: failed=%v %q", f.failed, f.message)
	}
	if _, err := DumpTestDatabase(context.Background(), "host=/nonexistent-socket-dir user=u sslmode=disable connect_timeout=2"); err == nil {
		t.Error("a database that cannot be reached was read")
	}
}

// [REQ-270]
func TestTplACopyOfTheMigratedTemplateIsTheDatabaseThatOpenMakesFromAnEmptyOne(t *testing.T) {
	ctx := context.Background()
	fresh := NewTestDatabase(t)
	empty, err := DumpTestDatabase(ctx, fresh)
	noErr(t, "dump the empty database", err)
	for _, line := range empty {
		if strings.HasPrefix(line, "relation ") || strings.HasPrefix(line, "row ") {
			t.Fatalf("a new test database is not empty: %s", line)
		}
	}
	p, err := Open(ctx, Config{DSN: fresh})
	noErr(t, "open", err)
	noErr(t, "close", p.Close())
	want, err := DumpTestDatabase(ctx, fresh)
	noErr(t, "dump the migrated database", err)
	copied := NewMigratedTestDatabase(t)
	got, err := DumpTestDatabase(ctx, copied)
	noErr(t, "dump the copy", err)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the copy differs from the database Open made:\n%s", tplDiff(want, got))
	}
	steps, err := embedded()
	noErr(t, "migrations", err)
	if !strings.Contains(strings.Join(want, "\n"), "row public.schema_migrations") || len(steps) == 0 {
		t.Fatalf("the dump does not show the migrations: %d lines", len(want))
	}
	// every copy has a token of the installation of its own, as a database made the slow way has (ADR-0113)
	tokens := map[string]bool{}
	for _, dsn := range []string{fresh, copied, NewMigratedTestDatabase(t)} {
		var token string
		noErr(t, "token", tplOpen(t, dsn).QueryRowContext(ctx, `SELECT token FROM install_token`).Scan(&token))
		if len(token) != 12 || tokens[token] {
			t.Errorf("the token %q is not one of its own", token)
		}
		tokens[token] = true
	}
	if !strings.Contains(strings.Join(want, "\n"), `row public.install_token {"id": 1, "token": "a token of its own"}`) {
		t.Error("the dump shows the token of the installation")
	}
	key := TestMigrationsKey()
	for _, m := range steps {
		if !strings.Contains(key, m.checksum) || !strings.Contains(key, m.name) {
			t.Errorf("the key of the migrated template leaves out %04d_%s", m.version, m.name)
		}
	}
	if !strings.Contains(key, "(error <nil>), storage format ") {
		t.Errorf("the key leaves out the storage format or holds an error: %s", key)
	}
	// the clock: a time of now is recent, an old one is shown as it is
	db := tplOpen(t, copied)
	_, err = db.ExecContext(ctx, `CREATE TABLE tpl_times (at timestamptz, old timestamptz, none timestamptz); INSERT INTO tpl_times VALUES (now(), '2001-02-03T04:05:06Z', NULL)`)
	noErr(t, "times", err)
	lines, err := DumpTestDatabase(ctx, copied)
	noErr(t, "dump the times", err)
	if !strings.Contains(strings.Join(lines, "\n"), `row public.tpl_times {"at": "recent", "old": "2001-02-03T04:05:06+00:00", "none": null}`) {
		t.Errorf("the times are not shown as they should be:\n%s", strings.Join(lines, "\n"))
	}
}

// [REQ-270]
func TestTplATestIsMadeParallelOnceWhateverHelpersAskAndASubtestIsLeftAsItIs(t *testing.T) {
	TestInParallel(t)
	TestInParallel(t) // t.Parallel twice would panic
	if _, ok := testsInParallel.Load(t); !ok {
		t.Error("the test was not made parallel")
	}
	t.Run("sub", func(t *testing.T) {
		TestInParallel(t)
		if _, ok := testsInParallel.Load(t); ok {
			t.Error("a subtest was made parallel")
		}
	})
}

// tplDiff is the lines that are in one dump and not in the other.
func tplDiff(want, got []string) string {
	in := map[string]int{}
	for _, l := range want {
		in[l]++
	}
	for _, l := range got {
		in[l]--
	}
	var out []string
	for _, l := range append(append([]string(nil), want...), got...) {
		switch n := in[l]; {
		case n > 0:
			out = append(out, "- "+l)
		case n < 0:
			out = append(out, "+ "+l)
		}
		in[l] = 0
	}
	return strings.Join(out, "\n")
}
