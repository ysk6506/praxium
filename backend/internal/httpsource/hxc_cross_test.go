package httpsource

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/egress"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

func hxcKind(t *testing.T, err error) string {
	t.Helper()
	se, ok := ingest.SourceErrorOf(err)
	if !ok {
		t.Fatalf("not an error of the source: %v", err)
	}
	return se.Kind
}

// [REQ-841]
// The credentials go to the authentication server and to no other host, and the token to the API and to no other: each of
// the two fake origins sees what is its own and none of the other's. The headers of the connection are the API's and go
// with no token request to another host; the headers of the token request go to its host alone.
func TestHxcTheCredentialsGoToTheTokenHostAndTheTokenToTheAPIOnly(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, `,"headers":{"X-Tenant":"acme"}`, ""), hxcHosts, "3600")
	pages, err := g.hxcRead()
	if err != nil || len(hsSerials(pages)) != 5 {
		t.Fatalf("%v, %d rows", err, len(hsSerials(pages)))
	}
	if len(g.hxcIdp.hsSeen) != 1 || len(g.hxcAPI.hsSeen) != 5 {
		t.Fatalf("the authentication server saw %d requests and the API %d", len(g.hxcIdp.hsSeen), len(g.hxcAPI.hsSeen))
	}
	asked := g.hxcIdp.hsSeen[0]
	form, _ := url.ParseQuery(asked.hsBody)
	if asked.hsMethod != ingest.MethodPost || asked.hsPath() != "/oauth2/token" || asked.hsFirst("Content-Type") != "application/x-www-form-urlencoded" ||
		form.Get("client_id") != hsUserValue || form.Get("client_secret") != hsPassValue || form.Get("grant_type") != "client_credentials" {
		t.Errorf("the token request: %s %s %v", asked.hsMethod, asked.hsPath(), asked.hsHeader)
	}
	if asked.hsFirst("X-Realm") != hxcRealm || asked.hsFirst("X-Tenant") != "" || asked.hsFirst("Authorization") != "" {
		t.Errorf("the headers of the token request: %v", asked.hsHeader)
	}
	if hsSeenBy(g.hxcAPI, hsUserValue, hsPassValue, base64.StdEncoding.EncodeToString([]byte(hsUserValue+":"+hsPassValue)), hxcRealm) {
		t.Error("the API was sent what is the authentication server's: a credential or its header")
	}
	if hsSeenBy(g.hxcIdp, g.hxcLast) || hsSeenBy(g.hxcIdp, "Bearer") {
		t.Error("the authentication server was sent the token")
	}
	for _, r := range g.hxcAPI.hsSeen {
		if r.hsFirst("Authorization") != "Bearer "+hxcMinted(1) || r.hsFirst("X-Tenant") != "acme" || r.hsPath() != "/api/v2/servers" {
			t.Errorf("a request of the API: %s %v", r.hsPath(), r.hsHeader)
		}
	}
	if len(g.hxcOptions) != 2 {
		t.Errorf("%d clients were made", len(g.hxcOptions))
	}
	if g.hxcShut() != 2 {
		t.Errorf("%d clients were let go of", g.hxcShut())
	}
}

// [REQ-841]
// The other two shapes of an authentication server: the client in a Basic header (OAuth's client_secret_basic) with a form
// that has no secret in it, and a key in a JSON body that is read from a secret with a token field.
func TestHxcTheClientInABasicHeaderAndTheKeyInAJSONBodyGoToTheTokenHostToo(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthBasic, `,"headers":{"X-Tenant":"acme"}`, ""), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	asked := g.hxcIdp.hsSeen[0]
	if asked.hsFirst("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte(hsUserValue+":"+hsPassValue)) || strings.Contains(asked.hsBody, hsPassValue) || asked.hsBody != "grant_type=client_credentials" {
		t.Errorf("the Basic request: %v %q", asked.hsHeader, asked.hsBody)
	}
	if asked.hsFirst("X-Tenant") != "" || hsSeenBy(g.hxcAPI, "Basic ", hsPassValue, hsUserValue) {
		t.Error("the header of the connection went to the authentication server, or the Basic header to the API")
	}

	g = hxcNew(t, hxcDoc(hxcKeyInJSON, "", ""), hxcHosts, "3600")
	g.hxcIdp.hsHandle = func(_ int, _ hsRequest) hsReply {
		g.hxcIssued++
		g.hxcLast = hxcMinted(g.hxcIssued)
		return hsJSON(`{"access_token":"` + g.hxcLast + `"}`)
	}
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	asked = g.hxcIdp.hsSeen[0]
	if err := json.Unmarshal([]byte(asked.hsBody), &body); err != nil || body["key"] != hsTokenValue || asked.hsFirst("Content-Type") != "application/json" || asked.hsPath() != "/api/login" {
		t.Errorf("the key in a JSON body: %q %v", asked.hsBody, err)
	}
	if hsSeenBy(g.hxcAPI, hsTokenValue) {
		t.Error("the key went to the API")
	}
}

