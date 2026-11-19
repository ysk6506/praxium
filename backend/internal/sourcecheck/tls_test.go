package sourcecheck

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

const scThreeRows = "SELECT g AS id FROM generate_series(1, 3) g"

// scViaProxy reads three rows from the real test server through a TLS proxy whose certificate is cert, trusting trust.
func scViaProxy(t *testing.T, seed, trustSeed byte, dns []string) scResult {
	t.Helper()
	host, port, user, pass := scTestServer(t)
	cert, _ := scCert(t, seed, dns, true)
	_, trust := scCert(t, trustSeed, dns, true)
	proxy := scTLSProxy(t, cert, host, port)
	src := scSource{host: "scfake.test", port: proxy, database: "postgres", user: user, pass: pass, caPEM: trust}
	rd, ds := scReader(t, src, scThreeRows, &scLoopback{})
	return scRun(scCtx, rd, ds, ingest.PageOptions{})
}

// [REQ-675] A source whose certificate is signed by the CA the definition names, for the name it is dialed by, is read
// over TLS: the rows come.
func TestScTLSWithTheRightCAReadsRows(t *testing.T) {
	res := scViaProxy(t, 1, 1, []string{"scfake.test"})
	if res.err != nil || res.rows() != 3 {
		t.Fatalf("rows %d, error %v", res.rows(), res.err)
	}
}

// [REQ-675] A certificate of another CA than the one the definition names is refused with the kind tls_rejected.
func TestScTLSWithAnotherCAIsRefused(t *testing.T) {
	res := scViaProxy(t, 1, 2, []string{"scfake.test"})
	scSrcErr(t, res.err, ingest.SrcTLSRejected)
}

// [REQ-675] A certificate for another name than the one the source is dialed by is refused with the kind tls_rejected,
// though its CA is the one the definition names.
func TestScTLSForAnotherNameIsRefused(t *testing.T) {
	host, port, user, pass := scTestServer(t)
	cert, trust := scCert(t, 1, []string{"other.test"}, false)
	proxy := scTLSProxy(t, cert, host, port)
	rd, ds := scReader(t, scSource{host: "scfake.test", port: proxy, database: "postgres", user: user, pass: pass, caPEM: trust}, scThreeRows, &scLoopback{})
	scSrcErr(t, scRun(scCtx, rd, ds, ingest.PageOptions{}).err, ingest.SrcTLSRejected)
}

// [REQ-675] A source that does not offer TLS is not read in plaintext when the definition asks for TLS: the read ends with
// the kind tls_rejected, and the client sends nothing after the server said no.
func TestScServerWithoutTLSGetsNoPlaintextFallback(t *testing.T) {
	f := scNewFake(t)
	src := scFakeSource(f, "fewcols")
	src.sslmode = ""
	rd, ds := scReader(t, src, "SELECT 1 AS id", &scLoopback{})
	scSrcErr(t, scRun(scCtx, rd, ds, ingest.PageOptions{}).err, ingest.SrcTLSRejected)
	if rest := <-f.after; len(rest) != 0 {
		t.Fatalf("the client sent %d bytes after the server refused TLS", len(rest))
	}
}
