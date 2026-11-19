package pgsource

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

type pgsTimeoutErr struct{}

func (pgsTimeoutErr) Error() string   { return "i/o timeout with text that must not go on" }
func (pgsTimeoutErr) Timeout() bool   { return true }
func (pgsTimeoutErr) Temporary() bool { return true }

// [REQ-659]
func TestPgsClassifiesEveryKindOfFailure(t *testing.T) {
	serverErr := func(code string) error {
		return &pq.Error{Code: pq.ErrorCode(code), Message: "server words of " + code, Position: "7"}
	}
	cases := []struct {
		name string
		err  error
		kind string
	}{
		{"bad sign-in", serverErr("28P01"), ingest.SrcAuthFailed},
		{"no right", serverErr("42501"), ingest.SrcAuthFailed},
		{"no database", serverErr("3D000"), ingest.SrcRequestRejected},
		{"write in read-only", serverErr("25006"), ingest.SrcRequestRejected},
		{"statement timeout", serverErr("57014"), ingest.SrcTimeout},
		{"lock timeout", serverErr("55P03"), ingest.SrcSourceBusy},
		{"syntax", serverErr("42601"), ingest.SrcRequestRejected},
		{"bad value", serverErr("22P02"), ingest.SrcRequestRejected},
		{"not supported", serverErr("0A000"), ingest.SrcRequestRejected},
		{"connection class", serverErr("08006"), ingest.SrcConnectionLost},
		{"shutdown", serverErr("57P01"), ingest.SrcConnectionLost},
		{"crash shutdown", serverErr("57P02"), ingest.SrcConnectionLost},
		{"resources", serverErr("53300"), ingest.SrcSourceBusy},
		{"starting up", serverErr("57P03"), ingest.SrcSourceBusy},
		{"serialization", serverErr("40001"), ingest.SrcSourceBusy},
		{"deadlock", serverErr("40P01"), ingest.SrcSourceBusy},
		{"unknown code", serverErr("XX000"), ingest.SrcInternal},
		{"no tls offered", pq.ErrSSLNotSupported, ingest.SrcTLSRejected},
		{"deadline", context.DeadlineExceeded, ingest.SrcTimeout},
		{"eof", io.EOF, ingest.SrcConnectionLost},
		{"unexpected eof", io.ErrUnexpectedEOF, ingest.SrcConnectionLost},
		{"bad conn", driver.ErrBadConn, ingest.SrcConnectionLost},
		{"network timeout", pgsTimeoutErr{}, ingest.SrcConnectionLost},
		{"unknown", errors.New("something of the driver"), ingest.SrcInternal},
		{"egress policy", &egress.Error{Kind: egress.KindPolicy, Reason: "r"}, ingest.SrcPolicyDenied},
		{"egress name", &egress.Error{Kind: egress.KindResolve, Reason: "r"}, ingest.SrcNameNotFound},
		{"egress tls", &egress.Error{Kind: egress.KindTLS, Reason: "r"}, ingest.SrcTLSRejected},
		{"egress timeout", &egress.Error{Kind: egress.KindTimeout, Reason: "r"}, ingest.SrcTimeout},
		{"egress connect", &egress.Error{Kind: egress.KindConnect, Reason: "r"}, ingest.SrcConnectionLost},
	}
	for _, c := range cases {
		got := classify(context.Background(), c.err)
		se, ok := ingest.SourceErrorOf(got)
		if !ok || se.Kind != c.kind {
			t.Errorf("%s: %v", c.name, got)
			continue
		}
		if strings.Contains(se.Message, "server words") || strings.Contains(se.Message, "must not go on") {
			t.Errorf("%s: the message has words of the source: %q", c.name, se.Message)
		}
		if se.Detail != "" && !strings.HasPrefix(c.name, "syntax") && c.name != "bad value" && c.name != "not supported" {
			t.Errorf("%s: a detail on a kind that has none", c.name)
		}
	}
	// The detail of a refused statement carries the sentence and the position, and only that.
	se, _ := ingest.SourceErrorOf(classify(context.Background(), serverErr("42601")))
	if se.Detail != "server words of 42601 (position 7)" {
		t.Errorf("%q", se.Detail)
	}
	// The caller's own end is not a failure of the source.
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if got := classify(done, serverErr("57014")); !errors.Is(got, context.Canceled) {
		t.Errorf("%v", got)
	}
	if got := classify(context.Background(), &egress.Error{Kind: egress.KindCanceled}); !errors.Is(got, context.Canceled) {
		t.Errorf("%v", got)
	}
	if got := classify(context.Background(), nil); got != nil {
		t.Errorf("%v", got)
	}
	already := ingest.NewSourceError(ingest.SrcTimeout, "m", nil)
	if got := classify(context.Background(), already); got != already {
		t.Errorf("a SourceError is changed")
	}
	// A name that may resolve later is passing trouble.
	if got, _ := ingest.SourceErrorOf(classify(context.Background(), &egress.Error{Kind: egress.KindResolve})); got == nil {
		t.Errorf("no error")
	}
}

// [REQ-487]
func TestPgsOpenFailuresAreTheKindsOfTheSecret(t *testing.T) {
	for _, code := range []kernel.Code{kernel.CodeNotFound, secrets.CodeScopeDenied, secrets.CodeAddressDenied, secrets.CodeVersionRetired, kernel.CodeInvalidValue} {
		se, ok := ingest.SourceErrorOf(openError(kernel.Errorf(code, "words with a secret-looking text")))
		if !ok || se.Kind != ingest.SrcSecretUnusable || strings.Contains(se.Message, "secret-looking") {
			t.Errorf("%v: %v", code, se)
		}
	}
	se, _ := ingest.SourceErrorOf(openError(errors.New("store down")))
	if se.Kind != ingest.SrcInternal {
		t.Errorf("%+v", se)
	}
}

