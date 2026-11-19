package pgsource

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-490] [REQ-655] [REQ-670]
func TestPgsSourceThatChangesBetweenPagesDoesNotBreakTheRead(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key, v text)`)
	r.exec(`INSERT INTO t SELECT i*10, 'v' FROM generate_series(1,20) i`)
	rd, ds := r.one(pgsDS("ds", pgsTable("t"), "k", "k", "v"), "")
	var keys []string
	n := 0
	for pg, err := range rd.ReadPages(pgsCtx, pgsSourceID, ds, ingest.PageOptions{MaxRows: 5}) {
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range pg.Rows {
			keys = append(keys, row.Key)
		}
		if n == 0 {
			// Between two pages no transaction is open on the source.
			var open int
			if err := r.admin.QueryRowContext(pgsCtx, `SELECT count(*) FROM pg_stat_activity WHERE datname=$1 AND application_name=$2 AND state LIKE 'idle in transaction%'`, r.dbName, applicationName).Scan(&open); err != nil || open != 0 {
				t.Fatalf("a transaction stays open between pages: %d %v", open, err)
			}
			// The source changes: a row before the position, a row after it, a row that goes, and a row that is changed.
			// The reader is a batch ahead of the page it gave, so the changes are made far ahead of it.
			r.exec(`INSERT INTO t VALUES (5,'early'), (155,'late')`)
			r.exec(`DELETE FROM t WHERE k = 170`)
			r.exec(`UPDATE t SET v = 'changed' WHERE k = 180`)
		}
		n++
	}
	if len(keys) == 0 {
		t.Fatal("no rows")
	}
	seen := map[string]bool{}
	prev := -1
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("key %s twice", k)
		}
		seen[k] = true
		var v int
		_, _ = fmt.Sscan(k, &v)
		if v <= prev {
			t.Fatalf("keys are not in order: %v", keys)
		}
		prev = v
	}
	if seen["5"] || seen["170"] || !seen["155"] || !seen["180"] {
		t.Fatalf("the read of a changing source is not what each page saw: %v", keys)
	}
}

// [REQ-490] [REQ-655] [REQ-670]
func TestPgsPageLimitsOfTheCallAreKept(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key, v text)`)
	r.exec(`INSERT INTO t SELECT i, repeat('x', 100) FROM generate_series(1,40) i`)
	ds := pgsDS("ds", pgsTable("t"), "k", "k", "v")
	res := r.read(ds, ingest.PageOptions{MaxRows: 100, MaxBytes: 1000})
	if res.err != nil || len(res.rows()) != 40 || len(res.pages) < 4 {
		t.Fatalf("%v rows %d pages %d", res.err, len(res.rows()), len(res.pages))
	}
	for _, pg := range res.pages {
		if pg.Bytes > 1000+120 {
			t.Fatalf("a page of %d bytes", pg.Bytes)
		}
	}
	res = r.read(ds, ingest.PageOptions{MaxRows: 10, MaxDatasetRows: 15})
	last := res.pages[len(res.pages)-1]
	if res.err != nil || last.End.Complete || len(res.rows()) > 15 || !last.Last {
		t.Fatalf("%v %d %+v", res.err, len(res.rows()), last.End)
	}
}

