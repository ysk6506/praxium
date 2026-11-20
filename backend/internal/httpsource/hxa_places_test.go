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

// The tests of the places where a secret and the token go into a request, and of what is taken out of what is given out
// (ADR-0100). Every helper here begins with hxa.

// hxaWeird is a value of a secret with every character that could break the structure of a body or of a header.
const hxaWeird = "a\"b\\c<d>&e\nf'g}]{["

// hxaPostDoc makes the definition of a source whose dataset reads with POST and the given body.
func hxaPostDoc(auth, body, pagination string) string {
	doc := hsDoc(auth, `{"method":"POST","path":"/servers","body":`+body+`}`, pagination, "")
	return strings.Replace(doc, `"records_path":"items"`, `"post_reads":true,"records_path":"items"`, 1)
}

// hxaAdd puts a secret in the store of the rig, allowed for this source and the address of the fake source.
func hxaAdd(t *testing.T, g *hsRig, id string, kind secrets.Kind, fields map[string]string) {
	t.Helper()
	in := secrets.CreateInput{ID: id, Kind: kind, Fields: secrets.NewFields(fields), Scope: []string{hsSource}, AllowedHosts: []secrets.Address{{Host: "api.example.test", Port: 443}}}
	if _, err := g.hsSvc.Create(hsCtx, in); err != nil {
		t.Fatal(err)
	}
}

// hxaLeaks lists the values (in any shape the adapter takes out) that a text has in it.
func hxaLeaks(text string, values ...string) []string {
	var found []string
	for _, f := range maskForms(values) {
		if strings.Contains(text, f) {
			found = append(found, f[:min(len(f), 6)]+"...")
		}
	}
	return found
}

func hxaNoLeak(t *testing.T, where string, v any, values ...string) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if found := hxaLeaks(string(data), values...); len(found) > 0 {
		t.Errorf("%s holds a value of a secret (%v)", where, found)
	}
}

func hxaKind(t *testing.T, err error) string {
	t.Helper()
	se, ok := ingest.SourceErrorOf(err)
	if !ok {
		t.Fatalf("not a source error: %v", err)
	}
	return se.Kind
}

// [REQ-742]
// A value that is a placeholder is a value of the body whatever it holds: quotes, backslashes, angle brackets and line
// breaks leave the structure of the JSON whole. A text a source gave (a cursor) is never read for a placeholder, and the
// braces a definition doubled are written as one.
func TestHxaABodyPlaceholderIsAValueAndTheStructureStaysWhole(t *testing.T) {
	pager := `{"kind":"cursor","cursor_param":"cursor","cursor_in":"body","next_cursor_path":"meta.next"}`
	auth := `{"kind":"custom","credential":{"secret_id":"sec_weird"}}`
	body := `{"auth":"{{credential.token}}","nest":{"list":["{{credential.token}}",{"k":"{{credential.token}}"}]},"lit":"{{kept}}","n":7}`
	g := hsNew(t, hxaPostDoc(auth, body, pager), func(n int, r hsRequest) hsReply {
		if n == 1 {
			return hsJSON(`{"items":` + hsRecords(0, 1) + `,"meta":{"next":"{{credential.token}}"}}`)
		}
		return hsJSON(`{"items":` + hsRecords(1, 2) + `,"meta":{"next":""}}`)
	})
	hxaAdd(t, g, "sec_weird", secrets.KindToken, map[string]string{"token": hxaWeird})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 2 {
		t.Fatalf("rows %d: %v", len(hsSerials(pages)), err)
	}
	first, second := g.hsSrv.hsSeen[0], g.hsSrv.hsSeen[1]
	var got map[string]any
	if err := json.Unmarshal([]byte(first.hsBody), &got); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}
	nest := got["nest"].(map[string]any)["list"].([]any)
	if got["auth"] != hxaWeird || nest[0] != hxaWeird || nest[1].(map[string]any)["k"] != hxaWeird || got["lit"] != "{kept}" || got["n"] != 7.0 {
		t.Errorf("the body %v", got)
	}
	if strings.Contains(first.hsBody, "\n") || first.hsMethod != "POST" || first.hsFirst("Content-Type") != "application/json" {
		t.Errorf("the request: %s %q", first.hsMethod, first.hsFirst("Content-Type"))
	}
	var next map[string]any
	if err := json.Unmarshal([]byte(second.hsBody), &next); err != nil || next["cursor"] != "{{credential.token}}" || next["auth"] != hxaWeird {
		t.Errorf("the cursor the source gave was read as a placeholder: %v %v", next["cursor"], err)
	}
}