// [REQ-845]
// A secret of two hosts is opened twice, once for each: for the address of the API and for the address of the token request,
// with the source and what the read is for the same. A read that is a check or a run asks the same.
func TestHxcTheSecretIsOpenedForTheAPIAndForTheTokenHost(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	want := []secrets.Use{
		{SourceID: hsSource, Host: hxcAPIHost, Port: 443, Purpose: secrets.PurposeRun},
		{SourceID: hsSource, Host: hxcIdpHost, Port: 443, Purpose: secrets.PurposeRun},
	}
	if fmt.Sprint(g.hxcHs.hsUses) != fmt.Sprint(want) {
		t.Errorf("the secret was opened for %v, want %v", g.hxcHs.hsUses, want)
	}
	g.hxcHs.hsUses = nil
	if res := g.hxcReader.Check(hsCtx, hsSource, g.hxcDef.Datasets[0]); res.Kind != ingest.CheckOK {
		t.Fatalf("a check: %+v", res)
	}
	if len(g.hxcHs.hsUses) != 2 || g.hxcHs.hsUses[0].Purpose != secrets.PurposeCheck || g.hxcHs.hsUses[1].Host != hxcIdpHost {
		t.Errorf("a check opened the secret for %v", g.hxcHs.hsUses)
	}
	g = hxcNew(t, hxcDoc(strings.Replace(hxcOAuthPost, "https://idp.example.test/", "https://idp.example.test:8443/", 1), "", ""), []secrets.Address{{Host: hxcAPIHost, Port: 443}, {Host: hxcIdpHost, Port: 8443}}, "3600")
	g.hxcIdp.hsHandle = func(n int, _ hsRequest) hsReply { return hsJSON(`{"access_token":"` + hxcMinted(1) + `"}`) }
	g.hxcLast = hxcMinted(1)
	if _, err := g.hxcRead(); err != nil || len(g.hxcHs.hsUses) != 2 || g.hxcHs.hsUses[1].Port != 8443 {
		t.Errorf("another port: %v %v", err, g.hxcHs.hsUses)
	}
}

// [REQ-845]
// A secret that is not allowed for the address of the token host, or not for the API's, or is allowed for the host and
// another port, is not opened, and no request is made to either host: not a check, not a preview, not a read.
func TestHxcASecretThatDoesNotAllowTheTokenHostSendsNothingAnywhere(t *testing.T) {
	for name, allowed := range map[string][]secrets.Address{
		"the API only":                {{Host: hxcAPIHost, Port: 443}},
		"the token host only":         {{Host: hxcIdpHost, Port: 443}},
		"the token host's other port": {{Host: hxcAPIHost, Port: 443}, {Host: hxcIdpHost, Port: 8443}},
		"another host":                {{Host: hxcAPIHost, Port: 443}, {Host: "other.example.test", Port: 443}},
		"none":                        nil,
	} {
		g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), allowed, "3600")
		_, err := g.hxcRead()
		if hxcKind(t, err) != ingest.SrcSecretUnusable {
			t.Errorf("%s: %v", name, err)
		}
		if res := g.hxcReader.Check(hsCtx, hsSource, g.hxcDef.Datasets[0]); res.Kind != ingest.CheckConfig {
			t.Errorf("%s: a check gave %+v", name, res)
		}
		if _, err := g.hxcReader.Preview(hsCtx, hsSource, g.hxcDef.Datasets[0], ingest.PreviewOptions{}); hxcKind(t, err) != ingest.SrcSecretUnusable {
			t.Errorf("%s: a preview gave %v", name, err)
		}
		if len(g.hxcIdp.hsSeen) != 0 || len(g.hxcAPI.hsSeen) != 0 || len(g.hxcOptions) != 0 {
			t.Errorf("%s: %d requests to the authentication server, %d to the API, %d clients made", name, len(g.hxcIdp.hsSeen), len(g.hxcAPI.hsSeen), len(g.hxcOptions))
		}
	}
}

