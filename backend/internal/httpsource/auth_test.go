package httpsource

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

func hsOne(_ int, _ hsRequest) hsReply { return hsJSON(`{"items":` + hsRecords(0, 2) + `}`) }

// [REQ-662]
func TestEachKindOfAuthenticationSignsTheRequestAndNothingElseDoes(t *testing.T) {
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(hsUserValue+":"+hsPassValue))
	cases := []struct {
		name, auth, header, want string
	}{
		{"none", `{"kind":"none"}`, "Authorization", ""},
		{"bearer", hsBearer, "Authorization", "Bearer " + hsTokenValue},
		{"basic", hsBasic, "Authorization", basic},
		{"header", hsHeader, "X-Api-Key", "Key " + hsTokenValue},
		{"header without a prefix", `{"kind":"header","credential":{"secret_id":"sec_token"},"header":"X-Token"}`, "X-Token", hsTokenValue},
	}
	for _, c := range cases {
		g := hsNew(t, hsDoc(c.auth, "", "", ""), hsOne)
		if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		r := g.hsSrv.hsSeen[0]
		if got := r.hsFirst(c.header); got != c.want {
			t.Errorf("%s: the %s header is not the expected one", c.name, c.header)
		}
		if c.want == "" && r.hsFirst("Authorization") != "" {
			t.Errorf("%s signs in with nothing and sent Authorization", c.name)
		}
		if r.hsFirst("Accept") != "application/json" || r.hsFirst("Accept-Encoding") != "" || r.hsMethod != "GET" {
			t.Errorf("%s: the request: %+v", c.name, r.hsHeader)
		}
	}
}

// [REQ-662]
func TestTheHeadersOfTheConnectionGoOnEveryRequestAndTheAuthenticationWinsTheSameName(t *testing.T) {
	doc := hsDoc(hsHeader, "", "", `,"headers":{"accept":"application/vnd.test+json","X-Team":"a {{b}}","x-api-key":"from the connection"}`)
	g := hsNew(t, doc, hsOne)
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	r := g.hsSrv.hsSeen[0]
	if r.hsFirst("Accept") != "application/vnd.test+json" || r.hsFirst("X-Team") != "a {b}" || r.hsFirst("X-Api-Key") != "Key "+hsTokenValue {
		t.Fatalf("headers: %+v", r.hsHeader)
	}
	if n := len(r.hsHeader["x-api-key"]) + len(r.hsHeader["X-Api-Key"]); n != 1 {
		t.Fatalf("the header was set %d times", n)
	}
}

// [REQ-662]
func TestATokenRequestAsksForATokenOnceAndSendsItOnEveryRequestAfterwards(t *testing.T) {
	g := hsNew(t, hsDoc(hsTokReq, "", hsPageKind(2, 1, false), ""), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			return hsJSON(`{"token":"` + hsMintedValue + `","expires":3600}`)
		}
		return hsAPI(ingest.PagePage, 5, 2, 1)(n, r)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 5 {
		t.Fatalf("rows %d: %v", len(hsSerials(pages)), err)
	}
	seen := g.hsSrv.hsSeen
	tokenRequests := 0
	for _, r := range seen {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			tokenRequests++
			var body map[string]string
			if r.hsMethod != "POST" || json.Unmarshal([]byte(r.hsBody), &body) != nil || body["username"] != hsUserValue || body["password"] != hsPassValue || r.hsFirst("Authorization") != "" {
				t.Fatalf("the token request: %s %s", r.hsMethod, r.hsPath())
			}
			continue
		}
		if r.hsFirst("Authorization") != "Bearer "+hsMintedValue {
			t.Fatalf("a data request was signed with %q", "something else")
		}
	}
	if tokenRequests != 1 {
		t.Fatalf("%d token requests", tokenRequests)
	}
}

// [REQ-662]
func TestATokenRequestMayBeAFormAndMayChooseTheHeaderAndThePrefix(t *testing.T) {
	auth := `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"request":{"path":"/auth/token","format":"form","body":{"grant_type":"password","user":"{{credential.username}}","pass":"{{credential.password}}","note":"{{kept}}"}},"token_path":"data.access","send_header":"X-Auth","send_prefix":""}`
	g := hsNew(t, hsDoc(auth, "", "", ""), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			return hsJSON(`{"data":{"access":"` + hsMintedValue + `"}}`)
		}
		return hsOne(n, r)
	})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	form, err := url.ParseQuery(g.hsSrv.hsSeen[0].hsBody)
	if err != nil || form.Get("grant_type") != "password" || form.Get("user") != hsUserValue || form.Get("pass") != hsPassValue || form.Get("note") != "{kept}" {
		t.Fatalf("the form: %v", err)
	}
	if g.hsSrv.hsSeen[0].hsFirst("Content-Type") != "application/x-www-form-urlencoded" || g.hsSrv.hsSeen[1].hsFirst("X-Auth") != hsMintedValue {
		t.Fatalf("headers %+v", g.hsSrv.hsSeen[1].hsHeader)
	}
}

