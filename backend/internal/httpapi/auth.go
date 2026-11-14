package httpapi

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The values of x-required-role that are not role names (rest-api.md 5-3).
const (
	// RoleAnonymous needs no login.
	RoleAnonymous = "anonymous"
	// RoleAuthenticated needs any login.
	RoleAuthenticated = "authenticated"
	// RoleRead needs a role that includes the read of its scope.
	RoleRead = "read"
)

// authorize judges a request against the roles a route lists (any one is enough)
// and writes the problem when the answer is no. It sets the subject on the call.
func (c *call) authorize(roles []string) bool {
	if contains(roles, RoleAnonymous) {
		return true
	}
	if !c.authenticate() {
		return false
	}
	for _, role := range roles {
		if c.satisfies(role) {
			return true
		}
	}
	c.fail("permission.role_missing", map[string]any{"required": roles})
	return false
}

func (c *call) satisfies(role string) bool {
	switch role {
	case RoleAuthenticated:
		return true
	case RoleRead:
		return c.s.authz.Decide(c.subject, access.AbilityRead, access.Target{}).Allowed
	}
	r, ok := access.ParseRole(role)
	return ok && c.subject.Holds(r)
}

// authenticate reads the bearer token and asks the Authenticator. It writes the problem and returns false when the
// request has no good token. A caller that keeps presenting refused tokens is blocked before its token is looked at
// (the limiter port, ADR-0044); an authenticator that cannot answer is a 503, never a 401 that would look like a
// refusal of the token.
func (c *call) authenticate() bool {
	header := c.r.Header.Get("Authorization")
	if header == "" {
		c.fail("auth.token_missing", nil)
		return false
	}
	key := c.clientKey()
	if c.s.limiter != nil {
		if wait, blocked := c.s.limiter.Blocked(key); blocked {
			c.authAudit("auth_blocked", "too many refused tokens")
			c.w.Header().Set("Retry-After", strconv.Itoa(int(wait/time.Second)+1))
			c.fail("rate_limited.requests", map[string]any{"retry_after_seconds": int(wait/time.Second) + 1})
			return false
		}
	}
	scheme, token, _ := strings.Cut(header, " ")
	token = strings.TrimSpace(token)
	if !strings.EqualFold(scheme, "Bearer") || token == "" || c.s.authn == nil {
		c.refuseToken(key, "token_invalid", "auth.token_invalid")
		return false
	}
	subject, err := c.askAuthenticator(token)
	switch {
	case err == nil:
		if c.s.limiter != nil {
			c.s.limiter.Succeeded(key)
		}
		c.subject, c.authed = subject, true
		return true
	case kernel.CodeOf(err) == access.CodeTokenExpired:
		c.refuseToken(key, "token_expired", "auth.token_expired")
	case isRefusal(err):
		c.refuseToken(key, string(kernel.CodeOf(err)), "auth.token_invalid")
	default:
		c.s.log.Error("the authenticator cannot answer", "error", err.Error(), "trace_id", c.trace)
		c.fail("server.unavailable", nil)
	}
	return false
}

// askAuthenticator prefers the authenticator that takes the context of the request.
func (c *call) askAuthenticator(token string) (access.Subject, error) {
	if ca, ok := c.s.authn.(access.ContextAuthenticator); ok {
		return ca.AuthenticateContext(c.r.Context(), token)
	}
	return c.s.authn.Authenticate(token)
}

// isRefusal says whether an error of the authenticator is a refusal of the token itself.
func isRefusal(err error) bool {
	switch kernel.CodeOf(err) {
	case access.CodeTokenInvalid, access.CodeTokenRevoked, access.CodeSubjectDisabled:
		return true
	}
	return false
}

// refuseToken counts the failure, writes the audit line and answers 401. The line names the reason and the caller's
// address, never the token that was presented.
func (c *call) refuseToken(key, reason, code string) {
	if c.s.limiter != nil {
		c.s.limiter.Failed(key)
	}
	c.authAudit("auth_failed", reason)
	c.fail(code, nil)
}

func (c *call) authAudit(event, reason string) {
	c.s.log.Warn(event, "audit", true, "event", event, "reason", reason, "remote", c.clientKey(),
		"path", c.r.URL.Path, "trace_id", c.trace)
}

// clientKey is where the caller comes from: the address of the connection, or behind a proxy that the operator
// declared, the last address the proxy appended to X-Forwarded-For (the one a caller cannot forge).
func (c *call) clientKey() string {
	if c.s.trustProxy {
		if parts := strings.Split(c.r.Header.Get("X-Forwarded-For"), ","); len(parts) > 0 {
			if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
				return last
			}
		}
	}
	host, _, err := net.SplitHostPort(c.r.RemoteAddr)
	if err != nil {
		host = c.r.RemoteAddr
	}
	return host
}
