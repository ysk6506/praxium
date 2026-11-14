package access

import (
	"slices"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The codes of this unit. A code is never renamed or reused.
const (
	// CodeTokenInvalid is a token that no authenticator accepts.
	CodeTokenInvalid kernel.Code = "access_token_invalid"
	// CodeTokenExpired is a token that was good once and has run out.
	CodeTokenExpired kernel.Code = "access_token_expired"
)

// SubjectKind says what a subject is.
type SubjectKind string

// The kinds of subject.
const (
	SubjectPerson        SubjectKind = "person"
	SubjectSystemAccount SubjectKind = "system_account"
)

// Subject is who is calling: an identity the authenticator vouches for and the
// roles that identity holds. Scope (which KGs, types and sources) is part of
// the grants of the identity backend and is taken as whole until it exists.
type Subject struct {
	Kind        SubjectKind
	ID          string
	DisplayName string
	Roles       []Role
	// TokenID names the access token the subject presented, when there is one.
	TokenID string
}

// Holds reports whether the subject has the role.
func (s Subject) Holds(r Role) bool { return slices.Contains(s.Roles, r) }

// HoldsAny reports whether the subject has at least one of the roles.
func (s Subject) HoldsAny(roles ...Role) bool {
	for _, r := range roles {
		if s.Holds(r) {
			return true
		}
	}
	return false
}

// Authenticator is the port that turns a bearer token into a subject. An
// error carries CodeTokenInvalid or CodeTokenExpired. The OIDC adapter and the
// system-account tokens implement it; so does the development authenticator,
// which exists only for development (ADR-0024).
type Authenticator interface {
	Authenticate(token string) (Subject, error)
}

// Target is what an ability is used on. The role table ignores it; the grants
// of the identity backend will use it for scope.
type Target struct {
	Kind string
	ID   string
}

// Closed reason codes of a refused action (rest-api.md section 4). The
// catalogue of the contract unit lists them with their meaning.
const (
	ReasonForbiddenRole    = "forbidden.role"
	ReasonForbiddenScope   = "forbidden.scope"
	ReasonConflictState    = "conflict.state"
	ReasonApprovalRequired = "blocked.approval_required"
	ReasonLimit            = "blocked.limit"
	ReasonDegraded         = "unavailable.degraded"
)

// Decision is the answer of the Authorizer: allowed, or refused with a reason code.
type Decision struct {
	Allowed    bool
	ReasonCode string
}

// Authorizer is the port of the one place that judges what a subject may do.
type Authorizer interface {
	Decide(s Subject, a Ability, t Target) Decision
}

// RoleTable is the Authorizer of the role table: a subject may do an ability
// when one of its roles holds it.
type RoleTable struct{}

// Decide allows an ability to a subject holding a role of the role table's
// row, and refuses otherwise with forbidden.role.
func (RoleTable) Decide(s Subject, a Ability, _ Target) Decision {
	if s.HoldsAny(RolesFor(a)...) {
		return Decision{Allowed: true}
	}
	return Decision{ReasonCode: ReasonForbiddenRole}
}