// [REQ-662]
func TestATokenTheSourceNoLongerAcceptsIsAskedForOnceMore(t *testing.T) {
	issued := 0
	g := hsNew(t, hsDoc(hsTokReq, "", "", ""), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			issued++
			return hsJSON(`{"token":"minted-` + string(rune('0'+issued)) + `"}`)
		}
		if r.hsFirst("Authorization") == "Bearer minted-1" {
			return hsStatus(401)
		}
		return hsOne(n, r)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 2 || issued != 2 {
		t.Fatalf("rows %d, tokens %d: %v", len(hsSerials(pages)), issued, err)
	}
	// a source that refuses every token is asked once more and no more
	issued = 0
	g = hsNew(t, hsDoc(hsTokReq, "", "", ""), func(_ int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			issued++
			return hsJSON(`{"token":"t"}`)
		}
		return hsStatus(401)
	})
	_, err = g.hsRead(ingest.PageOptions{})
	se, ok := ingest.SourceErrorOf(err)
	if !ok || se.Kind != ingest.SrcAuthFailed || issued != 2 || len(g.hsSrv.hsSeen) != 4 {
		t.Fatalf("err %v, tokens %d, requests %d", err, issued, len(g.hsSrv.hsSeen))
	}
}

// [REQ-662]
func TestTheOtherKindsStopAtOnceOnA401And403AndDoNotAskAgain(t *testing.T) {
	for _, status := range []int{401, 403} {
		g := hsNew(t, hsDoc(hsBearer, "", "", ""), func(_ int, _ hsRequest) hsReply { return hsStatus(status) })
		_, err := g.hsRead(ingest.PageOptions{})
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != ingest.SrcAuthFailed || se.Class != ingest.CheckConfig || len(g.hsSrv.hsSeen) != 1 || len(g.hsSleeps) != 0 {
			t.Fatalf("%d: %v after %d requests", status, err, len(g.hsSrv.hsSeen))
		}
	}
}

// [REQ-662]
func TestATokenRequestThatFailsIsTheSourcesRefusalOrAFormatError(t *testing.T) {
	cases := map[string]struct {
		rep  hsReply
		kind string
	}{
		"refused":          {hsStatus(401), ingest.SrcAuthFailed},
		"not JSON":         {hsJSON(`no`), ingest.SrcResponseFormat},
		"no token at path": {hsJSON(`{"other":"x"}`), ingest.SrcResponseFormat},
		"the source fails": {hsStatus(503), ingest.SrcSourceBusy},
	}
	for name, c := range cases {
		g := hsNew(t, hsDoc(hsTokReq, "", "", ""), func(_ int, _ hsRequest) hsReply { return c.rep })
		_, err := g.hsRead(ingest.PageOptions{})
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != c.kind || len(g.hsSrv.hsSeen) != 1 {
			t.Errorf("%s: %v after %d requests", name, err, len(g.hsSrv.hsSeen))
		}
	}
}

// [REQ-662]
func TestTheSecretIsOpenedForThisSourceAtTheHostAndPortOfTheRequestsAndForThePurposeOfTheRead(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", "", ""), hsOne)
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	g.hsReader.Check(hsCtx, hsSource, g.hsDef.Datasets[0])
	if _, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{}); err != nil {
		t.Fatal(err)
	}
	want := []secrets.Purpose{secrets.PurposeRun, secrets.PurposeCheck, secrets.PurposeDiscover}
	if len(g.hsUses) != 3 {
		t.Fatalf("%d opens", len(g.hsUses))
	}
	for i, use := range g.hsUses {
		if use.SourceID != hsSource || use.Host != "api.example.test" || use.Port != 443 || use.Purpose != want[i] {
			t.Errorf("open %d: %+v", i, use)
		}
	}
	if got := g.hsSrv.hsSeen[0].hsURL; !strings.HasPrefix(got, "https://api.example.test/api/v2/servers?") {
		t.Fatalf("the request went to %s", got)
	}
}

