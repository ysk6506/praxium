package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// hxbTLSListener is a server that does the handshake and then counts what the client sends after it (answer is what
// a PostgreSQL server says to the request for TLS: S to go on, anything else not to; ask turns that on).
type hxbTLSListener struct {
	port       string
	handshakes atomic.Int64
	after      atomic.Int64
	asked      atomic.Int64
}

func hxbListen(t *testing.T, cert tls.Certificate, postgres bool, answer byte) *hxbTLSListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	out := &hxbTLSListener{}
	_, out.port, _ = net.SplitHostPort(ln.Addr().String())
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				if postgres {
					var req [8]byte
					if _, err := io.ReadFull(c, req[:]); err != nil || binary.BigEndian.Uint32(req[4:]) != 80877103 {
						return
					}
					out.asked.Add(1)
					if _, err := c.Write([]byte{answer}); err != nil || answer != 'S' {
						return
					}
				}
				srv := tls.Server(c, cfg)
				if err := srv.Handshake(); err == nil {
					out.handshakes.Add(1)
				}
				n, _ := io.Copy(io.Discard, srv)
				out.after.Add(n)
			}()
		}
	}()
	return out
}

// [REQ-824] The probe shows what the server presents and sends nothing to it after the handshake began: the server of the
// test sees no byte of a request, and the client the probe is made from has no connection kept.
func TestHxbProbeShowsTheCertificateAndSendsNoRequest(t *testing.T) {
	cert, _ := ctIssue(t, "api.example.test", 5, hxbOld, hxbEnd)
	srv := hxbListen(t, cert, false, 0)
	c, res := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{})
	sum, err := c.ProbeCert(context.Background(), "https://api.example.test:"+srv.port+"/some/path?token=secret-detail")
	if err != nil {
		t.Fatal(err)
	}
	if sum.SPKISHA256 != hxbPin(t, cert) || sum.Subject != "CN=api.example.test" || sum.Issuer != "CN=api.example.test" ||
		len(sum.SANs) != 2 || sum.SANs[0] != "api.example.test" || sum.SANs[1] != "127.0.0.1" ||
		!sum.NotBefore.Equal(hxbOld) || !sum.NotAfter.Equal(hxbEnd) {
		t.Errorf("summary %+v", sum)
	}
	time.Sleep(100 * time.Millisecond)
	if srv.after.Load() != 0 || srv.handshakes.Load() != 0 || res.total() != 1 {
		t.Errorf("the server read %d bytes after the hello, finished %d handshakes; %d lookups", srv.after.Load(), srv.handshakes.Load(), res.total())
	}
}