// [REQ-489]
func TestPgsRefusesDatasetsItCannotPage(t *testing.T) {
	col := func(name string, paths ...string) ingest.Column { return ingest.Column{Name: name, Paths: paths} }
	cases := []struct {
		name string
		ds   ingest.Dataset
	}{
		{"neither table nor sql", ingest.Dataset{ID: "d", NaturalKey: []string{"k"}, Columns: []ingest.Column{col("k")}}},
		{"both table and sql", ingest.Dataset{ID: "d", Table: "t", SQL: "select 1", NaturalKey: []string{"k"}, Columns: []ingest.Column{col("k")}}},
		{"no natural key", ingest.Dataset{ID: "d", Table: "t", Columns: []ingest.Column{col("k")}}},
		{"key that is no column", ingest.Dataset{ID: "d", Table: "t", NaturalKey: []string{"z"}, Columns: []ingest.Column{col("k")}}},
		{"key with two paths", ingest.Dataset{ID: "d", Table: "t", NaturalKey: []string{"k"}, Columns: []ingest.Column{col("k", "a", "b")}}},
		{"key into a value", ingest.Dataset{ID: "d", Table: "t", NaturalKey: []string{"k"}, Columns: []ingest.Column{col("k", "a.b")}}},
		{"key with a bad path", ingest.Dataset{ID: "d", Table: "t", NaturalKey: []string{"k"}, Columns: []ingest.Column{col("k", "a[")}}},
		{"column with a bad path", ingest.Dataset{ID: "d", Table: "t", NaturalKey: []string{"k"}, Columns: []ingest.Column{col("k"), col("x", "a[")}}},
	}
	for _, c := range cases {
		_, err := newPlan(c.ds)
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != ingest.SrcRequestRejected {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// [REQ-655]
func TestPgsTokenRoundTripAndRefusals(t *testing.T) {
	raw := encodeToken("fp", []string{"a", "b"})
	keys, ok := decodeToken(raw, "fp", 2)
	if !ok || len(keys) != 2 || keys[1] != "b" {
		t.Fatalf("%v %v", keys, ok)
	}
	for name, c := range map[string]struct {
		raw  []byte
		fp   string
		keys int
	}{
		"empty": {nil, "fp", 2}, "other dataset": {raw, "x", 2}, "other shape": {raw, "fp", 1}, "not json": {[]byte("{"), "fp", 2},
		"too big":  {[]byte(strings.Repeat(" ", maxTokenBytes+1)), "fp", 2},
		"key size": {encodeToken("fp", []string{strings.Repeat("k", maxKeyBytes+1)}), "fp", 1},
	} {
		if _, ok := decodeToken(c.raw, c.fp, c.keys); ok {
			t.Errorf("%s was accepted", name)
		}
	}
	// A key that would make the token too big gives no token, so that the read can only start from the first page again.
	if got := encodeToken("fp", []string{strings.Repeat("k", maxTokenBytes)}); got != nil {
		t.Errorf("a token over the limit was made")
	}
}

// [REQ-487]
func TestPgsSettingsTakeTheDefaults(t *testing.T) {
	s := settingsOf(&ingest.Connection{Host: "h", Database: "d", SSLMode: ingest.SSLDisable})
	if s.port != ingest.DefaultPostgresPort || !s.disable || s.statement != ingest.DefaultStatementSeconds*time.Second ||
		s.lock != ingest.DefaultLockSeconds*time.Second || s.connect != ingest.DefaultConnectSeconds*time.Second {
		t.Fatalf("%+v", s)
	}
	s = settingsOf(&ingest.Connection{Port: 6000, StatementTimeoutSeconds: 3, LockTimeoutSeconds: 4, ConnectTimeoutSeconds: 5})
	if s.port != 6000 || s.disable || s.statement != 3*time.Second || s.lock != 4*time.Second || s.connect != 5*time.Second {
		t.Fatalf("%+v", s)
	}
}

func TestPgsKeyTextAndStringArgs(t *testing.T) {
	if s, ok := keyText("a"); !ok || s != "a" {
		t.Fatal(s)
	}
	if s, ok := keyText([]byte("b")); !ok || s != "b" {
		t.Fatal(s)
	}
	if _, ok := keyText(1); ok {
		t.Fatal("a number is not key text")
	}
	if args := stringArgs([]string{"x", "y"}); len(args) != 2 || args[1] != "y" {
		t.Fatalf("%v", args)
	}
}

// [REQ-655] The statements keep their shape: the key columns are quoted, the values are arguments, and nothing the
// user wrote is placed where a name goes.
func TestPgsStatementsQuoteNamesAndTakeValuesAsArguments(t *testing.T) {
	pl, err := newPlan(ingest.Dataset{ID: "d", Table: `sch"ema.ta"ble`, NaturalKey: []string{"a", "b"}, KeyCollation: `C`,
		Columns: []ingest.Column{{Name: "a"}, {Name: "arr"}, {Name: "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	pl.types = map[string]string{"a": "TEXT", "b": "INT4", "arr": "_INT4"}
	pl.types["arr"] = "_INT4"
	q := pl.pageSQL(true, 10)
	for _, want := range []string{`"sch""ema"."ta""ble"`, `to_jsonb`, "$1", "$2", "LIMIT 10", `COLLATE "C"`} {
		if !strings.Contains(q, want) {
			t.Errorf("%q is not in %s", want, q)
		}
	}
	if strings.Contains(q, "k1") && strings.Contains(q, "'") {
		t.Errorf("a value is in the statement: %s", q)
	}
	g := pl.groupSQL(5)
	if !strings.Contains(g, "$1") || !strings.Contains(g, "LIMIT 5") {
		t.Errorf("%s", g)
	}
}
