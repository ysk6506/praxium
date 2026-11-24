package httpsource

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The rig of the tests of a token request that goes to another host than the API's (ADR-0106): two fake origins, an API and
// an authentication server, that a reader reaches through two clients, each of which keeps what it was made with. Every
// name here begins with hxc so that it never meets a name of the older tests of this folder; the values are made up.

const (
	hxcAPIHost = "api.example.test"
	hxcIdpHost = "idp.example.test"
	hxcRealm   = "realm-not-a-secret"
)

func hxcMinted(n int) string { return fmt.Sprintf("minted-not-a-real-secret-%04d", n) }

// hxcRig is a reader of a source whose token is asked for at another host than the API's.
type hxcRig struct {
	hxcT       *testing.T
	hxcAPI     *hsServer
	hxcIdp     *hsServer
	hxcClock   *kernel.ManualClock
	hxcEvents  []ingest.Event
	hxcOptions []egress.ClientOptions
	hxcDef     ingest.Source
	hxcReader  *Reader
	hxcHs      *hsRig
	// hxcIssued is how many tokens the authentication server gave, and hxcLast the one it gave last.
	hxcIssued int
	hxcLast   string
	// hxcStep is how long the clock moves at every request of the API.
	hxcStep time.Duration
}

// hxcConnect makes the clients of the reader: the first of a read is the API's and the second the token host's, and each
// reaches the one fake origin it is for, so that a request that goes through the wrong client is never seen by the right one.
func (g *hxcRig) hxcConnect(opts egress.ClientOptions) (Doer, error) {
	n := len(g.hxcOptions)
	g.hxcOptions = append(g.hxcOptions, opts)
	if n%2 == 0 {
		return hsClient{hsS: g.hxcAPI}, nil
	}
	return hsClient{hsS: g.hxcIdp}, nil
}

// hxcShut is how many clients were let go of.
func (g *hxcRig) hxcShut() int { return g.hxcAPI.hsShut + g.hxcIdp.hsShut }

// hxcHosts are the addresses a secret of the rig may be used for, unless a test says another set.
var hxcHosts = []secrets.Address{{Host: hxcAPIHost, Port: 443}, {Host: hxcIdpHost, Port: 443}}

// hxcNew makes the rig. The authentication server gives a token for every request, with expires_in as a number of seconds
// (life), and the API gives its pages to the token it was given last and answers 401 to any other.
func hxcNew(t *testing.T, doc string, allowed []secrets.Address, life string) *hxcRig {
	t.Helper()
	g := &hxcRig{hxcT: t, hxcClock: kernel.NewManualClock(hsEpoch)}
	g.hxcIdp = &hsServer{hsHandle: func(n int, _ hsRequest) hsReply {
		g.hxcIssued++
		g.hxcLast = hxcMinted(g.hxcIssued)
		return hsJSON(`{"access_token":"` + g.hxcLast + `","expires_in":` + life + `,"token_type":"Bearer"}`)
	}}
	pages := hsAPI(ingest.PagePage, 5, 1, 1)
	g.hxcAPI = &hsServer{hsHandle: func(n int, r hsRequest) hsReply {
		if g.hxcStep > 0 {
			if _, err := g.hxcClock.Advance(g.hxcStep); err != nil {
				t.Fatal(err)
			}
		}
		if r.hsFirst("Authorization") != "Bearer "+g.hxcLast {
			return hsStatus(401)
		}
		return pages(n, r)
	}}
	g.hxcDef = hsParse(t, doc)
	svc, err := secrets.New(secrets.Config{Store: secrets.NewMemoryStore(), Keys: secrets.StaticKey(secrets.GenerateMasterKey(kernel.NewSeededBytes(9))), Clock: g.hxcClock, Bytes: kernel.NewSeededBytes(3)})
	if err != nil {
		t.Fatal(err)
	}
	g.hxcHs = &hsRig{hsT: t, hsSvc: svc}
	scope := []string{hsSource}
	for _, in := range []secrets.CreateInput{
		{ID: "sec_pw", Kind: secrets.KindPassword, Fields: secrets.NewFields(map[string]string{"username": hsUserValue, "password": hsPassValue}), Scope: scope, AllowedHosts: allowed},
		{ID: "sec_key", Kind: secrets.KindToken, Fields: secrets.NewFields(map[string]string{"token": hsTokenValue}), Scope: scope, AllowedHosts: allowed},
	} {
		if _, err := svc.Create(hsCtx, in); err != nil {
			t.Fatal(err)
		}
	}
	g.hxcMake()
	return g
}

