package egress

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	hxbOld = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	hxbEnd = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
)

// hxbPin is the pin of the certificate of a server.
func hxbPin(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return PinOf(parsed)
}

// hxbServe starts a TLS server whose certificate is chosen at every handshake by pick (nth handshake, from 1), and counts
// the requests it was given. h2 turns HTTP/2 on.
func hxbServe(t *testing.T, pick func(n int) tls.Certificate, h http.Handler, h2 bool) (port string, requests *atomic.Int64) {
	t.Helper()
	requests = new(atomic.Int64)
	var mu sync.Mutex
	handshakes := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		h.ServeHTTP(w, r)
	}))
	srv.EnableHTTP2 = h2
	srv.TLS = &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		mu.Lock()
		handshakes++
		n := handshakes
		mu.Unlock()
		c := pick(n)
		return &c, nil
	}}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port, requests
}

func hxbConst(c tls.Certificate) func(int) tls.Certificate {
	return func(int) tls.Certificate { return c }
}

func hxbBody(t *testing.T, resp *Response) string {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return string(b)
}

// [REQ-822] A source with pins is read when the public key of the certificate it presents is one of them, though the
// certificate is not signed by anyone the client trusts, is for another name, and has expired; over HTTP/1.1 and HTTP/2.
func TestHxbPinnedClientAcceptsTheKeyWhateverTheChainTheNameAndTheDates(t *testing.T) {
	cert, _ := ctIssue(t, "other.example.test", 1, hxbOld, hxbEnd)
	other, _ := ctIssue(t, "other.example.test", 2, hxbOld, hxbEnd)
	for _, h2 := range []bool{false, true} {
		port, requests := hxbServe(t, hxbConst(cert), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprintf(w, "proto %d", r.ProtoMajor)
		}), h2)
		// the pins of the client: a key that is not the server's, and the server's
		pins := []string{hxbPin(t, other), hxbPin(t, cert), hxbPin(t, cert)}
		c, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{Pins: pins})
		resp, err := ctGet(t, c, "https://api.example.test:"+port+"/items")
		if err != nil {
			t.Fatalf("h2=%v: %v", h2, err)
		}
		want := "proto 1"
		if h2 {
			want = "proto 2"
		}
		if got := hxbBody(t, resp); got != want || requests.Load() != 1 {
			t.Errorf("h2=%v: body %q, %d requests", h2, got, requests.Load())
		}
	}
}

// [REQ-822] A certificate whose public key is not one of the pins is refused as a TLS failure that is not temporary and
// that is not tried again, before any request is sent. The words hold no pin of the server's.
func TestHxbPinnedClientRefusesAnotherKeyBeforeAnyRequest(t *testing.T) {
	served, _ := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	pinned, _ := ctIssue(t, "api.example.test", 2, ctFrom, ctTo)
	port, requests := hxbServe(t, hxbConst(served), http.NotFoundHandler(), false)
	// the certificate is valid, for the name, and signed by itself: with the pin of another key the client does not take it
	c, res := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{Pins: []string{hxbPin(t, pinned)}})
	_, err := ctGet(t, c, "https://api.example.test:"+port+"/items?token=secret-detail")
	e := dtKind(t, err)
	if e.Kind != KindTLS || e.Temporary() || !strings.Contains(e.Reason, "not one of the pinned") {
		t.Fatalf("error %v", err)
	}
	for _, leak := range []string{"secret-detail", hxbPin(t, served), hxbPin(t, pinned)} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the message holds %q", leak)
		}
	}
	if requests.Load() != 0 || res.total() != 1 {
		t.Errorf("%d requests reached the server, %d lookups", requests.Load(), res.total())
	}
}

