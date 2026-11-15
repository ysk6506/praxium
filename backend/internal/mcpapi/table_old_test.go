package mcpapi

import (
	"net/http"
	"strings"
	"testing"
)

// initializeBody is the body of the older handshake.
func initializeBody(t *testing.T, version string) string {
	t.Helper()
	return message(t, 1, "initialize", map[string]any{
		"protocolVersion": version, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "Old", "version": "1"},
	})
}

func TestTableOlderVersionRows1To4(t *testing.T) {
	rg := newRig(t)
	for _, v := range legacyVersions {
		v := v
		t.Run("O1 initialize with a version it speaks gives that version, no session id: "+v, func(t *testing.T) {
			r := rg.do(legacyCall(t, tokViewer, v, "initialize", nil).withBody(initializeBody(t, v)))
			res := r.result(t)
			hasMembers(t, res, "protocolVersion", "capabilities", "serverInfo")
			if r.status != 200 || res["protocolVersion"] != v || r.header.Get("Mcp-Session-Id") != "" {
				t.Errorf("got %d %s %v", r.status, r.body, r.header)
			}
		})
		t.Run("O3 a notification is accepted with no body: "+v, func(t *testing.T) {
			c := legacyCall(t, tokViewer, v, "", nil)
			r := rg.do(c.withBody(message(t, nil, "notifications/initialized", nil)))
			if r.status != http.StatusAccepted || len(r.body) != 0 {
				t.Errorf("got %d %q", r.status, r.body)
			}
		})
		t.Run("O4 ping gives an empty result: "+v, func(t *testing.T) {
			r := rg.do(legacyCall(t, tokViewer, v, "ping", nil))
			if res := r.result(t); r.status != 200 || len(res) != 0 {
				t.Errorf("got %d %s", r.status, r.body)
			}
		})
	}
	t.Run("O2 initialize with a version it does not speak gives another it speaks", func(t *testing.T) {
		for _, asked := range []string{"2024-11-05", "2025-03-26", "9999-01-01", version2026} {
			r := rg.do(legacyCall(t, tokViewer, version2511, "initialize", nil).withBody(initializeBody(t, asked)))
			if got := r.result(t)["protocolVersion"]; r.status != 200 || got != version2511 {
				t.Errorf("asked %s: got %d %s", asked, r.status, r.body)
			}
		}
	})
}

// O5, O8 and O9 are values recommended in kg-mcp.md and not confirmed with a real client or a conformance suite.
func TestTableOlderVersionRows5To9(t *testing.T) {
	rg := newRig(t)
	for _, v := range legacyVersions {
		v := v
		t.Run("O5 server/discover is not a method of the older versions, 200 with -32601 (recommended value, not confirmed): "+v, func(t *testing.T) {
			wantRPCError(t, rg.do(legacyCall(t, tokViewer, v, "server/discover", nil)), http.StatusOK, codeMethodNotFound)
		})
		t.Run("O6 tools/list has no members of the new version: "+v, func(t *testing.T) {
			r := rg.do(legacyCall(t, tokViewer, v, "tools/list", nil))
			res := r.result(t)
			wantNoMembers(t, res, "resultType", "ttlMs", "cacheScope")
			if r.status != 200 || strings.Join(toolNames(t, res), ",") != "kg_whoami" {
				t.Errorf("got %d %s", r.status, r.body)
			}
		})
		t.Run("O7 tools/call has content, structuredContent and isError and no resultType: "+v, func(t *testing.T) {
			r := rg.do(legacyCall(t, tokViewer, v, "tools/call", map[string]any{"name": "kg_whoami"}))
			res := r.result(t)
			hasMembers(t, res, "content", "structuredContent", "isError")
			wantNoMembers(t, res, "resultType")
			if r.status != 200 || res["isError"] != false {
				t.Errorf("got %d %s", r.status, r.body)
			}
		})
		t.Run("O8 another method is 200 with -32601 (recommended value, not confirmed): "+v, func(t *testing.T) {
			wantRPCError(t, rg.do(legacyCall(t, tokViewer, v, "resources/list", nil)), http.StatusOK, codeMethodNotFound)
		})
		t.Run("O9 an unknown tool is 200 with -32602 and the names of the tools (recommended value, not confirmed): "+v, func(t *testing.T) {
			r := rg.do(legacyCall(t, tokViewer, v, "tools/call", map[string]any{"name": "kg_nothing"}))
			wantRPCError(t, r, http.StatusOK, codeInvalidParams)
			data, _ := r.rpc(t)["error"].(map[string]any)["data"].(map[string]any)
			if names, _ := data["tools"].([]any); len(names) != 1 || names[0] != "kg_whoami" {
				t.Errorf("data = %v", data)
			}
		})
	}
}

