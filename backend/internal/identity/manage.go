package identity

import (
	"context"
	"slices"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// newSubjectRec checks a request for a principal and builds its record.
func (s *Service) newSubjectRec(actor access.Subject, req access.PrincipalRequest) (subjectRec, error) {
	if req.Kind != access.SubjectPerson && req.Kind != access.SubjectSystemAccount {
		return subjectRec{}, kernel.Errorf(kernel.CodeInvalidValue, "the kind of a principal is person or system_account")
	}
	if err := checkName(req.Name); err != nil {
		return subjectRec{}, err
	}
	if err := checkText("the display name", req.DisplayName, maxDisplayName); err != nil {
		return subjectRec{}, err
	}
	if err := checkText("the description", req.Description, maxDescription); err != nil {
		return subjectRec{}, err
	}
	roles, err := checkRoles(req.Kind, req.Roles)
	if err != nil {
		return subjectRec{}, err
	}
	return subjectRec{
		V: recordVersion, Kind: string(req.Kind), DisplayName: req.DisplayName, Description: req.Description,
		Roles: roleNames(roles), CreatedAt: s.clock.Now(), CreatedBy: actor.ID,
	}, nil
}

// CreatePrincipal makes a user or a service account. Its name is its ID and is taken for good.
func (s *Service) CreatePrincipal(ctx context.Context, actor access.Subject, req access.PrincipalRequest) (access.Principal, error) {
	rec, err := s.newSubjectRec(actor, req)
	if err != nil {
		return access.Principal{}, err
	}
	if _, err := s.store.Put(ctx, kindSubject, req.Name, encode(rec), 0); err != nil {
		if kernel.CodeOf(err) == kernel.CodeConflict {
			return access.Principal{}, kernel.Errorf(kernel.CodeConflict, "the name %q is already used by a user or a service account", req.Name)
		}
		return access.Principal{}, err
	}
	s.audit("principal_created", actor, "target", req.Name, "target_kind", string(req.Kind), "roles", rec.Roles)
	return rec.principal(req.Name), nil
}

// ListPrincipals lists the principals of a kind in the order of their names.
func (s *Service) ListPrincipals(ctx context.Context, kind access.SubjectKind) ([]access.Principal, error) {
	out := []access.Principal{}
	err := s.store.Scan(ctx, kindSubject, func(id string, data []byte, _ int64) error {
		rec, err := decodeSubject(data)
		if err != nil {
			return err
		}
		if rec.Kind == string(kind) {
			out = append(out, rec.principal(id))
		}
		return nil
	})
	return out, err
}

// DisablePrincipal stops a principal: every token it owns is refused from now on. It cannot be undone through the
// API. The last administrator cannot be disabled, because no one could manage the rest then.
func (s *Service) DisablePrincipal(ctx context.Context, actor access.Subject, kind access.SubjectKind, id string) (access.Principal, error) {
	var out access.Principal
	for try := 0; try < updateTries; try++ {
		rec, version, err := s.loadSubject(ctx, id)
		if err != nil || rec.Kind != string(kind) {
			if err == nil || kernel.CodeOf(err) == kernel.CodeNotFound {
				return out, kernel.Errorf(kernel.CodeNotFound, "no %s %q", kind, id)
			}
			return out, err
		}
		if rec.Disabled {
			return rec.principal(id), nil
		}
		if rec.isAdmin() {
			if others, err := s.otherAdmins(ctx, id); err != nil {
				return out, err
			} else if !others {
				return out, kernel.Errorf(kernel.CodeConflict, "%q is the last administrator; make another one first", id)
			}
		}
		rec.Disabled, rec.DisabledAt, rec.DisabledBy = true, s.clock.Now(), actor.ID
		if _, err := s.store.Put(ctx, kindSubject, id, encode(rec), version); err != nil {
			if kernel.CodeOf(err) == kernel.CodeConflict {
				continue
			}
			return out, err
		}
		s.audit("principal_disabled", actor, "target", id, "target_kind", string(kind))
		return rec.principal(id), nil
	}
	return out, kernel.Errorf(kernel.CodeConflict, "%q was changed at the same time; try again", id)
}

// otherAdmins says whether an enabled administrator other than the one named exists.
func (s *Service) otherAdmins(ctx context.Context, except string) (bool, error) {
	found := false
	err := s.store.Scan(ctx, kindSubject, func(id string, data []byte, _ int64) error {
		rec, err := decodeSubject(data)
		if err != nil {
			return err
		}
		found = found || (id != except && rec.isAdmin())
		return nil
	})
	return found, err
}

// IssueToken makes a token for an owner and returns it with its secret, which is not kept anywhere.
func (s *Service) IssueToken(ctx context.Context, actor access.Subject, req access.TokenRequest) (access.IssuedToken, error) {
	owner, _, err := s.loadSubject(ctx, req.Owner)
	if err != nil {
		if kernel.CodeOf(err) == kernel.CodeNotFound {
			return access.IssuedToken{}, kernel.Errorf(kernel.CodeNotFound, "no user or service account %q", req.Owner)
		}
		return access.IssuedToken{}, err
	}
	if owner.Disabled {
		return access.IssuedToken{}, kernel.Errorf(kernel.CodeConflict, "%q is disabled and can hold no new token", req.Owner)
	}
	roles, lifetime, err := s.tokenTerms(owner, req)
	if err != nil {
		return access.IssuedToken{}, err
	}
	id, secret, ok := newTokenParts(s.random)
	if !ok {
		return access.IssuedToken{}, kernel.Errorf(kernel.CodeInternal, "the random source gave a token of the wrong shape")
	}
	now := s.clock.Now()
	rec := tokenRec{
		V: recordVersion, Owner: req.Owner, Roles: roleNames(roles), Hash: hashSecret(id, secret), Description: req.Description,
		CreatedAt: now, CreatedBy: actor.ID, ExpiresAt: now.Add(lifetime),
	}
	if _, err := s.store.Put(ctx, kindToken, id, encode(rec), 0); err != nil {
		return access.IssuedToken{}, err
	}
	s.audit("token_issued", actor, "token_id", id, "owner", req.Owner, "roles", rec.Roles, "expires_at", rec.ExpiresAt)
	return access.IssuedToken{Info: rec.info(id, access.SubjectKind(owner.Kind), now), Secret: formatToken(id, secret)}, nil
}

// tokenTerms settles the roles and the lifetime of a new token: the roles can only narrow the owner's, and the
// lifetime is the default when none is asked and never more than the longest.
func (s *Service) tokenTerms(owner subjectRec, req access.TokenRequest) ([]access.Role, time.Duration, error) {
	if err := checkText("the description", req.Description, maxDescription); err != nil {
		return nil, 0, err
	}
	lifetime := req.Lifetime
	switch {
	case lifetime == 0:
		lifetime = DefaultTokenLifetime
	case lifetime < time.Hour || lifetime > MaxTokenLifetime:
		return nil, 0, kernel.Errorf(kernel.CodeInvalidValue, "the lifetime of a token is from 1 hour to %d days", int(MaxTokenLifetime/(24*time.Hour)))
	}
	roles := req.Roles
	if len(roles) == 0 {
		roles = rolesOf(owner.Roles)
	}
	roles, err := checkRoles(access.SubjectKind(owner.Kind), roles)
	if err != nil {
		return nil, 0, err
	}
	for _, r := range roles {
		if !slices.Contains(owner.Roles, string(r)) {
			return nil, 0, kernel.Errorf(kernel.CodeInvalidValue, "the owner does not hold the role %s, so a token cannot", r)
		}
	}
	return roles, lifetime, nil
}

// ListTokens lists the tokens of an owner, or all tokens, in the order of their IDs.
func (s *Service) ListTokens(ctx context.Context, owner string) ([]access.TokenInfo, error) {
	kinds := map[string]access.SubjectKind{}
	if err := s.store.Scan(ctx, kindSubject, func(id string, data []byte, _ int64) error {
		rec, err := decodeSubject(data)
		kinds[id] = access.SubjectKind(rec.Kind)
		return err
	}); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	out := []access.TokenInfo{}
	err := s.store.Scan(ctx, kindToken, func(id string, data []byte, _ int64) error {
		rec, err := decodeToken(data)
		if err != nil {
			return err
		}
		if owner == "" || rec.Owner == owner {
			out = append(out, rec.info(id, kinds[rec.Owner], now))
		}
		return nil
	})
	return out, err
}

// GetToken returns one token, or a not_found error.
func (s *Service) GetToken(ctx context.Context, id string) (access.TokenInfo, error) {
	rec, _, err := s.loadToken(ctx, id)
	if err != nil {
		if kernel.CodeOf(err) == kernel.CodeNotFound {
			return access.TokenInfo{}, kernel.Errorf(kernel.CodeNotFound, "no token %q", id)
		}
		return access.TokenInfo{}, err
	}
	owner, _, err := s.loadSubject(ctx, rec.Owner)
	if err != nil && kernel.CodeOf(err) != kernel.CodeNotFound {
		return access.TokenInfo{}, err
	}
	return rec.info(id, access.SubjectKind(owner.Kind), s.clock.Now()), nil
}

// RevokeToken ends a token for good: it is refused from the next request on. Revoking a token twice is not an error.
func (s *Service) RevokeToken(ctx context.Context, actor access.Subject, id string) (access.TokenInfo, error) {
	for try := 0; try < updateTries; try++ {
		rec, version, err := s.loadToken(ctx, id)
		if err != nil {
			if kernel.CodeOf(err) == kernel.CodeNotFound {
				return access.TokenInfo{}, kernel.Errorf(kernel.CodeNotFound, "no token %q", id)
			}
			return access.TokenInfo{}, err
		}
		if rec.RevokedAt.IsZero() {
			rec.RevokedAt, rec.RevokedBy = s.clock.Now(), actor.ID
			if _, err := s.store.Put(ctx, kindToken, id, encode(rec), version); err != nil {
				if kernel.CodeOf(err) == kernel.CodeConflict {
					continue
				}
				return access.TokenInfo{}, err
			}
			s.audit("token_revoked", actor, "token_id", id, "owner", rec.Owner)
		}
		return s.GetToken(ctx, id)
	}
	return access.TokenInfo{}, kernel.Errorf(kernel.CodeConflict, "token %q was changed at the same time; try again", id)
}
