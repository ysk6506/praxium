package mcpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// The tokens of the tests and who they are.
const (
	tokViewer   = "tok-viewer-person"
	tokService  = "tok-viewer-service"
	tokAdmin    = "tok-admin-person"
	tokTwoRoles = "tok-viewer-and-editor"
	tokSvcEdit  = "tok-editor-service"
	tokExpired  = "tok-expired"
)

// fakeAuth is an authenticator over a fixed table.
type fakeAuth struct{ subjects map[string]access.Subject }

func (f fakeAuth) Authenticate(token string) (access.Subject, error) {
	if token == tokExpired {
		return access.Subject{}, kernel.Errorf(access.CodeTokenExpired, "expired")
	}
	s, ok := f.subjects[token]
	if !ok {
		return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "unknown")
	}
	return s, nil
}

func subjects() map[string]access.Subject {
	return map[string]access.Subject{
		tokViewer:   {Kind: access.SubjectPerson, ID: "dev-user", DisplayName: "Dev", Roles: []access.Role{access.RoleViewer}, TokenID: "tok_person"},
		tokService:  {Kind: access.SubjectSystemAccount, ID: "agent-1", DisplayName: "Agent", Roles: []access.Role{access.RoleViewer}, TokenID: "tok_service"},
		tokAdmin:    {Kind: access.SubjectPerson, ID: "boss", Roles: []access.Role{access.RoleAdmin}, TokenID: "tok_admin"},
		tokTwoRoles: {Kind: access.SubjectPerson, ID: "two", Roles: []access.Role{access.RoleViewer, access.RoleEditor}},
		tokSvcEdit:  {Kind: access.SubjectSystemAccount, ID: "svc-edit", Roles: []access.Role{access.RoleEditor}},
	}
}

// logSink collects the lines the handler logs.
type logSink struct {
	mu    sync.Mutex
	lines []map[string]any
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range bytes.Split(bytes.TrimSpace(p), []byte("\n")) {
		var m map[string]any
		if err := json.Unmarshal(line, &m); err == nil {
			l.lines = append(l.lines, m)
		}
	}
	return len(p), nil
}

func (l *logSink) find(msg string) []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, m := range l.lines {
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

// rig is a handler over fixed parts.
type rig struct {
	t   *testing.T
	h   *Handler
	log *logSink
}

type rigOption func(*Config)

func newRig(t *testing.T, opts ...rigOption) *rig {
	t.Helper()
	sink := &logSink{}
	log := slog.New(slog.NewJSONHandler(sink, nil))
	cfg := Config{
		Enabled: true,
		Gate:    kgread.Gate{Authn: fakeAuth{subjects()}, Authz: access.RoleTable{}, Log: log},
		Info:    Info{Name: "praxium", Version: "test-1", ModelVersion: "7", ModelHash: "abc123"},
		Clock:   kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)),
		IDs:     kernel.NewSequentialIDs(),
		Log:     log,
	}
	for _, o := range opts {
		o(&cfg)
	}
	h, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, h: h, log: sink}
}

// call is a request to send. A zero Header means the headers of a good request are not added: use good() for those.
type call struct {
	method string
	path   string
	host   string
	header map[string]string
	body   string
}

// reply is what came back.
type reply struct {
	status int
	header http.Header
	body   []byte
}

func (rg *rig) do(c call) reply {
	rg.t.Helper()
	method, path := c.method, c.path
	if method == "" {
		method = http.MethodPost
	}
	if path == "" {
		path = Path
	}
	r := httptest.NewRequest(method, path, strings.NewReader(c.body))
	r.Host = "127.0.0.1:8080"
	if c.host != "" {
		r.Host = c.host
	}
	r.RemoteAddr = "192.0.2.10:4000"
	for k, v := range c.header {
		if v == "" {
			r.Header.Del(k)
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	rg.h.ServeHTTP(w, r)
	res := w.Result()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		rg.t.Fatal(err)
	}
	return reply{status: res.StatusCode, header: res.Header, body: body}
}

// rpc reads the body as a JSON-RPC message.
func (r reply) rpc(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("the body is not JSON: %q (%v)", r.body, err)
	}
	return m
}

// errCode is the code of the JSON-RPC error of the reply.
func (r reply) errCode(t *testing.T) int {
	t.Helper()
	e, ok := r.rpc(t)["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error in %s", r.body)
	}
	return int(e["code"].(float64))
}

// result is the result member of the reply.
func (r reply) result(t *testing.T) map[string]any {
	t.Helper()
	res, ok := r.rpc(t)["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %s", r.body)
	}
	return res
}

// problemCode is the code of a problem body.
func (r reply) problemCode(t *testing.T) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(r.body, &p); err != nil || p.Code == "" {
		t.Fatalf("no problem code in %s", r.body)
	}
	return p.Code
}

// meta is the _meta of a request of 2026-07-28.
func meta() map[string]any {
	return map[string]any{
		metaVersionKey: version2026,
		metaCapsKey:    map[string]any{},
		metaInfoKey:    map[string]any{"name": "TestClient", "version": "1.2"},
	}
}

type jsonRaw = json.RawMessage

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// message is the body of a request with an id.
func message(t *testing.T, id any, method string, params map[string]any) string {
	t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	return jsonOf(t, m)
}

// modernCall is a good request of 2026-07-28 for a token.
func modernCall(t *testing.T, token, method string, params map[string]any) call {
	t.Helper()
	p := map[string]any{"_meta": meta()}
	for k, v := range params {
		p[k] = v
	}
	h := map[string]string{
		"Authorization": "Bearer " + token, "Content-Type": "application/json", "Accept": "application/json, text/event-stream",
		"MCP-Protocol-Version": version2026, "Mcp-Method": method,
	}
	if name, ok := params["name"].(string); ok {
		h["Mcp-Name"] = name
	}
	return call{header: h, body: message(t, 1, method, p)}
}

// legacyCall is a good request of an older version for a token.
func legacyCall(t *testing.T, token, version, method string, params map[string]any) call {
	t.Helper()
	h := map[string]string{
		"Authorization": "Bearer " + token, "Content-Type": "application/json", "Accept": "application/json, text/event-stream",
		"MCP-Protocol-Version": version,
	}
	return call{header: h, body: message(t, 1, method, params)}
}

// withHeader returns a copy of the call with one header set (an empty value removes it).
func (c call) withHeader(k, v string) call {
	h := map[string]string{}
	for hk, hv := range c.header {
		h[hk] = hv
	}
	h[k] = v
	c.header = h
	return c
}

func (c call) withBody(body string) call {
	c.body = body
	return c
}