// hxcMake makes the reader of the definition the rig holds.
func (g *hxcRig) hxcMake() {
	g.hxcT.Helper()
	r, err := New(Config{
		Source: g.hxcDef, Secrets: hsOpener{hsRig: g.hxcHs}, Connect: g.hxcConnect, Clock: g.hxcClock, Rng: kernel.NewSeededRng(1),
		Sleep: func(_ context.Context, d time.Duration) error {
			_, err := g.hxcClock.Advance(d)
			return err
		},
		Observer: ingest.ObserverFunc(func(e ingest.Event) { g.hxcEvents = append(g.hxcEvents, e) }),
	})
	if err != nil {
		g.hxcT.Fatal(err)
	}
	g.hxcReader = r
}

// hxcRead collects the pages of the first dataset.
func (g *hxcRig) hxcRead() ([]ingest.Page, error) {
	var pages []ingest.Page
	for p, err := range g.hxcReader.ReadPages(hsCtx, hsSource, g.hxcDef.Datasets[0], ingest.PageOptions{}) {
		if err != nil {
			return pages, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

// hxcAll is everything of a request that a server could read: its address, its headers and its body.
func hxcAll(r hsRequest) string {
	var b strings.Builder
	b.WriteString(r.hsMethod + " " + r.hsURL + "\n")
	for k, v := range r.hsHeader {
		b.WriteString(k + ": " + strings.Join(v, ",") + "\n")
	}
	b.WriteString(r.hsBody)
	return b.String()
}

// hsSeenBy tells whether any request of an origin has the text in it.
func hsSeenBy(o *hsServer, texts ...string) bool {
	for _, r := range o.hsSeen {
		for _, s := range texts {
			if strings.Contains(hxcAll(r), s) {
				return true
			}
		}
	}
	return false
}

const (
	hxcServers = `{"id":"ds_servers","request":{"path":"/servers"},"records_path":"items","pagination":{"kind":"page","page_param":"page","size_param":"pageSize","size":1,"first_page":1,"total_path":"pageInfo.totalCount"},"natural_key":["serial"],"columns":[{"name":"serial"}]}`
	// hxcPost is a dataset that reads with POST, for the tests that put a body to the API.
	hxcPost = `{"id":"ds_servers","post_reads":true,"request":{"method":"POST","path":"/servers","body":%s},"records_path":"items","natural_key":["serial"],"columns":[{"name":"serial"}]}`

	// hxcOAuthPost asks for the token by the form of OAuth 2.0 client credentials, the client in the form.
	hxcOAuthPost = `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"request":{"url":"https://idp.example.test/oauth2/token","format":"form","headers":{"X-Realm":"` + hxcRealm + `"},` +
		`"body":{"grant_type":"client_credentials","client_id":"{{credential.username}}","client_secret":"{{credential.password}}"}},"token_path":"access_token","expires_in_path":"expires_in"}`
	// hxcOAuthBasic is the same with the client in a Basic header.
	hxcOAuthBasic = `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"request":{"url":"https://idp.example.test/oauth2/token","credentials_in":"basic","format":"form","body":{"grant_type":"client_credentials"}},` +
		`"token_path":"access_token","expires_in_path":"expires_in"}`
	// hxcKeyInJSON gives a key in a JSON body and reads the token from a JSON that has it deep.
	hxcKeyInJSON = `{"kind":"token_request","credential":{"secret_id":"sec_key"},"request":{"url":"https://idp.example.test/api/login","format":"json","body":{"key":"{{credential.token}}"}},"token_path":"access_token"}`
)

// hxcDoc makes the definition of a source whose API is api.example.test, with the connection's own fields in conn.
func hxcDoc(auth, conn, dataset string) string {
	if dataset == "" {
		dataset = hxcServers
	}
	return `{"version":1,"id":"` + hsSource + `","adapter":"http","connection":{"base_url":"https://api.example.test/api/v2","requests_per_second":1000` + conn +
		`,"auth":` + auth + `},"datasets":[` + dataset + `]}`
}

func hxcPostDataset(body string) string { return strings.Replace(hxcPost, "%s", body, 1) }
