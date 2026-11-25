package httpsource

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// The tests that keep what the review of the token host (ADR-0106) found: where each request goes, a request that is not sent
// at all, one origin judged one way, and the small things about the lifetime and the count of the token requests. Every name
// begins with hxc.

// [REQ-841]
// The address a request is handed to the client with is the address of its own host: the token request is at the token host
// and a request of the API at the API's, whichever client the request went through.
func TestHxcTheAddressOfEachRequestIsItsOwnHost(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	if len(g.hxcIdp.hsSeen) != 1 || len(g.hxcAPI.hsSeen) == 0 {
		t.Fatalf("%d requests to the authentication server, %d to the API", len(g.hxcIdp.hsSeen), len(g.hxcAPI.hsSeen))
	}
	for _, r := range g.hxcIdp.hsSeen {
		if r.hsURL != "https://"+hxcIdpHost+"/oauth2/token" {
			t.Errorf("the token request was handed over with %q", r.hsURL)
		}
	}
	for _, r := range g.hxcAPI.hsSeen {
		if !strings.HasPrefix(r.hsURL, "https://"+hxcAPIHost+"/api/v2/servers?") {
			t.Errorf("a request of the API was handed over with %q", r.hsURL)
		}
	}
}

// [REQ-841]
// A value of the secret in a request of the API, when the token is asked for at another host, is refused before the token
// request is sent: no request goes to either host, the secret is not opened, and a check and a preview refuse it the same way.
func TestHxcASecretInARequestOfTheAPIIsRefusedBeforeTheTokenRequest(t *testing.T) {
	byBody := func() *hxcRig {
		g := hxcNew(t, hxcDoc(hxcOAuthPost, "", hxcPostDataset(`{"k":"v"}`)), hxcHosts, "3600")
		g.hxcDef.Datasets[0].Request.Body = map[string]any{"k": []any{map[string]any{"deep": "{{credential.password}}"}}}
		g.hxcMake()
		return g
	}
	byHeader := func() *hxcRig {
		g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
		g.hxcDef.Connection.Auth.Headers = []ingest.SecretHeader{{Name: "X-Client", From: "credential.username"}}
		g.hxcMake()
		return g
	}
	for _, c := range []struct {
		name  string
		build func() *hxcRig
	}{{"a body", byBody}, {"a header", byHeader}} {
		name, build := c.name, c.build
		g := build()
		_, err := g.hxcRead()
		if hxcKind(t, err) != ingest.SrcSecretUnusable || len(g.hxcIdp.hsSeen) != 0 || len(g.hxcAPI.hsSeen) != 0 || len(g.hxcHs.hsUses) != 0 {
			t.Errorf("%s, read: %v, %d requests to the authentication server, %d to the API, %d openings", name, err, len(g.hxcIdp.hsSeen), len(g.hxcAPI.hsSeen), len(g.hxcHs.hsUses))
		}
		g = build()
		if res := g.hxcReader.Check(hsCtx, hsSource, g.hxcDef.Datasets[0]); res.Kind != ingest.CheckConfig || len(g.hxcIdp.hsSeen)+len(g.hxcAPI.hsSeen) != 0 {
			t.Errorf("%s, check: %q, %d requests", name, res.Kind, len(g.hxcIdp.hsSeen)+len(g.hxcAPI.hsSeen))
		}
		g = build()
		if _, err := g.hxcReader.Preview(hsCtx, hsSource, g.hxcDef.Datasets[0], ingest.PreviewOptions{}); err == nil || len(g.hxcIdp.hsSeen)+len(g.hxcAPI.hsSeen) != 0 {
			t.Errorf("%s, preview: %v, %d requests", name, err, len(g.hxcIdp.hsSeen)+len(g.hxcAPI.hsSeen))
		}
	}
	// The token in a body of the API is what it is for, and the read goes on.
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", hxcPostDataset(`{"k":"{{auth.token}}"}`)), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil {
		t.Errorf("the token in a body of the API: %v", err)
	}
}

