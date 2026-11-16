package mcpapi

import (
	"net/http"
	"testing"
)

// goodCall is a request the interface takes: tools/list of the new version.
func goodCall(t *testing.T) call { return modernCall(t, tokViewer, "tools/list", nil) }

// [REQ-515] Security test 4: Mcp-Method and Mcp-Name that differ from the body.
func TestSecurity04HeadersThatDifferFromTheBody(t *testing.T) {
	rg := newRig(t)
	wantRPCError(t, rg.do(goodCall(t).withHeader("Mcp-Method", "tools/call")), 400, codeHeaderMismatch)
	wantRPCError(t, rg.do(goodCall(t).withHeader("Mcp-Method", "Tools/List")), 400, codeHeaderMismatch)
	call := modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_whoami"})
	wantRPCError(t, rg.do(call.withHeader("Mcp-Name", "kg_catalog")), 400, codeHeaderMismatch)
	if r := rg.do(call); r.status != http.StatusOK {
		t.Errorf("the good call must pass: %d %s", r.status, r.body)
	}
}

// [REQ-515] Security test 5: the base64 form of Mcp-Name, its letter case and the spaces around it.
func TestSecurity05McpNameForms(t *testing.T) {
	rg := newRig(t)
	call := modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_whoami"})
	cases := []struct {
		name, header string
		status       int
	}{
		{"plain", "kg_whoami", 200},
		{"base64 of the same name", "=?base64?a2dfd2hvYW1p?=", 200},
		{"sentinel in capitals is not the sentinel", "=?BASE64?a2dfd2hvYW1p?=", 400},
		{"base64 of another name", "=?base64?a2dfY2F0YWxvZw==?=", 400},
		{"broken base64", "=?base64?!!!?=", 400},
		{"base64 of bytes that are not UTF-8", "=?base64?/w==?=", 400},
		{"spaces around the name", " kg_whoami ", 400},
		{"letter case of the name", "KG_WHOAMI", 400},
	}
	for _, c := range cases {
		r := rg.do(call.withHeader("Mcp-Name", c.header))
		if r.status != c.status {
			t.Errorf("%s: got %d %s; want %d", c.name, r.status, r.body, c.status)
		}
		if c.status == 400 && r.errCode(t) != codeHeaderMismatch {
			t.Errorf("%s: code %d, want %d", c.name, r.errCode(t), codeHeaderMismatch)
		}
	}
	other := map[string]any{"name": "kg_\u00e9"}
	r := rg.do(modernCall(t, tokViewer, "tools/call", other).withHeader("Mcp-Name", "=?base64?a2dfw6k=?="))
	wantRPCError(t, r, 400, codeInvalidParams)
}

// [REQ-515] Security test 6: the media type of the body.
func TestSecurity06ContentType(t *testing.T) {
	rg := newRig(t)
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "application/jsonx", "multipart/form-data", ""} {
		r := rg.do(goodCall(t).withHeader("Content-Type", ct))
		if r.status != http.StatusUnsupportedMediaType || r.problemCode(t) != "invalid_input.unsupported_media_type" {
			t.Errorf("Content-Type %q: %d %s; want 415", ct, r.status, r.body)
		}
	}
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON"} {
		if r := rg.do(goodCall(t).withHeader("Content-Type", ct)); r.status != http.StatusOK {
			t.Errorf("Content-Type %q: %d %s; want 200", ct, r.status, r.body)
		}
	}
}

