package sourcecheck

import (
	"context"
	"crypto/tls"
	"io"
	"log"
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
	"github.com/ysk6506/praxium/backend/internal/httpsource"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The tests of this file send real requests over TLS to two servers on the loopback (an API and an authentication server), each
// with a certificate of its own authority made from a fixed seed, through the real egress client and a resolver that answers
// the names of the test, and read with the public API of the HTTP adapter only. They show what the fakes inside the adapter
// cannot: that each host is trusted by its own certificate authority and no other, that a redirect of the token host is not
// followed, that a name of the same address is its own host, that the token goes to no address the API names, and what the
// policy does to a token server that is not encrypted (ADR-0106, ADR-0105). Every name begins with hxt so that it never meets a
// name of another test of this folder.

const (
	hxtSource = "src_hxt"
	hxtToken  = "hxt-minted-not-a-real-token-0001"
	hxtUser   = "hxt-user-value"
	hxtPass   = "hxt-pass-value-0002"
)

// hxtSeen is what a server was asked.
type hxtSeen struct {
	mu   sync.Mutex
	reqs []string
}

func hxtAdd(s *hxtSeen, r *http.Request) {
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

func hxtHas(s *hxtSeen, texts ...string) bool {
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

func hxtCount(s *hxtSeen) int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.reqs) }

// hxtServe starts a server on the loopback: with TLS and the certificate when cert is given, in the clear when it is not.
func hxtServe(t *testing.T, cert *tls.Certificate, seen *hxtSeen, h func(w http.ResponseWriter, r *http.Request)) int {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hxtAdd(seen, r)
		h(w, r)
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	if cert != nil {
		srv.TLS = &tls.Config{Certificates: []tls.Certificate{*cert}}
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)
	_, p, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// hxtNames resolves the names it is given to the address it is given, and any other to the loopback.
type hxtNames map[string]string

func (n hxtNames) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if ip, ok := n[host]; ok {
		return []netip.Addr{netip.MustParseAddr(ip)}, nil
	}
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

type hxtWorld struct {
	apiSeen, idpSeen *hxtSeen
	apiPort, idpPort int
	apiPEM, idpPEM   string
	idpHandler       func(w http.ResponseWriter, r *http.Request)
	apiHandler       func(w http.ResponseWriter, r *http.Request)
	names            hxtNames
	policy           egress.Policy
}

// hxtNewWorld starts an API and a token server, each with the certificate of the authority named for it ("api" or "idp"; the
// authority of the API also covers the name alias.example.test, which is the same address).
func hxtNewWorld(t *testing.T, apiBy, idpBy string, plainIdp bool) *hxtWorld {
	t.Helper()
	w := &hxtWorld{apiSeen: &hxtSeen{}, idpSeen: &hxtSeen{}, names: hxtNames{}}
	apiCert, apiPEM := scCert(t, 21, []string{"api.example.test", "alias.example.test"}, false)
	idpCert, idpPEM := scCert(t, 22, []string{"idp.example.test"}, false)
	w.apiPEM, w.idpPEM = apiPEM, idpPEM
	pick := func(name string) *tls.Certificate {
		if name == "api" {
			return &apiCert
		}
		return &idpCert
	}
	w.idpHandler = func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"access_token":"` + hxtToken + `","expires_in":3600}`))
	}
	w.apiHandler = func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+hxtToken {
			rw.WriteHeader(401)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"items":[{"serial":"s1"}]}`))
	}
	w.apiPort = hxtServe(t, pick(apiBy), w.apiSeen, func(rw http.ResponseWriter, r *http.Request) { w.apiHandler(rw, r) })
	var idpCertPtr *tls.Certificate
	if !plainIdp {
		idpCertPtr = pick(idpBy)
	}
	w.idpPort = hxtServe(t, idpCertPtr, w.idpSeen, func(rw http.ResponseWriter, r *http.Request) { w.idpHandler(rw, r) })
	return w
}

func hxtAddresses(w *hxtWorld) []secrets.Address {
	return []secrets.Address{{Host: "api.example.test", Port: w.apiPort}, {Host: "idp.example.test", Port: w.idpPort}, {Host: "alias.example.test", Port: w.apiPort}}
}

