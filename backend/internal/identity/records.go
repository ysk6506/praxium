package identity

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// recordVersion is the version of the JSON of the records. A record of another version is refused, not guessed.
const recordVersion = 1

// subjectRec is the stored form of a principal. Its key in the store is its name.
type subjectRec struct {
	V           int       `json:"v"`
	Kind        string    `json:"kind"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description"`
	Roles       []string  `json:"roles"`
	Disabled    bool      `json:"disabled"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	DisabledAt  time.Time `json:"disabled_at,omitzero"`
	DisabledBy  string    `json:"disabled_by,omitempty"`
}

// tokenRec is the stored form of a token: never the secret, only its hash. Its key in the store is the token ID.
type tokenRec struct {
	V           int       `json:"v"`
	Owner       string    `json:"owner"`
	Roles       []string  `json:"roles"`
	Hash        string    `json:"hash"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	ExpiresAt   time.Time `json:"expires_at"`
	LastUsedAt  time.Time `json:"last_used_at,omitzero"`
	RevokedAt   time.Time `json:"revoked_at,omitzero"`
	RevokedBy   string    `json:"revoked_by,omitempty"`
}

// metaRec is the record that the bootstrap writes to claim the right to make the first administrator.
type metaRec struct {
	V         int       `json:"v"`
	ClaimedAt time.Time `json:"claimed_at"`
	Name      string    `json:"name"`
}

func encode(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		// Only plain strings, numbers and times go in: a failure here is a bug, not an input.
		panic(fmt.Sprintf("identity: cannot encode a record: %v", err))
	}
	return data
}

// decodeRecord reads a stored record and refuses one of another version.
func decodeRecord[T any](data []byte, version func(T) int) (T, error) {
	var rec T
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, kernel.Wrap(kernel.CodeInternal, err, "an identity record cannot be read")
	}
	if version(rec) != recordVersion {
		return rec, kernel.Errorf(kernel.CodeUnsupported, "an identity record has version %d; this praxium reads version %d", version(rec), recordVersion)
	}
	return rec, nil
}

func decodeSubject(data []byte) (subjectRec, error) {
	return decodeRecord(data, func(r subjectRec) int { return r.V })
}

func decodeToken(data []byte) (tokenRec, error) {
	return decodeRecord(data, func(r tokenRec) int { return r.V })
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._@-]{0,63}$`)

const (
	maxDisplayName = 128
	maxDescription = 256
)

// checkName says whether a name can be a principal's ID.
func checkName(name string) error {
	if !namePattern.MatchString(name) {
		return kernel.Errorf(kernel.CodeInvalidValue, "a name is 1 to 64 characters: lower-case letters, digits and . _ @ -, starting with a letter or a digit")
	}
	return nil
}

func checkText(what, text string, max int) error {
	if utf8.RuneCountInString(text) > max {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s is longer than %d characters", what, max)
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return kernel.Errorf(kernel.CodeInvalidValue, "%s holds a control character", what)
		}
	}
	return nil
}

// checkRoles validates the roles of a principal and returns them once each, in the order of the design.
func checkRoles(kind access.SubjectKind, roles []access.Role) ([]access.Role, error) {
	if len(roles) == 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "at least one role is needed")
	}
	var out []access.Role
	for _, r := range access.Roles() {
		if slices.Contains(roles, r) {
			out = append(out, r)
		}
	}
	for _, r := range roles {
		if !r.Valid() {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "%q is not one of the eight roles", string(r))
		}
	}
	for _, r := range out {
		switch {
		case r == access.RoleIngester && kind == access.SubjectPerson:
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the role ingester belongs to service accounts only")
		case r == access.RoleAdmin && kind == access.SubjectSystemAccount:
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "a service account cannot hold the role admin")
		}
	}
	return out, nil
}

func roleNames(roles []access.Role) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

// rolesOf reads stored role names; a name that is not a role is dropped, so a record can only ever narrow.
func rolesOf(names []string) []access.Role {
	var out []access.Role
	for _, n := range names {
		if r, ok := access.ParseRole(n); ok {
			out = append(out, r)
		}
	}
	return out
}

func (r subjectRec) principal(name string) access.Principal {
	return access.Principal{
		Kind: access.SubjectKind(r.Kind), ID: name, DisplayName: r.DisplayName, Description: r.Description,
		Roles: rolesOf(r.Roles), Disabled: r.Disabled, CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, DisabledAt: r.DisabledAt,
	}
}

func (r subjectRec) isAdmin() bool {
	return !r.Disabled && r.Kind == string(access.SubjectPerson) && slices.Contains(r.Roles, string(access.RoleAdmin))
}

func (r tokenRec) state(now time.Time) access.TokenState {
	switch {
	case !r.RevokedAt.IsZero():
		return access.TokenRevoked
	case !now.Before(r.ExpiresAt):
		return access.TokenExpired
	}
	return access.TokenActive
}

func (r tokenRec) info(id string, owner access.SubjectKind, now time.Time) access.TokenInfo {
	return access.TokenInfo{
		ID: id, Owner: r.Owner, OwnerKind: owner, Roles: rolesOf(r.Roles), Description: r.Description,
		State: r.state(now), CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, ExpiresAt: r.ExpiresAt,
		LastUsedAt: r.LastUsedAt, RevokedAt: r.RevokedAt, RevokedBy: r.RevokedBy,
	}
}