// [REQ-741]
// A secret and the token go in the headers the definition names, with the prefix; the kind custom sends nothing by itself,
// and a token that is sent by a template sends no header of the kind.
func TestHxaTheHeadersOfTheAuthenticationCarryTheSecretAndTheToken(t *testing.T) {
	auth := `{"kind":"custom","credential":{"secret_id":"sec_token"},"headers":[{"name":"X-Api-Key","from":"credential.token","prefix":"Token "},{"name":"x-plain","from":"credential.token"}]}`
	g := hsNew(t, hsDoc(auth, "", "", `,"headers":{"X-Api-Key":"from the connection","X-Team":"a {{b}}"}`), hsOne)
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	r := g.hsSrv.hsSeen[0]
	if r.hsFirst("X-Api-Key") != "Token "+hsTokenValue || r.hsFirst("X-Plain") != hsTokenValue || r.hsFirst("X-Team") != "a {b}" || r.hsFirst("Authorization") != "" {
		t.Errorf("headers %+v", r.hsHeader)
	}
	if n := len(r.hsHeader["X-Api-Key"]) + len(r.hsHeader["x-api-key"]); n != 1 {
		t.Errorf("the header was set %d times", n)
	}
	token := `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"send":"template","headers":[{"name":"X-Session","from":"token","prefix":"S "}],` +
		`"request":{"path":"/auth/token","format":"json","body":{"username":"{{credential.username}}","password":"{{credential.password}}"}},"token_path":"token"}`
	g = hsNew(t, hsDoc(token, "", "", ""), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			return hsJSON(`{"token":"` + hsMintedValue + `"}`)
		}
		return hsOne(n, r)
	})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	data := g.hsSrv.hsSeen[1]
	if data.hsFirst("X-Session") != "S "+hsMintedValue || data.hsFirst("Authorization") != "" {
		t.Errorf("headers %+v", data.hsHeader)
	}
}

// [REQ-742]
// A token request has a body that is a tree, and a body of data may carry the token: a token the source no longer accepts
// is asked for once more, and the request that is sent again carries the new one, in the body and in the header.
func TestHxaARequestSentAgainAfterA401CarriesTheNewToken(t *testing.T) {
	auth := `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"send":"template","headers":[{"name":"X-Session","from":"token"}],` +
		`"request":{"path":"/rpc","format":"json","body":{"jsonrpc":"2.0","method":"user.login","params":{"username":"{{credential.username}}","password":"{{credential.password}}"},"id":1}},"token_path":"result"}`
	issued := 0
	g := hsNew(t, hxaPostDoc(auth, `{"auth":"{{auth.token}}"}`, ""), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/rpc") {
			issued++
			return hsJSON(`{"result":"` + hsMintedValue + "-" + string(rune('0'+issued)) + `"}`)
		}
		if issued == 1 {
			return hsStatus(401)
		}
		return hsOne(n, r)
	})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	seen := g.hsSrv.hsSeen
	if len(seen) != 4 || issued != 2 {
		t.Fatalf("%d requests, %d tokens", len(seen), issued)
	}
	var login map[string]any
	if err := json.Unmarshal([]byte(seen[0].hsBody), &login); err != nil || login["method"] != "user.login" || login["params"].(map[string]any)["password"] != hsPassValue || login["id"] != 1.0 {
		t.Errorf("the token request %s: %v", seen[0].hsBody, err)
	}
	for i, want := range map[int]string{1: hsMintedValue + "-1", 3: hsMintedValue + "-2"} {
		var body map[string]any
		if err := json.Unmarshal([]byte(seen[i].hsBody), &body); err != nil || body["auth"] != want || seen[i].hsFirst("X-Session") != want {
			t.Errorf("request %d carries %v and %q, not %q", i, body["auth"], seen[i].hsFirst("X-Session"), want)
		}
	}
}