// hxtReader makes the reader of a definition whose token is asked for at tokenURL.
func hxtReader(t *testing.T, w *hxtWorld, tokenURL, connExtra, reqExtra, pagination string, allowed []secrets.Address) (*httpsource.Reader, ingest.Dataset) {
	t.Helper()
	auth := `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"request":{"url":"` + tokenURL + `","format":"form"` + reqExtra +
		`,"body":{"grant_type":"client_credentials","client_id":"{{credential.username}}","client_secret":"{{credential.password}}"}},"token_path":"access_token","expires_in_path":"expires_in"}`
	ds := `{"id":"ds_servers","request":{"path":"/servers"},"records_path":"items"` + pagination + `,"natural_key":["serial"],"columns":[{"name":"serial"}]}`
	doc := `{"version":1,"id":"` + hxtSource + `","adapter":"http","connection":{"base_url":"https://api.example.test:` + strconv.Itoa(w.apiPort) + `/api/v2","requests_per_second":1000,"headers":{"X-Tenant":"hxt-tenant-value"}` + connExtra +
		`,"auth":` + auth + `},"datasets":[` + ds + `]}`
	src, err := ingest.ParseAdapterSource(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("the test definition does not parse: %v", err)
	}
	clock := kernel.NewManualClock(scEpoch)
	svc, err := secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), Keys: secrets.StaticKey(secrets.GenerateMasterKey(kernel.NewSeededBytes(9))), Clock: clock, Bytes: kernel.NewSeededBytes(3)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(scCtx, secrets.CreateInput{ID: "sec_pw", Kind: secrets.KindPassword, Fields: secrets.NewFields(map[string]string{"username": hxtUser, "password": hxtPass}),
		Scope: []string{hxtSource}, AllowedHosts: allowed}); err != nil {
		t.Fatal(err)
	}
	d, err := egress.New(egress.Config{Policy: w.policy, Resolver: w.names})
	if err != nil {
		t.Fatal(err)
	}
	rd, err := httpsource.New(httpsource.Config{
		Source: src, Secrets: svc.Opener(), Connect: httpsource.ViaDialer(d), Clock: clock, Rng: kernel.NewSeededRng(1),
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return rd, src.Datasets[0]
}

// hxtRead builds the reader and reads the dataset to its end.
func hxtRead(t *testing.T, w *hxtWorld, tokenURL, connExtra, reqExtra, pagination string, allowed []secrets.Address) (rows int, err error) {
	t.Helper()
	rd, ds := hxtReader(t, w, tokenURL, connExtra, reqExtra, pagination, allowed)
	for p, perr := range rd.ReadPages(scCtx, hxtSource, ds, ingest.PageOptions{}) {
		if perr != nil {
			return rows, perr
		}
		rows += len(p.Rows)
	}
	return rows, nil
}

func hxtKind(err error) string {
	if err == nil {
		return ""
	}
	if se, ok := ingest.SourceErrorOf(err); ok {
		return se.Kind
	}
	return "other:" + err.Error()
}

func hxtPEM(pem string) string {
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(pem)
	return `,"ca_pem":"` + q + `"`
}

func hxtTokenURL(host string, port int) string {
	return "https://" + host + ":" + strconv.Itoa(port) + "/oauth2/token"
}

// [REQ-826] Each host is trusted by its own authority: the API by the ca_pem of the connection, the token server by the one
// of the token request. The credentials go to the token server only, and the token and the headers of the connection to the API only.
func TestHxtEachHostGetsItsOwnAuthority(t *testing.T) {
	w := hxtNewWorld(t, "api", "idp", false)
	rows, err := hxtRead(t, w, hxtTokenURL("idp.example.test", w.idpPort), hxtPEM(w.apiPEM), hxtPEM(w.idpPEM), "", hxtAddresses(w))
	if err != nil || rows != 1 {
		t.Fatalf("%d rows, %v", rows, err)
	}
	if hxtHas(w.apiSeen, hxtPass, hxtUser) || hxtHas(w.idpSeen, hxtToken, "Bearer", "hxt-tenant-value") {
		t.Error("LEAK: a credential at the API, or the token or a header of the connection at the token server")
	}
	if !hxtHas(w.idpSeen, "POST idp.example.test:"+strconv.Itoa(w.idpPort)) || !hxtHas(w.apiSeen, "GET api.example.test:"+strconv.Itoa(w.apiPort)) {
		t.Errorf("Host headers: idp=%v api=%v", w.idpSeen.reqs, w.apiSeen.reqs)
	}
}

// [REQ-826] The ca_pem of the connection is not trusted for the token server: its certificate is of the API's authority and the
// token request gives none. The token server is refused and the credentials are not sent.
func TestHxtTheAuthorityOfTheAPIIsNotTrustedForTheTokenServer(t *testing.T) {
	w := hxtNewWorld(t, "api", "api", false)
	_, err := hxtRead(t, w, hxtTokenURL("idp.example.test", w.idpPort), hxtPEM(w.apiPEM), "", "", hxtAddresses(w))
	if hxtKind(err) != ingest.SrcTLSRejected || hxtCount(w.idpSeen) != 0 || hxtCount(w.apiSeen) != 0 {
		t.Errorf("kind=%s idp=%d api=%d", hxtKind(err), hxtCount(w.idpSeen), hxtCount(w.apiSeen))
	}
}

// [REQ-826] The ca_pem of the token request is not trusted for the API.
func TestHxtTheAuthorityOfTheTokenServerIsNotTrustedForTheAPI(t *testing.T) {
	w := hxtNewWorld(t, "idp", "idp", false)
	_, err := hxtRead(t, w, hxtTokenURL("idp.example.test", w.idpPort), "", hxtPEM(w.idpPEM), "", hxtAddresses(w))
	if hxtKind(err) != ingest.SrcTLSRejected || hxtCount(w.apiSeen) != 0 {
		t.Errorf("kind=%s idp=%d api=%d", hxtKind(err), hxtCount(w.idpSeen), hxtCount(w.apiSeen))
	}
}

// [REQ-826] A redirect of the token server is not followed (307 and 308 keep the body): the credentials never reach the API.
func TestHxtARedirectOfTheTokenServerIsNotFollowed(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		w := hxtNewWorld(t, "api", "idp", false)
		w.idpHandler = func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Location", "https://api.example.test:"+strconv.Itoa(w.apiPort)+"/steal")
			rw.WriteHeader(code)
		}
		_, err := hxtRead(t, w, hxtTokenURL("idp.example.test", w.idpPort), hxtPEM(w.apiPEM), hxtPEM(w.idpPEM), "", hxtAddresses(w))
		if hxtKind(err) != ingest.SrcRequestRejected || hxtCount(w.apiSeen) != 0 || hxtCount(w.idpSeen) != 1 {
			t.Errorf("%d: kind=%s idp=%d api=%d", code, hxtKind(err), hxtCount(w.idpSeen), hxtCount(w.apiSeen))
		}
	}
}