// [REQ-841]
// A token the source refuses is asked for again, once for the request, and the old token goes nowhere: the second request to
// the authentication server has no token in it and the first token is in none of its requests.
func TestHxcAfterA401TheTokenIsAskedForAgainAndTheOldOneGoesToNoServerOfTheTwo(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	api := g.hxcAPI.hsHandle
	g.hxcAPI.hsHandle = func(n int, r hsRequest) hsReply {
		if n == 2 {
			g.hxcIdp.hsSeen = g.hxcIdp.hsSeen[:len(g.hxcIdp.hsSeen):len(g.hxcIdp.hsSeen)]
			return hsStatus(401)
		}
		return api(n, r)
	}
	pages, err := g.hxcRead()
	if err != nil || len(hsSerials(pages)) != 5 {
		t.Fatalf("%v, %d rows", err, len(hsSerials(pages)))
	}
	if len(g.hxcIdp.hsSeen) != 2 {
		t.Fatalf("%d requests to the authentication server", len(g.hxcIdp.hsSeen))
	}
	if hsSeenBy(g.hxcIdp, hxcMinted(1), hxcMinted(2), "Bearer") {
		t.Error("a token was sent to the authentication server")
	}
	if got := g.hxcAPI.hsSeen[2].hsFirst("Authorization"); got != "Bearer "+hxcMinted(2) {
		t.Errorf("the request after the 401 has %q", got)
	}
}

// [REQ-842]
// A source that refuses every other token is not asked for tokens as often as for data: a read asks for a token again for at
// most maxTokenRefreshes of the refusals and then ends with the refusal, with the token requests counted.
func TestHxcTokensAskedForAgainBecauseOfA401AreWithinABudget(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	api := g.hxcAPI.hsHandle
	g.hxcAPI.hsHandle = func(n int, r hsRequest) hsReply {
		if n%2 == 1 {
			return hsStatus(401)
		}
		return api(n, r)
	}
	pages, err := g.hxcRead()
	if hxcKind(t, err) != ingest.SrcAuthFailed {
		t.Fatalf("%v", err)
	}
	if len(g.hxcIdp.hsSeen) != 1+maxTokenRefreshes {
		t.Errorf("%d token requests, %d rows; the budget is %d refreshes", len(g.hxcIdp.hsSeen), len(hsSerials(pages)), maxTokenRefreshes)
	}
	g = hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	g.hxcAPI.hsHandle = func(int, hsRequest) hsReply { return hsStatus(401) }
	if _, err := g.hxcRead(); hxcKind(t, err) != ingest.SrcAuthFailed || len(g.hxcIdp.hsSeen) != 2 || len(g.hxcAPI.hsSeen) != 2 {
		t.Errorf("a source that refuses every token: %v, %d token requests, %d requests", err, len(g.hxcIdp.hsSeen), len(g.hxcAPI.hsSeen))
	}
}