// [REQ-504]
func TestTableNoVersionHeaderRows(t *testing.T) {
	rg := newRig(t)
	t.Run("H1 initialize with no header is the older handshake", func(t *testing.T) {
		r := rg.do(legacyCall(t, tokViewer, "", "initialize", nil).withBody(initializeBody(t, version2511)))
		if r.status != 200 || r.result(t)["protocolVersion"] != version2511 {
			t.Errorf("got %d %s", r.status, r.body)
		}
	})
	t.Run("H2 a notification with no header is refused, with an error that has no id", func(t *testing.T) {
		r := rg.do(legacyCall(t, tokViewer, "", "", nil).withBody(message(t, nil, "notifications/initialized", nil)))
		wantRPCError(t, r, 400, codeHeaderMismatch)
		if _, has := r.rpc(t)["id"]; has {
			t.Errorf("an error to a notification has no id: %s", r.body)
		}
	})
	t.Run("H3 every other method with no header is refused and the text says which versions", func(t *testing.T) {
		for _, m := range []string{"ping", "server/discover", "tools/list", "tools/call", "nothing/here"} {
			r := rg.do(legacyCall(t, tokViewer, "", m, map[string]any{"name": "kg_whoami"}))
			wantRPCError(t, r, 400, codeHeaderMismatch)
			msg := r.rpc(t)["error"].(map[string]any)["message"].(string)
			if !strings.Contains(msg, "MCP-Protocol-Version") || !strings.Contains(msg, strings.Join(supportedVersions, ", ")) {
				t.Errorf("%s: message %q", m, msg)
			}
		}
	})
	if got := rg.h.Counts()[countNoVersionHeader]; got != 6 {
		t.Errorf("the refusals for no header are counted: %d, want 6 (REQ-504)", got)
	}
}

// [REQ-504]
func TestTableUnsupportedVersionRows(t *testing.T) {
	rg := newRig(t)
	for _, v := range []string{"2024-11-05", "2025-03-26", "bogus", "2099-01-01"} {
		v := v
		t.Run("U1 initialize ignores the header: "+v, func(t *testing.T) {
			r := rg.do(legacyCall(t, tokViewer, v, "initialize", nil).withBody(initializeBody(t, version2506)))
			if r.status != 200 || r.result(t)["protocolVersion"] != version2506 {
				t.Errorf("got %d %s", r.status, r.body)
			}
		})
		t.Run("U2 a notification is refused with an error that has no id: "+v, func(t *testing.T) {
			r := rg.do(legacyCall(t, tokViewer, v, "", nil).withBody(message(t, nil, "notifications/initialized", nil)))
			wantRPCError(t, r, 400, codeUnsupportedVers)
			if _, has := r.rpc(t)["id"]; has {
				t.Errorf("an error to a notification has no id: %s", r.body)
			}
		})
		t.Run("U3 every other method is refused with the versions it speaks: "+v, func(t *testing.T) {
			for _, m := range []string{"ping", "tools/list", "tools/call", "server/discover"} {
				r := rg.do(legacyCall(t, tokViewer, v, m, nil))
				wantRPCError(t, r, 400, codeUnsupportedVers)
				data := r.rpc(t)["error"].(map[string]any)["data"].(map[string]any)
				if sup, _ := data["supported"].([]any); len(sup) != 3 || data["requested"] != v {
					t.Errorf("%s: data = %v", m, data)
				}
			}
		})
	}
	if got := rg.h.Counts()[countUnsupportedHeader]; got != 4*(1+4) {
		t.Errorf("the refusals for an unsupported version are counted: %d, want 20 (REQ-504)", got)
	}
}

// [REQ-504]
func TestRequestsAreCountedByVersion(t *testing.T) {
	rg := newRig(t)
	rg.do(modernCall(t, tokViewer, "tools/list", nil))
	rg.do(modernCall(t, tokViewer, "tools/list", nil))
	rg.do(legacyCall(t, tokViewer, version2511, "ping", nil))
	rg.do(legacyCall(t, tokViewer, version2506, "ping", nil))
	rg.do(legacyCall(t, tokViewer, version2506, "ping", nil))
	rg.do(legacyCall(t, tokViewer, version2506, "initialize", nil).withBody(initializeBody(t, version2506)))
	got := rg.h.Counts()
	want := map[string]int64{version2026: 2, version2511: 1, version2506: 2, countInitialize: 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("count %q = %d, want %d", k, got[k], n)
		}
	}
}
