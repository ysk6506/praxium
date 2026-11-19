package egress

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type ctZeros struct{}

func (ctZeros) Read(p []byte) (int, error) { return len(p), nil }

// ctIssue makes a self-signed server certificate for name and the loopback, from a fixed key, and returns it with
// its PEM. The dates are given so that a test can ask for an expired one.
func ctIssue(t *testing.T, name string, seed byte, notBefore, notAfter time.Time) (tls.Certificate, string) {
	t.Helper()
	key := ed25519.NewKeyFromSeed([]byte(strings.Repeat(string(rune('a'+seed)), ed25519.SeedSize)))
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(int64(seed) + 1),
		Subject:               pkix.Name{CommonName: name},
		DNSNames:              []string{name},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(ctZeros{}, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return cert, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

var (
	ctFrom = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	ctTo   = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
)

func ctServe(t *testing.T, cert tls.Certificate, h http.Handler) (string, *httptest.Server) {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port, srv
}

func ctClient(t *testing.T, names map[string]string, pol Policy, opt ClientOptions) (*Client, *dtResolver) {
	t.Helper()
	answers := map[string][][]netip.Addr{}
	for name, ip := range names {
		answers[name] = [][]netip.Addr{dtAddrs(t, ip)}
	}
	res := &dtResolver{answers: answers}
	d, err := New(Config{Policy: pol, Resolver: res})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(d, opt)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c, res
}

func ctGet(t *testing.T, c *Client, target string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return c.Do(req)
}

// [REQ-658]
func TestClientReachesAHostOfAFakeResolverOverVerifiedTLS(t *testing.T) {
	cert, caPEM := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	var gotHost string
	port, _ := ctServe(t, cert, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_, _ = io.WriteString(w, "hello")
	}))
	c, res := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{Hosts: []string{"api.example.test"}}, ClientOptions{CAPEM: caPEM})
	resp, err := ctGet(t, c, "https://api.example.test:"+port+"/items?page=1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "hello" || gotHost != "api.example.test:"+port || res.total() != 1 {
		t.Errorf("body %q, host %q, %d lookups", body, gotHost, res.total())
	}
}

// [REQ-658]
func TestClientDoesNotTrustWhatItWasNotGiven(t *testing.T) {
	cert, _ := ctIssue(t, "api.example.test", 1, ctFrom, ctTo)
	_, otherPEM := ctIssue(t, "api.example.test", 2, ctFrom, ctTo)
	port, _ := ctServe(t, cert, http.NotFoundHandler())
	names := map[string]string{"api.example.test": "127.0.0.1"}
	for name, pemText := range map[string]string{"another authority": otherPEM, "system authorities": ""} {
		c, _ := ctClient(t, names, Policy{}, ClientOptions{CAPEM: pemText})
		_, err := ctGet(t, c, "https://api.example.test:"+port+"/?token=secret-detail")
		e := dtKind(t, err)
		if e.Kind != KindTLS || !strings.Contains(e.Reason, "trusted authority") || e.Temporary() {
			t.Errorf("%s: %v", name, err)
		}
		if strings.Contains(err.Error(), "secret-detail") {
			t.Errorf("%s: message %q holds the query", name, err.Error())
		}
	}
}

// [REQ-658]
func TestClientRefusesAnExpiredCertificateAndAnotherHostName(t *testing.T) {
	old, oldPEM := ctIssue(t, "api.example.test", 3, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	port, _ := ctServe(t, old, http.NotFoundHandler())
	c, _ := ctClient(t, map[string]string{"api.example.test": "127.0.0.1"}, Policy{}, ClientOptions{CAPEM: oldPEM})
	_, err := ctGet(t, c, "https://api.example.test:"+port+"/")
	if e := dtKind(t, err); e.Kind != KindTLS || !strings.Contains(e.Reason, "expired") {
		t.Errorf("expired: %v", err)
	}
	good, goodPEM := ctIssue(t, "api.example.test", 4, ctFrom, ctTo)
	port, _ = ctServe(t, good, http.NotFoundHandler())
	c, _ = ctClient(t, map[string]string{"other.example.test": "127.0.0.1"}, Policy{}, ClientOptions{CAPEM: goodPEM})
	_, err = ctGet(t, c, "https://other.example.test:"+port+"/")
	if e := dtKind(t, err); e.Kind != KindTLS || !strings.Contains(e.Reason, "does not match") {
		t.Errorf("host name: %v", err)
	}
}

// [REQ-658]
func TestClientWithABadAuthorityTextIsNotMade(t *testing.T) {
	d, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient(d, ClientOptions{CAPEM: "nothing"}); dtKind(t, err).Kind != KindTLS {
		t.Errorf("error %v", err)
	}
}

// [REQ-658]
func TestClientIgnoresTheProxyOfTheEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "direct") }))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	// a name that is not the loopback: the environment would send it to the proxy
	c, _ := ctClient(t, map[string]string{"plain.example.test": "127.0.0.1"}, Policy{AllowInsecure: true}, ClientOptions{})
	resp, err := ctGet(t, c, "http://plain.example.test:"+port+"/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "direct" {
		t.Errorf("body %q", body)
	}
	tr := c.hc.Transport.(guard).base.(*http.Transport)
	if tr.Proxy != nil || tr.DialContext == nil || tr.DialTLSContext != nil || tr.TLSClientConfig.InsecureSkipVerify {
		t.Errorf("transport: %+v", tr)
	}
}

