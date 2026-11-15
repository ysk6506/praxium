package mcpapi

import (
	"net/http"
	"strings"
	"testing"
)

// [REQ-502]
func TestTokenRuleOutsideProduction(t *testing.T) {
	rg := newRig(t)
	for token, want := range map[string]int{tokViewer: 200, tokService: 200, tokAdmin: 403, tokTwoRoles: 403, tokSvcEdit: 403} {
		r := rg.do(modernCall(t, token, "tools/list", nil))
		if r.status != want {
			t.Errorf("%s: %d %s; want %d", token, r.status, r.body, want)
		}
		if want == 403 && r.problemCode(t) != "permission.mcp_not_allowed" {
			t.Errorf("%s: code %s", token, r.problemCode(t))
		}
	}
}

// [REQ-502]
func TestTokenRuleInProductionTakesServiceAccountsOnly(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.ServiceAccountsOnly = true })
	if r := rg.do(modernCall(t, tokService, "tools/list", nil)); r.status != 200 {
		t.Errorf("a service account with only viewer: %d %s", r.status, r.body)
	}
	r := rg.do(modernCall(t, tokViewer, "tools/list", nil))
	if r.status != http.StatusForbidden || r.problemCode(t) != "permission.mcp_not_allowed" {
		t.Errorf("a person with viewer in production: %d %s", r.status, r.body)
	}
	var p struct {
		Params map[string]any `json:"params"`
	}
	if err := jsonUnmarshal(r.body, &p); err != nil || p.Params["reason"] != reasonAccount {
		t.Errorf("the body says why: %s", r.body)
	}
	for _, token := range []string{tokAdmin, tokTwoRoles, tokSvcEdit} {
		if r := rg.do(modernCall(t, token, "tools/list", nil)); r.status != http.StatusForbidden {
			t.Errorf("%s in production: %d", token, r.status)
		}
	}
}

// [REQ-502] [REQ-505]
func TestDisabledInterfaceAnswersJSON404(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.Enabled = false })
	for _, p := range []string{"/mcp", "/mcp/x", "/.well-known/oauth-protected-resource", "/.well-known/openid-configuration", "/.well-known"} {
		for _, m := range []string{"POST", "GET"} {
			r := rg.do(call{method: m, path: p, header: map[string]string{"Authorization": "Bearer " + tokViewer}})
			if r.status != http.StatusNotFound || !strings.HasPrefix(r.header.Get("Content-Type"), "application/problem+json") || r.problemCode(t) != "not_found.resource" {
				t.Errorf("%s %s: %d %q %s; want a JSON 404", m, p, r.status, r.header.Get("Content-Type"), r.body)
			}
		}
	}
}

// [REQ-505]
func TestWellKnownIsJSON404WhenEnabled(t *testing.T) {
	rg := newRig(t)
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server", "/.well-known/openid-configuration/mcp", "/mcp/", "/mcp/extra"} {
		r := rg.do(call{method: "GET", path: p})
		if r.status != http.StatusNotFound || r.problemCode(t) != "not_found.resource" {
			t.Errorf("%s: %d %s; want a JSON 404", p, r.status, r.body)
		}
	}
	for _, p := range []string{"/mcp", "/mcp/", "/mcp/x", "/.well-known", "/.well-known/a/b"} {
		if !rg.h.Match(p) {
			t.Errorf("Match(%q) = false; the handler owns it", p)
		}
	}
	for _, p := range []string{"/", "/mcpx", "/v1/health", "/.wellknown/x", "/assets/mcp"} {
		if rg.h.Match(p) {
			t.Errorf("Match(%q) = true; the web UI and the API own it", p)
		}
	}
}

// [REQ-506] Row X4.
func TestRowX4TokenRefusalsAre401WithAProblem(t *testing.T) {
	rg := newRig(t)
	for _, c := range []struct{ auth, code string }{
		{"", "auth.token_missing"}, {"Bearer nope", "auth.token_invalid"}, {"Bearer " + tokExpired, "auth.token_expired"},
	} {
		r := rg.do(goodCall(t).withHeader("Authorization", c.auth))
		if r.status != 401 || r.header.Get("WWW-Authenticate") != "Bearer" || r.problemCode(t) != c.code || !strings.HasPrefix(r.header.Get("Content-Type"), "application/problem+json") {
			t.Errorf("%q: %d %v %s", c.auth, r.status, r.header, r.body)
		}
		if strings.Contains(r.header.Get("WWW-Authenticate"), "resource_metadata") {
			t.Error("the 401 carries no resource_metadata while the server publishes no authorization metadata")
		}
	}
}

// [REQ-503] Row X5, X6, X7, X8, X9: the rows of the transport that no version changes.
func TestTransportRowsX5ToX9(t *testing.T) {
	rg := newRig(t)
	t.Run("X5 a batch is -32600 with 400", func(t *testing.T) {
		wantRPCError(t, rg.do(goodCall(t).withBody(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)), 400, codeInvalidRequest)
	})
	t.Run("X6 a body that is not JSON is -32700 with 400 and no id", func(t *testing.T) {
		r := rg.do(goodCall(t).withBody(`{"jsonrpc":`))
		wantRPCError(t, r, 400, codeParse)
		if _, has := r.rpc(t)["id"]; has {
			t.Errorf("a parse error has no id: %s", r.body)
		}
	})
	t.Run("X7 another media type is 415", func(t *testing.T) {
		if r := rg.do(goodCall(t).withHeader("Content-Type", "text/plain")); r.status != 415 || r.problemCode(t) != "invalid_input.unsupported_media_type" {
			t.Errorf("got %d %s", r.status, r.body)
		}
	})
	t.Run("X8 a Host that is not the loopback, on a server bound to it, is 403", func(t *testing.T) {
		bound := newRig(t, func(c *Config) { c.LoopbackBound = true })
		if r := bound.do(goodCall(t).withHeaderHost("example.org")); r.status != 403 {
			t.Errorf("got %d", r.status)
		}
	})
	t.Run("X9 Accept: a request that takes JSON passes, one that does not is 406", func(t *testing.T) {
		for _, accept := range []string{"", "*/*", "application/json", "application/*", "application/json;q=0.5, text/event-stream", "text/event-stream, */*;q=0.1"} {
			if r := rg.do(goodCall(t).withHeader("Accept", accept)); r.status != 200 {
				t.Errorf("Accept %q: %d %s", accept, r.status, r.body)
			}
		}
		for _, accept := range []string{"text/event-stream", "text/html", "application/json;q=0", "application/jsonx"} {
			if r := rg.do(goodCall(t).withHeader("Accept", accept)); r.status != 406 || r.problemCode(t) != "invalid_input.not_acceptable" {
				t.Errorf("Accept %q: %d %s", accept, r.status, r.body)
			}
		}
	})
}

// [REQ-503] Row X3: a refused Origin is 403 in every version, whatever the version headers say.
func TestRowX3OriginIsRefusedInEveryVersion(t *testing.T) {
	rg := newRig(t)
	for _, c := range []call{goodCall(t), legacyCall(t, tokViewer, version2511, "ping", nil), legacyCall(t, tokViewer, "", "ping", nil)} {
		if r := rg.do(c.withHeader("Origin", "https://evil.example")); r.status != http.StatusForbidden {
			t.Errorf("got %d %s", r.status, r.body)
		}
	}
}