// [REQ-822] The check holds for every connection the client makes, so for the hop of a redirect and for a request that
// comes after the server changed its certificate; and what the server presents now is shown by the probe.
func TestHxbPinsHoldOnARedirectAndAfterTheServerChangesItsCertificate(t *testing.T) {
	first, _ := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	second, _ := ctIssue(t, "api.example.test", 2, ctFrom, ctTo)
	pins := []string{hxbPin(t, first)}
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close") // the hop has to be a new connection, with its own handshake
		http.Redirect(w, r, "/b", http.StatusFound)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "b") })
	// the first handshake presents the pinned key, the later ones another
	port, requests := hxbServe(t, func(n int) tls.Certificate {
		if n == 1 {
			return first
		}
		return second
	}, mux, false)
	c, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{Pins: pins, MaxRedirects: 2})
	_, err := ctGet(t, c, "https://api.example.test:"+port+"/a")
	if e := dtKind(t, err); e.Kind != KindTLS || !strings.Contains(e.Reason, "not one of the pinned") {
		t.Fatalf("the hop of the redirect: %v", err)
	}
	if requests.Load() != 1 {
		t.Errorf("%d requests reached the server; the hop that failed the check must not be one", requests.Load())
	}
	// a request after the certificate changed
	c.CloseIdleConnections()
	if _, err := ctGet(t, c, "https://api.example.test:"+port+"/b"); dtKind(t, err).Kind != KindTLS {
		t.Errorf("after the change: %v", err)
	}
	sum, err := c.ProbeCert(context.Background(), "https://api.example.test:"+port+"/")
	if err != nil {
		t.Fatal(err)
	}
	if sum.SPKISHA256 != hxbPin(t, second) || sum.SPKISHA256 == pins[0] {
		t.Errorf("the probe shows %q, the server presents %q", sum.SPKISHA256, hxbPin(t, second))
	}
}

// [REQ-822] There is no way to a client that does not verify: no pins is a client that verifies and refuses a certificate
// of its own, a list that is empty, too long or not in the form is no client, and pins and an authority are not given
// together.
func TestHxbWithoutPinsTheVerificationCannotBeSkipped(t *testing.T) {
	cert, _ := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	port, requests := hxbServe(t, hxbConst(cert), http.NotFoundHandler(), false)
	for name, pins := range map[string][]string{"nil": nil, "empty": {}} {
		c, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{Pins: pins})
		_, err := ctGet(t, c, "https://api.example.test:"+port+"/")
		if e := dtKind(t, err); e.Kind != KindTLS || !strings.Contains(e.Reason, "trusted authority") {
			t.Errorf("%s: %v", name, err)
		}
		tr := c.hc.Transport.(guard).base.(*http.Transport)
		if tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.VerifyConnection != nil {
			t.Errorf("%s: the transport has no verification", name)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("%d requests reached the server", requests.Load())
	}
	d, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	good := hxbPin(t, cert)
	many := make([]string, MaxPins+1)
	for i := range many {
		sum := sha256.Sum256([]byte{byte(i)})
		many[i] = pinPrefix + base64.StdEncoding.EncodeToString(sum[:])
	}
	for name, opt := range map[string]ClientOptions{
		"too many":          {Pins: many},
		"no prefix":         {Pins: []string{strings.TrimPrefix(good, pinPrefix)}},
		"not base64":        {Pins: []string{pinPrefix + "!!!"}},
		"short":             {Pins: []string{pinPrefix + base64.StdEncoding.EncodeToString([]byte("short"))}},
		"upper case":        {Pins: []string{"SHA256/" + strings.TrimPrefix(good, pinPrefix)}},
		"url encoding":      {Pins: []string{pinPrefix + base64.URLEncoding.EncodeToString(sha256.New().Sum(nil))}},
		"empty item":        {Pins: []string{good, ""}},
		"with an authority": {Pins: []string{good}, CAPEM: "x"},
	} {
		if c, err := NewClient(d, opt); err == nil || c != nil || dtKind(t, err).Kind != KindTLS {
			t.Errorf("%s: client %v, error %v", name, c, err)
		}
	}
	// the longest list that is allowed is
	if _, err := NewClient(d, ClientOptions{Pins: many[:MaxPins]}); err != nil {
		t.Errorf("%d pins: %v", MaxPins, err)
	}
}

