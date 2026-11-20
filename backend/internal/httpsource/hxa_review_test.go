package httpsource

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The tests that came of the independent review of ADR-0100: what the adapter takes out of what it gives out, for every
// shape it writes a value in and for every field of the secret. Every helper here begins with hxa. Values are made up.

// hxaEscapedForms are the ways a body is written that a server may hand back as a text: the encoder of the bodies, and one
// that leaves the characters of HTML alone.
func hxaEscapedForms(v string) []string { return jsonTexts(v) }

// [REQ-744]
// A server that hands back the bytes of the body it was sent, as a text (an echo of the request, an error that quotes it),
// gives a value of the secret in the way JSON writes it. That way is taken out of the rows, the preview and the events.
func TestHxaABodyHandedBackAsATextHasNoValueOfTheSecretInTheWayJSONWritesIt(t *testing.T) {
	for _, secret := range []string{"tok&value<not>real-0001", `pw"quote\back-not-real`, "tok sep-not-real-01", "tok-plain-not-real-0001", "line\nbreak\ttab-not-real-01"} {
		auth := `{"kind":"custom","credential":{"secret_id":"sec_x"}}`
		doc := strings.Replace(hxaPostDoc(auth, `{"auth":"{{credential.token}}"}`, ""), `{"name":"rack","paths":["location.rack"]}`, `{"name":"raw"},{"name":"rack","paths":["location.rack"]}`, 1)
		g := hsNew(t, doc, func(n int, r hsRequest) hsReply {
			data, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"serial": "s-1", "raw": r.hsBody}}})
			return hsJSON(string(data))
		})
		hxaAdd(t, g, "sec_x", secrets.KindToken, map[string]string{"token": secret})
		pages, err := g.hsRead(ingest.PageOptions{})
		if err != nil || len(pages) == 0 || len(pages[0].Rows) == 0 {
			t.Fatalf("%q: %v", secret, err)
		}
		raw := pages[0].Rows[0].Fields["raw"]
		if !strings.Contains(g.hsSrv.hsSeen[0].hsBody, hxaEscapedForms(secret)[0]) {
			t.Fatalf("%q: the body that was sent is not in the way the test thinks: %s", secret, g.hsSrv.hsSeen[0].hsBody)
		}
		for _, form := range append(hxaEscapedForms(secret), secret) {
			if strings.Contains(raw, form) {
				t.Errorf("%q: the row holds %q: %s", secret, form, raw)
			}
		}
		if !strings.Contains(raw, ingest.MaskedValue) {
			t.Errorf("%q: nothing was taken out of the row: %s", secret, raw)
		}
		g.hsSrv.hsSeen = nil
		pv, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{MaxPages: 1, MaxRows: 10})
		if err != nil {
			t.Fatal(err)
		}
		hxaNoLeak(t, "the preview", pv, append(hxaEscapedForms(secret), secret)...)
	}
}

