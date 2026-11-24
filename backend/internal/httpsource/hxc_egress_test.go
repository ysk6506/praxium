package httpsource

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// hxcTLS is a real server with a certificate of its own that counts what it is asked.
type hxcTLS struct {
	hxcPort int
	hxcHits atomic.Int32
}

func hxcServe(t *testing.T, cert tls.Certificate, h func(w http.ResponseWriter, r *http.Request)) *hxcTLS {
	t.Helper()
	s := &hxcTLS{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hxcHits.Add(1)
		h(w, r)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s.hxcPort, _ = strconv.Atoi(port)
	return s
}

// hxcReal is the reader of two real TLS servers reached through the real egress client, the names of both resolved to the
// loopback by a fake resolver.
type hxcReal struct {
	*hxcRig
	hxcAPIS, hxcIdpS *hxcTLS
	hxcReal          *Reader
}

type hxcShape struct {
	apiCA, idpCA     string // what the servers are signed by
	connCA, tokenCA  string // what the definition trusts
	redirect         bool
	policy           egress.Policy
	addr             string
	tokenURLOverride string
}

func hxcReal2(t *testing.T, sh hxcShape) *hxcReal {
	t.Helper()
	apiPEM, apiCert := hxcAuthority(t, "api ca", hxcAPIHost)
	idpPEM, idpCert := hxcAuthority(t, "idp ca", hxcIdpHost)
	var rig *hxcRig
	api := hxcServe(t, apiCert, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+hxcMinted(1) {
			w.WriteHeader(401)
			return
		}
		q := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"serial":"s`+q+`"}],"pageInfo":{"totalCount":2}}`)
	})
	idp := hxcServe(t, idpCert, func(w http.ResponseWriter, r *http.Request) {
		if sh.redirect {
			http.Redirect(w, r, "https://"+hxcAPIHost+":"+strconv.Itoa(api.hxcPort)+"/steal", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"`+hxcMinted(1)+`","expires_in":3600}`)
	})
	pick := func(set, want string) string {
		if set == "api" {
			return apiPEM
		}
		if set == "idp" {
			return idpPEM
		}
		return want
	}
	quoted := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	tokenURL := "https://" + hxcIdpHost + ":" + strconv.Itoa(idp.hxcPort) + "/oauth2/token"
	if sh.tokenURLOverride != "" {
		tokenURL = strings.Replace(sh.tokenURLOverride, "{PORT}", strconv.Itoa(idp.hxcPort), 1)
	}
	auth := strings.Replace(hxcOAuthPost, "https://idp.example.test/oauth2/token", tokenURL, 1)
	if c := pick(sh.tokenCA, ""); c != "" {
		auth = strings.Replace(auth, `"format":"form"`, `"ca_pem":`+quoted(c)+`,"format":"form"`, 1)
	}
	conn := ""
	if c := pick(sh.connCA, ""); c != "" {
		conn = `,"ca_pem":` + quoted(c)
	}
	doc := strings.Replace(hxcDoc(auth, conn, ""), "https://api.example.test/api/v2", "https://"+hxcAPIHost+":"+strconv.Itoa(api.hxcPort)+"/api/v2", 1)
	allowed := []secrets.Address{{Host: hxcAPIHost, Port: api.hxcPort}, {Host: hxcIdpHost, Port: idp.hxcPort}}
	rig = hxcNew(t, doc, allowed, "3600")
	ip := "127.0.0.1"
	if sh.addr != "" {
		ip = sh.addr
	}
	d, err := egress.New(egress.Config{Policy: sh.policy, Resolver: hsResolver{hsAddrs: []netip.Addr{netip.MustParseAddr(ip)}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := rig.hxcReader.cfg
	cfg.Connect = ViaDialer(d)
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &hxcReal{hxcRig: rig, hxcAPIS: api, hxcIdpS: idp, hxcReal: r}
}

func (g *hxcReal) hxcGo() (int, error) {
	n := 0
	for _, err := range g.hxcReal.ReadPages(hsCtx, hsSource, g.hxcDef.Datasets[0], ingest.PageOptions{}) {
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// [REQ-844]
// Each host is trusted by its own authority: the API's by connection.ca_pem and the token host's by auth.request.ca_pem, and a
// read over real TLS to both works.
func TestHxcRealTLSEachHostIsTrustedByItsOwnAuthority(t *testing.T) {
	g := hxcReal2(t, hxcShape{connCA: "api", tokenCA: "idp"})
	n, err := g.hxcGo()
	if err != nil || n == 0 || g.hxcAPIS.hxcHits.Load() == 0 || g.hxcIdpS.hxcHits.Load() != 1 {
		t.Errorf("%d pages, %v, %d hits on the API, %d on the token host", n, err, g.hxcAPIS.hxcHits.Load(), g.hxcIdpS.hxcHits.Load())
	}
}

// [REQ-844]
// The authority of one host does not trust the other: the token host signed by an authority the token request does not name
// (none, or the API's) is refused, and the API is never asked.
func TestHxcRealTLSTheAuthorityOfTheAPIDoesNotTrustTheTokenHost(t *testing.T) {
	for name, sh := range map[string]hxcShape{
		"none for the token host":       {connCA: "api"},
		"the API's for the token host":  {connCA: "api", tokenCA: "api"},
		"the token's for the API":       {connCA: "idp", tokenCA: "idp"},
		"only the token host's trusted": {tokenCA: "idp"},
	} {
		g := hxcReal2(t, sh)
		_, err := g.hxcGo()
		if name == "the token's for the API" || name == "only the token host's trusted" {
			if hxcKind(t, err) != ingest.SrcTLSRejected {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if hxcKind(t, err) != ingest.SrcTLSRejected || g.hxcAPIS.hxcHits.Load() != 0 || g.hxcIdpS.hxcHits.Load() != 0 {
			t.Errorf("%s: %v, %d hits on the API", name, err, g.hxcAPIS.hxcHits.Load())
		}
	}
}

// [REQ-844]
// A redirect from the token host is not followed, to another origin or any: the API sees nothing of it.
func TestHxcRealTLSARedirectOfTheTokenHostIsNotFollowed(t *testing.T) {
	g := hxcReal2(t, hxcShape{connCA: "api", tokenCA: "idp", redirect: true})
	_, err := g.hxcGo()
	if err == nil || g.hxcAPIS.hxcHits.Load() != 0 || g.hxcIdpS.hxcHits.Load() != 1 {
		t.Errorf("%v, %d hits on the API, %d on the token host", err, g.hxcAPIS.hxcHits.Load(), g.hxcIdpS.hxcHits.Load())
	}
}

// [REQ-844]
// The token request is judged by the policy of the egress like any request: a policy that allows the API's host alone refuses
// the token host, and nothing is sent to either.
func TestHxcRealTLSThePolicyJudgesTheTokenHostToo(t *testing.T) {
	g := hxcReal2(t, hxcShape{connCA: "api", tokenCA: "idp", policy: egress.Policy{Hosts: []string{hxcAPIHost}}})
	_, err := g.hxcGo()
	if hxcKind(t, err) != ingest.SrcPolicyDenied || g.hxcAPIS.hxcHits.Load() != 0 || g.hxcIdpS.hxcHits.Load() != 0 {
		t.Errorf("%v, %d hits on the API, %d on the token host", err, g.hxcAPIS.hxcHits.Load(), g.hxcIdpS.hxcHits.Load())
	}
	g = hxcReal2(t, hxcShape{connCA: "api", tokenCA: "idp", policy: egress.Policy{DenyLoopback: true}})
	_, err = g.hxcGo()
	if hxcKind(t, err) != ingest.SrcPolicyDenied || g.hxcAPIS.hxcHits.Load() != 0 || g.hxcIdpS.hxcHits.Load() != 0 {
		t.Errorf("a loopback that is denied: %v", err)
	}
}

// [REQ-844]
// A token request without TLS to an address that is not the loopback is refused by the policy, and the credentials are not
// sent.
func TestHxcRealTLSATokenRequestWithoutTLSToAnAddressThatIsNotTheLoopbackIsRefused(t *testing.T) {
	g := hxcReal2(t, hxcShape{connCA: "api", addr: "10.1.2.3", tokenURLOverride: "http://" + hxcIdpHost + ":{PORT}/oauth2/token"})
	_, err := g.hxcGo()
	if hxcKind(t, err) != ingest.SrcPolicyDenied || g.hxcAPIS.hxcHits.Load() != 0 || g.hxcIdpS.hxcHits.Load() != 0 {
		t.Errorf("%v", err)
	}
}
