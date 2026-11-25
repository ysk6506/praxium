package httpsource

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The tests of this file send real requests over TLS to two servers on the loopback (an API and an authentication server), each
// with a certificate of its own authority, through the real egress client and a resolver that answers every name with the
// loopback. They show what the fakes of the other hxc tests cannot: that each host is trusted by its own certificate authority
// and no other, that a redirect of the token host is not followed, that a name of the same address is its own host, and that
// the token goes to no address the API names. Every name begins with hxcTLS so that it never meets a name of another test.

type hxcTLSAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  string
}

func hxcTLSNewAuthority(t *testing.T, cn string) hxcTLSAuthority {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return hxcTLSAuthority{cert: c, key: key, pem: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

func (ca hxcTLSAuthority) hxcLeaf(t *testing.T, names ...string) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

type hxcTLSSeen struct {
	mu   sync.Mutex
	reqs []string
}

func (s *hxcTLSSeen) hxcAdd(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var b strings.Builder
	b.WriteString(r.Method + " " + r.Host + " " + r.URL.String() + "\n")
	for k, v := range r.Header {
		b.WriteString(k + ": " + strings.Join(v, ",") + "\n")
	}
	b.Write(body)
	s.mu.Lock()
	s.reqs = append(s.reqs, b.String())
	s.mu.Unlock()
}

func (s *hxcTLSSeen) hxcHas(texts ...string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.reqs {
		for _, x := range texts {
			if strings.Contains(r, x) {
				return true
			}
		}
	}
	return false
}
func (s *hxcTLSSeen) hxcN() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.reqs) }

func hxcTLSServe(t *testing.T, cert tls.Certificate, seen *hxcTLSSeen, h func(w http.ResponseWriter, r *http.Request)) (int, *httptest.Server) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.hxcAdd(r)
		h(w, r)
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(p)
	return port, srv
}

const hxcTLSToken = "hxc-minted-not-a-real-token-0001"

type hxcTLSWorld struct {
	apiSeen, idpSeen *hxcTLSSeen
	apiPort, idpPort int
	apiCA, idpCA     hxcTLSAuthority
	idpHandler       func(w http.ResponseWriter, r *http.Request)
	apiHandler       func(w http.ResponseWriter, r *http.Request)
}

// hxcRun builds a definition, the secret store and a reader over the real egress client and reads the first dataset.
func (w *hxcTLSWorld) hxcRun(t *testing.T, tokenHostPort string, connExtra, reqExtra, pagination string, allowed []secrets.Address) ([]ingest.Page, []ingest.Event, error) {
	t.Helper()
	auth := `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"request":{"url":"https://` + tokenHostPort + `/oauth2/token","format":"form"` + reqExtra +
		`,"body":{"grant_type":"client_credentials","client_id":"{{credential.username}}","client_secret":"{{credential.password}}"}},"token_path":"access_token","expires_in_path":"expires_in"}`
	ds := `{"id":"ds_servers","request":{"path":"/servers"},"records_path":"items"` + pagination + `,"natural_key":["serial"],"columns":[{"name":"serial"}]}`
	doc := `{"version":1,"id":"` + hsSource + `","adapter":"http","connection":{"base_url":"https://api.example.test:` + strconv.Itoa(w.apiPort) + `/api/v2","requests_per_second":1000,"headers":{"X-Tenant":"hxc-tenant-value"}` + connExtra +
		`,"auth":` + auth + `},"datasets":[` + ds + `]}`
	g := hxcNew(t, doc, allowed, "3600")
	d, err := egress.New(egress.Config{Resolver: hsResolver{hsAddrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := g.hxcReader.cfg
	cfg.Connect = ViaDialer(d)
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var pages []ingest.Page
	for p, err := range r.ReadPages(hsCtx, hsSource, g.hxcDef.Datasets[0], ingest.PageOptions{}) {
		if err != nil {
			return pages, g.hxcEvents, err
		}
		pages = append(pages, p)
	}
	return pages, g.hxcEvents, nil
}

func hxcTLSQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func hxcTLSNewWorld(t *testing.T, apiLeafBy, idpLeafBy string) *hxcTLSWorld {
	w := &hxcTLSWorld{apiSeen: &hxcTLSSeen{}, idpSeen: &hxcTLSSeen{}, apiCA: hxcTLSNewAuthority(t, "hxc api ca"), idpCA: hxcTLSNewAuthority(t, "hxc idp ca")}
	by := func(name string) hxcTLSAuthority {
		if name == "api" {
			return w.apiCA
		}
		return w.idpCA
	}
	w.idpHandler = func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"access_token":"` + hxcTLSToken + `","expires_in":3600}`))
	}
	w.apiHandler = func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+hxcTLSToken {
			rw.WriteHeader(401)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"items":[{"serial":"s1"}]}`))
	}
	w.apiPort, _ = hxcTLSServe(t, by(apiLeafBy).hxcLeaf(t, "api.example.test", "alias.example.test"), w.apiSeen, func(rw http.ResponseWriter, r *http.Request) { w.apiHandler(rw, r) })
	w.idpPort, _ = hxcTLSServe(t, by(idpLeafBy).hxcLeaf(t, "idp.example.test"), w.idpSeen, func(rw http.ResponseWriter, r *http.Request) { w.idpHandler(rw, r) })
	return w
}

