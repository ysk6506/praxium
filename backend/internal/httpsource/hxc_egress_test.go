package httpsource

import (
	"context"
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

// hxcByName answers the name of each host with an address of its own, so that a policy can refuse one of the two hosts and let
// the other in.
type hxcByName map[string]netip.Addr

func (r hxcByName) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return []netip.Addr{r[host]}, nil
}

// hxcAPIAddr is the address the name of the API has in the rig; addr, which the rig is given, is the token host's.
const hxcAPIAddr = "10.1.2.3"

// hxcReal makes the rig for a token host at an explicit port and a reader that goes through the real egress client. The name
// of the API is at an address a policy lets in, and the name of the token host is at addr, so that what a policy refuses is the
// token host alone.
func hxcReal(t *testing.T, scheme, auth string, policy egress.Policy, addr string) (*hxcRig, *Reader) {
	t.Helper()
	tokenURL := scheme + "://" + hxcIdpHost + ":" + strconv.Itoa(hxcIdpPort) + "/oauth2/token"
	a := strings.Replace(auth, "https://idp.example.test/oauth2/token", tokenURL, 1)
	doc := strings.Replace(hxcDoc(a, "", ""), "https://api.example.test/api/v2", "https://"+hxcAPIHost+":"+strconv.Itoa(hxcAPIPort)+"/api/v2", 1)
	g := hxcNew(t, doc, []secrets.Address{{Host: hxcAPIHost, Port: hxcAPIPort}, {Host: hxcIdpHost, Port: hxcIdpPort}}, "3600")
	d, err := egress.New(egress.Config{Policy: policy, Resolver: hxcByName{hxcAPIHost: netip.MustParseAddr(hxcAPIAddr), hxcIdpHost: netip.MustParseAddr(addr)}})
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

// hxcRealRefusedAt reads, and gives the kind the read ended with and the host and port of the event that says it failed.
func hxcRealRefusedAt(t *testing.T, r *Reader, g *hxcRig) (string, string, int) {
	t.Helper()
	kind := hxcRealKind(t, r, g)
	for _, e := range g.hxcEvents {
		if e.Kind == ingest.EventFailed {
			return kind, e.Host, e.Port
		}
	}
	t.Fatal("the read did not fail")
	return "", "", 0
}

// [REQ-844]
// The token request is judged by the policy of the egress like any request, and the host that is refused is the token host's
// (the API's name is at an address that each of these policies lets in, so the refusal is the token host's alone): a policy
// that allows the API's host alone, a loopback that is denied, and an address that is always refused.
func TestHxcRealEgressThePolicyJudgesTheTokenHostToo(t *testing.T) {
	for _, c := range []struct {
		name   string
		policy egress.Policy
		addr   string
	}{
		{"a policy of the API's host alone", egress.Policy{Hosts: []string{hxcAPIHost}}, "127.0.0.1"},
		{"a loopback that is denied", egress.Policy{DenyLoopback: true}, "127.0.0.1"},
		{"an address that is always refused", egress.Policy{}, "169.254.169.254"},
	} {
		g, r := hxcReal(t, "https", hxcOAuthPost, c.policy, c.addr)
		kind, host, port := hxcRealRefusedAt(t, r, g)
		if kind != ingest.SrcPolicyDenied || host != hxcIdpHost || port != hxcIdpPort {
			t.Errorf("%s: %q at %s:%d", c.name, kind, host, port)
		}
		if len(g.hxcAPI.hsSeen) != 0 || len(g.hxcIdp.hsSeen) != 0 {
			t.Errorf("%s: a request was sent", c.name)
		}
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