// [REQ-490] [REQ-670]
func TestPgsAKeyWithMoreRowsThanTheDatasetMayHaveIsIncomplete(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int, n int)`)
	r.exec(`INSERT INTO t SELECT 1, i FROM generate_series(1,50) i`)
	res := r.read(pgsDS("ds", pgsTable("t"), "k", "k", "n"), ingest.PageOptions{MaxRows: 5, MaxDatasetRows: 20})
	if res.err != nil {
		t.Fatal(res.err)
	}
	last := res.pages[len(res.pages)-1]
	if last.End.Complete || !last.Last {
		t.Fatalf("%+v", last.End)
	}
}

// [REQ-487] [REQ-659] [REQ-660] [REQ-670]
func TestPgsSecretsAppearNowhere(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key, password text, label text)`)
	r.exec(`INSERT INTO t VALUES (1, 'row-secret-value', 'visible')`)
	secretsSeen := []string{r.pass, r.user}

	var texts []string
	add := func(vs ...any) {
		for _, v := range vs {
			for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
				texts = append(texts, fmt.Sprintf(f, v))
			}
		}
	}
	// A failed sign-in, a refused statement and a failed connection.
	r.addSecret("sec_wrong", r.user, "wrong-password-for-tests")
	secretsSeen = append(secretsSeen, "wrong-password-for-tests")
	r.secretID = "sec_wrong"
	rd, ds := r.one(pgsOneDS(), "")
	for _, err := range rd.ReadPages(pgsCtx, pgsSourceID, ds, ingest.PageOptions{}) {
		add(err)
		if se, ok := ingest.SourceErrorOf(err); ok {
			texts = append(texts, se.Message)
		}
	}
	res := rd.Check(pgsCtx, pgsSourceID, ds)
	add(res, res.Steps)
	r.secretID = pgsSecretID
	rd, ds = r.one(pgsDS("ds", pgsSQL(`SELECT k, 'x' || nope FROM t`), "k", "k"), "")
	for _, err := range rd.ReadPages(pgsCtx, pgsSourceID, ds, ingest.PageOptions{}) {
		add(err)
		if se, ok := ingest.SourceErrorOf(err); ok {
			texts = append(texts, se.Message)
		}
	}
	// A good read: events and the preview.
	rd, ds = r.one(pgsDS("ds", pgsTable("t"), "k", "k", "password", "label"), "")
	good := pgsRead(rd, ds, ingest.PageOptions{})
	if good.err != nil {
		t.Fatal(good.err)
	}
	pv, err := rd.Preview(pgsCtx, pgsSourceID, ds, ingest.PreviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	add(pv)
	ok := rd.Check(pgsCtx, pgsSourceID, ds)
	add(ok)
	for _, e := range r.eventsSeen() {
		add(e)
	}
	joined := strings.Join(texts, "\n")
	for _, s := range secretsSeen {
		if strings.Contains(joined, s) {
			t.Fatalf("a secret value is in the output of the adapter (%d chars)", len(s))
		}
	}
	// The preview hides the value of a column that is named like a secret.
	if !pv.Masked || strings.Contains(fmt.Sprint(pv), "row-secret-value") || !strings.Contains(fmt.Sprint(pv), "visible") {
		t.Fatalf("preview not masked: %+v", pv)
	}
}

// [REQ-487] [REQ-670]
func TestPgsDoesNotReadTheEnvironmentOrTheHomeFolder(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t VALUES (1)`)
	home := t.TempDir() // the home folder of a hostile environment; the files in it are tried by the prototype of ADR-0093 (the test cannot write files)
	t.Setenv("HOME", home)
	t.Setenv("PGPASSFILE", filepath.Join(home, ".pgpass"))
	t.Setenv("PGSERVICEFILE", filepath.Join(home, "service/pg_service.conf"))
	t.Setenv("PGSERVICE", "x")
	t.Setenv("PGHOST", "203.0.113.9")
	t.Setenv("PGPORT", "1")
	t.Setenv("PGUSER", "hostile")
	t.Setenv("PGPASSWORD", "hostile-password")
	t.Setenv("PGDATABASE", "hostile")
	t.Setenv("PGSSLMODE", "verify-full")
	t.Setenv("PGSSLROOTCERT", filepath.Join(home, ".postgresql/root.crt"))
	t.Setenv("PGSSLCERT", filepath.Join(home, ".postgresql/postgresql.crt"))
	t.Setenv("PGSSLKEY", filepath.Join(home, ".postgresql/postgresql.key"))
	t.Setenv("PGOPTIONS", "-c default_transaction_read_only=off")
	t.Setenv("PGAPPNAME", "hostile-app")
	t.Setenv("PGTZ", "Pacific/Auckland")
	t.Setenv("PGCONNECT_TIMEOUT", "1")
	res := r.read(pgsOneDS(), ingest.PageOptions{})
	if res.err != nil || len(res.rows()) != 1 {
		t.Fatalf("a read with a hostile environment: %v", res.err)
	}
	// A client certificate of the home folder is not sent either: with sslmode disable there is no TLS at all.
	if got := rdCheck(r); got.Kind != ingest.CheckOK {
		t.Fatalf("%+v", got)
	}
}

func rdCheck(r *pgsRig) ingest.CheckResult {
	rd, ds := r.one(pgsOneDS(), "")
	return rd.Check(pgsCtx, pgsSourceID, ds)
}

// [REQ-487] [REQ-661] [REQ-670]
func TestPgsTLSModeAgainstAServerWithoutTLSRefusesAndDoesNotFallBack(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t VALUES (1)`)
	r.sslmode = "verify-full"
	res := r.read(pgsOneDS(), ingest.PageOptions{})
	pgsSrcErr(t, res.err, ingest.SrcTLSRejected)
	if len(res.rows()) != 0 {
		t.Fatal("rows came over a connection without TLS")
	}
	// The same with a CA of the definition: the server still has no TLS, so there is still no fallback.
	for _, extra := range []string{`"ca_pem":` + strconv.Quote(pgsCert)} {
		rd, ds := r.one(pgsOneDS(), extra)
		res = pgsRead(rd, ds, ingest.PageOptions{})
		pgsSrcErr(t, res.err, ingest.SrcTLSRejected)
		if len(res.rows()) != 0 {
			t.Fatal("rows came over a connection without TLS")
		}
	}
	// The check says it is the TLS step.
	rd, ds := r.one(pgsOneDS(), "")
	chk := rd.Check(pgsCtx, pgsSourceID, ds)
	last := chk.Steps[len(chk.Steps)-1]
	if chk.Kind != ingest.CheckConfig || last.Name != stepTLS || last.Kind != ingest.CheckConfig {
		t.Fatalf("%+v", chk)
	}
	// The definition has no way to turn the verification off.
	for _, mode := range []string{"require", "prefer", "allow", "verify-ca"} {
		_, err := ingest.ParseAdapterSource(strings.NewReader(r.sourceDoc("", pgsOneDS())[0:0] + strings.Replace(r.sourceDoc("", pgsOneDS()), `"sslmode":"verify-full"`, `"sslmode":"`+mode+`"`, 1)))
		if err == nil {
			t.Fatalf("sslmode %s is accepted by the definition", mode)
		}
	}
}

