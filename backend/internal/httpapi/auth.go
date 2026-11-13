package httpapi

import (
	"strings"

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

// authenticate reads the bearer token and asks the Authenticator. It writes
// the 401 problem and returns false when the request has no good token.
func (c *call) authenticate() bool {
	header := c.r.Header.Get("Authorization")
	if header == "" {
		c.fail("auth.token_missing", nil)
		return false
	}
	scheme, token, _ := strings.Cut(header, " ")
	token = strings.TrimSpace(token)
	if !strings.EqualFold(scheme, "Bearer") || token == "" || c.s.authn == nil {
		c.fail("auth.token_invalid", nil)
		return false
	}
	subject, err := c.s.authn.Authenticate(token)
	if err != nil {
		if kernel.CodeOf(err) == access.CodeTokenExpired {
			c.fail("auth.token_expired", nil)
		} else {
			c.fail("auth.token_invalid", nil)
		}
		return false
	}
	c.subject, c.authed = subject, true
	return true
}
