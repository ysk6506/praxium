package pgconn

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/fstest"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// timeoutLike is an error of the network as the driver returns it: it says Timeout(), and its type is not named here.
type timeoutLike struct{ error }

func (timeoutLike) Timeout() bool { return true }

func timeoutError() error { return timeoutLike{errors.New("i/o timeout")} }

func pgErr(code string, fatal bool) error {
	sev := "ERROR"
	if fatal {
		sev = "FATAL"
	}
	return &pq.Error{Code: pq.ErrorCode(code), Severity: sev}
}

// [REQ-554] [REQ-555]
func TestErrorsOfTheDriverAreSortedByWhatTheySayAboutTheConnection(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want Kind
	}{
		"nothing":                 {nil, KindNone},
		"cancelled":               {context.Canceled, KindCaller},
		"expired":                 {fmt.Errorf("x: %w", context.DeadlineExceeded), KindCaller},
		"unique violation":        {pgErr("23505", false), KindServer},
		"time limit":              {pgErr("57014", false), KindServer},
		"read only":               {pgErr("25006", false), KindRoleChanged},
		"starting up":             {pgErr("57P03", false), KindRoleChanged},
		"starting up, fatal":      {pgErr("57P03", true), KindRoleChanged},
		"connection exception":    {pgErr("08006", false), KindRoleChanged},
		"another fatal":           {pgErr("28P01", true), KindLost},
		"bad connection":          {driver.ErrBadConn, KindLost},
		"wrapped bad connection":  {kernel.Wrap(kernel.CodeInternal, driver.ErrBadConn, "x"), KindLost},
		"end of file":             {io.EOF, KindLost},
		"a network error":         {timeoutError(), KindLost},
		"anything else":           {errors.New("who knows"), KindLost},
		"wrapped answer":          {kernel.Wrap(kernel.CodeInternal, pgErr("25006", false), "x"), KindRoleChanged},
		"joined by several hosts": {errors.Join(errors.New("host one"), pgErr("57P03", true)), KindRoleChanged},
	} {
		if got := Classify(tc.err); got != tc.want {
			t.Errorf("%s: %s, wanted %s", name, got, tc.want)
		}
	}
}

// [REQ-555]
func TestTheWorkOfATransactionRenewsThePoolOnlyForErrorsOfTheDriver(t *testing.T) {
	for name, tc := range map[string]struct {
		err      error
		readOnly bool
		want     Kind
	}{
		"a missing row":             {sql.ErrNoRows, false, KindNone},
		"an error of the store":     {kernel.Errorf(kernel.CodeConflict, "mine"), false, KindNone},
		"a plain error":             {errors.New("broken"), false, KindNone},
		"read only in a write":      {pgErr("25006", false), false, KindRoleChanged},
		"read only in a read":       {pgErr("25006", true), true, KindServer},
		"starting up in a read":     {pgErr("57P03", false), true, KindRoleChanged},
		"a lost connection":         {driver.ErrBadConn, false, KindLost},
		"a network error":           {kernel.Wrap(kernel.CodeInternal, timeoutError(), "x"), true, KindLost},
		"an answer of the server":   {pgErr("23505", false), false, KindServer},
		"a cancelled context":       {fmt.Errorf("x: %w", context.Canceled), false, KindNone},
		"an unexpected end of data": {io.ErrUnexpectedEOF, false, KindLost},
	} {
		got := kindOfWork(tc.err, tc.readOnly)
		want := tc.want
		if got != want {
			t.Errorf("%s: %s, wanted %s", name, got, want)
		}
	}
}

