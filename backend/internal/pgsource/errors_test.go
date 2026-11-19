package pgsource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

func pgsOneDS() string { return pgsDS("ds", pgsTable("t"), "k", "k") }

// [REQ-487] [REQ-659] [REQ-670]
func TestPgsSortsFailuresIntoTheClosedKinds(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t VALUES (1)`)

	// A wrong password.
	r.addSecret("sec_wrong", r.user, "wrong-password-for-tests")
	r.secretID = "sec_wrong"
	res := r.read(pgsOneDS(), ingest.PageOptions{})
	pgsSrcErr(t, res.err, ingest.SrcAuthFailed)
	r.secretID = pgsSecretID

	// A role that does not exist.
	r.addSecret("sec_nobody", "nobody_here", "x")
	r.secretID = "sec_nobody"
	pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcAuthFailed)
	r.secretID = pgsSecretID

	// A database that does not exist.
	rd, src := r.reader(strings.Replace(r.sourceDoc("", pgsOneDS()), r.dbName, "no_such_db_for_tests", 1))
	pgsSrcErr(t, pgsRead(rd, src.Datasets[0], ingest.PageOptions{}).err, ingest.SrcRequestRejected)

	// A table that does not exist: the words the person sees are in Detail, the message names nothing.
	res = r.read(pgsDS("ds", pgsTable("no_such_table"), "k", "k"), ingest.PageOptions{})
	se := pgsSrcErr(t, res.err, ingest.SrcRequestRejected)
	if !strings.Contains(se.Detail, "no_such_table") {
		t.Fatalf("detail %q", se.Detail)
	}
	if strings.Contains(se.Message, "no_such_table") || strings.Contains(se.Error(), "no_such_table") {
		t.Fatalf("the message names the table: %q", se.Message)
	}
	for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
		if strings.Contains(fmt.Sprintf(f, se), "no_such_table") {
			t.Fatalf("%s writes the detail", f)
		}
	}

	// A syntax error and a column that does not exist, both with the server's sentence in Detail.
	for _, stmt := range []string{`SELEKT k FROM t`, `SELECT k FROM t WHERE nope = 1`, `SELECT k FROM t ORDER BY`} {
		se = pgsSrcErr(t, r.read(pgsDS("ds", pgsSQL(stmt), "k", "k"), ingest.PageOptions{}).err, ingest.SrcRequestRejected)
		if se.Detail == "" {
			t.Fatalf("%q has no detail", stmt)
		}
	}

	// A secret of the wrong kind, a secret that is not there, and an address the secret is not allowed for.
	if _, err := r.svc.Create(pgsCtx, secrets.CreateInput{ID: "sec_tok", Kind: secrets.KindToken,
		Fields: secrets.NewFields(map[string]string{"token": "t"}), Scope: []string{pgsSourceID}, AllowedHosts: []secrets.Address{{Host: r.host, Port: r.port}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"sec_tok", "sec_missing"} {
		r.secretID = id
		pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcSecretUnusable)
	}
	r.secretID = pgsSecretID
	r.docPort = r.port + 1
	pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcSecretUnusable)

	// A port nobody listens on.
	r.addSecretAt("sec_closed", r.user, r.pass, 1)
	r.secretID, r.docPort = "sec_closed", 1
	pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcConnectionLost)
}

// [REQ-487] [REQ-670]
func TestPgsAskedForAnotherSourceIsAnInternalFailure(t *testing.T) {
	r := pgsNewRig(t)
	rd, src := r.reader(r.sourceDoc("", pgsOneDS()))
	for _, err := range rd.ReadPages(pgsCtx, "src_other", src.Datasets[0], ingest.PageOptions{}) {
		pgsSrcErr(t, err, ingest.SrcInternal)
		return
	}
	t.Fatal("no error")
}

// [REQ-490] [REQ-670]
func TestPgsStatementOfAnotherKindOfTextIsRefused(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	for _, stmt := range []string{
		`SELECT k FROM t; DROP TABLE t`,
		`SELECT k FROM t) AS q; DROP TABLE t; SELECT k FROM (SELECT k FROM t`,
		`INSERT INTO t VALUES (9) RETURNING k`,
		`WITH i AS (INSERT INTO t VALUES (9) RETURNING k) SELECT k FROM i`,
		`DELETE FROM t RETURNING k`,
		`SELECT set_config('transaction_read_only','off',true), k FROM t`,
		`CREATE TABLE z AS SELECT 1 AS k`,
		`COPY t TO STDOUT`,
	} {
		res := r.read(pgsDS("ds", pgsSQL(stmt), "k", "k"), ingest.PageOptions{})
		if res.err == nil && len(res.rows()) > 0 && !strings.Contains(stmt, "set_config") {
			t.Fatalf("%q gave rows", stmt)
		}
		if res.err != nil {
			pgsSrcErr(t, res.err, ingest.SrcRequestRejected)
		}
	}
	var n int
	if err := r.db.QueryRowContext(pgsCtx, `SELECT count(*) FROM t`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the source's table changed: %d %v", n, err)
	}
	var z *string
	if err := r.db.QueryRowContext(pgsCtx, `SELECT to_regclass('z')::text`).Scan(&z); err != nil || z != nil {
		t.Fatalf("a table was created")
	}
}

// [REQ-490] [REQ-670]
func TestPgsStatementTimeoutEndsTheQuery(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t VALUES (1)`)
	rd, src := r.one(pgsDS("ds", pgsSQL(`SELECT k, pg_sleep(5) FROM t`), "k", "k"), `"statement_timeout_seconds":1`)
	res := pgsRead(rd, src, ingest.PageOptions{})
	pgsSrcErr(t, res.err, ingest.SrcTimeout)
}