// [REQ-826] Another name of the same address and port as the API is its own host: the token request is judged under its own name,
// so with no authority of its own it is refused, and a secret that does not allow that name is not opened for it.
func TestHxtAnAliasOfTheAPIsAddressIsItsOwnHost(t *testing.T) {
	w := hxtNewWorld(t, "api", "idp", false)
	_, err := hxtRead(t, w, hxtTokenURL("alias.example.test", w.apiPort), hxtPEM(w.apiPEM), "", "", hxtAddresses(w))
	if hxtKind(err) != ingest.SrcTLSRejected || hxtCount(w.apiSeen) != 0 {
		t.Errorf("no authority for the token request: kind=%s api=%d", hxtKind(err), hxtCount(w.apiSeen))
	}
	w = hxtNewWorld(t, "api", "idp", false)
	only := []secrets.Address{{Host: "api.example.test", Port: w.apiPort}}
	_, err = hxtRead(t, w, hxtTokenURL("alias.example.test", w.apiPort), hxtPEM(w.apiPEM), hxtPEM(w.apiPEM), "", only)
	if hxtKind(err) != ingest.SrcSecretUnusable || hxtCount(w.apiSeen) != 0 {
		t.Errorf("a secret that does not allow the alias: kind=%s api=%d", hxtKind(err), hxtCount(w.apiSeen))
	}
}