// [REQ-743]
// A value of a secret or a token that cannot be a value of a header (a line break, a control character) is not sent: the
// error is one of the twelve, no request is made for it and none is made again. (A NUL cannot be kept in a secret at all.)
func TestHxaAValueThatCannotBeAHeaderIsNotSentAndNotRetried(t *testing.T) {
	custom := `{"kind":"custom","credential":{"secret_id":"sec_bad"},"headers":[{"name":"X-K","from":"credential.token"}]}`
	bearer := `{"kind":"bearer","credential":{"secret_id":"sec_bad"}}`
	for name, auth := range map[string]string{"custom": custom, "bearer": bearer} {
		for _, value := range []string{"abc\n", "abc\r\nX-Injected: 1", "ab\x7fc", "ab\x01c"} {
			g := hsNew(t, hsDoc(auth, "", "", ""), hsOne)
			hxaAdd(t, g, "sec_bad", secrets.KindToken, map[string]string{"token": value})
			_, err := g.hsRead(ingest.PageOptions{})
			if err == nil || hxaKind(t, err) != ingest.SrcSecretUnusable {
				t.Errorf("%s %q: %v", name, value, err)
			}
			if len(g.hsSrv.hsSeen) != 0 {
				t.Errorf("%s %q: %d requests were sent", name, value, len(g.hsSrv.hsSeen))
			}
			for _, e := range g.hsEvents {
				if e.Kind == ingest.EventRetry || e.Kind == ingest.EventRequest {
					t.Errorf("%s %q: an event %s", name, value, e.Kind)
				}
			}
			if se, _ := ingest.SourceErrorOf(err); se != nil && strings.Contains(se.Message, "X-Injected") {
				t.Errorf("the value is in the words of the error")
			}
		}
	}
	// A tab is a character a header may have.
	g := hsNew(t, hsDoc(custom, "", "", ""), hsOne)
	hxaAdd(t, g, "sec_bad", secrets.KindToken, map[string]string{"token": "a\tb"})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil || g.hsSrv.hsSeen[0].hsFirst("X-K") != "a\tb" {
		t.Errorf("a tab: %v", err)
	}
	// A token the source gave, with a line break: the request for the token was made, the one for data is not.
	tokReq := hsTokReq
	g = hsNew(t, hsDoc(tokReq, "", "", ""), func(n int, r hsRequest) hsReply {
		return hsJSON(`{"token":"minted-not-a-real\r\nX-Injected: 1"}`)
	})
	_, err := g.hsRead(ingest.PageOptions{})
	if err == nil || hxaKind(t, err) != ingest.SrcResponseFormat || len(g.hsSrv.hsSeen) != 1 || !strings.HasSuffix(g.hsSrv.hsSeen[0].hsPath(), "/auth/token") {
		t.Errorf("a token with a line break: %v, %d requests", err, len(g.hsSrv.hsSeen))
	}
	// A secret that lacks the field a body names: nothing is sent either.
	g = hsNew(t, hxaPostDoc(`{"kind":"custom","credential":{"secret_id":"sec_pw"}}`, `{"k":"{{credential.token}}"}`, ""), hsOne)
	if _, err := g.hsRead(ingest.PageOptions{}); err == nil || hxaKind(t, err) != ingest.SrcSecretUnusable || len(g.hsSrv.hsSeen) != 0 {
		t.Errorf("a field the secret lacks: %v, %d requests", err, len(g.hsSrv.hsSeen))
	}
}

// [REQ-740]
// The credentials go in a Basic header of a request that has no body, and a token that is the whole answer is read as a
// JSON text; an answer that is anything else ends the read with the request for data not sent.
func TestHxaBasicCredentialsAndATokenThatIsTheWholeBody(t *testing.T) {
	auth := `{"kind":"token_request","credential":{"secret_id":"sec_pw"},"request":{"path":"/api/session","credentials_in":"basic"},"token_from":"body","send_header":"X-Session","send_prefix":""}`
	g := hsNew(t, hsDoc(auth, "", "", ""), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/api/session") {
			return hsJSON(`"` + hsMintedValue + `"`)
		}
		return hsOne(n, r)
	})
	if _, err := g.hsRead(ingest.PageOptions{}); err != nil {
		t.Fatal(err)
	}
	login, data := g.hsSrv.hsSeen[0], g.hsSrv.hsSeen[1]
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(hsUserValue+":"+hsPassValue))
	if login.hsMethod != "POST" || login.hsFirst("Authorization") != want || login.hsBody != "" || login.hsFirst("Content-Type") != "" {
		t.Errorf("the token request: %s %q body %q type %q", login.hsMethod, login.hsFirst("Authorization"), login.hsBody, login.hsFirst("Content-Type"))
	}
	if data.hsFirst("X-Session") != hsMintedValue || data.hsFirst("Authorization") != "" {
		t.Errorf("the request for data: %+v", data.hsHeader)
	}
	for name, answer := range map[string]string{"an object": `{"a":1}`, "a number": `5`, "an empty text": `""`, "null": `null`, "a list": `["x"]`, "two values": `"x" "y"`, "no JSON": `not json`} {
		g := hsNew(t, hsDoc(auth, "", "", ""), func(n int, r hsRequest) hsReply { return hsJSON(answer) })
		_, err := g.hsRead(ingest.PageOptions{})
		if err == nil || hxaKind(t, err) != ingest.SrcResponseFormat || len(g.hsSrv.hsSeen) != 1 {
			t.Errorf("%s: %v, %d requests", name, err, len(g.hsSrv.hsSeen))
		}
	}
	colon := hsNew(t, hsDoc(strings.Replace(auth, "sec_pw", "sec_colon", 1), "", "", ""), hsOne)
	hxaAdd(t, colon, "sec_colon", secrets.KindPassword, map[string]string{"username": "a:b", "password": "a-long-enough-password"})
	if _, err := colon.hsRead(ingest.PageOptions{}); err == nil || hxaKind(t, err) != ingest.SrcSecretUnusable || len(colon.hsSrv.hsSeen) != 0 {
		t.Errorf("a user name with a colon: %v, %d requests", err, len(colon.hsSrv.hsSeen))
	}
}

