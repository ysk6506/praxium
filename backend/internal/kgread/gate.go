package kgread

import (
	"context"
	"log/slog"
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

// Gate turns a credential into a subject and judges the subject against the roles a route lists.
type Gate struct {
	// Authn answers a token; nil means no one can authenticate.
	Authn access.Authenticator
	// Authz judges abilities; it must not be nil.
	Authz access.Authorizer
	// Limiter slows down a caller that keeps presenting refused tokens; nil means no limit.
	Limiter access.FailureLimiter
	// Log receives the audit lines and the failures of the authenticator.
	Log *slog.Logger
}

// Caller is who is asking, for the limiter and the audit line: Key is where the caller comes from (the adapter
// finds it), Path what was asked, Trace the id of the call. The credential itself is never logged.
type Caller struct {
	Key, Path, Trace string
}

// Authenticate reads the credential (the value of an Authorization header: "Bearer <token>") and asks the
// authenticator. It returns the problem when the caller has no good token. The token is looked at first: a good one
// passes even from an address that is blocked, because many people share one address behind a proxy. A refused token
// from a blocked address is answered rate_limited and is not counted (the limiter port, ADR-0044); an authenticator
// that cannot answer is server.unavailable, never a refusal of the token.
func (g Gate) Authenticate(ctx context.Context, who Caller, credential string) (access.Subject, *Problem) {
	if credential == "" {
		return access.Subject{}, &Problem{Code: "auth.token_missing"}
	}
	scheme, token, _ := strings.Cut(credential, " ")
	token = strings.TrimSpace(token)
	if !strings.EqualFold(scheme, "Bearer") || token == "" || g.Authn == nil {
		return access.Subject{}, g.refuseToken(who, "token_invalid", "auth.token_invalid")
	}
	subject, err := g.ask(ctx, token)
	switch {
	case err == nil:
		return subject, nil
	case kernel.CodeOf(err) == access.CodeTokenExpired:
		return access.Subject{}, g.refuseToken(who, "token_expired", "auth.token_expired")
	case isRefusal(err):
		return access.Subject{}, g.refuseToken(who, string(kernel.CodeOf(err)), "auth.token_invalid")
	}
	g.Log.Error("the authenticator cannot answer", "error", err.Error(), "trace_id", who.Trace)
	return access.Subject{}, &Problem{Code: "server.unavailable"}
}

// ask prefers the authenticator that takes a context.
func (g Gate) ask(ctx context.Context, token string) (access.Subject, error) {
	if ca, ok := g.Authn.(access.ContextAuthenticator); ok {
		return ca.AuthenticateContext(ctx, token)
	}
	return g.Authn.Authenticate(token)
}

// isRefusal says whether an error of the authenticator is a refusal of the token itself.
func isRefusal(err error) bool {
	switch kernel.CodeOf(err) {
	case access.CodeTokenInvalid, access.CodeTokenRevoked, access.CodeSubjectDisabled:
		return true
	}
	return false
}

// refuseToken answers a refused token. From an address that is blocked it is rate_limited and nothing is counted or
// logged, so the block does not grow and the log does not fill. Otherwise the failure is counted, the audit line names
// the reason and the caller's address, and the answer is the code; the failure that starts a block is logged once as
// auth_blocked.
func (g Gate) refuseToken(who Caller, reason, code string) *Problem {
	if l := g.Limiter; l != nil {
		if wait, blocked := l.Blocked(who.Key); blocked {
			secs := int(wait/time.Second) + 1
			return &Problem{Code: "rate_limited.requests", Params: map[string]any{"retry_after_seconds": secs}, RetryAfter: secs}
		}
		l.Failed(who.Key)
		g.audit(who, "auth_failed", reason)
		if _, blocked := l.Blocked(who.Key); blocked {
			g.audit(who, "auth_blocked", "too many refused tokens")
		}
		return &Problem{Code: code}
	}
	g.audit(who, "auth_failed", reason)
	return &Problem{Code: code}
}

func (g Gate) audit(who Caller, event, reason string) {
	g.Log.Warn("audit", "audit", true, "event", event, "reason", reason, "remote", who.Key, "path", who.Path, "trace_id", who.Trace)
}

// Authorize judges a subject against the roles a route lists (any one is enough). It returns the problem when the
// answer is no.
func (g Gate) Authorize(subject access.Subject, roles []string) *Problem {
	for _, role := range roles {
		if g.Satisfies(subject, role) {
			return nil
		}
	}
	return &Problem{Code: "permission.role_missing", Params: map[string]any{"required": roles}}
}

// Satisfies says whether the subject meets one entry of x-required-role: a pseudo role of this package or a role name.
func (g Gate) Satisfies(subject access.Subject, role string) bool {
	switch role {
	case RoleAuthenticated:
		return true
	case RoleRead:
		return g.Authz.Decide(subject, access.AbilityRead, access.Target{}).Allowed
	}
	r, ok := access.ParseRole(role)
	return ok && subject.Holds(r)
}