func (w *hxcTLSWorld) hxcAddresses() []secrets.Address {
	return []secrets.Address{{Host: "api.example.test", Port: w.apiPort}, {Host: "idp.example.test", Port: w.idpPort}, {Host: "alias.example.test", Port: w.apiPort}}
}

func hxcTLSKind(err error) string {
	if err == nil {
		return ""
	}
	if se, ok := ingest.SourceErrorOf(err); ok {
		return se.Kind
	}
	return "other:" + err.Error()
}

func TestHxcTLSHappyPathEachHostGetsItsOwn(t *testing.T) {
	w := hxcTLSNewWorld(t, "api", "idp")
	idp := "idp.example.test:" + strconv.Itoa(w.idpPort)
	pages, events, err := w.hxcRun(t, idp, `,"ca_pem":`+hxcTLSQuote(w.apiCA.pem), `,"ca_pem":`+hxcTLSQuote(w.idpCA.pem), "", w.hxcAddresses())
	if err != nil || len(hsSerials(pages)) != 1 {
		t.Fatalf("%v %d", err, len(hsSerials(pages)))
	}
	if w.apiSeen.hxcHas(hsPassValue, hsUserValue) || w.idpSeen.hxcHas(hxcTLSToken, "Bearer", "hxc-tenant-value") {
		t.Error("LEAK: a credential at the API or the token/connection header at the token host")
	}
	if !w.idpSeen.hxcHas("POST idp.example.test:"+strconv.Itoa(w.idpPort)) || !w.apiSeen.hxcHas("GET api.example.test:"+strconv.Itoa(w.apiPort)) {
		t.Errorf("Host headers: idp=%v api=%v", w.idpSeen.reqs, w.apiSeen.reqs)
	}
	for _, e := range events {
		if e.Path == "/oauth2/token" && (e.Host != "idp.example.test" || e.Port != w.idpPort) {
			t.Errorf("event of token request says %s:%d", e.Host, e.Port)
		}
	}
}

// The connection's ca_pem must not be trusted for the token host: the token host's leaf is signed by the API's CA, the definition
// gives no auth.request.ca_pem. The token host must be refused and the credentials not sent.
func TestHxcTLSTheAPICAIsNotTrustedForTheTokenHost(t *testing.T) {
	w := hxcTLSNewWorld(t, "api", "api")
	idp := "idp.example.test:" + strconv.Itoa(w.idpPort)
	_, _, err := w.hxcRun(t, idp, `,"ca_pem":`+hxcTLSQuote(w.apiCA.pem), "", "", w.hxcAddresses())
	if hxcTLSKind(err) != ingest.SrcTLSRejected || w.idpSeen.hxcN() != 0 || w.apiSeen.hxcN() != 0 {
		t.Errorf("kind=%s idp=%d api=%d", hxcTLSKind(err), w.idpSeen.hxcN(), w.apiSeen.hxcN())
	}
}

// The token host's ca_pem must not be trusted for the API.
func TestHxcTLSTheTokenCAIsNotTrustedForTheAPI(t *testing.T) {
	w := hxcTLSNewWorld(t, "idp", "idp")
	idp := "idp.example.test:" + strconv.Itoa(w.idpPort)
	_, _, err := w.hxcRun(t, idp, "", `,"ca_pem":`+hxcTLSQuote(w.idpCA.pem), "", w.hxcAddresses())
	if hxcTLSKind(err) != ingest.SrcTLSRejected || w.apiSeen.hxcN() != 0 {
		t.Errorf("kind=%s idp=%d api=%d", hxcTLSKind(err), w.idpSeen.hxcN(), w.apiSeen.hxcN())
	}
}