// [REQ-662]
func TestASecretThatMaySeeNoSuchUseIsTheSettingsFault(t *testing.T) {
	cases := map[string]string{
		"another source's secret":    `{"kind":"bearer","credential":{"secret_id":"sec_other"}}`,
		"a secret that is not there": `{"kind":"bearer","credential":{"secret_id":"sec_none"}}`,
		"a password for a token":     `{"kind":"bearer","credential":{"secret_id":"sec_pw"}}`,
		"a token for a password":     `{"kind":"basic","credential":{"secret_id":"sec_token"}}`,
	}
	for name, auth := range cases {
		g := hsNew(t, hsDoc(auth, "", "", ""), hsOne)
		_, err := g.hsRead(ingest.PageOptions{})
		se, ok := ingest.SourceErrorOf(err)
		if !ok || se.Kind != ingest.SrcSecretUnusable || se.Class != ingest.CheckConfig || len(g.hsSrv.hsSeen) != 0 || g.hsSrv.hsClients != 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// [REQ-662]
func TestASecretWhoseStoreFailsIsTheProductsFaultAndOneAtTheWrongAddressIsRefused(t *testing.T) {
	doc := strings.Replace(hsDoc(hsBearer, "", "", ""), "https://api.example.test/api/v2", "https://other.example.test/api/v2", 1)
	g := hsNew(t, doc, hsOne)
	_, err := g.hsRead(ingest.PageOptions{})
	if se, ok := ingest.SourceErrorOf(err); !ok || se.Kind != ingest.SrcSecretUnusable {
		t.Fatalf("a secret that is not for this address: %v", err)
	}
	if got := openError(hsCtx, errTooLarge); ingest.ClassifyCheckError(got) != ingest.CheckProduct {
		t.Fatalf("a failure of the store is %v", got)
	}
}

// [REQ-662]
func TestAGetIsTheDefaultAPostNeedsPostReadsAndAPostIsSentOnce(t *testing.T) {
	request := `{"method":"POST","path":"/servers/search","body":{"q":"x"}}`
	doc := strings.Replace(hsDoc("", request, "", ""), `"records_path":"items"`, `"records_path":"items","post_reads":true`, 1)
	g := hsNew(t, doc, func(_ int, _ hsRequest) hsReply { return hsStatus(503) })
	_, err := g.hsRead(ingest.PageOptions{})
	se, ok := ingest.SourceErrorOf(err)
	if !ok || se.Kind != ingest.SrcSourceBusy || len(g.hsSrv.hsSeen) != 1 || len(g.hsSleeps) != 0 {
		t.Fatalf("a POST that fails in a passing way: %v after %d requests", err, len(g.hsSrv.hsSeen))
	}
	if r := g.hsSrv.hsSeen[0]; r.hsMethod != "POST" || r.hsBody != `{"q":"x"}` {
		t.Fatalf("request: %+v", r)
	}
	if _, err := ingest.ParseAdapterSource(strings.NewReader(strings.Replace(doc, `,"post_reads":true`, "", 1))); err == nil {
		t.Fatal("a POST that does not say post_reads is a definition error")
	}
	if _, err := ingest.ParseAdapterSource(strings.NewReader(strings.Replace(doc, `"POST"`, `"DELETE"`, 1))); err == nil {
		t.Fatal("a method other than GET and POST is a definition error")
	}
}

// [REQ-662]
func TestTheQueryAndThePathAreMadeFromTheDefinitionWithTheBracesUnescaped(t *testing.T) {
	request := `{"path":"/servers","query":{"filter":"{{a}}","b name":"x y","note":"k&v=1"}}`
	g := hsNew(t, hsDoc("", request, "", ""), hsOne)
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	q := g.hsSrv.hsSeen[0].hsQuery()
	if q.Get("filter") != "{a}" || q.Get("b name") != "x y" || q.Get("note") != "k&v=1" {
		t.Fatalf("query %v", q)
	}
}

// [REQ-662]
func TestAnAdapterThatSignsInNeedsAWayToOpenSecretsAndAClockAndAConnector(t *testing.T) {
	g := hsNew(t, hsDoc(hsBearer, "", "", ""), hsOne)
	cfg := Config{Source: g.hsDef, Connect: g.hsSrv.hsConnect, Clock: g.hsClock, Rng: nilRng{}}
	if _, err := New(cfg); err == nil {
		t.Fatal("a source with a secret and no way to open one")
	}
	cfg.Secrets = hsOpener{hsRig: g}
	cfg.Connect = nil
	if _, err := New(cfg); err == nil {
		t.Fatal("no connector")
	}
	file := g.hsDef
	file.Adapter = ingest.AdapterFile
	if _, err := New(Config{Source: file, Connect: g.hsSrv.hsConnect, Clock: g.hsClock, Rng: nilRng{}}); err == nil {
		t.Fatal("a file source")
	}
	bad := g.hsDef
	bad.Connection = &ingest.Connection{BaseURL: "ftp://api.example.test"}
	if _, err := New(Config{Source: bad, Secrets: hsOpener{hsRig: g}, Connect: g.hsSrv.hsConnect, Clock: g.hsClock, Rng: nilRng{}}); err == nil {
		t.Fatal("a base address that is not http")
	}
	bad.Connection = &ingest.Connection{BaseURL: "https://exa mple.test"}
	if _, err := New(Config{Source: bad, Connect: g.hsSrv.hsConnect, Clock: g.hsClock, Rng: nilRng{}}); err == nil {
		t.Fatal("a host that is not a host")
	}
}

type nilRng struct{}

func (nilRng) Uint64() uint64   { return 0 }
func (nilRng) Float64() float64 { return 0 }