// [REQ-490] [REQ-670]
func TestPgsCheckTellsItsSteps(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t SELECT generate_series(1,3)`)
	rd, ds := r.one(pgsOneDS(), "")
	res := rd.Check(pgsCtx, pgsSourceID, ds)
	if res.Kind != ingest.CheckOK || res.Rows != 3 {
		t.Fatalf("%+v", res)
	}
	var names []string
	for _, s := range res.Steps {
		if s.Kind != ingest.CheckOK {
			t.Fatalf("%+v", s)
		}
		names = append(names, s.Name)
	}
	want := []string{stepSecret, stepConnection, stepTLS, stepSignIn, stepQuery, stepKeys, stepFirstPage}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", names)
	}
	// Each failure ends at the step it belongs to.
	cases := []struct {
		name, ds, step string
		secret         string
		kind           ingest.CheckKind
	}{
		{"a missing table", pgsDS("ds", pgsTable("nope"), "k", "k"), stepQuery, pgsSecretID, ingest.CheckConfig},
		{"a key that is no column", pgsDS("ds", pgsSQL(`SELECT 1 AS other`), "k", "k"), stepKeys, pgsSecretID, ingest.CheckConfig},
		{"a key that is a float", pgsDS("ds", pgsSQL(`SELECT 1.5::float8 AS k`), "k", "k"), stepKeys, pgsSecretID, ingest.CheckConfig},
		{"a wrong password", pgsOneDS(), stepSignIn, "sec_wrong", ingest.CheckConfig},
		{"a secret that is not there", pgsOneDS(), stepSecret, "sec_none", ingest.CheckConfig},
	}
	r.addSecret("sec_wrong", r.user, "wrong-password-for-tests")
	for _, c := range cases {
		r.secretID = c.secret
		rd, d := r.one(c.ds, "")
		got := rd.Check(pgsCtx, pgsSourceID, d)
		if got.Kind != c.kind || len(got.Steps) == 0 || got.Steps[len(got.Steps)-1].Name != c.step || got.Steps[len(got.Steps)-1].Kind != c.kind {
			t.Fatalf("%s: %+v", c.name, got)
		}
	}
	// A dataset that has no first page it can finish is told as passing trouble.
	r.secretID = pgsSecretID
	r.exec(`CREATE TABLE big (k int, n int)`)
	r.exec(`INSERT INTO big SELECT 1, i FROM generate_series(1,30) i`)
	rd, d := r.one(pgsDS("ds", pgsTable("big"), "k", "k", "n"), "")
	if got := rd.Check(pgsCtx, pgsSourceID, d); got.Kind == ingest.CheckProduct {
		t.Fatalf("%+v", got)
	}
}

// [REQ-660] [REQ-670]
func TestPgsObserverIsToldWhatHappensAndNothingMore(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key)`)
	r.exec(`INSERT INTO t SELECT generate_series(1,12)`)
	rd, ds := r.one(pgsOneDS(), "")
	if res := pgsRead(rd, ds, ingest.PageOptions{MaxRows: 5}); res.err != nil || len(res.pages) != 3 {
		t.Fatalf("%v", res.err)
	}
	pages, rows := 0, 0
	for _, e := range r.eventsSeen() {
		if e.Source != pgsSourceID || e.Dataset != "ds" || e.Host != r.host || e.Port != r.port {
			t.Fatalf("%+v", e)
		}
		if e.Kind == ingest.EventPage {
			pages++
			rows += e.Rows
		}
	}
	if pages != 3 || rows != 12 {
		t.Fatalf("pages %d rows %d", pages, rows)
	}
	// A failure is told with its class and its kind.
	r.secretID = "sec_none"
	rd, ds = r.one(pgsOneDS(), "")
	_ = pgsRead(rd, ds, ingest.PageOptions{})
	evs := r.eventsSeen()
	last := evs[len(evs)-1]
	if last.Kind != ingest.EventFailed || last.ErrorKind != ingest.SrcSecretUnusable || last.Class != ingest.CheckConfig {
		t.Fatalf("%+v", last)
	}
}

