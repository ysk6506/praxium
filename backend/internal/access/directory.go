package access

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// More codes of this unit (ADR-0044). The first two say why an authenticator refused a token that it knew; the
// server answers all of them with the same problem, and only the audit log tells them apart.
const (
	// CodeTokenRevoked is a token that someone revoked.
	CodeTokenRevoked kernel.Code = "access_token_revoked"
	// CodeSubjectDisabled is a token whose owner is disabled.
	CodeSubjectDisabled kernel.Code = "access_subject_disabled"
)

// ContextAuthenticator is an Authenticator that reads a store and so takes the context of the request. The server
// prefers it over Authenticate when the authenticator has it.
type ContextAuthenticator interface {
	Authenticator
	AuthenticateContext(ctx context.Context, token string) (Subject, error)
}

// Principal is a user or a service account as the management API shows it. ID is its name: unique among all
// principals and never changed.
type Principal struct {
	Kind        SubjectKind
	ID          string
	DisplayName string
	Description string
	Roles       []Role
	Disabled    bool
	CreatedAt   time.Time
	CreatedBy   string
	// DisabledAt is the zero time while the principal is enabled.
	DisabledAt time.Time
}

// PrincipalRequest asks for a new principal.
type PrincipalRequest struct {
	Kind        SubjectKind
	Name        string
	DisplayName string
	Description string
	Roles       []Role
}

// TokenState is what a token can do at a moment.
type TokenState string

// The states of a token. A revoked token stays listed.
const (
	TokenActive  TokenState = "active"
	TokenExpired TokenState = "expired"
	TokenRevoked TokenState = "revoked"
)

// TokenInfo is a token as the management API shows it. It never holds the secret or its hash.
type TokenInfo struct {
	ID          string
	Owner       string
	OwnerKind   SubjectKind
	Roles       []Role
	Description string
	State       TokenState
	CreatedAt   time.Time
	CreatedBy   string
	ExpiresAt   time.Time
	// LastUsedAt is the zero time for a token never used. It is kept to the minute.
	LastUsedAt time.Time
	RevokedAt  time.Time
	RevokedBy  string
}

// TokenRequest asks for a new token of an owner. No roles means the roles the owner has; the roles of a token can
// only narrow those. No lifetime means the default.
type TokenRequest struct {
	Owner       string
	Roles       []Role
	Description string
	Lifetime    time.Duration
}

// IssuedToken is a new token. Secret is the whole token in plain text: this is the one time it exists outside the
// hands of the caller, and it is never stored.
type IssuedToken struct {
	Info   TokenInfo
	Secret string
}

// Directory is the port of the management of principals and tokens. The server's routes call it; the identity unit
// implements it. The actor is who asks, for the audit log. Errors carry kernel codes: invalid_value for a request
// that breaks a rule, not_found, and conflict for a state that does not allow it.
type Directory interface {
	CreatePrincipal(ctx context.Context, actor Subject, req PrincipalRequest) (Principal, error)
	ListPrincipals(ctx context.Context, kind SubjectKind) ([]Principal, error)
	DisablePrincipal(ctx context.Context, actor Subject, kind SubjectKind, id string) (Principal, error)
	IssueToken(ctx context.Context, actor Subject, req TokenRequest) (IssuedToken, error)
	// ListTokens lists the tokens of one owner, or all of them when owner is empty.
	ListTokens(ctx context.Context, owner string) ([]TokenInfo, error)
	GetToken(ctx context.Context, id string) (TokenInfo, error)
	RevokeToken(ctx context.Context, actor Subject, id string) (TokenInfo, error)
}

// FailureLimiter is the port that slows down a caller who keeps presenting tokens that are refused. The key is
// where the caller comes from. A good token passes from a blocked key; a refused one from a blocked key is answered 429 and not counted.
type FailureLimiter interface {
	// Blocked says whether the key may not try now, and for how long.
	Blocked(key string) (retryAfter time.Duration, blocked bool)
	// Failed records one refused token. A good token clears nothing: the failures of others on the same address
	// stay, and run out with the time window.
	Failed(key string)
}