// [REQ-822] The settings of a pinned connection are made from a set of pins that is never empty, and nothing else makes a
// settings that skips the verification.
func TestHxbPinsAreParsedIntoAnEmptyProofSet(t *testing.T) {
	if cfg, err := pinnedConfig(nil); cfg != nil || err == nil {
		t.Errorf("no pins: %v %v", cfg, err)
	}
	if set, err := parsePins([]string{}); err == nil || len(set.sums) != 0 {
		t.Errorf("an empty list: %v", err)
	}
	var zero pinSet
	cert, _ := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	parsed, _ := x509.ParseCertificate(cert.Certificate[0])
	if err := zero.verify([]*x509.Certificate{parsed}); err == nil {
		t.Error("a set with no pin accepted a certificate")
	}
	pin := PinOf(parsed)
	canon, err := ParsePin(pin)
	if err != nil || canon != pin {
		t.Errorf("ParsePin: %q %v", canon, err)
	}
	set, err := parsePins([]string{pin, pin})
	if err != nil || len(set.sums) != 1 {
		t.Errorf("a pin written twice: %v %d", err, len(set.sums))
	}
	cfg, err := pinnedConfig([]string{pin})
	if err != nil || cfg.MinVersion != tls.VersionTLS12 || !cfg.InsecureSkipVerify || cfg.VerifyConnection == nil {
		t.Fatalf("settings: %+v %v", cfg, err)
	}
	// no certificate at all is refused, a certificate of another key is refused with what it presented
	if err := cfg.VerifyConnection(tls.ConnectionState{}); err == nil {
		t.Error("a connection with no certificate was accepted")
	}
	other, _ := ctIssue(t, "api.example.test", 2, ctFrom, ctTo)
	otherParsed, _ := x509.ParseCertificate(other.Certificate[0])
	err = cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{otherParsed, parsed}})
	var mismatch *PinMismatchError
	if !errors.As(err, &mismatch) || mismatch.Presented != PinOf(otherParsed) {
		t.Errorf("another key: %v", err)
	}
	// the key of the certificate that is first in the chain is judged, not one of the others
	if err := cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{parsed, otherParsed}}); err != nil {
		t.Errorf("the pinned certificate first: %v", err)
	}
}

// [REQ-822] A failure of a pin is a TLS failure that is not temporary whatever wraps it, so a GET is not tried again.
func TestHxbAPinMismatchIsMappedAsTLSAndNotAsAConnectionFailure(t *testing.T) {
	m := &PinMismatchError{Presented: "sha256/" + strings.Repeat("A", 43) + "="}
	for name, err := range map[string]error{
		"plain":       m,
		"wrapped":     fmt.Errorf("x: %w", m),
		"as url":      &url.Error{Op: "Get", URL: "https://secret-detail.example.test/", Err: m},
		"as verified": &tls.CertificateVerificationError{Err: m},
	} {
		e, ok := Sanitize(err).(*Error)
		if !ok || e.Kind != KindTLS || e.Temporary() || !strings.Contains(e.Reason, "not one of the pinned") || strings.Contains(e.Error(), "secret-detail") || strings.Contains(e.Error(), "AAAA") {
			t.Errorf("%s: %v", name, Sanitize(err))
		}
	}
}

// [REQ-822] The settings for PostgreSQL are made by egress and handed to the driver's function, and nobody else holds them:
// verified against the authority, or pinned, and not both.
func TestHxbPostgresSettingsAreRegisteredByEgress(t *testing.T) {
	cert, caPEM := ctIssue(t, "db.example.test", 1, ctFrom, ctTo)
	pin := hxbPin(t, cert)
	registered := map[string]*tls.Config{}
	register := func(key string, cfg *tls.Config) error {
		if cfg == nil {
			delete(registered, key)
			return nil
		}
		registered[key] = cfg
		return nil
	}
	undo, err := RegisterPostgresTLS(register, "k1", "", nil)
	if err != nil || registered["k1"] == nil || registered["k1"].InsecureSkipVerify || registered["k1"].VerifyConnection != nil || registered["k1"].MinVersion != tls.VersionTLS12 {
		t.Fatalf("verified: %+v %v", registered["k1"], err)
	}
	undo()
	if len(registered) != 0 {
		t.Error("the settings were not taken out")
	}
	if _, err := RegisterPostgresTLS(register, "k2", caPEM, nil); err != nil || registered["k2"].RootCAs == nil {
		t.Errorf("with an authority: %v", err)
	}
	if undo, err = RegisterPostgresTLS(register, "k3", "", []string{pin}); err != nil || registered["k3"] == nil || registered["k3"].VerifyConnection == nil {
		t.Fatalf("pinned: %v", err)
	}
	undo()
	before := len(registered)
	for name, call := range map[string]func() error{
		"pins and an authority": func() error { _, err := RegisterPostgresTLS(register, "k4", caPEM, []string{pin}); return err },
		"a pin that is no pin":  func() error { _, err := RegisterPostgresTLS(register, "k5", "", []string{"sha256/x"}); return err },
		"a bad authority":       func() error { _, err := RegisterPostgresTLS(register, "k6", "nothing", nil); return err },
		"a driver that refuses": func() error {
			_, err := RegisterPostgresTLS(func(string, *tls.Config) error { return errors.New("secret-detail") }, "k7", "", nil)
			return err
		},
	} {
		err := call()
		if err == nil || dtKind(t, err).Kind != KindTLS || strings.Contains(err.Error(), "secret-detail") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(registered) != before {
		t.Error("a refused call registered settings")
	}
}