// [REQ-824] The probe goes through the dialer, so every check of the policy holds for it; it takes an https address only.
func TestHxbProbeIsJudgedByThePolicyAndTakesHTTPSOnly(t *testing.T) {
	cert, _ := ctIssue(t, "api.example.test", 5, ctFrom, ctTo)
	srv := hxbListen(t, cert, false, 0)
	refused, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{DenyLoopback: true}, ClientOptions{})
	if _, err := refused.ProbeCert(context.Background(), "https://api.example.test:"+srv.port+"/"); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("a loopback that is denied: %v", err)
	}
	meta, _ := ctClient(t, map[string]string{"meta.example.test": "169.254.169.254"}, Policy{}, ClientOptions{})
	if _, err := meta.ProbeCert(context.Background(), "https://meta.example.test/"); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("a metadata address: %v", err)
	}
	open, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{})
	for _, bad := range []string{"http://api.example.test/", "ftp://api.example.test/", "https://u:p@api.example.test/", "https:///x", "", "https://api.example.test:99999/", "//api.example.test"} {
		if _, err := open.ProbeCert(context.Background(), bad); dtKind(t, err).Kind != KindPolicy {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// a server that does not speak TLS gives an error that holds no address
	plain, _ := hxbPlainServer(t)
	if _, err := open.ProbeCert(context.Background(), "https://api.example.test:"+plain+"/"); err == nil || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("a server without TLS: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := open.ProbeCert(ctx, "https://api.example.test:"+srv.port+"/"); err == nil {
		t.Error("a cancelled probe succeeded")
	}
}

func hxbPlainServer(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var n atomic.Int64
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n.Add(1)
			go func() {
				defer c.Close()
				_, _ = io.WriteString(c, "HTTP/1.1 400 Bad Request\r\n\r\n")
			}()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port, &n
}

// [REQ-824] [REQ-826] The probe of a PostgreSQL server begins as the protocol says, with the request for TLS (eight bytes),
// and goes on only when the server answers S; it sends nothing after the hello.
func TestHxbProbeOfAPostgresServerAsksForTLSFirst(t *testing.T) {
	cert, _ := ctIssue(t, "db.example.test", 6, ctFrom, ctTo)
	srv := hxbListen(t, cert, true, 'S')
	dialer, derr := New(Config{})
	if derr != nil {
		t.Fatal(derr)
	}
	port, _ := strconv.Atoi(srv.port)
	sum, err := dialer.ProbePostgresCert(context.Background(), "127.0.0.1", port)
	if err != nil {
		t.Fatal(err)
	}
	if sum.SPKISHA256 != hxbPin(t, cert) || srv.asked.Load() != 1 {
		t.Errorf("summary %+v, %d requests for TLS", sum, srv.asked.Load())
	}
	time.Sleep(100 * time.Millisecond)
	if srv.after.Load() != 0 {
		t.Errorf("%d bytes after the hello", srv.after.Load())
	}
	// a server that says no is not probed further
	no := hxbListen(t, cert, true, 'N')
	port, _ = strconv.Atoi(no.port)
	if _, err := dialer.ProbePostgresCert(context.Background(), "127.0.0.1", port); dtKind(t, err).Kind != KindTLS {
		t.Errorf("a server that does not offer TLS: %v", err)
	}
	if _, err := dialer.ProbePostgresCert(context.Background(), "127.0.0.1", 0); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("port 0: %v", err)
	}
	deny, err := New(Config{Policy: Policy{DenyLoopback: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deny.ProbePostgresCert(context.Background(), "127.0.0.1", port); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("a loopback that is denied: %v", err)
	}
}

// [REQ-824] What a server says in its certificate is cleaned before anyone is shown it: control characters, the marks that
// turn the direction of a text and the zero-width ones are taken out, a bad UTF-8 byte is replaced, a text is cut at 256 bytes,
// and no more than 20 names are given, with how many were left out.
func TestHxbTheSummaryOfACertificateIsCleaned(t *testing.T) {
	cert := &x509.Certificate{
		Subject:  pkix.Name{CommonName: "a\x00b‮c​d\ne f\x7fg"},
		Issuer:   pkix.Name{CommonName: strings.Repeat("é", 300)},
		DNSNames: []string{"bad\xffname\r\n.example.test"},
	}
	for i := 0; i < 24; i++ {
		cert.DNSNames = append(cert.DNSNames, "n"+strconv.Itoa(i)+".example.test")
	}
	cert.URIs = []*url.URL{{Scheme: "spiffe", Host: "x.example.test", Path: "/‮ev"}}
	sum := summarize(cert)
	if sum.Subject != "CN=abcdefg" {
		t.Errorf("subject %q", sum.Subject)
	}
	if len(sum.Issuer) > summaryTextMax || !strings.HasPrefix(sum.Issuer, "CN=é") {
		t.Errorf("issuer of %d bytes", len(sum.Issuer))
	}
	if len(sum.SANs) != summarySANMax || sum.SANsMore != 26-summarySANMax {
		t.Errorf("%d names and %d more", len(sum.SANs), sum.SANsMore)
	}
	if sum.SANs[0] != "bad�name.example.test" {
		t.Errorf("first name %q", sum.SANs[0])
	}
	all := sum.Subject + sum.Issuer + strings.Join(sum.SANs, "")
	for _, r := range all {
		if r < 0x20 || r == 0x7f || r == '‮' || r == '​' || r == ' ' {
			t.Errorf("the text holds %U", r)
		}
	}
}

// [REQ-824] The probe does not make the client one that does not verify, and it is no request of the client: the pinned
// client keeps its settings after a probe.
func TestHxbProbeLeavesTheClientAsItWas(t *testing.T) {
	cert, _ := ctIssue(t, "api.example.test", 7, ctFrom, ctTo)
	port, requests := hxbServe(t, hxbConst(cert), http.NotFoundHandler(), false)
	c, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{})
	if _, err := c.ProbeCert(context.Background(), "https://api.example.test:"+port+"/"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctGet(t, c, "https://api.example.test:"+port+"/"); dtKind(t, err).Kind != KindTLS {
		t.Errorf("after the probe the client takes a certificate it does not trust: %v", err)
	}
	if requests.Load() != 0 {
		t.Errorf("%d requests", requests.Load())
	}
}

// [REQ-824] A summary that was cut says so, so that a long first part cannot hide what follows without anyone seeing that
// something is missing; one that was not cut does not.
func TestHxbTheSummaryOfACertificateSaysWhenItWasCut(t *testing.T) {
	short := summarize(&x509.Certificate{Subject: pkix.Name{CommonName: "short"}, Issuer: pkix.Name{CommonName: "issuer"}, DNSNames: []string{"a.example.test"}})
	if short.Truncated {
		t.Error("a summary that holds all of the certificate is marked as cut")
	}
	for name, cert := range map[string]*x509.Certificate{
		"subject": {Subject: pkix.Name{CommonName: strings.Repeat("s", 300)}},
		"issuer":  {Issuer: pkix.Name{CommonName: strings.Repeat("i", 300)}},
		"name":    {DNSNames: []string{strings.Repeat("n", 300) + ".example.test"}},
		"names": {DNSNames: func() []string {
			var out []string
			for i := 0; i <= summarySANMax; i++ {
				out = append(out, "n"+strconv.Itoa(i)+".example.test")
			}
			return out
		}()},
	} {
		if !summarize(cert).Truncated {
			t.Errorf("a cut %s is not marked", name)
		}
	}
	// exactly at the limit is not cut
	if got := summarize(&x509.Certificate{Subject: pkix.Name{CommonName: strings.Repeat("s", summaryTextMax-3)}}); got.Truncated || len(got.Subject) != summaryTextMax {
		t.Errorf("at the limit: %d bytes, cut %v", len(got.Subject), got.Truncated)
	}
}