// [REQ-744]
// What a source hands back is taken out of the rows, the events and the exchanges of a preview in every place a secret now
// goes: the value of a header the definition names, a value of a body, the Basic value and the shape of it that the
// OAuth 2.0 client authentication writes.
func TestHxaWhatAPlaceOfTheSecretGivesIsTakenOutWhenTheSourceHandsItBack(t *testing.T) {
	auth := `{"kind":"custom","credential":{"secret_id":"sec_token"},"headers":[{"name":"X-Api-Key","from":"credential.token","prefix":"Token "}]}`
	handle := func(n int, r hsRequest) hsReply {
		var body map[string]any
		_ = json.Unmarshal([]byte(r.hsBody), &body)
		echoed, _ := body["auth"].(string)
		key := r.hsFirst("X-Api-Key")
		rec := map[string]any{"serial": "s-1", "seen": key, "rack": echoed, "b64": base64.StdEncoding.EncodeToString([]byte(key)), "enc": url.QueryEscape(key), "deep": []any{map[string]any{"x": "<" + echoed + ">"}}, "n": 1}
		data, _ := json.Marshal(map[string]any{"items": []any{rec}, "next": "/api/v2/servers/" + url.PathEscape(echoed)})
		return hsJSON(string(data))
	}
	g := hsNew(t, hsfColumns(hxaPostDoc(auth, `{"auth":"{{credential.token}}"}`, `{"kind":"next_url","next_path":"next"}`)), func(n int, r hsRequest) hsReply {
		if n > 1 {
			return hsJSON(`{"items":[],"next":""}`)
		}
		return handle(n, r)
	})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 1 {
		t.Fatalf("%v", err)
	}
	for _, p := range pages {
		hxaNoLeak(t, "a row", p.Rows, hsTokenValue, "Token "+hsTokenValue)
	}
	if got := pages[0].Rows[0].Fields; !strings.Contains(got["seen"], ingest.MaskedValue) || got["rack"] != ingest.MaskedValue || got["serial"] != "s-1" {
		t.Errorf("the row %v", got)
	}
	hxaNoLeak(t, "the events", g.hsEvents, hsTokenValue, "Token "+hsTokenValue)
	g.hsSrv.hsSeen = nil
	pv, err := g.hsReader.Preview(hsCtx, hsSource, g.hsDef.Datasets[0], ingest.PreviewOptions{MaxPages: 2, MaxRows: 10})
	if err != nil || !pv.Masked {
		t.Fatalf("%v, masked %v", err, pv.Masked)
	}
	hxaNoLeak(t, "the preview", pv, hsTokenValue, "Token "+hsTokenValue)

	basicAuth := `{"kind":"token_request","credential":{"secret_id":"sec_oauth"},"request":{"path":"/auth/token","format":"form","credentials_in":"basic","body":{"grant_type":"client_credentials"}},"token_path":"token"}`
	user, pass := "client-id-0001", "p@ss w/rd+%&=long"
	var basicSeen string
	g = hsNew(t, hsfColumns(hsDoc(basicAuth, "", "", "")), func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			basicSeen = r.hsFirst("Authorization")
			return hsJSON(`{"token":"` + hsMintedValue + `"}`)
		}
		form := base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(user) + ":" + url.QueryEscape(pass)))
		rec := map[string]any{"serial": user, "seen": basicSeen, "rack": "Basic " + form, "b64": base64.StdEncoding.EncodeToString([]byte(basicSeen)), "enc": url.QueryEscape(pass), "deep": []any{map[string]any{"x": pass}}}
		data, _ := json.Marshal(map[string]any{"items": []any{rec}})
		return hsJSON(string(data))
	})
	hxaAdd(t, g, "sec_oauth", secrets.KindPassword, map[string]string{"username": user, "password": pass})
	pages, err = g.hsRead(ingest.PageOptions{})
	if err != nil || len(hsSerials(pages)) != 1 {
		t.Fatalf("%v", err)
	}
	if basicSeen != "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)) {
		t.Errorf("the Basic header %q", basicSeen)
	}
	row := pages[0].Rows[0].Fields
	for _, name := range []string{"seen", "rack", "b64", "enc"} {
		if !strings.Contains(row[name], ingest.MaskedValue) || strings.Contains(row[name], "Basic ") && !strings.HasPrefix(row[name], "***") {
			t.Errorf("the column %s of the row is %q", name, row[name])
		}
	}
	hxaNoLeak(t, "the rows of the Basic source", pages, pass, "Basic "+basicSeen)
	hxaNoLeak(t, "the events of the Basic source", g.hsEvents, pass, user)
}