// A redirect of the token host is not followed: the credentials (307 keeps the body) never reach the API.
func TestHxcTLSARedirectOfTheTokenHostIsNotFollowed(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		w := hxcTLSNewWorld(t, "api", "idp")
		w.idpHandler = func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Location", "https://api.example.test:"+strconv.Itoa(w.apiPort)+"/steal")
			rw.WriteHeader(code)
		}
		idp := "idp.example.test:" + strconv.Itoa(w.idpPort)
		_, _, err := w.hxcRun(t, idp, `,"ca_pem":`+hxcTLSQuote(w.apiCA.pem), `,"ca_pem":`+hxcTLSQuote(w.idpCA.pem), "", w.hxcAddresses())
		if hxcTLSKind(err) != ingest.SrcRequestRejected || w.apiSeen.hxcN() != 0 || w.idpSeen.hxcN() != 1 {
			t.Errorf("%d: kind=%s idp=%d api=%d", code, hxcTLSKind(err), w.idpSeen.hxcN(), w.apiSeen.hxcN())
		}
	}
}

// Another name of the same IP and port as the API: the token request is judged under its own name (SNI, certificate name,
// certificate authority). With the system's authorities it is refused; nothing is sent.
func TestHxcTLSAnAliasOfTheAPIsAddressIsItsOwnHost(t *testing.T) {
	w := hxcTLSNewWorld(t, "api", "idp")
	alias := "alias.example.test:" + strconv.Itoa(w.apiPort)
	_, _, err := w.hxcRun(t, alias, `,"ca_pem":`+hxcTLSQuote(w.apiCA.pem), "", "", w.hxcAddresses())
	if hxcTLSKind(err) != ingest.SrcTLSRejected || w.apiSeen.hxcN() != 0 {
		t.Errorf("no token ca: kind=%s api=%d", hxcTLSKind(err), w.apiSeen.hxcN())
	}
	// With the token host's own ca_pem (the API's CA, the leaf has the alias name): reaches the same server under the alias name; the
	// headers of the connection are not sent with it, and the secret must allow the alias.
	w = hxcTLSNewWorld(t, "api", "idp")
	alias = "alias.example.test:" + strconv.Itoa(w.apiPort)
	_, _, err = w.hxcRun(t, alias, `,"ca_pem":`+hxcTLSQuote(w.apiCA.pem), `,"ca_pem":`+hxcTLSQuote(w.apiCA.pem), "", []secrets.Address{{Host: "api.example.test", Port: w.apiPort}})
	if hxcTLSKind(err) != ingest.SrcSecretUnusable || w.apiSeen.hxcN() != 0 {
		t.Errorf("alias not allowed by the secret: kind=%s api=%d", hxcTLSKind(err), w.apiSeen.hxcN())
	}
}

// The API names the token host as the next page (Link header, next_url): the token must not go there. The address of the next page is
// judged as the API's request and is refused by the policy of the client (a page of the API is at the API's host).
func TestHxcTLSANextPageAtTheTokenHostIsNotFollowed(t *testing.T) {
	for _, c := range []struct{ name, pagination string }{
		{"link", `,"pagination":{"kind":"link_header"}`},
		{"next_url", `,"pagination":{"kind":"next_url","next_path":"next"}`},
	} {
		w := hxcTLSNewWorld(t, "api", "idp")
		target := "https://idp.example.test:" + strconv.Itoa(w.idpPort) + "/oauth2/token"
		w.apiHandler = func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.Header().Set("Link", `<`+target+`>; rel="next"`)
			_, _ = rw.Write([]byte(`{"items":[{"serial":"s1"}],"next":"` + target + `"}`))
		}
		idp := "idp.example.test:" + strconv.Itoa(w.idpPort)
		_, _, err := w.hxcRun(t, idp, `,"ca_pem":`+hxcTLSQuote(w.apiCA.pem), `,"ca_pem":`+hxcTLSQuote(w.idpCA.pem), c.pagination, w.hxcAddresses())
		if w.idpSeen.hxcHas(hxcTLSToken, "Bearer") || w.idpSeen.hxcN() != 1 {
			t.Errorf("%s: the token went to the token host (%d requests there)", c.name, w.idpSeen.hxcN())
		}
		if k := hxcTLSKind(err); k != ingest.SrcPolicyDenied {
			t.Errorf("%s: the read ended with %q", c.name, k)
		}
	}
}