// [REQ-826] The API names the token server as the next page (a Link header, a next_url): the token does not go there. The next
// page is a request of the API and is refused as one.
func TestHxtANextPageAtTheTokenServerIsNotFollowed(t *testing.T) {
	for _, c := range []struct{ name, pagination string }{
		{"link", `,"pagination":{"kind":"link_header"}`},
		{"next_url", `,"pagination":{"kind":"next_url","next_path":"next"}`},
	} {
		w := hxtNewWorld(t, "api", "idp", false)
		target := hxtTokenURL("idp.example.test", w.idpPort)
		w.apiHandler = func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.Header().Set("Link", `<`+target+`>; rel="next"`)
			_, _ = rw.Write([]byte(`{"items":[{"serial":"s1"}],"next":"` + target + `"}`))
		}
		_, err := hxtRead(t, w, target, hxtPEM(w.apiPEM), hxtPEM(w.idpPEM), c.pagination, hxtAddresses(w))
		if hxtHas(w.idpSeen, hxtToken, "Bearer") || hxtCount(w.idpSeen) != 1 {
			t.Errorf("%s: the token went to the token server (%d requests there)", c.name, hxtCount(w.idpSeen))
		}
		if k := hxtKind(err); k != ingest.SrcPolicyDenied {
			t.Errorf("%s: the read ended with %q", c.name, k)
		}
	}
}

// [REQ-820] [REQ-821] A token server that is not encrypted (http://) is asked only where the policy allows plaintext: on the
// loopback here. The check says that the secret crosses the network without encryption, though the API has TLS.
func TestHxtATokenServerInThePlaintextRangeIsAskedAndTheCheckSaysItIsNotEncrypted(t *testing.T) {
	w := hxtNewWorld(t, "api", "idp", true)
	tokenURL := "http://idp.example.test:" + strconv.Itoa(w.idpPort) + "/oauth2/token"
	rows, err := hxtRead(t, w, tokenURL, hxtPEM(w.apiPEM), "", "", hxtAddresses(w))
	if err != nil || rows != 1 {
		t.Fatalf("%d rows, %v", rows, err)
	}
	if !hxtHas(w.idpSeen, "POST idp.example.test:"+strconv.Itoa(w.idpPort)) || hxtHas(w.apiSeen, hxtPass) {
		t.Errorf("idp=%v", w.idpSeen.reqs)
	}
	rd, ds := hxtReader(t, w, tokenURL, hxtPEM(w.apiPEM), "", "", hxtAddresses(w))
	res := rd.Check(scCtx, hxtSource, ds)
	found := false
	for _, n := range res.Notes {
		found = found || n.Code == ingest.NotePlaintext
	}
	if res.Kind != ingest.CheckOK || !found {
		t.Errorf("the check: %+v", res)
	}
}

// [REQ-820] The same token server at an address that is not in the plaintext range (not the loopback, no range given) is not
// connected to: the policy refuses it and the credentials are not sent, whatever the API has.
func TestHxtATokenServerOutsideThePlaintextRangeIsRefused(t *testing.T) {
	w := hxtNewWorld(t, "api", "idp", true)
	w.names["idp.example.test"] = "10.9.9.9"
	tokenURL := "http://idp.example.test:" + strconv.Itoa(w.idpPort) + "/oauth2/token"
	_, err := hxtRead(t, w, tokenURL, hxtPEM(w.apiPEM), "", "", hxtAddresses(w))
	if hxtKind(err) != ingest.SrcPolicyDenied || hxtCount(w.idpSeen) != 0 || hxtCount(w.apiSeen) != 0 {
		t.Errorf("kind=%s idp=%d api=%d", hxtKind(err), hxtCount(w.idpSeen), hxtCount(w.apiSeen))
	}
}
