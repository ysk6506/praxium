package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func wantCode(t *testing.T, what string, err error, code kernel.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: no error, wanted %s", what, code)
	}
	if got := kernel.CodeOf(err); got != code {
		t.Fatalf("%s: code %s (%v), wanted %s", what, got, err, code)
	}
}

func noErr(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// [REQ-270]
func TestConfigFillsDefaultsAndRefusesWhatIsWrong(t *testing.T) {
	_, err := Config{}.resolve()
	wantCode(t, "no DSN", err, kernel.CodeInvalidValue)
	_, err = Config{DSN: "x", Limits: map[Role]Limits{"nobody": {}}}.resolve()
	wantCode(t, "unknown role", err, kernel.CodeInvalidValue)
	_, err = Config{DSN: "x", Limits: map[Role]Limits{RoleBulk: {Statement: -time.Second}}}.resolve()
	wantCode(t, "negative limit", err, kernel.CodeInvalidValue)
	rc, err := Config{DSN: "x", Limits: map[Role]Limits{RoleBulk: {Statement: time.Hour}}}.resolve()
	noErr(t, "resolve", err)
	if rc.MaxOpenConns != 16 || rc.MaxIdleConns != 4 || rc.PollInterval != 50*time.Millisecond || rc.ConnMaxLifetime != 30*time.Minute {
		t.Fatalf("defaults %+v", rc.Config)
	}
	if rc.limits[RoleBulk].Statement != time.Hour || rc.limits[RoleAPIRead] != DefaultLimits(RoleAPIRead) {
		t.Fatalf("limits %+v", rc.limits)
	}
	for _, r := range Roles() {
		l := DefaultLimits(r)
		if l.Statement <= 0 || l.LockWait <= 0 || l.IdleInTx <= 0 {
			t.Fatalf("role %s has a limit that is not set: %+v", r, l)
		}
	}
}

// [REQ-270]
func TestConfigNeverPrintsItsDSN(t *testing.T) {
	cfg := Config{DSN: "postgres://user:hunter2@host/db"}
	for _, text := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), cfg.String()} {
		if strings.Contains(text, "hunter2") || strings.Contains(text, "postgres://") {
			t.Fatalf("the DSN is in %q", text)
		}
	}
}