// [REQ-555]
func TestACommitIsKnownOnlyWhenTheDriverOrTheServerSaysSo(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want Outcome
	}{
		"success":                 {nil, OutcomeCommitted},
		"the server refused":      {pgErr("40001", false), OutcomeNotCommitted},
		"a deferred constraint":   {pgErr("23505", false), OutcomeNotCommitted},
		"a failed transaction":    {pq.ErrInFailedTransaction, OutcomeNotCommitted},
		"the context ended":       {context.Canceled, OutcomeNotCommitted},
		"the transaction is done": {sql.ErrTxDone, OutcomeNotCommitted},
		"bad connection":          {driver.ErrBadConn, OutcomeUnknown},
		"fatal answer":            {pgErr("57P01", true), OutcomeUnknown},
		"end of file":             {io.EOF, OutcomeUnknown},
		"a network error":         {timeoutError(), OutcomeUnknown},
		"anything else":           {errors.New("who knows"), OutcomeUnknown},
	} {
		if got := CommitOutcome(tc.err); got != tc.want {
			t.Errorf("%s: %d, wanted %d", name, got, tc.want)
		}
	}
}

// [REQ-554]
func TestTheUniqueKeyQuestionAnswersAsTheCodesOfThisBackendDo(t *testing.T) {
	unique, other := pgErr("23505", false), pgErr("42P01", false)
	if !IsUniqueViolation(unique) || IsUniqueViolation(other) || IsUniqueViolation(errors.New("x")) || IsUniqueViolation(nil) {
		t.Fatal("IsUniqueViolation does not tell a taken key from the rest")
	}
	if kernel.CodeOf(DBErr(unique, "x")) != kernel.CodeConflict || kernel.CodeOf(DBErr(other, "x")) == kernel.CodeConflict {
		t.Fatal("DBErr and IsUniqueViolation disagree")
	}
	for _, c := range Codes() {
		if m, ok := CodeMeaning(c); !ok || m == "" {
			t.Errorf("code %s has no meaning", c)
		}
	}
	if _, ok := CodeMeaning("nope"); ok || len(Codes()) != 4 {
		t.Error("the catalogue of codes is not what it should be")
	}
	if Kind(KindLost).String() != "lost" {
		t.Error("kind names")
	}
}

// [REQ-559]
func TestWhatMayNotStayOnASessionIsFoundInTheSourcesOfThisUnit(t *testing.T) {
	got, err := SessionStateViolations(ownSources)
	noErr(t, "scan", err)
	if len(got) != 0 {
		t.Fatalf("the sources leave something on a session or write outside a transaction: %v", got)
	}
	bad := fstest.MapFS{
		"a.go": &fstest.MapFile{Data: []byte("package a\nvar q = []string{\"LISTEN x\", \"NOTIFY x\", \"CREATE TEMP TABLE t (a int)\", \"SET search_path = a\"," +
			"\"PREPARE p AS SELECT 1\", \"SELECT pg_advisory_lock(1)\", \"SELECT set_config('a', 'b', false)\"}\n" +
			"func f(db interface{ Exec(string) }) { db.Exec(\"x\"); db.Prepare(\"y\") }\n" +
			"func k(s struct{ db interface{ Exec(string) } }) { s.db.Exec(\"x\") }\n")},
		"ok.go":     &fstest.MapFile{Data: []byte("package a\nvar q = []string{\"SET LOCAL x = 1\", \"UPDATE t SET a = 1\", \"SELECT set_config('a', 'b', true)\"}\nfunc g(tx interface{ Exec(string) }) { tx.Exec(\"x\") }\n")},
		"a_test.go": &fstest.MapFile{Data: []byte("package a\nvar q = \"LISTEN x\"\n")},
		"testdb.go": &fstest.MapFile{Data: []byte("package a\nfunc h(db interface{ Exec(string) }) { db.Exec(\"CREATE DATABASE x\") }\n")},
	}
	got, err = SessionStateViolations(bad)
	noErr(t, "scan the bad ones", err)
	if len(got) != 10 {
		t.Fatalf("%d violations found, wanted 10: %v", len(got), got)
	}
	if _, err := SessionStateViolations(fstest.MapFS{"broken.go": &fstest.MapFile{Data: []byte("not go")}}); err == nil {
		t.Error("a file that does not parse was accepted")
	}
}