// [REQ-744]
// What a read takes out is every field of the secret that is open, not the fields that read uses: a source that lists what
// came in (an audit log) hands back what another dataset sent, and the read of that list is as safe as the other.
func TestHxaEveryFieldOfTheOpenSecretIsTakenOutWhicheverDatasetIsRead(t *testing.T) {
	auth := `{"kind":"custom","credential":{"secret_id":"sec_two"},"headers":[{"name":"X-User","from":"credential.username"}]}`
	doc := `{"version":1,"id":"` + hsSource + `","adapter":"http","connection":{"base_url":"https://api.example.test/api/v2","requests_per_second":1000,"auth":` + auth + `},"datasets":[` +
		`{"id":"ds_a","post_reads":true,"request":{"method":"POST","path":"/login-check","body":{"p":"{{credential.password}}"}},"records_path":"items","natural_key":["serial"],"columns":[{"name":"serial"}]},` +
		`{"id":"ds_b","request":{"path":"/audit"},"records_path":"items","natural_key":["serial"],"columns":[{"name":"serial"},{"name":"raw"}]}]}`
	pw := "pw-long-enough-not-real-0042"
	var last string
	g := hsNew(t, doc, func(n int, r hsRequest) hsReply {
		if r.hsMethod == "POST" {
			last = r.hsBody
			return hsJSON(`{"items":[{"serial":"a-1"}]}`)
		}
		var body map[string]any
		_ = json.Unmarshal([]byte(last), &body)
		data, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"serial": "b-1", "raw": body["p"]}}})
		return hsJSON(string(data))
	})
	hxaAdd(t, g, "sec_two", secrets.KindPassword, map[string]string{"username": "user-two", "password": pw})
	if _, err := g.hsReadDS(g.hsDef.Datasets[0], ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	pages, err := g.hsReadDS(g.hsDef.Datasets[1], ingest.PageOptions{})
	if err != nil || len(pages) == 0 || len(pages[0].Rows) == 0 {
		t.Fatalf("%v", err)
	}
	if raw := pages[0].Rows[0].Fields["raw"]; strings.Contains(raw, pw) || raw != ingest.MaskedValue {
		t.Errorf("the row of the other dataset holds the password: %q", raw)
	}
	g.hsSrv.hsSeen = nil
	pv, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[1], ingest.PreviewOptions{MaxPages: 1, MaxRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	hxaNoLeak(t, "the preview of the other dataset", pv, pw)
	hxaNoLeak(t, "the events", g.hsEvents, pw)
}

// [REQ-744]
// A token of a page that holds a value of the secret of 4 to 7 bytes is not given with the page: the token is kept in the
// record of the run, and a short value is a value of the secret like a long one.
func TestHxaAShortValueOfTheSecretIsNotCarriedInTheTokenOfAPage(t *testing.T) {
	short := "k7Bytes"
	handle := func(n int, r hsRequest) hsReply {
		tok := strings.TrimPrefix(r.hsFirst("Authorization"), "Bearer ")
		if r.hsQuery().Get("cursor") == "" {
			return hsJSON(`{"items":[{"serial":"s-1","location":{"rack":"r"}}],"meta":{"next":"` + tok + `"}}`)
		}
		return hsJSON(`{"items":[{"serial":"s-2","location":{"rack":"r"}}],"meta":{"next":""}}`)
	}
	doc := hsDoc(`{"kind":"bearer","credential":{"secret_id":"sec_short"}}`, "", `{"kind":"cursor","cursor_param":"cursor","cursor_in":"query","next_cursor_path":"meta.next"}`, "")
	g := hsNew(t, doc, handle)
	hxaAdd(t, g, "sec_short", secrets.KindToken, map[string]string{"token": short})
	pages, err := g.hsRead(ingest.PageOptions{MaxRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range pages {
		if strings.Contains(string(p.Token), short) {
			t.Errorf("page %d has a token that holds the value: %s", i, p.Token)
		}
	}
}

// [REQ-743]
// A value of the secret that goes into a header is looked at when the secret is opened: a token request that has gone
// out with the credentials before the value is found wrong is a request that should not have been made.
func TestHxaAValueForAHeaderIsRefusedBeforeTheRequestForTheToken(t *testing.T) {
	tok := `{"kind":"token_request","credential":{"secret_id":"sec_lf"},"request":{"path":"/auth/token","format":"json","body":{"u":"{{credential.username}}"}},"token_path":"token","headers":[{"name":"X-P","from":"credential.password"}]}`
	g := hsNew(t, hsDoc(tok, "", "", ""), func(n int, r hsRequest) hsReply { return hsJSON(`{"token":"` + hsMintedValue + `"}`) })
	hxaAdd(t, g, "sec_lf", secrets.KindPassword, map[string]string{"username": "demo-user-x", "password": "pw-with\r\nX-Injected: 1"})
	_, err := g.hsRead(ingest.PageOptions{})
	if hxaKind(t, err) != ingest.SrcSecretUnusable || len(g.hsSrv.hsSeen) != 0 {
		t.Errorf("%v, %d requests were sent", err, len(g.hsSrv.hsSeen))
	}
}

// [REQ-740]
// The older kind basic refuses a user name with a colon as the token request does: a colon would end the name early.
func TestHxaTheKindBasicRefusesAUserNameWithAColon(t *testing.T) {
	g := hsNew(t, hsDoc(`{"kind":"basic","credential":{"secret_id":"sec_b"}}`, "", "", ""), hsOne)
	hxaAdd(t, g, "sec_b", secrets.KindPassword, map[string]string{"username": "u:x", "password": "pw-not-real-0001"})
	_, err := g.hsRead(ingest.PageOptions{})
	if hxaKind(t, err) != ingest.SrcSecretUnusable || len(g.hsSrv.hsSeen) != 0 {
		t.Errorf("%v, %d requests", err, len(g.hsSrv.hsSeen))
	}
}

// [REQ-742]
// A token request with a format and no body sends an empty JSON object.
func TestHxaATokenRequestWithNoBodySendsAnEmptyObject(t *testing.T) {
	auth := `{"kind":"token_request","credential":{"secret_id":"sec_nb"},"request":{"path":"/auth/token","format":"json"},"token_path":"token"}`
	g := hsNew(t, hsDoc(auth, "", "", ""), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			return hsJSON(`{"token":"` + hsMintedValue + `"}`)
		}
		return hsOne(n, r)
	})
	hxaAdd(t, g, "sec_nb", secrets.KindPassword, map[string]string{"username": "demo-user-x", "password": "pw-not-real-0001"})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := g.hsSrv.hsSeen[0].hsBody; got != "{}" {
		t.Errorf("the body of the token request is %q", got)
	}
}