// [REQ-270]
func TestOpenFailuresDoNotShowThePassword(t *testing.T) {
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
func TestStoredValuesRoundTripAndDamageIsRefused(t *testing.T) {
	attrs := map[int]graph.Attr{
		1: {Value: "text", Grade: "fact", Basis: "configured", SourceTime: "2030-01-01T00:00:00Z", SourceRef: "r1"},
		2: {Value: int64(1) << 60, Grade: "fact", Basis: "configured"},
		3: {Value: []any{"a", int64(2), true, nil, map[string]any{"k": int64(3)}}, Grade: "fact", Basis: "configured"},
		4: {Value: nil, Grade: "fact", Basis: "configured"},
	}
	raw, err := encodeAttrs(attrs)
	noErr(t, "encode", err)
	back, err := decodeAttrs(raw)
	noErr(t, "decode", err)
	if fmt.Sprint(back) != fmt.Sprint(attrs) || back[2].Value != int64(1)<<60 {
		t.Fatalf("attrs came back as %v", back)
	}
	for name, bad := range map[string]string{"not json": "{", "key": `{"x":{"v":1,"g":"a","b":"c"}}`, "decimal": `{"1":{"v":1.5,"g":"a","b":"c"}}`,
		"nested decimal list": `{"1":{"v":[1.5],"g":"a","b":"c"}}`, "nested decimal map": `{"1":{"v":{"a":1.5},"g":"a","b":"c"}}`} {
		if _, err := decodeAttrs([]byte(bad)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
	if _, err := decodeValue([]byte("{")); err == nil {
		t.Error("a damaged value decoded")
	}
	if _, err := encodeValue(make(chan int)); err == nil {
		t.Error("a channel was encoded")
	}
	if _, err := encodeAttrs(map[int]graph.Attr{1: {Value: make(chan int)}}); err == nil {
		t.Error("an attribute with a channel was encoded")
	}
	if _, err := hashOf([]byte{1, 2}); err == nil {
		t.Error("a short hash was read")
	}
	if h, err := hashOf(nil); err != nil || h != (kernel.Hash{}) {
		t.Errorf("null hash %v %v", h, err)
	}
	if _, err := sumFrom([]byte{1}); err == nil {
		t.Error("a short sum was read")
	}
	if _, err := sumOf("zz"); err == nil {
		t.Error("a bad sum text was read")
	}
	if _, err := partsOf([]byte(`[{"e":"o","t":"t","s":"zz","r":1}]`)); err == nil {
		t.Error("a part with a bad sum was read")
	}
	if _, err := partsOf([]byte("{")); err == nil {
		t.Error("a damaged part list was read")
	}
	if _, err := refsOf([]byte("{")); err == nil {
		t.Error("damaged refs were read")
	}
	if _, err := linkMapOf([]byte(`[["Bad Id!","y"]]`)); err == nil {
		t.Error("a bad link id was read")
	}
	if _, err := linkMapOf([]byte(`[["ok","Bad Id!"]]`)); err == nil {
		t.Error("a bad second link id was read")
	}
	if _, err := linkMapOf([]byte("{")); err == nil {
		t.Error("a damaged link map was read")
	}
	if _, err := parseObject("not an id!"); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Errorf("a damaged id: %v", err)
	}
	if _, err := rollbackOf([]byte("{")); err == nil {
		t.Error("a damaged rollback was read")
	}
	if _, err := rollbackOf([]byte(`{"s":"zz","n":0,"p":[]}`)); err == nil {
		t.Error("a rollback with a bad sum was read")
	}
	if _, err := rollbackOf([]byte(`{"s":"` + strings.Repeat("0", 64) + `","n":0,"p":{}}`)); err == nil {
		t.Error("a rollback with damaged parts was read")
	}
	if _, err := hashStateOf(make([]byte, 32), 0, []byte("{")); err == nil {
		t.Error("a hash state with damaged parts was read")
	}
	if _, err := hashStateOf([]byte{1}, 0, []byte("[]")); err == nil {
		t.Error("a hash state with a short sum was read")
	}
}

func files(m map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range m {
		out["m/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// [REQ-270]
func TestMigrationFilesAreNumberedWithoutGapsAndCarryKnownDirectives(t *testing.T) {
	got, err := loadMigrations(files(map[string]string{
		"0002_b.sql": "-- praxium:statement_timeout=5s\nSELECT 2;", "0001_a.sql": "-- comment\n\n-- praxium:lock_timeout=1s\nSELECT 1;",
	}), "m")
	noErr(t, "load", err)
	if len(got) != 2 || got[0].name != "a" || got[0].lockWait != time.Second || got[1].statement != 5*time.Second || got[0].checksum == got[1].checksum {
		t.Fatalf("migrations %+v", got)
	}
	for name, set := range map[string]map[string]string{
		"a gap":              {"0001_a.sql": "x", "0003_c.sql": "x"},
		"starts at 2":        {"0002_b.sql": "x"},
		"a bad name":         {"one.sql": "x"},
		"an unknown option":  {"0001_a.sql": "-- praxium:colour=red\nx"},
		"a bad duration":     {"0001_a.sql": "-- praxium:lock_timeout=soon\nx"},
		"a negative timeout": {"0001_a.sql": "-- praxium:lock_timeout=-1s\nx"},
	} {
		if _, err := loadMigrations(files(set), "m"); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	if _, err := loadMigrations(files(nil), "elsewhere"); err == nil {
		t.Error("a missing folder loaded")
	}
	if _, err := embedded(); err != nil {
		t.Errorf("the migrations of this build: %v", err)
	}
}

// [REQ-270]
func TestDriverErrorsGetTheCodesOfThisBackend(t *testing.T) {
	if dbErr(nil, "x") != nil {
		t.Error("nil became an error")
	}
	wantCode(t, "unique", dbErr(&pq.Error{Code: "23505"}, "x"), kernel.CodeConflict)
	for _, state := range []pq.ErrorCode{"55P03", "57014", "25P03"} {
		err := dbErr(&pq.Error{Code: state}, "x")
		wantCode(t, string(state), err, kernel.CodeInternal)
		if !strings.Contains(err.Error(), "time limit") {
			t.Errorf("%s: %v", state, err)
		}
	}
	wantCode(t, "other", dbErr(&pq.Error{Code: "42P01"}, "x"), kernel.CodeInternal)
	own := kernel.Errorf(graph.CodeHeadMoved, "moved")
	if dbErr(own, "x") != own {
		t.Error("an error with a code changed")
	}
	wantCode(t, "no rows", notFound(sql.ErrNoRows, "x", graph.KGID{}), graph.CodeKGNotFound)
}

// [REQ-270]
func TestTestDatabasesNeedTheServerAndNeverSkip(t *testing.T) {
	t.Setenv(TestDSNEnv, "")
	f := &failRecorder{}
	if got := NewTestDatabase(f); got != "" || !f.failed || !strings.Contains(f.message, "with-postgres.sh") {
		t.Fatalf("without the variable: %q failed=%v %q", got, f.failed, f.message)
	}
	if NewTestStore(&failRecorder{}) != nil {
		t.Error("a store without a server")
	}
	if got := withDatabase("postgres://u:p@h:5432/one?sslmode=disable", "two"); got != "postgres://u:p@h:5432/two?sslmode=disable" {
		t.Errorf("url form: %s", got)
	}
	if got := withDatabase("host=/s user=u", "two"); got != "host=/s user=u dbname=two" {
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

type failRecorder struct {
	failed  bool
	message string
}

func (f *failRecorder) Helper() {}
func (f *failRecorder) Fatalf(format string, args ...any) {
	f.failed, f.message = true, fmt.Sprintf(format, args...)
}
func (f *failRecorder) Logf(string, ...any) {}
func (f *failRecorder) Cleanup(func())      {}

func shardOf(t *testing.T, st *Store) *shard {
	t.Helper()
	sh, err := st.rt.locate(graph.KGID{})
	noErr(t, "locate", err)
	if len(st.rt.all()) != 1 || st.rt.all()[0] != sh {
		t.Fatal("one database must be the only shard")
	}
	return sh
}

// [REQ-270]
func TestMigrationsAreRecordedAndAnAppliedOneIsNeverEdited(t *testing.T) {
	ctx := context.Background()
	st := NewTestStore(t)
	sh := shardOf(t, st)
	steps, err := embedded()
	noErr(t, "embedded", err)
	noErr(t, "again", migrate(ctx, sh, steps))
	edited := append([]migration(nil), steps...)
	edited[0].checksum = "edited"
	wantCode(t, "an edited step", migrate(ctx, sh, edited), kernel.CodeConflict)
	more := append(append([]migration(nil), steps...), migration{version: len(steps) + 1, name: "extra", sql: "CREATE TABLE extra_table (kg_id text PRIMARY KEY)", checksum: "x", lockWait: time.Second, statement: time.Minute})
	noErr(t, "a new step", migrate(ctx, sh, more))
	noErr(t, "a binary older than the database", migrate(ctx, sh, steps))
	broken := append(append([]migration(nil), more...), migration{version: len(steps) + 2, name: "broken", sql: "SELECT * FROM no_such_table", checksum: "y"})
	if err := migrate(ctx, sh, broken); err == nil {
		t.Fatal("a broken step was recorded")
	}
	var n int
	noErr(t, "count", sh.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n))
	if n != len(steps)+1 {
		t.Fatalf("%d steps recorded, wanted %d: a broken step leaves no trace", n, len(steps)+1)
	}
}

// holdMigrationLock holds the lock the runner takes until release is called.
func holdMigrationLock(t *testing.T, sh *shard) (release func()) {
	t.Helper()
	tx, err := sh.db.BeginTx(context.Background(), nil)
	noErr(t, "begin", err)
	_, err = tx.ExecContext(context.Background(), `SELECT pg_advisory_xact_lock($1)`, migrationLockKey)
	noErr(t, "lock", err)
	return func() { _ = tx.Rollback() }
}

// [REQ-270]
func TestMigrationWaitsForTheLockWithinItsLimitAndThenTakesTurns(t *testing.T) {
	ctx := context.Background()
	st := NewTestStoreWith(t, Config{Limits: map[Role]Limits{RoleMigration: {Statement: time.Minute, LockWait: 50 * time.Millisecond, IdleInTx: time.Minute}}})
	sh := shardOf(t, st)
	steps, err := embedded()
	noErr(t, "embedded", err)
	for i := range steps {
		steps[i].lockWait = 0
	}
	release := holdMigrationLock(t, sh)
	time.AfterFunc(250*time.Millisecond, release)
	noErr(t, "after the other runner finished", migrate(ctx, sh, steps))
	release = holdMigrationLock(t, sh)
	err = migrate(ctx, sh, steps)
	release()
	wantCode(t, "a lock that never goes", err, kernel.CodeInternal)
	if !strings.Contains(err.Error(), "time limit") {
		t.Fatalf("the error does not say a limit passed: %v", err)
	}
	release = holdMigrationLock(t, sh)
	cancelled, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	err = migrate(cancelled, sh, steps)
	release()
	if err == nil {
		t.Fatal("a cancelled migration went on")
	}
}

// [REQ-270]
func TestTheLimitsOfARoleApplyToItsTransactionsOnly(t *testing.T) {
	ctx := context.Background()
	st := NewTestStoreWith(t, Config{Limits: map[Role]Limits{RoleAPIRead: {Statement: 150 * time.Millisecond, LockWait: time.Second, IdleInTx: time.Minute}}})
	sh := shardOf(t, st)
	err := sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `SELECT pg_sleep(5)`)
		return dbErr(err, "sleep")
	})
	wantCode(t, "a slow read", err, kernel.CodeInternal)
	if !strings.Contains(err.Error(), "time limit") {
		t.Fatalf("the error does not say a limit passed: %v", err)
	}
	show := func(role Role) string {
		var v string
		noErr(t, "show", sh.tx(ctx, role, false, func(tx *sql.Tx) error { return tx.QueryRowContext(ctx, `SHOW statement_timeout`).Scan(&v) }))
		return v
	}
	if got := show(RoleAPIRead); got != "150ms" {
		t.Errorf("api_read statement_timeout %q", got)
	}
	if got, want := show(RoleBulk), DefaultLimits(RoleBulk).Statement; got == "150ms" || want <= 0 {
		t.Errorf("the limit of one role leaked into another: %q", got)
	}
}

// [REQ-270]
func TestAStoreClosesItsPoolAndReportsTheFirstFailure(t *testing.T) {
	st := NewTestStore(t)
	calls := 0
	st.closers = append(st.closers, func() error { calls++; return fmt.Errorf("first") }, func() error { calls++; return fmt.Errorf("second") })
	if err := st.Close(); err == nil || err.Error() != "first" || calls != 2 {
		t.Fatalf("close: %v after %d calls", err, calls)
	}
}

// [REQ-270]
func TestConfigDefaultsAndRoleOrder(t *testing.T) {
	if orInt(0, 7) != 7 || orInt(3, 7) != 3 || orInt(-1, 7) != 7 {
		t.Fatal("orInt does not keep a positive value and default the rest")
	}
	if orDuration(0, time.Second) != time.Second || orDuration(time.Minute, time.Second) != time.Minute || orDuration(-1, time.Second) != time.Second {
		t.Fatal("orDuration does not keep a positive value and default the rest")
	}
	got := sortedRoles(map[Role]Limits{"z_role": {}, "a_role": {}, "m_role": {}})
	if len(got) != 3 || got[0] != "a_role" || got[1] != "m_role" || got[2] != "z_role" {
		t.Fatalf("roles are not sorted: %v", got)
	}
}
