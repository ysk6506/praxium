package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

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

// [REQ-270]
func TestAStoreClosesItsPoolAndReportsTheFirstFailure(t *testing.T) {
	st := NewTestStore(t)
	calls := 0
	st.closers = append(st.closers, func() error { calls++; return fmt.Errorf("first") }, func() error { calls++; return fmt.Errorf("second") })
	if err := st.Close(); err == nil || err.Error() != "first" || calls != 2 {
		t.Fatalf("close: %v after %d calls", err, calls)
	}
}