// [REQ-840]
// The definition check and the adapter tell one origin the same way: an address that the check lets in is read by the adapter
// and is the same origin or another there as it is in the check, so that a ca_pem that the check allows is the one the adapter
// uses; an address that is written another way than the client reads it is refused by the check.
func TestHxcTheCheckAndTheAdapterTellTheOriginTheSameWay(t *testing.T) {
	ca := strconv.Quote(hxcCert)
	for _, c := range []struct {
		base, url string
		accepted  bool
	}{
		{"https://api.example.test", "https://api.example.test/t", true},
		{"https://api.example.test", "https://API.Example.Test/t", true},
		{"https://api.example.test", "https://api.example.test:443/t", true},
		{"https://api.example.test:443", "https://api.example.test/t", true},
		{"https://api.example.test", "HTTPS://api.example.test/t", true},
		{"https://api.example.test", "http://api.example.test:443/t", true},
		{"https://api.example.test", "https://api.example.test:8443/t", true},
		{"https://api.example.test", "https://api.example.test.evil.test/t", true},
		{"https://api.example.test", "https://xn--pi-xia.example.test/t", true},
		{"https://[::1]:8443", "https://[::1]:8443/t", true},
		{"https://[0:0:0:0:0:0:0:1]:8443", "https://[::1]:8443/t", true},
		{"https://api.example.test.", "https://api.example.test/t", true},
		{"https://api.example.test:0443", "https://api.example.test/t", true},
		{"https://127.0.0.1", "https://[::ffff:127.0.0.1]/t", true},
		{"https://api.example.test", "https://api.example.test./t", false},
		{"https://api.example.test", "https://api.example.test:0443/t", false},
		{"https://api.example.test", "https://[0:0:0:0:0:0:0:1]/t", false},
		{"https://api.example.test", "https://[::1%25eth0]/t", false},
		{"https://127.0.0.1", "https://127.1/t", false},
		{"https://127.0.0.1", "https://2130706433/t", false},
		{"https://127.0.0.1", "https://0x7f.0.0.1/t", false},
		{"https://127.0.0.1", "https://0177.0.0.1/t", false},
		{"https://api.example.test", "https://аpi.example.test/t", false},
		{"https://api.example.test", "https://a..example.test/t", false},
		{"https://api.example.test", "https://%61pi.example.test/t", false},
		{"https://api.example.test", "https://" + strings.Repeat("a", 64) + ".example.test/t", false},
		{"https://api.example.test", "https://api.example.test:99999/t", false},
		{"https://api.example.test", "https://api.example.test:0/t", false},
	} {
		for _, withCA := range []bool{false, true} {
			request := strings.Replace(hxcOAuthPost, "https://idp.example.test/oauth2/token", c.url, 1)
			if withCA {
				request = strings.Replace(request, `"format":"form"`, `"ca_pem":`+ca+`,"format":"form"`, 1)
			}
			doc := strings.Replace(hxcDoc(request, "", ""), "https://api.example.test/api/v2", c.base+"/api/v2", 1)
			def, err := ingest.ParseAdapterSource(strings.NewReader(doc))
			if err != nil {
				// The check refuses an address that is not written the way the client reads it, and a ca_pem for the
				// origin of the base address (it would be read by nobody). Any other refusal is wrong.
				if c.accepted && !withCA {
					t.Errorf("%s %s: refused: %v", c.base, c.url, err)
				}
				continue
			}
			if !c.accepted {
				t.Errorf("%s %s (ca_pem %v): the check lets in an address that is not written the way the client reads it", c.base, c.url, withCA)
			}
			set, serr := readSettings(def)
			cross := ingest.TokenGoesToAnotherOrigin(def)
			if serr != nil || set.token.cross != cross {
				t.Errorf("%s %s (ca_pem %v): the check says another origin %v, the adapter %v (%v)", c.base, c.url, withCA, cross, set.token.cross, serr)
			}
			if withCA && set.token.caPEM == "" {
				t.Errorf("%s %s: the check let a ca_pem in and the adapter reads none", c.base, c.url)
			}
		}
	}
}

// [REQ-840]
// A port that is not from 1 to 65535 is no port of the adapter either, for the base address and for the token request: a
// definition that is made by hand does not get a reader.
func TestHxcAPortThatIsNoPortIsRefusedByTheReader(t *testing.T) {
	for _, port := range []string{"0", "65536", "99999", "99999999999999999999"} {
		def := ingest.Source{ID: hsSource, Adapter: ingest.AdapterHTTP, Connection: &ingest.Connection{BaseURL: "https://api.example.test:" + port}}
		if _, err := readSettings(def); err == nil {
			t.Errorf("a base address with port %s was read", port)
		}
		def = ingest.Source{ID: hsSource, Adapter: ingest.AdapterHTTP, Connection: &ingest.Connection{BaseURL: "https://api.example.test",
			Auth: &ingest.Auth{Kind: ingest.AuthTokenRequest, Credential: &ingest.SecretRef{SecretID: "sec_pw"}, Request: &ingest.TokenRequest{URL: "https://idp.example.test:" + port + "/t"}}}}
		if _, err := readSettings(def); err == nil {
			t.Errorf("a token address with port %s was read", port)
		}
	}
	def := ingest.Source{ID: hsSource, Adapter: ingest.AdapterHTTP, Connection: &ingest.Connection{BaseURL: "https://api.example.test:65535"}}
	if set, err := readSettings(def); err != nil || set.port != 65535 {
		t.Errorf("port 65535: %v", err)
	}
}

