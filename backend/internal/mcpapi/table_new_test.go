package mcpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// The tests of kg-mcp.md 2.1 are one case to a row, named by the row's number. They compare what a row means (the
// status, the code, the members that must be there) and not the bytes, so they hold if the transport is replaced.

func wantRPCError(t *testing.T, r reply, status, code int) {
	t.Helper()
	if r.status != status || r.errCode(t) != code {
		t.Errorf("got %d %s; want %d and code %d", r.status, r.body, status, code)
	}
}

func wantNoMembers(t *testing.T, res map[string]any, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, ok := res[n]; ok {
			t.Errorf("the result must not have %q: %v", n, res)
		}
	}
}

func hasMembers(t *testing.T, res map[string]any, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, ok := res[n]; !ok {
			t.Errorf("the result lacks %q: %v", n, res)
		}
	}
}

func toolNames(t *testing.T, res map[string]any) []string {
	t.Helper()
	list, _ := res["tools"].([]any)
	var out []string
	for _, e := range list {
		out = append(out, e.(map[string]any)["name"].(string))
	}
	return out
}

func TestTableNewVersionRows1To7(t *testing.T) {
	rg := newRig(t)
	t.Run("N1 initialize is the older handshake whatever the header", func(t *testing.T) {
		c := modernCall(t, tokViewer, "initialize", map[string]any{"protocolVersion": version2511})
		r := rg.do(c)
		res := r.result(t)
		if r.status != http.StatusOK || res["protocolVersion"] != version2511 || r.header.Get("Mcp-Session-Id") != "" {
			t.Errorf("got %d %s", r.status, r.body)
		}
		wantNoMembers(t, res, "resultType")
	})
	t.Run("N2 a notification is accepted with no body", func(t *testing.T) {
		for _, method := range []string{"notifications/initialized", "notifications/anything"} {
			r := rg.do(modernCall(t, tokViewer, method, nil).withBody(message(t, nil, method, map[string]any{"_meta": meta()})))
			if r.status != http.StatusAccepted || len(r.body) != 0 {
				t.Errorf("%s: got %d %q; want 202 and no body", method, r.status, r.body)
			}
		}
	})
	t.Run("N3 ping is not a method of the new version", func(t *testing.T) {
		wantRPCError(t, rg.do(modernCall(t, tokViewer, "ping", nil)), http.StatusNotFound, codeMethodNotFound)
	})
	t.Run("N4 server/discover", func(t *testing.T) {
		r := rg.do(modernCall(t, tokViewer, "server/discover", nil))
		res := r.result(t)
		hasMembers(t, res, "resultType", "supportedVersions", "capabilities", "_meta", "instructions", "ttlMs", "cacheScope")
		sv, _ := res["supportedVersions"].([]any)
		if r.status != 200 || res["resultType"] != "complete" || len(sv) != 3 || res["cacheScope"] != "private" {
			t.Errorf("got %d %s", r.status, r.body)
		}
		info, _ := objectAt(res, "_meta", "io.modelcontextprotocol/serverInfo")
		if info["name"] != "praxium" || info["version"] != "test-1" {
			t.Errorf("serverInfo = %v", info)
		}
	})
	t.Run("N5 tools/list", func(t *testing.T) {
		r := rg.do(modernCall(t, tokViewer, "tools/list", nil))
		res := r.result(t)
		hasMembers(t, res, "resultType", "tools", "ttlMs", "cacheScope")
		if r.status != 200 || res["resultType"] != "complete" || strings.Join(toolNames(t, res), ",") != "kg_whoami" {
			t.Errorf("got %d %s", r.status, r.body)
		}
	})
	t.Run("N6 tools/call", func(t *testing.T) {
		r := rg.do(modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_whoami"}))
		res := r.result(t)
		hasMembers(t, res, "resultType", "content", "structuredContent", "isError")
		if r.status != 200 || res["resultType"] != "complete" || res["isError"] != false || r.header.Get("Content-Type") != "application/json" {
			t.Errorf("got %d %s", r.status, r.body)
		}
	})
	t.Run("N7 a method it does not have", func(t *testing.T) {
		wantRPCError(t, rg.do(modernCall(t, tokViewer, "resources/list", nil)), http.StatusNotFound, codeMethodNotFound)
	})
}

func TestTableNewVersionRows8To10(t *testing.T) {
	rg := newRig(t)
	t.Run("N8 the header and the _meta name different versions", func(t *testing.T) {
		wantRPCError(t, rg.do(modernCall(t, tokViewer, "tools/list", nil).withHeader("MCP-Protocol-Version", version2511)), 400, codeHeaderMismatch)
		wantRPCError(t, rg.do(modernCall(t, tokViewer, "tools/list", nil).withHeader("MCP-Protocol-Version", "")), 400, codeHeaderMismatch)
	})
	t.Run("N9 the _meta lacks the version or the capabilities", func(t *testing.T) {
		noVersion := modernCall(t, tokViewer, "tools/list", nil).withBody(message(t, 1, "tools/list", map[string]any{"_meta": map[string]any{metaCapsKey: map[string]any{}}}))
		wantRPCError(t, rg.do(noVersion), 400, codeInvalidParams)
		noCaps := modernCall(t, tokViewer, "tools/list", nil).withBody(message(t, 1, "tools/list", map[string]any{"_meta": map[string]any{metaVersionKey: version2026}}))
		wantRPCError(t, rg.do(noCaps), 400, codeInvalidParams)
		noMeta := modernCall(t, tokViewer, "tools/list", nil).withBody(message(t, 1, "tools/list", nil))
		wantRPCError(t, rg.do(noMeta), 400, codeInvalidParams)
	})
	t.Run("N10 Mcp-Method and Mcp-Name must be there and say what the body says", func(t *testing.T) {
		list := modernCall(t, tokViewer, "tools/list", nil)
		wantRPCError(t, rg.do(list.withHeader("Mcp-Method", "")), 400, codeHeaderMismatch)
		wantRPCError(t, rg.do(list.withHeader("Mcp-Method", "tools/call")), 400, codeHeaderMismatch)
		call := modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_whoami"})
		wantRPCError(t, rg.do(call.withHeader("Mcp-Name", "")), 400, codeHeaderMismatch)
		wantRPCError(t, rg.do(call.withHeader("Mcp-Name", "kg_other")), 400, codeHeaderMismatch)
	})
}

// failingTool answers with a bug of the server, which is a protocol error and not a result.
func failingTool() Tool {
	return Tool{
		Name: "kg_boom", Description: "Fails.", Kind: KindKGRead, Input: []byte(`{"type":"object"}`), Output: []byte(`{"type":"object"}`),
		Run: func(_ context.Context, _ Call) Result {
			return Result{Problem: &kgread.Problem{Code: "server.internal"}}
		},
	}
}

// N11 and N12 are values recommended in kg-mcp.md and not confirmed with a real client or a conformance suite.
func TestTableNewVersionRows11To13(t *testing.T) {
	rg := newRig(t, func(c *Config) { c.Tools = []Tool{failingTool()} })
	t.Run("N11 an unknown tool is -32602 with the names of the tools (recommended value, not confirmed)", func(t *testing.T) {
		r := rg.do(modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_nothing"}))
		wantRPCError(t, r, http.StatusBadRequest, codeInvalidParams)
		data, _ := r.rpc(t)["error"].(map[string]any)["data"].(map[string]any)
		names, _ := data["tools"].([]any)
		if len(names) != 2 || names[0] != "kg_boom" || names[1] != "kg_whoami" || data["requested"] != "kg_nothing" {
			t.Errorf("data = %v", data)
		}
	})
	t.Run("N12 a bug of the server is -32603 with 500 (recommended value, not confirmed)", func(t *testing.T) {
		wantRPCError(t, rg.do(modernCall(t, tokViewer, "tools/call", map[string]any{"name": "kg_boom"})), http.StatusInternalServerError, codeInternal)
	})
	t.Run("N13 a _meta that names an older version follows that version if the header agrees (recommended value, not confirmed)", func(t *testing.T) {
		for _, v := range legacyVersions {
			m := map[string]any{metaVersionKey: v, metaCapsKey: map[string]any{}}
			c := modernCall(t, tokViewer, "tools/list", nil).withHeader("MCP-Protocol-Version", v).withBody(message(t, 1, "tools/list", map[string]any{"_meta": m}))
			res := rg.do(c).result(t)
			wantNoMembers(t, res, "resultType", "ttlMs", "cacheScope")
			hasMembers(t, res, "tools")
			other := version2511
			if v == version2511 {
				other = version2506
			}
			wantRPCError(t, rg.do(c.withHeader("MCP-Protocol-Version", other)), 400, codeHeaderMismatch)
		}
	})
}