// [REQ-505] [REQ-515] Security test 7: where the request says it comes from.
func TestSecurity07Origin(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.AllowedOrigins = []string{"https://console.example"} })
	for _, origin := range []string{"https://evil.example", "null", "http://127.0.0.1:8080", "https://console.example.evil.example", "HTTPS://CONSOLE.EXAMPLE"} {
		r := rg.do(goodCall(t).withHeader("Origin", origin))
		if r.status != http.StatusForbidden || r.errCode(t) == 0 {
			t.Errorf("Origin %q: %d %s; want 403 with a JSON-RPC error", origin, r.status, r.body)
		}
	}
	if r := rg.do(goodCall(t).withHeader("Origin", "https://console.example")); r.status != http.StatusOK {
		t.Errorf("an origin of the list is taken: %d %s", r.status, r.body)
	}
	if r := rg.do(goodCall(t)); r.status != http.StatusOK {
		t.Errorf("no Origin (a client that is not a browser) is taken: %d %s", r.status, r.body)
	}
	empty := newRig(t)
	if r := empty.do(goodCall(t).withHeader("Origin", "http://127.0.0.1:8080")); r.status != http.StatusForbidden {
		t.Errorf("the same origin is refused when the list is empty (the default): %d", r.status)
	}
}

// [REQ-505] [REQ-515] Security test 8: a server bound to a loopback address takes only a loopback Host.
func TestSecurity08Host(t *testing.T) {
	bound := newRig(t, func(c *Config) { c.CheckHost = true })
	for _, host := range []string{"evil.example", "evil.example:8080", "127.0.0.1.evil.example", "localhost.evil.example", "10.0.0.5:8080", "[::1", "[::1]x"} {
		if r := bound.do(goodCall(t).withHeaderHost(host)); r.status != http.StatusForbidden {
			t.Errorf("Host %q: %d; want 403", host, r.status)
		}
	}
	for _, host := range []string{"127.0.0.1", "127.0.0.1:8080", "localhost:9", "LOCALHOST", "[::1]:8080", "[::1]", "::1"} {
		if r := bound.do(goodCall(t).withHeaderHost(host)); r.status != http.StatusOK {
			t.Errorf("Host %q: %d %s; want 200", host, r.status, r.body)
		}
	}
	open := newRig(t)
	if r := open.do(goodCall(t).withHeaderHost("mcp.example")); r.status != http.StatusOK {
		t.Errorf("a server not bound to the loopback takes any Host: %d", r.status)
	}
}

func (c call) withHeaderHost(host string) call {
	c.host = host
	return c
}

// [REQ-506] [REQ-515] Security test 9: with no good token every request is refused, whatever else is wrong with it,
// so a read-only interface still needs the token. A wrong Origin or Host is refused earlier, by its own rule.
func TestSecurity09NoTokenNoAnswer(t *testing.T) {
	rg := newRig(t)
	for _, c := range []call{
		goodCall(t),
		goodCall(t).withHeader("Content-Type", "text/plain"),
		goodCall(t).withHeader("MCP-Protocol-Version", "1999-01-01"),
		goodCall(t).withHeader("MCP-Protocol-Version", ""),
		goodCall(t).withHeader("Mcp-Method", "tools/call"),
		goodCall(t).withBody(`[]`),
		goodCall(t).withBody(`{`),
		goodCall(t).withHeader("Accept", "text/html"),
	} {
		for _, auth := range []string{"", "Bearer wrong-token", "Bearer " + tokExpired, "Basic dXNlcjpwYXNz", "Bearer"} {
			r := rg.do(c.withHeader("Authorization", auth))
			if r.status != http.StatusUnauthorized || r.header.Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("Authorization %q: %d %s; want 401 with WWW-Authenticate: Bearer", auth, r.status, r.body)
			}
		}
	}
	if got := rg.do(goodCall(t).withHeader("Authorization", "")).problemCode(t); got != "auth.token_missing" {
		t.Errorf("code = %s", got)
	}
	if got := rg.do(goodCall(t).withHeader("Authorization", "Bearer "+tokExpired)).problemCode(t); got != "auth.token_expired" {
		t.Errorf("code = %s", got)
	}
}