// [REQ-842]
// A token is asked for again before it ends, on the clock the reader is given: a lifetime that is a number or a text of
// digits, kept between 30 seconds and a day, with a margin of a tenth of it or 30 seconds before its end; a lifetime that is
// none, or not given, is a token that holds until the source refuses it.
func TestHxcATokenIsAskedForAgainBeforeItEndsOnTheClockOfTheReader(t *testing.T) {
	for _, c := range []struct {
		name     string
		life     string
		step     time.Duration
		requests int
		want     int
	}{
		{"an hour is held for most of it", `3600`, 1900 * time.Second, 5, 3},
		{"an hour that is not over yet", `3600`, 700 * time.Second, 5, 1},
		{"a text of digits", `"600"`, 280 * time.Second, 5, 2},
		{"a text with a fraction", `"600.5"`, 280 * time.Second, 5, 2},
		{"the shortest is 30 seconds, asked for again at 27", `1`, 20 * time.Second, 5, 3},
		{"zero is the shortest too", `0`, 20 * time.Second, 5, 3},
		{"a number under zero is the shortest", `-50`, 20 * time.Second, 5, 3},
		{"a day is the longest", `999999999`, 40000 * time.Second, 5, 2},
		{"a number in the form of a power", `1e2`, 60 * time.Second, 5, 3},
		{"a text that is no number is no lifetime", `"soon"`, 1000000 * time.Second, 5, 1},
		{"a text with a power is no number", `"1e2"`, 1000000 * time.Second, 5, 1},
		{"a text with a sign is no number", `"-5"`, 1000000 * time.Second, 5, 1},
		{"an object is no lifetime", `{"a":1}`, 1000000 * time.Second, 5, 1},
		{"null is no lifetime", `null`, 1000000 * time.Second, 5, 1},
		{"a flag is no lifetime", `true`, 1000000 * time.Second, 5, 1},
	} {
		g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, c.life)
		g.hxcStep = c.step
		if _, err := g.hxcRead(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(g.hxcAPI.hsSeen) != c.requests || len(g.hxcIdp.hsSeen) != c.want {
			t.Errorf("%s: %d tokens for %d requests, want %d", c.name, len(g.hxcIdp.hsSeen), len(g.hxcAPI.hsSeen), c.want)
		}
	}
	g := hxcNew(t, hxcDoc(strings.Replace(hxcOAuthPost, `,"expires_in_path":"expires_in"`, ``, 1), "", ""), hxcHosts, "30")
	g.hxcStep = 1000000 * time.Second
	if _, err := g.hxcRead(); err != nil || len(g.hxcIdp.hsSeen) != 1 {
		t.Errorf("a definition that names no lifetime: %v, %d token requests", err, len(g.hxcIdp.hsSeen))
	}
}

func TestHxcTheLifetimeOfATokenIsKeptBetweenTheShortestAndTheLongest(t *testing.T) {
	doc := func(js string) any {
		v, err := decodeJSON([]byte(js))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		js, path string
		want     time.Duration
	}{
		{`{"e":3600}`, "e", time.Hour - 30*time.Second},
		{`{"e":300}`, "e", 300*time.Second - 30*time.Second},
		{`{"e":100}`, "e", 90 * time.Second},
		{`{"e":40}`, "e", 36 * time.Second},
		{`{"e":10}`, "e", 27 * time.Second},
		{`{"e":0}`, "e", 27 * time.Second},
		{`{"e":99999999999}`, "e", 24*time.Hour - 30*time.Second},
		{`{"e":"3600"}`, "e", time.Hour - 30*time.Second},
		{`{"a":{"e":"120"}}`, "a.e", 108 * time.Second},
		{`{"e":3600}`, "", 0},
		{`{"e":3600}`, "x", 0},
		{`{"e":3600}`, "a[", 0},
		{`{"e":"1h"}`, "e", 0},
		{`{"e":" 5"}`, "e", 0},
		{`{"e":[5]}`, "e", 0},
		{`{"e":1e999}`, "e", 0},
	} {
		if got := lifeOf(doc(c.js), c.path); got != c.want {
			t.Errorf("%s at %q: %s, want %s", c.js, c.path, got, c.want)
		}
	}
}

