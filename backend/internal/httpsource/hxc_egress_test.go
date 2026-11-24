package httpsource

import (
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The tests of this file send nothing: the real egress client judges the address of the token host before a connection is
// made, and the name of every host is answered by a fake resolver. (The trust of each host's certificate authority, the
// refusal of redirects and the plaintext rule are the egress unit's own tests; here the token host reaches that unit.)

const (
	hxcAPIPort = 8443
	hxcIdpPort = 9443
)

// hxcReal makes the rig for a token host at an explicit port and a reader that goes through the real egress client.
func hxcReal(t *testing.T, scheme, auth string, policy egress.Policy, addr string) (*hxcRig, *Reader) {
	t.Helper()
	tokenURL := scheme + "://" + hxcIdpHost + ":" + strconv.Itoa(hxcIdpPort) + "/oauth2/token"
	a := strings.Replace(auth, "https://idp.example.test/oauth2/token", tokenURL, 1)
	doc := strings.Replace(hxcDoc(a, "", ""), "https://api.example.test/api/v2", "https://"+hxcAPIHost+":"+strconv.Itoa(hxcAPIPort)+"/api/v2", 1)
	g := hxcNew(t, doc, []secrets.Address{{Host: hxcAPIHost, Port: hxcAPIPort}, {Host: hxcIdpHost, Port: hxcIdpPort}}, "3600")
	d, err := egress.New(egress.Config{Policy: policy, Resolver: hsResolver{hsAddrs: []netip.Addr{netip.MustParseAddr(addr)}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := g.hxcReader.cfg
	cfg.Connect = ViaDialer(d)
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g, r
}

func hxcRealKind(t *testing.T, r *Reader, g *hxcRig) string {
	t.Helper()
	for _, err := range r.ReadPages(hsCtx, hsSource, g.hxcDef.Datasets[0], ingest.PageOptions{}) {
		if err != nil {
			return hxcKind(t, err)
		}
	}
	return ""
}

// [REQ-844]
// The token request is judged by the policy of the egress like any request: a policy that allows the API's host alone refuses
// the token host, a denied loopback refuses it, and the secret is not sent (nothing is connected to).
func TestHxcRealEgressThePolicyJudgesTheTokenHostToo(t *testing.T) {
	g, r := hxcReal(t, "https", hxcOAuthPost, egress.Policy{Hosts: []string{hxcAPIHost}}, "127.0.0.1")
	if k := hxcRealKind(t, r, g); k != ingest.SrcPolicyDenied {
		t.Errorf("a policy of the API's host alone: %q", k)
	}
	g, r = hxcReal(t, "https", hxcOAuthPost, egress.Policy{DenyLoopback: true}, "127.0.0.1")
	if k := hxcRealKind(t, r, g); k != ingest.SrcPolicyDenied {
		t.Errorf("a loopback that is denied: %q", k)
	}
	g, r = hxcReal(t, "https", hxcOAuthPost, egress.Policy{}, "169.254.169.254")
	if k := hxcRealKind(t, r, g); k != ingest.SrcPolicyDenied {
		t.Errorf("an address that is always refused: %q", k)
	}
}

// [REQ-844]
// A token request without TLS to an address that is not the loopback is refused by the policy.
func TestHxcRealEgressATokenRequestWithoutTLSToAnAddressThatIsNotTheLoopbackIsRefused(t *testing.T) {
	g, r := hxcReal(t, "http", hxcOAuthPost, egress.Policy{}, "10.1.2.3")
	if k := hxcRealKind(t, r, g); k != ingest.SrcPolicyDenied {
		t.Errorf("%q", k)
	}
}

// [REQ-844]
// A certificate authority of the token host that cannot be used (a definition that was changed after it was checked) is a
// refusal of the token request, and nothing is sent.
func TestHxcRealEgressACertificateAuthorityThatCannotBeUsedForTheTokenHostIsRefused(t *testing.T) {
	good := strings.Replace(hxcOAuthPost, `"format":"form"`, `"ca_pem":`+strconv.Quote(hxcCert)+`,"format":"form"`, 1)
	g, r := hxcReal(t, "https", good, egress.Policy{}, "10.1.2.3")
	cfg := r.cfg
	cfg.Source.Connection.Auth.Request.CAPEM = "this is not a certificate"
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if k := hxcRealKind(t, r, g); k != ingest.SrcTLSRejected {
		t.Errorf("%q", k)
	}
}
