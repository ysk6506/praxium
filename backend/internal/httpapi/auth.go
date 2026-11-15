package httpapi

import (
	"net"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kgread"
)

// The values of x-required-role that are not role names (rest-api.md 5-3).
const (
	// RoleAnonymous needs no login.
	RoleAnonymous = kgread.RoleAnonymous
	// RoleAuthenticated needs any login.
	RoleAuthenticated = kgread.RoleAuthenticated
	// RoleRead needs a role that includes the read of its scope.
	RoleRead = kgread.RoleRead
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
	if p := c.s.gate.Authorize(c.subject, roles); p != nil {
		c.failProblem(p)
		return false
	}
	return true
}

// authenticate reads the bearer token of the request and asks the gate (kgread.Gate.Authenticate has the rules). It
// writes the problem and returns false when the request has no good token.
func (c *call) authenticate() bool {
	who := kgread.Caller{Key: c.clientKey(), Path: c.r.URL.Path, Trace: c.trace}
	subject, p := c.s.gate.Authenticate(c.r.Context(), who, c.r.Header.Get("Authorization"))
	if p != nil {
		c.failProblem(p)
		return false
	}
	c.subject, c.authed = subject, true
	return true
}

// failProblem answers with a problem of the use case: its code, params and field entries, and the Retry-After
// header of a caller who has to wait.
func (c *call) failProblem(p *kgread.Problem) {
	if p.RetryAfter > 0 {
		c.w.Header().Set("Retry-After", strconv.Itoa(p.RetryAfter))
	}
	var errs []fieldError
	for _, f := range p.Fields {
		errs = append(errs, fieldError{Pointer: f.Pointer, Code: f.Code, Params: f.Params})
	}
	c.fail(p.Code, p.Params, errs...)
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