// [REQ-490] [REQ-670]
func TestPgsLockTimeoutSaysTheSourceIsBusy(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t VALUES (1)`)
	// Another session holds the table with an exclusive lock: the read waits for the lock time, then says the source is busy.
	tx, err := r.db.BeginTx(pgsCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(pgsCtx, `LOCK TABLE t IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	rd, ds := r.one(pgsOneDS(), `"lock_timeout_seconds":1`)
	pgsSrcErr(t, pgsRead(rd, ds, ingest.PageOptions{}).err, ingest.SrcSourceBusy)
}

// [REQ-490] [REQ-670]
func TestPgsCancelStopsTheQueryOfTheSource(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t VALUES (1)`)
	rd, ds := r.one(pgsDS("ds", pgsSQL(`SELECT k, pg_sleep(30) FROM t`), "k", "k"), "")
	ctx, cancel := context.WithCancel(pgsCtx)
	time.AfterFunc(700*time.Millisecond, cancel)
	var got error
	for _, err := range rd.ReadPages(ctx, pgsSourceID, ds, ingest.PageOptions{}) {
		got = err
		break
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("want the context's own error, got %v", got)
	}
	// The query is gone on the server too.
	for attempt := 0; ; attempt++ {
		var n int
		err := r.admin.QueryRowContext(pgsCtx, `SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND application_name = $2 AND state <> 'idle'`, r.dbName, applicationName).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
		if attempt >= 100 {
			t.Fatalf("%d queries of the reader still run on the source", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// [REQ-490] [REQ-670]
func TestPgsReadsDoNotLeaveConnectionsOrChangeTheSourcesSettings(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t SELECT generate_series(1,50)`)
	rd, ds := r.one(pgsOneDS(), "")
	if res := pgsRead(rd, ds, ingest.PageOptions{MaxRows: 5}); res.err != nil || len(res.rows()) != 50 {
		t.Fatalf("%v", res.err)
	}
	var n int
	if err := r.admin.QueryRowContext(pgsCtx, `SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND application_name = $2`, r.dbName, applicationName).Scan(&n); err != nil || n != 0 {
		t.Fatalf("connections left: %d %v", n, err)
	}
	// The role's and the database's settings are as they were.
	var cfg int
	if err := r.admin.QueryRowContext(pgsCtx, `SELECT count(*) FROM pg_db_role_setting`).Scan(&cfg); err != nil || cfg != 0 {
		t.Fatalf("settings were changed: %d %v", cfg, err)
	}
}

// [REQ-487] [REQ-661] [REQ-670]
func TestPgsConnectsOnlyThroughTheEgressDialer(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	d, err := egress.New(egress.Config{Policy: egress.Policy{DenyLoopback: true}})
	if err != nil {
		t.Fatal(err)
	}
	r.dialer = d
	se := pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcPolicyDenied)
	if strings.Contains(se.Message, r.host) {
		t.Fatalf("the message has the host: %q", se.Message)
	}
	// A policy that lets only another port through.
	d, err = egress.New(egress.Config{Policy: egress.Policy{Ports: []uint16{uint16(r.port + 1)}}})
	if err != nil {
		t.Fatal(err)
	}
	r.dialer = d
	pgsSrcErr(t, r.read(pgsOneDS(), ingest.PageOptions{}).err, ingest.SrcPolicyDenied)
	// The check says which step it was.
	rd, ds := r.one(pgsOneDS(), "")
	res := rd.Check(pgsCtx, pgsSourceID, ds)
	if res.Kind != ingest.CheckConfig {
		t.Fatalf("%+v", res)
	}
}

// [REQ-487] [REQ-670]
func TestPgsNewRefusesWhatItCannotRead(t *testing.T) {
	r := pgsNewRig(t)
	src := r.parse(r.sourceDoc("", pgsOneDS()))
	ok := Config{Source: src, Secrets: r.svc.Opener(), Dialer: r.dialer}
	if _, err := New(ok); err != nil {
		t.Fatal(err)
	}
	noSecrets, noDialer, other := ok, ok, ok
	noSecrets.Secrets = nil
	noDialer.Dialer = nil
	other.Source.Adapter = ingest.AdapterFile
	noConn := ok
	noConn.Source.Connection = nil
	for _, c := range []Config{noSecrets, noDialer, other, noConn} {
		if _, err := New(c); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Fatalf("%v", err)
		}
	}
}