// [REQ-658]
func TestPlainHTTPIsForTheLoopbackUnlessThePolicyAllowsIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	names := map[string]string{"plain.example.test": "127.0.0.1", "localhost": "127.0.0.1"}
	c, res := ctClient(t, names, Policy{}, ClientOptions{})
	for _, ok := range []string{"http://127.0.0.1:" + port + "/", "http://localhost:" + port + "/", "http://LOCALHOST.:" + port + "/"} {
		resp, err := ctGet(t, c, ok)
		if err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
		_ = resp.Body.Close()
	}
	before := res.total()
	if _, err := ctGet(t, c, "http://plain.example.test:"+port+"/?token=secret-detail"); dtKind(t, err).Kind != KindPolicy || strings.Contains(err.Error(), "secret-detail") {
		t.Errorf("plain http to a name: %v", err)
	}
	if res.total() != before {
		t.Error("the name was resolved for a request that is refused")
	}
	open, _ := ctClient(t, names, Policy{AllowInsecure: true}, ClientOptions{})
	resp, err := ctGet(t, open, "http://plain.example.test:"+port+"/")
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	_ = resp.Body.Close()
}

// [REQ-658]
func TestClientRefusesRequestsThatAreNotPlainAddresses(t *testing.T) {
	c, res := ctClient(t, map[string]string{"x.example.test": "127.0.0.1"}, Policy{}, ClientOptions{})
	for _, target := range []string{
		"ftp://x.example.test/", "file:///etc/passwd", "gopher://x.example.test/",
		"https://user:pass-detail@x.example.test/", "https://2130706433/", "https://0x7f.1/", "https:///path",
	} {
		_, err := ctGet(t, c, target)
		e := dtKind(t, err)
		if e.Kind != KindPolicy || strings.Contains(err.Error(), "pass-detail") {
			t.Errorf("%s: %v", target, err)
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://x.example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "internal.example.test"
	if _, err := c.Do(req); dtKind(t, err).Kind != KindPolicy {
		t.Errorf("a Host header of another host: %v", err)
	}
	if res.total() != 0 {
		t.Error("a refused request reached the resolver")
	}
}

// [REQ-658]
func TestClientDoesNotShowAnAddressOrAQueryOfARefusedHost(t *testing.T) {
	c, _ := ctClient(t, map[string]string{"meta.example.test": "169.254.169.254"}, Policy{}, ClientOptions{})
	_, err := ctGet(t, c, "https://meta.example.test/latest/meta-data?token=secret-detail")
	e := dtKind(t, err)
	if e.Kind != KindPolicy || e.Host != "meta.example.test" {
		t.Fatalf("error %v", err)
	}
	for _, leak := range []string{"secret-detail", "169.254", "latest", "https://"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("message %q holds %q", err.Error(), leak)
		}
	}
}

func ctRedirectServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/next", http.StatusFound) })
	mux.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "arrived") })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	mux.HandleFunc("/cross", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+r.URL.Query().Get("port")+"/next", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return port
}

// [REQ-658]
func TestRedirectsAreNotFollowedByDefault(t *testing.T) {
	port := ctRedirectServer(t)
	c, _ := ctClient(t, nil, Policy{}, ClientOptions{})
	_, err := ctGet(t, c, "http://127.0.0.1:"+port+"/start?token=secret-detail")
	if e := dtKind(t, err); e.Kind != KindRedirect || strings.Contains(err.Error(), "secret-detail") {
		t.Errorf("error %v", err)
	}
}

