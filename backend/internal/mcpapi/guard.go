package mcpapi

import (
	"net/http"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// originOK is the check of where the request comes from, before anyone is asked for a token (kg-mcp.md 3): an Origin
// header, which only a browser sends, is taken only from the list of origins the operator gave (empty by default), and
// "null" never is; a server bound to a loopback address takes only a loopback Host, so a page that a name of its own
// points at the loopback cannot reach it (DNS rebinding). It writes the 403 itself.
func (h *Handler) originOK(a answer, r *http.Request) bool {
	if origin := r.Header.Values("Origin"); len(origin) > 0 {
		if len(origin) > 1 || origin[0] == "null" || !h.origins[origin[0]] {
			a.rpcError(http.StatusForbidden, nil, rpcError{Code: codeInvalidRequest, Message: "The Origin of the request is not allowed."})
			return false
		}
	}
	if h.cfg.CheckHost && !isLoopbackHost(r.Host) {
		a.rpcError(http.StatusForbidden, nil, rpcError{Code: codeInvalidRequest, Message: "The Host of the request is not allowed."})
		return false
	}
	return true
}

// isLoopbackHost says whether a Host header names the loopback: localhost, 127.0.0.1 or [::1], with or without a port.
func isLoopbackHost(host string) bool {
	name := host
	if strings.HasPrefix(host, "[") {
		end := strings.Index(host, "]")
		if end < 0 {
			return false
		}
		name = host[1:end]
		if rest := host[end+1:]; rest != "" && !strings.HasPrefix(rest, ":") {
			return false
		}
	} else if strings.Count(host, ":") == 1 {
		name, _, _ = strings.Cut(host, ":")
	}
	switch strings.ToLower(name) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// authenticate asks the gate for the subject of the token and applies the rule of which tokens /mcp takes. It writes the
// answer of a refusal.
func (h *Handler) authenticate(a answer, r *http.Request) (access.Subject, bool) {
	who := kgread.Caller{Key: h.clientKey(r), Path: r.URL.Path, Trace: a.trace}
	subject, p := h.cfg.Gate.Authenticate(r.Context(), who, r.Header.Get("Authorization"))
	if p != nil {
		a.kgProblem(p)
		return subject, false
	}
	if p := h.cfg.Gate.Authorize(subject, []string{kgread.RoleRead}); p != nil {
		a.kgProblem(p)
		return subject, false
	}
	if reason := h.refusedToken(subject); reason != "" {
		a.problem("permission.mcp_not_allowed", map[string]any{"reason": reason})
		return subject, false
	}
	return subject, true
}

// The reasons a token is not taken, as the 403 names them.
const (
	reasonRoles   = "roles_must_be_viewer_only"
	reasonAccount = "must_be_service_account"
)

// refusedToken is why /mcp does not take the subject's token, or "" when it takes it: the subject must hold the viewer
// role and no other (a token that can also manage must not sit in the settings file of an agent harness), and in
// production it must be a service account (ADR-0062 3).
func (h *Handler) refusedToken(s access.Subject) string {
	if len(s.Roles) != 1 || s.Roles[0] != access.RoleViewer {
		return reasonRoles
	}
	if h.cfg.ServiceAccountsOnly && s.Kind != access.SubjectSystemAccount {
		return reasonAccount
	}
	return ""
}

// clientKey is where the caller comes from, for the limiter and the audit line: the address of the connection, or behind
// a proxy that the operator declared, the last address the proxy appended to X-Forwarded-For. It is the REST server's
// rule, read here because the address of a request is the transport's.
func (h *Handler) clientKey(r *http.Request) string {
	if h.cfg.TrustProxy {
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			return last
		}
	}
	return hostOfAddr(r.RemoteAddr)
}

// hostOfAddr is the host part of "host:port" or "[host]:port".
func hostOfAddr(addr string) string {
	if strings.HasPrefix(addr, "[") {
		if end := strings.Index(addr, "]"); end > 0 {
			return addr[1:end]
		}
	}
	if i := strings.LastIndex(addr, ":"); i >= 0 && strings.Count(addr, ":") == 1 {
		return addr[:i]
	}
	return addr
}
