package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/identity"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// mcpAssembly is the /mcp handler of a server built the way the server builds it (mcpMounts), over the real identity
// service, so the rules that the assembly decides (production tokens, the Host check, the limiter) are the ones tested.
type mcpAssembly struct {
	t       *testing.T
	handler http.Handler
	svc     *identity.Service
}

type assemblyOptions struct {
	mode, addr string
	behind     bool
	origins    string
	limiter    access.FailureLimiter
}

func newMCPAssembly(t *testing.T, o assemblyOptions) *mcpAssembly {
	t.Helper()
	d := wiring{deps: fixedDeps()}
	svc, err := identity.New(identity.Config{Store: identity.NewMemoryStore(), Clock: d.clock, Random: kernel.NewToken, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	kit := authKit{authn: svc, dir: svc, limiter: o.limiter, name: "built-in", close: func() error { return nil }}
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	plan := serverPlan{opts: serveOptions{mode: o.mode, addr: o.addr, behindProxy: o.behind, mcp: mcpOptions{on: true, set: true, origins: o.origins}}}
	mounts, err := mcpMounts(plan, kit, d, slog.New(slog.DiscardHandler), reg, nil)
	if err != nil || len(mounts) != 1 {
		t.Fatalf("mcpMounts: %v %v", mounts, err)
	}
	return &mcpAssembly{t: t, handler: mounts[0].Handler, svc: svc}
}

// token makes an account and a token for it with the roles, and gives the secret.
func (a *mcpAssembly) mcpTokenFor(kind access.SubjectKind, name string, accountRoles, tokenRoles []access.Role) string {
	a.t.Helper()
	ctx := context.Background()
	if _, err := a.svc.CreatePrincipal(ctx, identity.OfflineActor, access.PrincipalRequest{Kind: kind, Name: name, DisplayName: name, Roles: accountRoles}); err != nil {
		a.t.Fatal(err)
	}
	issued, err := a.svc.IssueToken(ctx, identity.OfflineActor, access.TokenRequest{Owner: name, Roles: tokenRoles, Lifetime: 24 * time.Hour})
	if err != nil {
		a.t.Fatal(err)
	}
	return issued.Secret
}

func (a *mcpAssembly) mcpSend(token, host, body string, headers map[string]string) (int, string) {
	a.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Host = host
	req.RemoteAddr = "192.0.2.10:4000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

var viewerOnly = []access.Role{access.RoleViewer}

const whoamiBody = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"kg_whoami","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`

var whoamiHeaders = map[string]string{"MCP-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": "kg_whoami"}

// [REQ-506] The tokens /mcp takes are judged by the roles the token holds. A service account with viewer and editor
// has a token of both roles refused, and a token narrowed to viewer taken, with kg_whoami naming the viewer role only
// (ADR-0068, which amends ADR-0062 on this).
func TestMCPTakesTheTokenByItsOwnRoles(t *testing.T) {
	a := newMCPAssembly(t, assemblyOptions{mode: "development", addr: "0.0.0.0:8080"})
	both := []access.Role{access.RoleViewer, access.RoleEditor}
	wide := a.mcpTokenFor(access.SubjectSystemAccount, "harness", both, both)
	status, body := a.mcpSend(wide, "kg.example", whoamiBody, whoamiHeaders)
	if status != 403 || !strings.Contains(body, "permission.mcp_not_allowed") || !strings.Contains(body, "roles_must_be_viewer_only") {
		t.Fatalf("a token of viewer and editor: %d %s", status, body)
	}
	if strings.Contains(body, wide) {
		t.Fatal("the answer must not carry the token")
	}
	narrow := func() string {
		issued, err := a.svc.IssueToken(context.Background(), identity.OfflineActor, access.TokenRequest{Owner: "harness", Roles: viewerOnly, Lifetime: 24 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		return issued.Secret
	}()
	status, body = a.mcpSend(narrow, "kg.example", whoamiBody, whoamiHeaders)
	if status != 200 || !strings.Contains(body, `"roles":["viewer"]`) {
		t.Fatalf("a token narrowed to viewer: %d %s", status, body)
	}
}

// [REQ-506] In production /mcp takes only the token of a service account, and a person's viewer token is refused.
func TestMCPInProductionTakesServiceAccountsOnly(t *testing.T) {
	a := newMCPAssembly(t, assemblyOptions{mode: "production", addr: "0.0.0.0:8080"})
	person := a.mcpTokenFor(access.SubjectPerson, "yeonsu", viewerOnly, viewerOnly)
	status, body := a.mcpSend(person, "kg.example", whoamiBody, whoamiHeaders)
	if status != 403 || !strings.Contains(body, "must_be_service_account") {
		t.Fatalf("the viewer token of a person: %d %s", status, body)
	}
	service := a.mcpTokenFor(access.SubjectSystemAccount, "harness", viewerOnly, viewerOnly)
	if status, body := a.mcpSend(service, "kg.example", whoamiBody, whoamiHeaders); status != 200 {
		t.Fatalf("the viewer token of a service account: %d %s", status, body)
	}
	dev := newMCPAssembly(t, assemblyOptions{mode: "development", addr: "0.0.0.0:8080"})
	person = dev.mcpTokenFor(access.SubjectPerson, "yeonsu", viewerOnly, viewerOnly)
	if status, body := dev.mcpSend(person, "kg.example", whoamiBody, whoamiHeaders); status != 200 {
		t.Fatalf("in development a person's viewer token is taken: %d %s", status, body)
	}
}

// [REQ-505] A server on a loopback address takes only a loopback Host; on another address any Host; and one declared
// to be behind a proxy takes any Host too (the proxy hands over the outer name), with the Origin check kept.
func TestMCPHostCheckFollowsTheAddressAndTheProxy(t *testing.T) {
	cases := []struct {
		name    string
		addr    string
		behind  bool
		host    string
		want    int
		origin  string
		wantOrg int
	}{
		{"loopback, outer Host", "127.0.0.1:8080", false, "kg.example.com", 403, "", 0},
		{"loopback, loopback Host", "127.0.0.1:8080", false, "127.0.0.1:8080", 200, "", 0},
		{"all addresses, outer Host", "0.0.0.0:8080", false, "kg.example.com", 200, "", 0},
		{"loopback behind a proxy, outer Host", "127.0.0.1:8080", true, "kg.example.com", 200, "https://evil.example", 403},
	}
	for _, c := range cases {
		a := newMCPAssembly(t, assemblyOptions{mode: "production", addr: c.addr, behind: c.behind})
		token := a.mcpTokenFor(access.SubjectSystemAccount, "harness", viewerOnly, viewerOnly)
		if status, body := a.mcpSend(token, c.host, whoamiBody, whoamiHeaders); status != c.want {
			t.Errorf("%s: %d %s; want %d", c.name, status, body, c.want)
		}
		if c.origin != "" {
			headers := map[string]string{"Origin": c.origin}
			for k, v := range whoamiHeaders {
				headers[k] = v
			}
			if status, _ := a.mcpSend(token, c.host, whoamiBody, headers); status != c.wantOrg {
				t.Errorf("%s: an Origin that is not on the list: %d; want %d", c.name, status, c.wantOrg)
			}
		}
	}
}

// blockingLimiter is the failure limiter of the server, as a spy: it blocks every address and counts the failures.
type blockingLimiter struct{ blocked, failed int }

func (b *blockingLimiter) Blocked(string) (time.Duration, bool) {
	b.blocked++
	return time.Minute, true
}
func (b *blockingLimiter) Failed(string) { b.failed++ }

// [REQ-506] /mcp uses the limiter of the server (the object the REST routes use): an address it blocks gets 429.
func TestMCPUsesTheLimiterOfTheServer(t *testing.T) {
	lim := &blockingLimiter{}
	a := newMCPAssembly(t, assemblyOptions{mode: "production", addr: "0.0.0.0:8080", limiter: lim})
	status, _ := a.mcpSend("pxm_nope", "kg.example", whoamiBody, whoamiHeaders)
	if status != http.StatusTooManyRequests || lim.blocked == 0 || lim.failed != 0 {
		t.Fatalf("%d, asked %d times, counted %d failures", status, lim.blocked, lim.failed)
	}
}