// [REQ-658]
func TestRedirectsToTheSameOriginAreFollowedUpToTheLimitAndNoFurther(t *testing.T) {
	port := ctRedirectServer(t)
	c, _ := ctClient(t, nil, Policy{}, ClientOptions{MaxRedirects: 2})
	resp, err := ctGet(t, c, "http://127.0.0.1:"+port+"/start")
	if err != nil {
		t.Fatalf("same origin: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "arrived" {
		t.Errorf("body %q", body)
	}
	if _, err = ctGet(t, c, "http://127.0.0.1:"+port+"/loop"); dtKind(t, err).Kind != KindRedirect || !strings.Contains(err.Error(), "too many") {
		t.Errorf("loop: %v", err)
	}
	if _, err = ctGet(t, c, "http://127.0.0.1:"+port+"/cross?port="+port); dtKind(t, err).Kind != KindRedirect || !strings.Contains(err.Error(), "another origin") {
		t.Errorf("cross origin: %v", err)
	}
}

// [REQ-658]
func TestRequestTimeAndCancellationAreReported(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	c, _ := ctClient(t, nil, Policy{}, ClientOptions{Timeout: 30 * time.Millisecond})
	_, err := ctGet(t, c, srv.URL+"/?token=secret-detail")
	if e := dtKind(t, err); e.Kind != KindTimeout || !e.Temporary() || strings.Contains(err.Error(), "secret-detail") {
		t.Errorf("timeout: %v", err)
	}
	slow, _ := ctClient(t, nil, Policy{}, ClientOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_, err = slow.Do(req)
	if e := dtKind(t, err); e.Kind != KindCanceled {
		t.Errorf("cancel: %v", err)
	}
}

// [REQ-658]
func TestDefaultClientTimeIsThirtySeconds(t *testing.T) {
	c, _ := ctClient(t, nil, Policy{}, ClientOptions{})
	if c.hc.Timeout != 30*time.Second {
		t.Errorf("timeout %v", c.hc.Timeout)
	}
}

// [REQ-658]
func TestSameOriginComparesSchemeHostAndPort(t *testing.T) {
	must := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	same := [][2]string{
		{"https://api.example.test/a", "https://API.example.test:443/b?x=1"},
		{"http://api.example.test/a", "http://api.example.test.:80/b"},
		{"https://api.example.test:8443/a", "https://api.example.test:8443/"},
		{"https://[2001:db8::1]/a", "https://[2001:DB8::1]:443/b"},
	}
	for _, p := range same {
		if !SameOrigin(must(p[0]), must(p[1])) {
			t.Errorf("%s and %s differ", p[0], p[1])
		}
	}
	different := [][2]string{
		{"https://api.example.test/", "http://api.example.test/"},
		{"https://api.example.test/", "https://api.example.test:8443/"},
		{"https://api.example.test/", "https://other.example.test/"},
		{"https://api.example.test/", "https://api.example.test.evil.test/"},
		{"https://api.example.test/", "https://2130706433/"},
		{"https://127.0.0.1/", "https://localhost/"},
	}
	for _, p := range different {
		if SameOrigin(must(p[0]), must(p[1])) {
			t.Errorf("%s and %s are the same", p[0], p[1])
		}
	}
	if SameOrigin(nil, must("https://a.test/")) || SameOrigin(must("https://a.test/"), nil) {
		t.Error("a nil URL has an origin")
	}
}

// [REQ-658]
func TestResolveSameOriginTakesOnlyTheOriginOfTheSource(t *testing.T) {
	base, err := url.Parse("https://api.example.test/api/v2/servers?page=1")
	if err != nil {
		t.Fatal(err)
	}
	good := map[string]string{
		"/api/v2/servers?page=2": "https://api.example.test/api/v2/servers?page=2",
		"servers?page=2":         "https://api.example.test/api/v2/servers?page=2",
		"?page=3":                "https://api.example.test/api/v2/servers?page=3",
		"https://api.example.test/api/v2/servers?page=4":  "https://api.example.test/api/v2/servers?page=4",
		"https://API.example.test:443/x#fragment-removed": "https://API.example.test:443/x",
		"//api.example.test/x":                            "https://api.example.test/x",
	}
	for in, want := range good {
		got, err := ResolveSameOrigin(base, in)
		if err != nil || got.String() != want {
			t.Errorf("%q: %v, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{
		"https://evil.test/x", "http://api.example.test/x", "https://api.example.test:8443/x", "//evil.test/x", "https://user@api.example.test/x",
		"https://api.example.test\\@evil.test/x", "/x\r\nHost: evil.test", "/x\x00", "https://api.example.test/%zz", "javascript:alert(1)",
		"https://127.0.0.1/x?token=secret-detail",
	} {
		got, err := ResolveSameOrigin(base, in)
		if err == nil {
			t.Errorf("%q accepted as %v", in, got)
			continue
		}
		if e := dtKind(t, err); e.Kind != KindPolicy || strings.Contains(err.Error(), "secret-detail") || strings.Contains(err.Error(), "evil") {
			t.Errorf("%q: %v", in, err)
		}
	}
}

// [REQ-658]
func TestPlaintextIsAllowedForTheLoopbackAndForTheOperatorsWordOnly(t *testing.T) {
	strict, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	open, err := New(Config{Policy: Policy{AllowInsecure: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "127.8.8.8", "::1", "localhost", "LocalHost."} {
		if !strict.PlaintextAllowed(host) {
			t.Errorf("%s is refused without TLS", host)
		}
	}
	for _, host := range []string{"db.example.test", "10.0.0.5", "8.8.8.8", "127.1", "a b", ""} {
		if strict.PlaintextAllowed(host) {
			t.Errorf("%s is allowed without TLS", host)
		}
	}
	if !open.PlaintextAllowed("db.example.test") || !open.PlaintextAllowed("10.0.0.5") || open.PlaintextAllowed("127.1") {
		t.Error("the policy that allows plaintext is not applied, or lets a bad host through")
	}
}