// [REQ-842]
// A token has the length of a header and no more, and one that cannot be a header is refused: nothing is sent to the API with
// it, and the read ends in the shape of the answer.
func TestHxcATokenThatIsTooLongOrCannotBeAHeaderIsRefusedBeforeItIsSent(t *testing.T) {
	for name, tok := range map[string]string{
		"over the length": strings.Repeat("a", maxTokenBytes+1),
		"a line break":    `abc\r\nX-Evil: 1`,
		"a control":       `abc\u0001def`,
	} {
		g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
		g.hxcIdp.hsHandle = func(int, hsRequest) hsReply { return hsJSON(`{"access_token":"` + tok + `","expires_in":3600}`) }
		_, err := g.hxcRead()
		if hxcKind(t, err) != ingest.SrcResponseFormat || len(g.hxcAPI.hsSeen) != 0 || len(g.hxcIdp.hsSeen) != 1 {
			t.Errorf("%s: %v, %d requests to the API", name, err, len(g.hxcAPI.hsSeen))
		}
	}
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	long := strings.Repeat("b", maxTokenBytes)
	g.hxcIdp.hsHandle = func(int, hsRequest) hsReply {
		g.hxcLast = long
		return hsJSON(`{"access_token":"` + long + `","expires_in":3600}`)
	}
	if _, err := g.hxcRead(); err != nil || len(g.hxcAPI.hsSeen) != 5 {
		t.Errorf("a token of the longest length: %v", err)
	}
}

// [REQ-842]
// The token request is a request of the read: max_pages, which is the most requests one read makes, counts it. A source of
// five pages read with max_pages 3 gives the token request and two pages, and says it is not complete.
func TestHxcTheTokenRequestIsCountedInTheRequestsOfARead(t *testing.T) {
	doc := hxcDoc(hxcOAuthPost, "", strings.Replace(hxcServers, `"total_path":"pageInfo.totalCount"`, `"total_path":"pageInfo.totalCount","max_pages":3`, 1))
	g := hxcNew(t, doc, hxcHosts, "3600")
	pages, err := g.hxcRead()
	if err != nil {
		t.Fatal(err)
	}
	last := hsLast(t, pages)
	if last.End.Complete || !strings.Contains(last.End.Reason, "max_pages") || len(g.hxcAPI.hsSeen) != 2 || len(g.hxcIdp.hsSeen) != 1 {
		t.Errorf("complete %v, %q, %d requests to the API, %d to the authentication server", last.End.Complete, last.End.Reason, len(g.hxcAPI.hsSeen), len(g.hxcIdp.hsSeen))
	}
}

// [REQ-844]
// The events and the exchanges of the token request say the host it went to: the events of what happens with the token
// request have the authentication server's host and port, the events of the data the API's, and the failure of the token
// request is the authentication server's.
func TestHxcTheEventsOfTheTokenRequestSayTheHostItWentTo(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	var token, data int
	for _, e := range g.hxcEvents {
		switch {
		case e.Path == "/oauth2/token":
			token++
			if e.Host != hxcIdpHost || e.Port != 443 {
				t.Errorf("an event of the token request says %s:%d", e.Host, e.Port)
			}
		case e.Kind == ingest.EventRequest || e.Kind == ingest.EventResponse:
			data++
			if e.Host != hxcAPIHost || e.Port != 443 {
				t.Errorf("an event of the API says %s:%d", e.Host, e.Port)
			}
		}
	}
	if token != 2 || data != 10 {
		t.Errorf("%d events of the token request and %d of the data", token, data)
	}
	pv, err := g.hxcReader.Preview(hsCtx, hsSource, g.hxcDef.Datasets[0], ingest.PreviewOptions{})
	if err != nil || len(pv.Exchanges) < 2 {
		t.Fatalf("%v, %d exchanges", err, len(pv.Exchanges))
	}
	if first, second := pv.Exchanges[0], pv.Exchanges[1]; first.Host != hxcIdpHost || first.Path != "/oauth2/token" || second.Host != hxcAPIHost || second.Port != 443 {
		t.Errorf("the exchanges: %+v %+v", first, second)
	}

	g = hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	g.hxcIdp.hsHandle = func(int, hsRequest) hsReply { return hsStatus(503) }
	_, err = g.hxcRead()
	if hxcKind(t, err) != ingest.SrcSourceBusy || !strings.Contains(err.Error(), "token request") || len(g.hxcAPI.hsSeen) != 0 || len(g.hxcIdp.hsSeen) != 1 {
		t.Errorf("the token host is busy: %v", err)
	}
	failed := g.hxcEvents[len(g.hxcEvents)-1]
	if failed.Kind != ingest.EventFailed || failed.Host != hxcIdpHost {
		t.Errorf("the event of the failure: %+v", failed)
	}
}

