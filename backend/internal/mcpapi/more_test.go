package mcpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// The id of a request is given back as it came: a string stays a string and a number a number.
func TestTheIDIsGivenBackAsItCame(t *testing.T) {
	rg := newRig(t)
	for _, id := range []string{`"abc"`, `7`, `-3`, `1.5`} {
		body := `{"jsonrpc":"2.0","id":` + id + `,"method":"ping"}`
		r := rg.do(legacyCall(t, tokViewer, version2511, "ping", nil).withBody(body))
		if got := string(r.rpcRaw(t, "id")); got != id {
			t.Errorf("id %s came back as %s", id, got)
		}
	}
}

func (r reply) rpcRaw(t *testing.T, member string) []byte {
	t.Helper()
	m := map[string]jsonRaw{}
	if err := jsonUnmarshal(r.body, &m); err != nil {
		t.Fatal(err)
	}
	return m[member]
}

// A message that is not a valid JSON-RPC message is -32600 or -32602, and no answer reads it as another.
func TestMalformedMessagesAreRefused(t *testing.T) {
	rg := newRig(t)
	base := goodCall(t)
	for body, code := range map[string]int{
		`{"jsonrpc":"1.0","id":1,"method":"tools/list"}`:                 codeInvalidRequest,
		`{"id":1,"method":"tools/list"}`:                                 codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1}`:                                       codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1,"method":""}`:                           codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1,"method":5}`:                            codeInvalidRequest,
		`{"jsonrpc":"2.0","id":null,"method":"tools/list"}`:              codeInvalidRequest,
		`{"jsonrpc":"2.0","id":true,"method":"tools/list"}`:              codeInvalidRequest,
		`{"jsonrpc":"2.0","id":[1],"method":"tools/list"}`:               codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":[1]}`:    codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":"x"}`:    codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":null}`:   codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"a":}}`: codeParse,
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"} \x00":  codeParse,
	} {
		r := rg.do(base.withBody(body))
		got := r.errCode(t)
		if r.status != 400 || got != code {
			t.Errorf("%q: %d code %d; want 400 and %d", body, r.status, got, code)
		}
	}
	// A member the server does not know is ignored, as JSON-RPC allows.
	if r := rg.do(legacyCall(t, tokViewer, version2511, "tools/list", nil).withBody(`{"jsonrpc":"2.0","id":1,"method":"tools/list","extra":{"a":[1]}}`)); r.status != http.StatusOK {
		t.Errorf("an unknown member: %d %s", r.status, r.body)
	}
}

// The older handshake without a version is -32602 in the answer of the older versions (status 200).
func TestInitializeNeedsAVersionAndAcceptsNotifications(t *testing.T) {
	rg := newRig(t)
	wantRPCError(t, rg.do(legacyCall(t, tokViewer, "", "initialize", map[string]any{})), http.StatusOK, codeInvalidParams)
	if r := rg.do(legacyCall(t, tokViewer, "", "initialize", nil).withBody(message(t, nil, "initialize", map[string]any{"protocolVersion": version2506}))); r.status != http.StatusAccepted {
		t.Errorf("initialize as a notification: %d", r.status)
	}
}

// There is one page of tools: a cursor is refused, a null cursor is none.
func TestToolsListTakesNoCursor(t *testing.T) {
	rg := newRig(t)
	wantRPCError(t, rg.do(modernCall(t, tokViewer, "tools/list", map[string]any{"cursor": "abc"})), 400, codeInvalidParams)
	if r := rg.do(modernCall(t, tokViewer, "tools/list", map[string]any{"cursor": nil})); r.status != 200 {
		t.Errorf("a null cursor is none: %d %s", r.status, r.body)
	}
}

// Behind a proxy the caller is the last address of X-Forwarded-For; otherwise the address of the connection.
func TestCallerAddressForTheLimiter(t *testing.T) {
	cases := []struct {
		trust      bool
		remote, xf string
		want       string
	}{
		{false, "192.0.2.10:4000", "198.51.100.1", "192.0.2.10"},
		{true, "192.0.2.10:4000", "203.0.113.9, 198.51.100.1", "198.51.100.1"},
		{true, "192.0.2.10:4000", "", "192.0.2.10"},
		{false, "[2001:db8::1]:4000", "", "2001:db8::1"},
		{false, "::1", "", "::1"},
		{false, "nohost", "", "nohost"},
	}
	for _, c := range cases {
		h := &Handler{cfg: Config{TrustProxy: c.trust}}
		r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
		r.RemoteAddr = c.remote
		if c.xf != "" {
			r.Header.Set("X-Forwarded-For", c.xf)
		}
		if got := h.clientKey(r); got != c.want {
			t.Errorf("%+v: %q, want %q", c, got, c.want)
		}
	}
}

// blockedLimiter says every key must wait.
type blockedLimiter struct{ failed int }

func (b *blockedLimiter) Blocked(string) (time.Duration, bool) { return 90 * time.Second, true }
func (b *blockedLimiter) Failed(string)                        { b.failed++ }

// A refused token from a blocked address is 429 with Retry-After, as on the REST server, and is not counted.
func TestARefusedTokenFromABlockedAddressIs429(t *testing.T) {
	lim := &blockedLimiter{}
	rg := newRig(t, func(c *Config) { c.Gate.Limiter = lim })
	r := rg.do(goodCall(t).withHeader("Authorization", "Bearer nope"))
	if r.status != http.StatusTooManyRequests || r.header.Get("Retry-After") != "91" || r.problemCode(t) != "rate_limited.requests" || lim.failed != 0 {
		t.Errorf("%d %v %s failed=%d", r.status, r.header, r.body, lim.failed)
	}
	if r := rg.do(goodCall(t)); r.status != 200 {
		t.Errorf("a good token passes from a blocked address: %d", r.status)
	}
}