// [REQ-842]
// A token that ran out while the body of an answer was read is asked for again before the request is sent again, as before
// every request: the source that refuses a token past its time is not given the old one.
func TestHxcARequestThatIsSentAgainAfterABrokenBodyHasATokenThatHolds(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "0")
	var issuedAt time.Time
	g.hxcIdp.hsHandle = func(int, hsRequest) hsReply {
		g.hxcIssued++
		g.hxcLast, issuedAt = hxcMinted(g.hxcIssued), g.hxcClock.Now()
		return hsJSON(`{"access_token":"` + g.hxcLast + `","expires_in":0}`)
	}
	pages := hsAPI(ingest.PagePage, 1, 1, 1)
	g.hxcAPI.hsHandle = func(n int, r hsRequest) hsReply {
		if _, err := g.hxcClock.Advance(28 * time.Second); err != nil {
			t.Fatal(err)
		}
		// The source holds a token for 30 seconds, as the lifetime of the answer says at least, and the clock moves 28 at every request.
		if r.hsFirst("Authorization") != "Bearer "+g.hxcLast || !g.hxcClock.Now().Before(issuedAt.Add(30*time.Second)) {
			return hsStatus(401)
		}
		rep := pages(n, r)
		if n == 1 {
			rep.hsReadErr = &egress.Error{Kind: egress.KindConnect, Reason: "the connection failed or broke"}
		}
		return rep
	}
	if _, err := g.hxcRead(); err != nil {
		t.Fatalf("the read: %v", err)
	}
	if g.hxcIssued != 2 || len(g.hxcAPI.hsSeen) != 2 {
		t.Errorf("%d tokens, %d requests to the API", g.hxcIssued, len(g.hxcAPI.hsSeen))
	}
}

// [REQ-842]
// A definition that names where the lifetime is, and an answer that has none there, is told once in a read, with the words of
// the reader and no value of the answer; an answer that has it says nothing.
func TestHxcALifetimeThatCannotBeReadIsToldOnceInARead(t *testing.T) {
	count := func(g *hxcRig) (n int) {
		for _, e := range g.hxcEvents {
			if e.Kind == lifeUnread {
				n++
				if e.Reason == "" || strings.Contains(e.Reason, "not-a-number") || strings.Contains(e.Reason, hxcMinted(1)) {
					t.Errorf("the event says %q", e.Reason)
				}
			}
		}
		return n
	}
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	g.hxcIdp.hsHandle = func(int, hsRequest) hsReply {
		g.hxcIssued++
		g.hxcLast = hxcMinted(g.hxcIssued)
		return hsJSON(`{"access_token":"` + g.hxcLast + `","expires_in":"not-a-number"}`)
	}
	pages := hsAPI(ingest.PagePage, 5, 1, 1)
	g.hxcAPI.hsHandle = func(n int, r hsRequest) hsReply {
		if n == 1 {
			return hsStatus(401) // the token is asked for once more, and the event is not told twice
		}
		return pages(n, r)
	}
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	if g.hxcIssued != 2 || count(g) != 1 {
		t.Errorf("%d tokens, %d events", g.hxcIssued, count(g))
	}
	g = hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil || count(g) != 0 {
		t.Errorf("a lifetime that is read: %v, %d events", err, count(g))
	}
	g = hxcNew(t, hxcDoc(strings.Replace(hxcOAuthPost, `,"expires_in_path":"expires_in"`, "", 1), "", ""), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil || count(g) != 0 {
		t.Errorf("a definition that names no lifetime: %v, %d events", err, count(g))
	}
}

// [REQ-842]
// A read that ends at max_pages says how many of the requests it counted were for a token.
func TestHxcTheReasonOfMaxPagesSaysHowManyRequestsWereForATokenToo(t *testing.T) {
	doc := hxcDoc(hxcOAuthPost, "", strings.Replace(hxcServers, `"total_path":"pageInfo.totalCount"`, `"total_path":"pageInfo.totalCount","max_pages":3`, 1))
	g := hxcNew(t, doc, hxcHosts, "3600")
	pages, err := g.hxcRead()
	if err != nil {
		t.Fatal(err)
	}
	reason := hsLast(t, pages).End.Reason
	if !strings.Contains(reason, "max_pages") || !strings.Contains(reason, "1 of those requests asked for a token") {
		t.Errorf("%q", reason)
	}
	// A source that asks for no token at another host has the reason it always had.
	g = hxcNew(t, hxcDoc(`{"kind":"bearer","credential":{"secret_id":"sec_key"}}`, "", strings.Replace(hxcServers, `"total_path":"pageInfo.totalCount"`, `"total_path":"pageInfo.totalCount","max_pages":3`, 1)), hxcHosts, "3600")
	g.hxcLast = hsTokenValue // the API gives its pages to the token it is sent, which is the secret's own here
	pages, err = g.hxcRead()
	if err != nil {
		t.Fatal(err)
	}
	if reason := hsLast(t, pages).End.Reason; strings.Contains(reason, "token") {
		t.Errorf("%q", reason)
	}
}