// [REQ-844]
// A check tells the token request as an item of its own when it goes to another host, between the secret and the connection
// to the API, and a token host that does not answer is the item that fails (the API was not reached).
func TestHxcACheckTellsTheTokenRequestAsAnItemOfItsOwn(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	res := g.hxcReader.Check(hsCtx, hsSource, g.hxcDef.Datasets[0])
	var names []string
	for _, s := range res.Steps {
		names = append(names, s.Name)
	}
	if res.Kind != ingest.CheckOK || strings.Join(names, ",") != "secret,token_request,connect,sign_in,first_response,records_path" {
		t.Errorf("%+v", res)
	}
	g = hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	g.hxcIdp.hsHandle = func(int, hsRequest) hsReply { return hsStatus(503) }
	res = g.hxcReader.Check(hsCtx, hsSource, g.hxcDef.Datasets[0])
	if res.Kind != ingest.CheckTransient || len(res.Steps) != 2 || res.Steps[1].Name != StepToken || res.Steps[1].Kind == ingest.CheckOK || len(g.hxcAPI.hsSeen) != 0 {
		t.Errorf("%+v", res)
	}
	g = hxcNew(t, hxcDoc(strings.Replace(hxcOAuthPost, "https://idp.example.test/", "https://api.example.test/", 1), "", ""), []secrets.Address{{Host: hxcAPIHost, Port: 443}}, "3600")
	g.hxcAPI.hsHandle = func(n int, r hsRequest) hsReply {
		if r.hsPath() == "/oauth2/token" {
			return hsJSON(`{"access_token":"` + hxcMinted(1) + `"}`)
		}
		g.hxcLast = hxcMinted(1)
		return hsAPI(ingest.PagePage, 5, 1, 1)(n, r)
	}
	g.hxcLast = hxcMinted(1)
	res = g.hxcReader.Check(hsCtx, hsSource, g.hxcDef.Datasets[0])
	for _, s := range res.Steps {
		if s.Name == StepToken {
			t.Errorf("a token request to the API's own origin has an item of its own: %+v", res)
		}
	}
}