// brokenAuth cannot answer.
type brokenAuth struct{}

func (brokenAuth) Authenticate(string) (access.Subject, error) {
	return access.Subject{}, errors.New("store down")
}

// An authenticator that cannot answer is 503 and not a refusal of the token.
func TestAnAuthenticatorThatCannotAnswerIs503(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.Gate.Authn = brokenAuth{} })
	if r := rg.do(goodCall(t)); r.status != 503 || r.problemCode(t) != "server.unavailable" {
		t.Errorf("%d %s", r.status, r.body)
	}
}

// A subject that cannot read at all is refused by the role check of the gate, as on the REST server.
func TestASubjectWithoutTheReadAbilityIsRefused(t *testing.T) {
	rg := newRig(t, func(c *Config) {
		c.Gate.Authn = fakeAuth{map[string]access.Subject{"x": {Kind: access.SubjectPerson, ID: "x", Roles: []access.Role{"nothing"}}}}
	})
	if r := rg.do(goodCall(t).withHeader("Authorization", "Bearer x")); r.status != 403 || r.problemCode(t) != "permission.role_missing" {
		t.Errorf("%d %s", r.status, r.body)
	}
}

// refusingTool answers with a problem that has params and fields, which are cleaned.
func refusingTool() Tool {
	return Tool{
		Name: "kg_refuse", Description: "Refuses.", Kind: KindKGRead, Input: []byte(`{"type":"object"}`), Output: []byte(`{"type":"object"}`),
		Run: func(context.Context, Call) Result {
			return Result{Problem: &kgread.Problem{
				Code: "not_found.resource", Params: map[string]any{"id": "a‮b", "list": []string{"x\u0007y"}, "n": 3, "ok": true, "nil": nil, "nested": map[string]any{"k\u0007": []any{"v​", 1.5}}, "odd": struct{}{}},
				Fields: []kgread.FieldError{{Pointer: "/a‮", Code: "invalid_input.field", Params: map[string]any{"reason": "x\u0007"}}},
			}}
		},
	}
}

// The params and the fields of a refusal are cleaned as a whole.
func TestRefusalParamsAreCleanedDeeply(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.Tools = []Tool{refusingTool()} })
	r := rg.do(legacyCall(t, tokViewer, version2511, "tools/call", map[string]any{"name": "kg_refuse"}))
	text := r.result(t)["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.ContainsAny(text, "‮\u0007​") || !strings.Contains(text, `"id":"ab"`) || !strings.Contains(text, `"/a"`) || !strings.Contains(text, `"odd":"?"`) {
		t.Errorf("text = %s", text)
	}
}

// A value of a tool that cannot be written out is a bug of the server: -32603, as kg-mcp.md 4.4 says (500 on the new
// version, 200 on the older ones), and not a result of the tool.
func TestAResultThatCannotBeWrittenIsInternalError(t *testing.T) {
	bad := refusingTool()
	bad.Name = "kg_unwritable"
	bad.Run = func(context.Context, Call) Result { return Result{Structured: map[string]any{"c": make(chan int)}} }
	rg := newRig(t, func(c *Config) { c.Tools = []Tool{bad} })
	args := map[string]any{"name": "kg_unwritable"}
	wantRPCError(t, rg.do(legacyCall(t, tokViewer, version2511, "tools/call", args)), http.StatusOK, codeInternal)
	wantRPCError(t, rg.do(modernCall(t, tokViewer, "tools/call", args)), http.StatusInternalServerError, codeInternal)
}

// The value of q in an Accept header that is not a number means zero.
func TestAcceptQualityThatIsNotANumberIsZero(t *testing.T) {
	if acceptsJSON("application/json;q=abc") || !acceptsJSON("application/json;q=1;x=y") {
		t.Error("the quality of a media range is read as a number")
	}
}

// Arguments that are not an object to a tool that takes none are not read as keys.
func TestNoArgumentsToleratesAnEmptyObject(t *testing.T) {
	if p := noArguments([]byte(`{}`)); p != nil {
		t.Errorf("an empty object is no argument: %v", p)
	}
	if p := noArguments([]byte(`[1]`)); p != nil {
		t.Errorf("a non-object is the transport's to refuse: %v", p)
	}
	p := noArguments([]byte(`{"b":1,"a":2}`))
	if p == nil || len(p.Fields) != 2 || p.Fields[0].Pointer != "/a" {
		t.Errorf("the keys are named in order: %+v", p)
	}
}

// A refused hex digit or a short escape is a syntax error, and a number of every form is read.
func TestParserCornerForms(t *testing.T) {
	for _, in := range []string{`"\u00zz"`, `"\ud83d\u00zz"`, `"\ud83d"`, `-`, `1e+`, `1E5`, `0.0`, `-0`, `[ ]`, `{ }`} {
		_, err := parseStrict([]byte(in))
		valid := in == `1E5` || in == `0.0` || in == `-0` || in == `[ ]` || in == `{ }`
		if valid != (err == nil) {
			t.Errorf("parseStrict(%q) error = %v", in, err)
		}
	}
}
