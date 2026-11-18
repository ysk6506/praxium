package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// mcpPost sends a message to /mcp of a running local server as an agent harness of the new version would.
func mcpPost(t *testing.T, r *demoRun, token, body string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, r.base+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header, string(out)
}

const listBody = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`

var listHeaders = map[string]string{"MCP-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/list"}

func TestLocalServesMCPWithTheTokenOfTheRun(t *testing.T) {
	r := &startLocal(t, true, keepSample(t)).demoRun
	status, _, body := mcpPost(t, r, r.token, listBody, listHeaders)
	if status != 200 || !strings.Contains(body, `"kg_whoami"`) || !strings.Contains(body, `"resultType":"complete"`) {
		t.Fatalf("tools/list: %d %s", status, body)
	}
	status, header, body := mcpPost(t, r, "", listBody, listHeaders)
	if status != 401 || header.Get("WWW-Authenticate") != "Bearer" || !strings.Contains(body, "auth.token_missing") {
		t.Fatalf("no token: %d %v %s", status, header, body)
	}
	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"kg_whoami","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	status, _, body = mcpPost(t, r, r.token, callBody, map[string]string{"MCP-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": "kg_whoami"})
	if status != 200 || !strings.Contains(body, `"local-user"`) || strings.Contains(body, r.token) {
		t.Fatalf("kg_whoami: %d %s", status, body)
	}
	if code, body := demoGet(r, "/mcp", r.token); code != 405 || strings.Contains(body, "<html") {
		t.Fatalf("GET /mcp: %d %s; want a JSON 405", code, body)
	}
	if code, body := demoGet(r, "/.well-known/oauth-protected-resource", ""); code != 404 || strings.Contains(body, "<html") || !strings.Contains(body, "not_found.resource") {
		t.Fatalf("/.well-known/: %d %s; want a JSON 404 and not the page", code, body)
	}
	if code, body := demoGet(r, "/", ""); code != 200 || !strings.Contains(body, "<html") {
		t.Fatalf("the web UI must still answer at /: %d", code)
	}
	if code := demoStop(r); code != 0 {
		t.Fatalf("stop: %d", code)
	}
}

// [REQ-505]
func TestLocalWithMCPOffAnswersJSON404(t *testing.T) {
	r := &startLocal(t, true, keepSample(t), "-mcp=false").demoRun
	status, _, body := mcpPost(t, r, r.token, listBody, listHeaders)
	if status != 404 || strings.Contains(body, "<html") || !strings.Contains(body, "not_found.resource") {
		t.Fatalf("/mcp with the interface off: %d %s", status, body)
	}
	if code, body := demoGet(r, "/.well-known/openid-configuration", ""); code != 404 || strings.Contains(body, "<html") {
		t.Fatalf("/.well-known/ with the interface off: %d %s", code, body)
	}
	if code, _ := demoGet(r, "/v1/kgs", r.token); code != 200 {
		t.Fatalf("the REST API is not touched by the flag: %d", code)
	}
}

func TestMCPIsOffInProductionUntilAskedFor(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"-mode", "development"}, true},
		{[]string{"-mode", "production"}, false},
		{[]string{"-mcp"}, true},
		{[]string{"-mode", "development", "-mcp=false"}, false},
		{[]string{"-mode", "production", "-mcp=true"}, true},
	} {
		o, err := parseServe(c.args)
		if err != nil {
			t.Fatal(err)
		}
		if got := o.mcp.enabledFor(o.mode); got != c.want {
			t.Errorf("api serve %v: MCP on = %v, want %v", c.args, got, c.want)
		}
	}
}

// [REQ-505]
func TestMCPAllowedOriginsMustBeOrigins(t *testing.T) {
	good := mcpOptions{origins: " https://console.example , http://localhost:5173,,"}
	got, err := good.allowedOrigins()
	if err != nil || len(got) != 2 || got[0] != "https://console.example" || got[1] != "http://localhost:5173" {
		t.Fatalf("origins = %v, %v", got, err)
	}
	for _, bad := range []string{"console.example", "https://console.example/path", "ftp://x", "https://", "https://u@console.example", "*"} {
		if _, err := (mcpOptions{origins: bad}).allowedOrigins(); err == nil {
			t.Errorf("%q is not an origin and must be refused", bad)
		}
	}
}