// [REQ-515] Security test 10 and rows X1 and X2: the other methods are 405 with Allow: POST, and no CORS.
func TestSecurity10OtherMethods(t *testing.T) {
	rg := newRig(t)
	for _, m := range []string{"GET", "DELETE", "OPTIONS", "PUT", "PATCH", "HEAD"} {
		for _, auth := range []string{"Bearer " + tokViewer, ""} {
			r := rg.do(call{method: m, header: map[string]string{"Authorization": auth, "Origin": ""}})
			if r.status != http.StatusMethodNotAllowed || r.header.Get("Allow") != "POST" {
				t.Errorf("%s: %d Allow=%q; want 405 and Allow: POST", m, r.status, r.header.Get("Allow"))
			}
			for k := range r.header {
				if len(k) > 14 && k[:14] == "Access-Control" {
					t.Errorf("%s: the answer must carry no CORS header, has %s", m, k)
				}
			}
			if m != "HEAD" && r.problemCode(t) != "invalid_input.method_not_allowed" {
				t.Errorf("%s: the body is a JSON problem: %s", m, r.body)
			}
		}
	}
}

// [REQ-515] Security test 11: a version the server does not speak, and a header that differs from the _meta.
func TestSecurity11UnsupportedVersionAndMismatch(t *testing.T) {
	rg := newRig(t)
	r := rg.do(legacyCall(t, tokViewer, "2024-11-05", "tools/list", nil))
	wantRPCError(t, r, 400, codeUnsupportedVers)
	data := r.rpc(t)["error"].(map[string]any)["data"].(map[string]any)
	if sup, _ := data["supported"].([]any); len(sup) != 3 || sup[0] != version2026 {
		t.Errorf("the refusal lists the versions the server speaks: %v", data)
	}
	mismatch := rg.do(goodCall(t).withHeader("MCP-Protocol-Version", version2506))
	wantRPCError(t, mismatch, 400, codeHeaderMismatch)
	if d, _ := mismatch.rpc(t)["error"].(map[string]any)["data"].(map[string]any); d == nil || len(d["supported"].([]any)) != 3 {
		t.Errorf("a mismatch also lists the versions the server speaks: %s", mismatch.body)
	}
	unknownMeta := map[string]any{metaVersionKey: "2099-01-01", metaCapsKey: map[string]any{}}
	c := goodCall(t).withHeader("MCP-Protocol-Version", "2099-01-01").withBody(message(t, 1, "tools/list", map[string]any{"_meta": unknownMeta}))
	wantRPCError(t, rg.do(c), 400, codeUnsupportedVers)
	if r := rg.do(goodCall(t).withHeader("MCP-Protocol-Version", "x\x01y")); r.status >= 500 {
		t.Errorf("a header with a control character must not break the handler: %d", r.status)
	}
}

// [REQ-515] Security test 12: an unknown method is 404 with -32601, a notification is 202.
func TestSecurity12UnknownMethodAndNotification(t *testing.T) {
	rg := newRig(t)
	wantRPCError(t, rg.do(modernCall(t, tokViewer, "no/such/method", nil)), http.StatusNotFound, codeMethodNotFound)
	note := goodCall(t).withBody(message(t, nil, "no/such/notification", map[string]any{"_meta": meta()}))
	if r := rg.do(note); r.status != http.StatusAccepted || len(r.body) != 0 {
		t.Errorf("a notification: %d %q; want 202 and no body", r.status, r.body)
	}
}

// [REQ-515] Security test 15 and row X10: a session id or a stream position in the request is not read and
// no session id is made, in any version.
func TestSecurity15SessionID(t *testing.T) {
	rg := newRig(t)
	calls := []call{
		goodCall(t),
		legacyCall(t, tokViewer, version2511, "tools/list", nil),
		legacyCall(t, tokViewer, version2506, "initialize", nil).withBody(initializeBody(t, version2506)),
	}
	for _, c := range calls {
		plain := rg.do(c)
		with := rg.do(c.withHeader("Mcp-Session-Id", "abc123").withHeader("Last-Event-ID", "42"))
		if with.status != plain.status || with.header.Get("Mcp-Session-Id") != "" || plain.header.Get("Mcp-Session-Id") != "" {
			t.Errorf("the session id changed the answer or was made: %d vs %d, %v", plain.status, with.status, with.header)
		}
		if string(with.body) != string(plain.body) {
			t.Errorf("the session id changed the body:\n%s\n%s", plain.body, with.body)
		}
	}
}