// [REQ-489] [REQ-670]
func TestPgsPreviewGivesMaskedFirstRows(t *testing.T) {
	r := pgsNewRig(t)
	r.exec(`CREATE TABLE t (k int primary key, api_token text, name text)`)
	r.exec(`INSERT INTO t SELECT i, 'tok-'||i, 'n'||i FROM generate_series(1,30) i`)
	rd, ds := r.one(pgsDS("ds", pgsTable("t"), "k", "k", "api_token", "name"), "")
	pv, err := rd.Preview(pgsCtx, pgsSourceID, ds, ingest.PreviewOptions{MaxPages: 1, MaxRows: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Pages) != 1 || len(pv.Pages[0].Rows) != 5 || !pv.Masked {
		t.Fatalf("%+v", pv)
	}
	for _, row := range pv.Pages[0].Rows {
		if row.Fields["api_token"] != ingest.MaskedValue || !strings.HasPrefix(row.Fields["name"], "n") {
			t.Fatalf("%+v", row.Fields)
		}
	}
	// A failing preview is a SourceError.
	r.secretID = "sec_none"
	rd, ds = r.one(pgsOneDS(), "")
	_, err = rd.Preview(pgsCtx, pgsSourceID, ds, ingest.PreviewOptions{})
	pgsSrcErr(t, err, ingest.SrcSecretUnusable)
}

// pgsCert is a self-signed certificate made up for the tests; it signs nothing the tests connect to.
const pgsCert = "-----BEGIN CERTIFICATE-----\nMIIBhTCCASugAwIBAgIURtGaGZSb7VAVHQQp7STkhcncoR8wCgYIKoZIzj0EAwIw\nFzEVMBMGA1UEAwwMZXhhbXBsZS50ZXN0MCAXDTI2MTExOTEwMjEyN1oYDzIxMjYx\nMDI2MTAyMTI3WjAXMRUwEwYDVQQDDAxleGFtcGxlLnRlc3QwWTATBgcqhkjOPQIB\nBggqhkjOPQMBBwNCAASCdORDNqElggutE2cVqCCOTZ44AGGGM7ACcNJtJstRWgLZ\njCOmhbFMo01h2e0j2AL6MazLrO2T6jji4af51UPho1MwUTAdBgNVHQ4EFgQUVXuy\nfSxGvyMsczWPNhux+3ufAVAwHwYDVR0jBBgwFoAUVXuyfSxGvyMsczWPNhux+3uf\nAVAwDwYDVR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNIADBFAiEAo/VQIH+m9EXV\nQiZCDGBFQCvCy5hLs3tw573C7HWM7aQCIH6ZkOZqpPiLzlSDmQtrce0tjz0n/oWp\nLxeolirs3Vs/\n-----END CERTIFICATE-----"