// [REQ-844]
// Each host has its own certificate authority: the API's is the connection's ca_pem and the token host's is the
// auth.request.ca_pem, and the clients are made with those; a token host that gives none is trusted by the system's
// authorities and not by the API's. The timeout of the connection is the timeout of both.
func TestHxcEachHostHasItsOwnCertificateAuthority(t *testing.T) {
	apiCA, idpCA := hxcCert+"\n", hxcCert
	quoted := func(pem string) string { b, _ := json.Marshal(pem); return string(b) }
	both := strings.Replace(hxcOAuthPost, `"format":"form"`, `"ca_pem":`+quoted(idpCA)+`,"format":"form"`, 1)
	g := hxcNew(t, hxcDoc(both, `,"timeout_seconds":7,"ca_pem":`+quoted(apiCA), ""), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	if len(g.hxcOptions) != 2 || g.hxcOptions[0].CAPEM != apiCA || g.hxcOptions[1].CAPEM != idpCA || g.hxcOptions[0].Timeout != 7*time.Second || g.hxcOptions[1].Timeout != 7*time.Second {
		t.Errorf("the clients were made with %d options", len(g.hxcOptions))
	}
	g = hxcNew(t, hxcDoc(hxcOAuthPost, `,"ca_pem":`+quoted(apiCA), ""), hxcHosts, "3600")
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	if g.hxcOptions[0].CAPEM != apiCA || g.hxcOptions[1].CAPEM != "" {
		t.Errorf("the certificate authority of the API was given to the token host: %q", g.hxcOptions[1].CAPEM)
	}
	g = hxcNew(t, hxcDoc(both, `,"ca_pem":`+quoted(apiCA), ""), hxcHosts, "3600")
	g.hxcReader.cfg.Connect = func(o egress.ClientOptions) (Doer, error) {
		if o.CAPEM == idpCA {
			return nil, errors.New("not a certificate authority")
		}
		return g.hxcConnect(o)
	}
	_, err := g.hxcRead()
	if hxcKind(t, err) != ingest.SrcTLSRejected || !strings.Contains(err.Error(), "token host") || len(g.hxcIdp.hsSeen) != 0 || len(g.hxcAPI.hsSeen) != 0 {
		t.Errorf("a certificate authority that cannot be used for the token host: %v", err)
	}
	if g.hxcShut() != 1 {
		t.Errorf("the client of the API was %d times let go of", g.hxcShut())
	}
}

// [REQ-840]
// An address of the API's own origin, written as a whole one, is the API's host: the path of it is the whole path (and not
// one under the base address), the headers of the connection go with it as with a path, there is one client and the secret
// is opened once.
func TestHxcAWholeAddressOfTheAPIsOwnOriginIsTheAPIsHost(t *testing.T) {
	doc := hxcDoc(strings.Replace(hxcOAuthPost, "https://idp.example.test/oauth2/token", "https://api.example.test/oauth2/token", 1), `,"headers":{"X-Tenant":"acme"}`, "")
	g := hxcNew(t, doc, []secrets.Address{{Host: hxcAPIHost, Port: 443}}, "3600")
	g.hxcAPI.hsHandle = func(n int, r hsRequest) hsReply {
		if r.hsPath() == "/oauth2/token" {
			g.hxcIssued++
			g.hxcLast = hxcMinted(g.hxcIssued)
			return hsJSON(`{"access_token":"` + g.hxcLast + `","expires_in":3600}`)
		}
		return hsAPI(ingest.PagePage, 5, 1, 1)(n, r)
	}
	if _, err := g.hxcRead(); err != nil {
		t.Fatal(err)
	}
	first := g.hxcAPI.hsSeen[0]
	if first.hsPath() != "/oauth2/token" || first.hsFirst("X-Tenant") != "acme" || first.hsFirst("X-Realm") != hxcRealm || len(g.hxcOptions) != 1 || len(g.hxcHs.hsUses) != 1 {
		t.Errorf("%s %v, %d clients, %d openings", first.hsPath(), first.hsHeader, len(g.hxcOptions), len(g.hxcHs.hsUses))
	}
}

// [REQ-841]
// What the definition check keeps out is kept out once more where a request is made: a definition that is built by hand and
// puts a value of the secret in a request of the API, when the token is asked for at another host, is refused, and nothing is
// sent to either host; the token is not in the token request either.
func TestHxcARequestOfTheAPIThatHasASecretIsRefusedWhereItIsMadeToo(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", hxcPostDataset(`{"k":"v"}`)), hxcHosts, "3600")
	g.hxcDef.Datasets[0].Request.Body = map[string]any{"k": "{{credential.password}}"}
	g.hxcMake()
	if _, err := g.hxcRead(); hxcKind(t, err) != ingest.SrcSecretUnusable || len(g.hxcAPI.hsSeen) != 0 {
		t.Errorf("a credential in a body of the API: %v, %d requests to the API", err, len(g.hxcAPI.hsSeen))
	}
	if hsSeenBy(g.hxcAPI, hsPassValue) {
		t.Error("the secret reached the API")
	}

	g = hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	g.hxcDef.Connection.Auth.Headers = []ingest.SecretHeader{{Name: "X-Client", From: "credential.password"}}
	g.hxcMake()
	if _, err := g.hxcRead(); hxcKind(t, err) != ingest.SrcSecretUnusable || len(g.hxcAPI.hsSeen) != 0 || hsSeenBy(g.hxcAPI, hsPassValue) {
		t.Errorf("an entry of auth.headers with a field of the secret: %v", err)
	}

	g = hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	g.hxcDef.Connection.Auth.Request.Body["x"] = "{{auth.token}}"
	g.hxcMake()
	if _, err := g.hxcRead(); hxcKind(t, err) != ingest.SrcInternal || len(g.hxcIdp.hsSeen) != 0 || len(g.hxcAPI.hsSeen) != 0 {
		t.Errorf("the token in the token request: %v", err)
	}
}

// [REQ-841]
// What a signer gives where a request of the API and the token request differ: the token request has the credentials of the
// secret opened for its host and never the token; a request of the API has the token and, when the token host is another,
// no credential.
func TestHxcASignerGivesTheTokenRequestTheCredentialsAndTheAPITheToken(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	rn, err := g.hxcReader.begin(hsCtx, hsSource, g.hxcDef.Datasets[0], secrets.PurposeRun)
	if err != nil {
		t.Fatal(err)
	}
	defer rn.close()
	sg := rn.signer
	if v, err := sg.valueIn(ingest.Placeholder{Field: "password"}, true); err != nil || v != hsPassValue {
		t.Errorf("the credentials of the token request: %q %v", v, err)
	}
	if _, err := sg.valueIn(ingest.Placeholder{Field: "password"}, false); hxcKind(t, err) != ingest.SrcSecretUnusable {
		t.Errorf("a credential for a request of the API: %v", err)
	}
	if _, err := sg.valueIn(ingest.Placeholder{Token: true}, true); err == nil {
		t.Error("the token for the token request")
	}
	if a, ok := sg.tokenAddress(); !ok || a.Host != hxcIdpHost || a.Port != 443 {
		t.Errorf("the token address: %+v", a)
	}
	if a, ok := sg.address(); !ok || a.Host != hxcAPIHost {
		t.Errorf("the address: %+v", a)
	}
	tmpl := map[string]any{"a": []any{"{{credential.username}}"}}
	if _, err := sg.fillIn(tmpl, false, true); err != nil {
		t.Errorf("a list in the token request: %v", err)
	}
	if _, err := sg.fillIn(tmpl, false, false); err == nil {
		t.Error("a list of a request of the API that has a credential")
	}
	sg.expires = time.Time{}
	h := map[string][]string{}
	if err := sg.setTokenBasic(h); err != nil || h["Authorization"][0] != "Basic "+base64.StdEncoding.EncodeToString([]byte(hsUserValue+":"+hsPassValue)) {
		t.Errorf("Basic of the token request: %v %v", h, err)
	}
}

// [REQ-844]
// A reader that reads many datasets one after the other, each with its own client and its own openings: the token host's
// client is made and let go of with the read, never kept for the next.
func TestHxcTheClientOfTheTokenHostLivesAsLongAsTheRead(t *testing.T) {
	g := hxcNew(t, hxcDoc(hxcOAuthPost, "", ""), hxcHosts, "3600")
	for i := 1; i <= 3; i++ {
		if _, err := g.hxcRead(); err != nil {
			t.Fatal(err)
		}
		if len(g.hxcOptions) != 2*i || g.hxcShut() != 2*i || len(g.hxcHs.hsUses) != 2*i {
			t.Errorf("read %d: %d clients, %d let go of, %d openings", i, len(g.hxcOptions), g.hxcShut(), len(g.hxcHs.hsUses))
		}
	}
	if g.hxcIssued != 3 {
		t.Errorf("%d tokens for three reads", g.hxcIssued)
	}
}

// hxcCert is a throwaway certificate (no one holds its key) that stands for the authority of a host; the API's is the same with
// a line break after it, so that the two are told apart.
const hxcCert = "-----BEGIN CERTIFICATE-----\nMIIBhTCCASugAwIBAgIURtGaGZSb7VAVHQQp7STkhcncoR8wCgYIKoZIzj0EAwIw\nFzEVMBMGA1UEAwwMZXhhbXBsZS50ZXN0MCAXDTI2MTExOTEwMjEyN1oYDzIxMjYx\nMDI2MTAyMTI3WjAXMRUwEwYDVQQDDAxleGFtcGxlLnRlc3QwWTATBgcqhkjOPQIB\nBggqhkjOPQMBBwNCAASCdORDNqElggutE2cVqCCOTZ44AGGGM7ACcNJtJstRWgLZ\njCOmhbFMo01h2e0j2AL6MazLrO2T6jji4af51UPho1MwUTAdBgNVHQ4EFgQUVXuy\nfSxGvyMsczWPNhux+3ufAVAwHwYDVR0jBBgwFoAUVXuyfSxGvyMsczWPNhux+3uf\nAVAwDwYDVR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNIADBFAiEAo/VQIH+m9EXV\nQiZCDGBFQCvCy5hLs3tw573C7HWM7aQCIH6ZkOZqpPiLzlSDmQtrce0tjz0n/oWp\nLxeolirs3Vs/\n-----END CERTIFICATE-----"
