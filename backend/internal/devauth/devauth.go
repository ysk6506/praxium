package devauth

import (
	"crypto/subtle"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ModeProduction is the mode this authenticator refuses to serve in.
const ModeProduction = "production"

// MinTokenLength is the shortest token accepted, so a typo does not become an open door.
const MinTokenLength = 16

// Config is the whole configuration of the development authenticator.
type Config struct {
	// Mode is the mode the server runs in; "production" is refused.
	Mode string
	// Token is the static bearer token. An empty token means the
	// authenticator is not configured, and New refuses.
	Token string
	// SubjectID and DisplayName name the one subject; SubjectID defaults to "dev-user".
	SubjectID   string
	DisplayName string
	// Roles are the roles of the subject; at least one.
	Roles []access.Role
}

// Authenticator accepts exactly one token. Build it with New.
type Authenticator struct {
	token   []byte
	subject access.Subject
}

// New builds the authenticator or says why it must not exist: a mode named
// production, a token that is empty or short, or a role that is not one of the
// eight.
func New(cfg Config) (*Authenticator, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), ModeProduction) {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the development authenticator must not run in mode %q", ModeProduction)
	}
	if len(cfg.Token) < MinTokenLength {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the development token must be at least %d characters", MinTokenLength)
	}
	if len(cfg.Roles) == 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the development subject needs at least one role")
	}
	for _, r := range cfg.Roles {
		if !r.Valid() {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "unknown role %q", string(r))
		}
	}
	id := cfg.SubjectID
	if id == "" {
		id = "dev-user"
	}
	return &Authenticator{
		token: []byte(cfg.Token),
		subject: access.Subject{
			Kind: access.SubjectPerson, ID: id, DisplayName: cfg.DisplayName,
			Roles: append([]access.Role(nil), cfg.Roles...),
		},
	}, nil
}

// Authenticate gives the one subject for the one token, comparing in constant time.
func (a *Authenticator) Authenticate(token string) (access.Subject, error) {
	if subtle.ConstantTimeCompare([]byte(token), a.token) != 1 {
		return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "the token is not the development token")
	}
	s := a.subject
	s.Roles = append([]access.Role(nil), s.Roles...)
	return s, nil
}
