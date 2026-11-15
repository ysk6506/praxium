package mcpapi

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/contract"
)

// The code and the contract file say the same: names, descriptions, schemas, annotations.
func TestRegistryMatchesTheContractFile(t *testing.T) {
	doc, err := contract.MCPTools()
	if err != nil {
		t.Fatal(err)
	}
	tools := DefaultRegistry(ToolDeps{}).List(KindKGRead)
	if len(tools) != len(doc.Tools) {
		t.Fatalf("the registry has %d tools of the kind, the contract file %d", len(tools), len(doc.Tools))
	}
	for i, tool := range tools {
		inCode, err := Definition(tool)
		if err != nil {
			t.Fatal(err)
		}
		inFile, err := json.Marshal(doc.Tools[i])
		if err != nil {
			t.Fatal(err)
		}
		if !sameJSON(t, inCode, inFile) {
			t.Errorf("tool %s differs.\ncode: %s\nfile: %s", tool.Name, inCode, inFile)
		}
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

// tools/list of /mcp is exactly the tools of the contract file, in every version, even if the registry holds
// tools of other kinds; those can be neither listed nor called.
func TestToolsListEqualsTheContractAndHidesOtherKinds(t *testing.T) {
	foreign := Tool{
		Name: "gw_restart", Description: "A tool of another surface.", Kind: "gateway", Input: []byte(`{"type":"object"}`), Output: []byte(`{"type":"object"}`),
		Run: func(context.Context, Call) Result { return Result{Structured: map[string]any{"ran": true}} },
	}
	misnamed := foreign
	misnamed.Name = "kg_sneaky"
	rg := newRig(t, func(c *Config) { c.Tools = []Tool{foreign, misnamed} })
	doc, err := contract.MCPTools()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, tool := range doc.Tools {
		want = append(want, tool.Name)
	}
	lists := map[string]reply{
		version2026: rg.do(modernCall(t, tokViewer, "tools/list", nil)),
		version2511: rg.do(legacyCall(t, tokViewer, version2511, "tools/list", nil)),
		version2506: rg.do(legacyCall(t, tokViewer, version2506, "tools/list", nil)),
	}
	for v, r := range lists {
		if got := toolNames(t, r.result(t)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s lists %v; the contract says %v", v, got, want)
		}
	}
	for _, name := range []string{"gw_restart", "kg_sneaky"} {
		r := rg.do(modernCall(t, tokViewer, "tools/call", map[string]any{"name": name}))
		wantRPCError(t, r, 400, codeInvalidParams)
	}
}

// The order of tools/list is the same call after call.
func TestToolsListOrderIsFixed(t *testing.T) {
	extra := func(name string) Tool {
		return Tool{Name: name, Description: "x", Kind: KindKGRead, Input: []byte(`{"type":"object"}`), Output: []byte(`{"type":"object"}`)}
	}
	rg := newRig(t, func(c *Config) { c.Tools = []Tool{extra("kg_zeta"), extra("kg_alpha"), extra("kg_mid")} })
	first := rg.do(modernCall(t, tokViewer, "tools/list", nil)).body
	for i := 0; i < 20; i++ {
		if got := rg.do(modernCall(t, tokViewer, "tools/list", nil)).body; string(got) != string(first) {
			t.Fatalf("the list changed between calls")
		}
	}
	names := toolNames(t, rg.do(modernCall(t, tokViewer, "tools/list", nil)).result(t))
	if strings.Join(names, ",") != "kg_alpha,kg_mid,kg_whoami,kg_zeta" {
		t.Errorf("order = %v; want by name", names)
	}
}

// Every tool has the read-only annotations in the list.
func TestToolsAreAnnotatedReadOnly(t *testing.T) {
	rg := newRig(t)
	list, _ := rg.do(modernCall(t, tokViewer, "tools/list", nil)).result(t)["tools"].([]any)
	for _, e := range list {
		a, _ := e.(map[string]any)["annotations"].(map[string]any)
		if a["readOnlyHint"] != true || a["destructiveHint"] != false || a["openWorldHint"] != false {
			t.Errorf("annotations = %v", a)
		}
	}
}

// callWhoami runs kg_whoami in the version and returns the result of the call.
func callWhoami(t *testing.T, rg *rig, token string) map[string]any {
	t.Helper()
	r := rg.do(modernCall(t, token, "tools/call", map[string]any{"name": "kg_whoami"}))
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	return r.result(t)
}

// [REQ-518]
func TestWhoamiAnswersAboutTheCaller(t *testing.T) {
	rg := newRig(t)
	res := callWhoami(t, rg, tokService)
	sc, _ := res["structuredContent"].(map[string]any)
	data, _ := sc["data"].(map[string]any)
	acct, _ := data["account"].(map[string]any)
	if acct["id"] != "agent-1" || acct["kind"] != "system_account" || acct["display_name"] != "Agent" {
		t.Errorf("account = %v", acct)
	}
	if roles, _ := data["roles"].([]any); len(roles) != 1 || roles[0] != "viewer" {
		t.Errorf("roles = %v", data["roles"])
	}
	token, _ := data["token"].(map[string]any)
	if token["id"] != "tok_service" || token["expires_at"] != nil {
		t.Errorf("the token has an id and, without a directory, no expiry: %v", token)
	}
	limits, _ := data["limits"].(map[string]any)
	if limits["enforced"] != false || limits["note"] == "" {
		t.Errorf("limits say that none is enforced: %v", limits)
	}
	srv, _ := data["server"].(map[string]any)
	model, _ := data["model"].(map[string]any)
	if srv["name"] != "praxium" || srv["version"] != "test-1" || srv["read_only"] != true || model["version"] != "7" || model["hash"] != "abc123" {
		t.Errorf("server = %v, model = %v", srv, model)
	}
	if pv, _ := srv["protocol_versions"].([]any); len(pv) != 3 {
		t.Errorf("protocol_versions = %v", pv)
	}
}

// [REQ-518] The answer never holds the token value, and the person's token has no expiry in the development
// authenticator, where there is no directory.
func TestWhoamiNeverAnswersTheTokenValue(t *testing.T) {
	rg := newRig(t)
	res := callWhoami(t, rg, tokViewer)
	if strings.Contains(jsonOf(t, res), tokViewer) {
		t.Errorf("the answer holds the token: %v", res)
	}
}

// fakeDirectory answers the expiry of a token.
type fakeDirectory struct {
	access.Directory
	expires time.Time
}

func (f fakeDirectory) GetToken(_ context.Context, id string) (access.TokenInfo, error) {
	return access.TokenInfo{ID: id, ExpiresAt: f.expires}, nil
}

// [REQ-518]
func TestWhoamiGivesTheExpiryOfATokenOfTheDirectory(t *testing.T) {
	when := time.Date(2031, 5, 6, 7, 8, 9, 0, time.FixedZone("x", 3600))
	rg := newRig(t, func(c *Config) { c.Directory = fakeDirectory{expires: when} })
	sc := callWhoami(t, rg, tokService)["structuredContent"].(map[string]any)
	token := sc["data"].(map[string]any)["token"].(map[string]any)
	if token["expires_at"] != "2031-05-06T06:08:09Z" {
		t.Errorf("token = %v", token)
	}
}

// [REQ-518] The structured result and the text blocks hold the same JSON in the order meta, next, data, and
// the first block is the fixed line.
func TestResultShapeIsMetaNextData(t *testing.T) {
	rg := newRig(t)
	for _, v := range legacyVersions {
		res := rg.do(legacyCall(t, tokViewer, v, "tools/call", map[string]any{"name": "kg_whoami"})).result(t)
		content := res["content"].([]any)
		first := content[0].(map[string]any)["text"].(string)
		second := content[1].(map[string]any)["text"].(string)
		if first != firstLine || !sameJSON(t, []byte(second), []byte(jsonOf(t, res["structuredContent"]))) {
			t.Errorf("%s: content = %v", v, content)
		}
		i, j, k := strings.Index(second, `"meta"`), strings.Index(second, `"next"`), strings.Index(second, `"data"`)
		if !(i >= 0 && i < j && j < k) {
			t.Errorf("%s: the order of members must be meta, next, data: %s", v, second)
		}
	}
}

// A tool's refusal is a result with isError, a text and no structuredContent; the words of the caller in it
// are cleaned.
func TestToolRefusalIsAnErrorResult(t *testing.T) {
	rg := newRig(t)
	args := map[string]any{"name": "kg_whoami", "arguments": map[string]any{"bad\u202e\u0007key": 1}}
	for _, r := range []reply{
		rg.do(modernCall(t, tokViewer, "tools/call", args)),
		rg.do(legacyCall(t, tokViewer, version2511, "tools/call", args)),
	} {
		res := r.result(t)
		if r.status != 200 || res["isError"] != true {
			t.Fatalf("%d %s", r.status, r.body)
		}
		wantNoMembers(t, res, "structuredContent")
		text := res["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, `"code":"invalid_input.field"`) || !strings.Contains(text, "/badkey") || strings.ContainsAny(text, "\u202e\u0007") {
			t.Errorf("the text of the refusal: %s", text)
		}
	}
}

// Arguments that are not an object are a protocol error; a name that is not a string cannot match the
// Mcp-Name header of the new version and is a mismatch there, and a protocol error in the older versions.
func TestMalformedCallIsAProtocolError(t *testing.T) {
	rg := newRig(t)
	for _, params := range []map[string]any{{"name": "kg_whoami", "arguments": []any{1}}, {"name": "kg_whoami", "arguments": "x"}} {
		wantRPCError(t, rg.do(modernCall(t, tokViewer, "tools/call", params)), 400, codeInvalidParams)
	}
	for _, params := range []map[string]any{{"name": 5}, {"arguments": map[string]any{}}} {
		c := modernCall(t, tokViewer, "tools/call", params).withHeader("Mcp-Name", "kg_whoami")
		wantRPCError(t, rg.do(c), 400, codeHeaderMismatch)
		wantRPCError(t, rg.do(legacyCall(t, tokViewer, version2511, "tools/call", params)), 200, codeInvalidParams)
	}
	ok := modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_whoami", "arguments": nil})
	if r := rg.do(ok); r.status != 200 || r.result(t)["isError"] != false {
		t.Errorf("null arguments mean none: %d %s", r.status, r.body)
	}
}

// The name the caller sent is cleaned where the error repeats it.
func TestUnknownToolNameIsCleanedWhereRepeated(t *testing.T) {
	rg := newRig(t)
	name := "kg_x\u202e\u200b\u0007" + strings.Repeat("y", 200)
	r := rg.do(modernCall(t, tokViewer, "tools/call", map[string]any{"name": name}).withHeader("Mcp-Name", name))
	wantRPCError(t, r, 400, codeInvalidParams)
	data := r.rpc(t)["error"].(map[string]any)["data"].(map[string]any)
	requested := data["requested"].(string)
	if len(requested) != shortText || strings.ContainsAny(requested, "\u202e\u200b\u0007") || !strings.HasPrefix(requested, "kg_xyy") {
		t.Errorf("requested = %q", requested)
	}
	if msg := r.rpc(t)["error"].(map[string]any)["message"]; msg != "Unknown tool." {
		t.Errorf("the message is a fixed text: %v", msg)
	}
}

// One line is written for each call of a tool, with the fields of the record and none of the arguments.
func TestReadRecordIsOneLinePerCall(t *testing.T) {
	rg := newRig(t)
	callWhoami(t, rg, tokService)
	rg.do(legacyCall(t, tokViewer, version2506, "tools/call", map[string]any{"name": "kg_whoami"}))
	rg.do(modernCall(t, tokViewer, "tools/list", nil))
	lines := rg.log.find("mcp_read")
	if len(lines) != 2 {
		t.Fatalf("%d lines; want 2 (a call of a tool each, and none for tools/list)", len(lines))
	}
	l := lines[0]
	if l["tool"] != "kg_whoami" || l["token_id"] != "tok_service" || l["protocol_version"] != version2026 || l["outcome"] != "ok" ||
		l["client_info"] != "TestClient/1.2" || l["trace_id"] == "" || l["result_count"] != float64(0) || l["truncated"] != false {
		t.Errorf("line = %v", l)
	}
	if lines[1]["protocol_version"] != version2506 || lines[1]["client_info"] != "" {
		t.Errorf("an older version carries no clientInfo in the call: %v", lines[1])
	}
}

// The client's own words and the trace parent are cleaned and cut.
func TestReadRecordCleansTheClientsWords(t *testing.T) {
	rg := newRig(t)
	m := meta()
	m[metaInfoKey] = map[string]any{"name": "Evil\u202e\u0007" + strings.Repeat("n", 300), "version": "1\n2"}
	m["traceparent"] = "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01" + strings.Repeat("z", 100)
	c := modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_whoami"}).withBody(message(t, 1, "tools/call", map[string]any{"_meta": m, "name": "kg_whoami"}))
	rg.do(c)
	l := rg.log.find("mcp_read")[0]
	info, _ := l["client_info"].(string)
	if len([]rune(info)) > 2*clientText+1 || strings.ContainsAny(info, "\u202e\u0007\n") || !strings.HasSuffix(info, "/1 2") {
		t.Errorf("client_info = %q", info)
	}
	if tp, _ := l["traceparent"].(string); len(tp) != 55 {
		t.Errorf("traceparent = %q", tp)
	}
}

// [REQ-504] kg_whoami tells the numbers of requests by version since the start.
func TestWhoamiShowsTheCountsByVersion(t *testing.T) {
	rg := newRig(t)
	rg.do(legacyCall(t, tokViewer, version2511, "ping", nil))
	rg.do(legacyCall(t, tokViewer, "", "ping", nil))
	sc := callWhoami(t, rg, tokViewer)["structuredContent"].(map[string]any)
	counts := sc["data"].(map[string]any)["server"].(map[string]any)["requests_by_version"].(map[string]any)
	if counts[version2511] != float64(1) || counts[version2026] != float64(1) || counts[countNoVersionHeader] != float64(1) {
		t.Errorf("counts = %v", counts)
	}
}

// A handler needs its parts.
func TestNewNeedsItsParts(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("a handler with no clock, ids, logger or gate is refused")
	}
}
