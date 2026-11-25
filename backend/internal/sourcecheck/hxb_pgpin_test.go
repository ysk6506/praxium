package sourcecheck

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgsource"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The tests of the pins of a PostgreSQL source on a real server behind a terminator of TLS (ADR-0105). Every helper here
// begins with hxb so that it never meets a name of the older tests of this folder.

// hxbPinOf is the pin of the certificate in a PEM text.
func hxbPinOf(t *testing.T, pemText string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		t.Fatal("no certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return egress.PinOf(cert)
}

// hxbReader makes the reader of a source whose connection has the given extra JSON (tls, ca_pem, sslmode).
func hxbReader(t *testing.T, host string, port int, user, pass, extra string) (*pgsource.Reader, ingest.Dataset) {
	t.Helper()
	return hxbReaderSQL(t, host, port, user, pass, extra, scThreeRows)
}

// hxbReaderSQL is hxbReader for a dataset with the statement it is given.
func hxbReaderSQL(t *testing.T, host string, port int, user, pass, extra, statement string) (*pgsource.Reader, ingest.Dataset) {
	t.Helper()
	svc, err := secrets.New(secrets.Config{
		Store: secrets.NewMemoryStore(), Keys: secrets.StaticKey(secrets.GenerateMasterKey(kernel.NewSeededBytes(7))),
		Clock: kernel.NewManualClock(scEpoch), Bytes: kernel.NewSeededBytes(8),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(scCtx, secrets.CreateInput{
		ID: "sec_sc", Kind: secrets.KindPassword, Fields: secrets.NewFields(map[string]string{"username": user, "password": pass}),
		Scope: []string{"src_sc"}, AllowedHosts: []secrets.Address{{Host: host, Port: port}},
	}); err != nil {
		t.Fatal(err)
	}
	d, err := egress.New(egress.Config{Resolver: &scLoopback{}})
	if err != nil {
		t.Fatal(err)
	}
	doc := fmt.Sprintf(`{"version":1,"id":"src_sc","adapter":"postgres","connection":{"host":%q,"port":%d,"database":"postgres","credential":{"secret_id":"sec_sc"}%s},"datasets":[{"id":"d","sql":%q,"natural_key":["id"],"columns":[{"name":"id"}]}]}`,
		host, port, extra, statement)
	src, err := ingest.ParseAdapterSource(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("the test definition does not parse: %v", err)
	}
	rd, err := pgsource.New(pgsource.Config{Source: src, Secrets: svc.Opener(), Dialer: d})
	if err != nil {
		t.Fatal(err)
	}
	return rd, src.Datasets[0]
}

func hxbPins(pins ...string) string {
	quoted := make([]string, len(pins))
	for i, p := range pins {
		quoted[i] = fmt.Sprintf("%q", p)
	}
	return `,"tls":{"pins":[` + strings.Join(quoted, ",") + `]}`
}

// [REQ-826] A PostgreSQL server whose certificate is signed by no one the definition trusts and is for another name is read
// when the definition pins its key: the rows come over TLS.
func TestHxbAPinnedPostgresServerIsReadWhateverItsChainAndName(t *testing.T) {
	host, port, user, pass := scTestServer(t)
	cert, certPEM := scCert(t, 1, []string{"other.test"}, false)
	_, otherPEM := scCert(t, 2, []string{"other.test"}, false)
	proxy := scTLSProxy(t, cert, host, port)
	rd, ds := hxbReader(t, "scfake.test", proxy, user, pass, hxbPins(hxbPinOf(t, otherPEM), hxbPinOf(t, certPEM)))
	res := scRun(scCtx, rd, ds, ingest.PageOptions{})
	if res.err != nil || res.rows() != 3 {
		t.Fatalf("rows %d, error %v", res.rows(), res.err)
	}
	// the same server without the pin is refused: the pin is what lets it in
	bare, ds2 := hxbReader(t, "scfake.test", proxy, user, pass, "")
	scSrcErr(t, scRun(scCtx, bare, ds2, ingest.PageOptions{}).err, ingest.SrcTLSRejected)
}

// [REQ-826] A key that is not one of the pins is refused with the kind tls_rejected, though the certificate is for the name and
// signed by the authority the definition would name: the pins replace the verification and are not added to it. The
// check shows what the server presented, and the pin that would let it in.
func TestHxbAnotherKeyThanThePinsIsRefusedAndTheCheckShowsIt(t *testing.T) {
	host, port, user, pass := scTestServer(t)
	cert, certPEM := scCert(t, 1, []string{"scfake.test"}, true)
	_, otherPEM := scCert(t, 2, []string{"scfake.test"}, true)
	proxy := scTLSProxy(t, cert, host, port)
	rd, ds := hxbReader(t, "scfake.test", proxy, user, pass, hxbPins(hxbPinOf(t, otherPEM)))
	failed := scRun(scCtx, rd, ds, ingest.PageOptions{}).err
	scSrcErr(t, failed, ingest.SrcTLSRejected)
	if se, _ := ingest.SourceErrorOf(failed); se == nil || !strings.Contains(se.Message, "pinned in the definition") || strings.Contains(se.Message, "expired") {
		t.Errorf("the words of the failure: %v", failed)
	}
	res := rd.Check(scCtx, "src_sc", ds)
	if res.Kind != ingest.CheckConfig || res.Cert == nil || res.Cert.SPKISHA256 != hxbPinOf(t, certPEM) || res.Cert.SPKISHA256 == hxbPinOf(t, otherPEM) {
		t.Fatalf("check %+v, certificate %+v", res, res.Cert)
	}
	if got := res.Steps[len(res.Steps)-1]; got.Name != "tls" || got.Kind != ingest.CheckConfig {
		t.Errorf("the step that failed: %+v", got)
	}
	if len(res.Notes) != 1 || res.Notes[0].Code != ingest.NotePinExpiry {
		t.Errorf("notes %+v", res.Notes)
	}
	// a certificate that is not trusted, with no pins, is shown the same way
	plain, ds2 := hxbReader(t, "scfake.test", proxy, user, pass, "")
	got := plain.Check(scCtx, "src_sc", ds2)
	if got.Kind != ingest.CheckConfig || got.Cert == nil || got.Cert.SPKISHA256 != hxbPinOf(t, certPEM) || len(got.Notes) != 0 {
		t.Errorf("without pins: %+v %+v", got, got.Cert)
	}
}

// [REQ-826] A check that works carries no certificate, and says when the connection has no TLS.
func TestHxbACheckThatWorksShowsNoCertificateAndNotesThePlaintext(t *testing.T) {
	host, port, user, pass := scTestServer(t)
	cert, certPEM := scCert(t, 1, []string{"other.test"}, false)
	proxy := scTLSProxy(t, cert, host, port)
	rd, ds := hxbReader(t, "scfake.test", proxy, user, pass, hxbPins(hxbPinOf(t, certPEM)))
	res := rd.Check(scCtx, "src_sc", ds)
	if res.Kind != ingest.CheckOK || res.Cert != nil || len(res.Notes) != 1 || res.Notes[0].Code != ingest.NotePinExpiry {
		t.Errorf("%+v", res)
	}
	f := scNewFake(t)
	src := scFakeSource(f, "fewcols")
	fake, ds3 := hxbReader(t, src.host, src.port, "u", "p", `,"sslmode":"disable"`)
	got := fake.Check(scCtx, "src_sc", ds3)
	found := false
	for _, n := range got.Notes {
		found = found || n.Code == ingest.NotePlaintext
	}
	if !found || got.Cert != nil {
		t.Errorf("notes %+v, certificate %+v", got.Notes, got.Cert)
	}
}

// hxbActive is how many statements that hold the marker are running on the server, as the server says it (another connection,
// in the clear, to the same server).
func hxbActive(t *testing.T, host string, port int, user, pass, marker string) int {
	t.Helper()
	statement := "SELECT count(*) AS id FROM pg_catalog.pg_stat_activity WHERE state = 'active' AND pid <> pg_catalog.pg_backend_pid() AND query LIKE '%" + marker + "%'"
	rd, ds := hxbReaderSQL(t, host, port, user, pass, `,"sslmode":"disable"`, statement)
	res := scRun(scCtx, rd, ds, ingest.PageOptions{})
	if res.err != nil || len(res.pages) == 0 {
		t.Fatalf("the look at the server: %v", res.err)
	}
	var n int
	if _, err := fmt.Sscan(res.pages[0].Rows[0].Fields["id"], &n); err != nil {
		t.Fatalf("the count %v: %v", res.pages[0].Rows[0], err)
	}
	return n
}

// [REQ-826] The request that cancels a statement is a connection of its own, and it goes to the pinned server the way the
// first one did: with TLS, checked against the pins (the driver is given the same registered settings for it). The server is
// behind a terminator of TLS that serves nothing that does not begin with the request for TLS, and a statement that sleeps is
// stopped by the cancel request only (when the client closes the connection the server does not notice until the statement
// ends), so that the statement ending early is the server's own word that the cancel request got there over TLS.
func TestHxbTheCancelRequestOfAReadGoesToThePinnedServerOverTLS(t *testing.T) {
	host, port, user, pass := scTestServer(t)
	cert, certPEM := scCert(t, 1, []string{"other.test"}, false)
	proxy := scTLSProxy(t, cert, host, port)
	const marker = "hxbcancelmarker"
	rd, ds := hxbReaderSQL(t, "scfake.test", proxy, user, pass, hxbPins(hxbPinOf(t, certPEM)),
		"SELECT g AS id FROM generate_series(1, 3) g, pg_catalog.pg_sleep(60) /* "+marker+" */")
	ctx, cancel := context.WithCancel(scCtx)
	defer cancel()
	done := make(chan scResult, 1)
	go func() { done <- scRun(ctx, rd, ds, ingest.PageOptions{}) }()
	running := false
	for i := 0; i < 1000 && !running; i++ {
		running = hxbActive(t, host, port, user, pass, marker) > 0
		if !running {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !running {
		t.Fatal("the statement did not start")
	}
	cancel()
	if res := <-done; res.err == nil {
		t.Fatal("the read of a statement that sleeps a minute ended without an error")
	}
	stopped := false
	for i := 0; i < 250 && !stopped; i++ {
		stopped = hxbActive(t, host, port, user, pass, marker) == 0
		if !stopped {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !stopped {
		t.Error("the statement still runs on the server: the cancel request did not get there over TLS")
	}
}