// [REQ-744]
// A user name, a client id and a value shorter than eight bytes are no part of what is taken out of the rows: the data of the
// source has them as it has other words, and a key made of them stays what it was. The records of what happened, which a
// person reads, have them taken out as always.
func TestHxaAUserNameInTheDataChangesNoRowAndNoKey(t *testing.T) {
	handle := func(n int, r hsRequest) hsReply {
		if strings.HasSuffix(r.hsPath(), "/auth/token") {
			return hsJSON(`{"token":"` + hsMintedValue + `"}`)
		}
		if n > 3 {
			return hsJSON(`{"items":[],"next":""}`)
		}
		return hsJSON(`{"items":[{"serial":"` + hsUserValue + `","location":{"rack":"` + hsUserValue + `-rack"}},{"serial":"short-pw","location":{"rack":"abcdefg"}}],"next":"/api/v2/servers/` + hsUserValue + `"}`)
	}
	short := strings.Replace(hsTokReq, "sec_pw", "sec_short", 1)
	g := hsNew(t, hsDoc(short, "", `{"kind":"next_url","next_path":"next"}`, ""), handle)
	hxaAdd(t, g, "sec_short", secrets.KindPassword, map[string]string{"username": "abcdefghij", "password": "abcdefg"})
	pages, err := g.hsRead(ingest.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := hsSerials(pages); len(got) == 0 || got[0] != hsUserValue || got[1] != "short-pw" {
		t.Errorf("the keys %v", got)
	}
	if rack := pages[0].Rows[0].Fields["rack"]; rack != hsUserValue+"-rack" {
		t.Errorf("the rack %q", rack)
	}
	// The user name of the secret in this read is abcdefghij and its password abcdefg (seven bytes).
	if rack := pages[0].Rows[1].Fields["rack"]; rack != "abcdefg" {
		t.Errorf("a password of seven bytes is no value to take out of a row: %q", rack)
	}
	user := hsNew(t, hsDoc(hsTokReq, "", `{"kind":"next_url","next_path":"next"}`, ""), handle)
	pv, err := user.hsReader.Preview(hsCtx, hsSource, user.hsDef.Datasets[0], ingest.PreviewOptions{MaxPages: 3, MaxRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := pv.Pages[0].Rows[0].Fields["serial"]; got != hsUserValue {
		t.Errorf("the serial in the preview %q", got)
	}
	hxaNoLeak(t, "the events", user.hsEvents, hsUserValue, hsPassValue)
	hxaNoLeak(t, "the exchanges", pv.Exchanges, hsUserValue, hsPassValue)
	masked := false
	for _, e := range pv.Exchanges {
		masked = masked || strings.Contains(e.Path, ingest.MaskedValue)
	}
	if !masked {
		t.Errorf("the path of the exchange that has the user name was not taken out: %+v", pv.Exchanges)
	}
}
