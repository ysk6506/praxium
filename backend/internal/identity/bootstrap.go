package identity

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

const (
	// bootstrapKey is the key of the record that the bootstrap claims.
	bootstrapKey = "bootstrap"
	// claimTTL is how long a claim keeps another bootstrap out. A bootstrap that dies leaves its claim for this long.
	claimTTL = 2 * time.Minute
)

// Bootstrap makes the first administrator and a token for it, for the person who runs the server. There is no
// default account and no default password: this is the only way in, and it refuses when an enabled administrator
// exists. The token's secret is in the result and nowhere else.
//
// Two bootstraps at once cannot both succeed: the claim is a record made with the version that was read, so one of
// two claims loses, and a claim that is not yet old keeps every other bootstrap out until it ends. A bootstrap that
// fails gives its claim back; one that dies leaves it, and the next can claim after claimTTL.
func (s *Service) Bootstrap(ctx context.Context, name, displayName string, lifetime time.Duration) (access.Principal, access.IssuedToken, error) {
	none := func(err error) (access.Principal, access.IssuedToken, error) {
		return access.Principal{}, access.IssuedToken{}, err
	}
	req := access.PrincipalRequest{Kind: access.SubjectPerson, Name: name, DisplayName: displayName, Roles: []access.Role{access.RoleAdmin}}
	rec, err := s.newSubjectRec(OfflineActor, req)
	if err != nil {
		return none(err)
	}
	if _, _, err := s.tokenTerms(rec, access.TokenRequest{Lifetime: lifetime}); err != nil {
		return none(err)
	}
	if err := s.refuseWhenAdministered(ctx); err != nil {
		return none(err)
	}
	version, err := s.claimBootstrap(ctx, name)
	if err != nil {
		return none(err)
	}
	principal, issued, err := s.makeFirstAdministrator(ctx, req, lifetime)
	if err != nil {
		s.releaseBootstrap(ctx, version)
		return none(err)
	}
	s.audit("bootstrap", OfflineActor, "target", name, "token_id", issued.Info.ID)
	return principal, issued, nil
}

func (s *Service) makeFirstAdministrator(ctx context.Context, req access.PrincipalRequest, lifetime time.Duration) (access.Principal, access.IssuedToken, error) {
	if err := s.refuseWhenAdministered(ctx); err != nil {
		return access.Principal{}, access.IssuedToken{}, err
	}
	principal, err := s.CreatePrincipal(ctx, OfflineActor, req)
	if err != nil {
		return access.Principal{}, access.IssuedToken{}, err
	}
	issued, err := s.IssueToken(ctx, OfflineActor, access.TokenRequest{Owner: req.Name, Description: "the first token, made by the bootstrap", Lifetime: lifetime})
	return principal, issued, err
}

// HasAdministrator says whether an enabled administrator exists. A server in production mode does not start
// without one: nobody could manage it.
func (s *Service) HasAdministrator(ctx context.Context) (bool, error) {
	found := false
	err := s.store.Scan(ctx, kindSubject, func(_ string, data []byte, _ int64) error {
		rec, err := decodeSubject(data)
		found = found || rec.isAdmin()
		return err
	})
	return found, err
}

func (s *Service) refuseWhenAdministered(ctx context.Context) error {
	found, err := s.HasAdministrator(ctx)
	if err != nil {
		return err
	}
	if found {
		s.audit("bootstrap_refused", OfflineActor, "reason", "an administrator exists")
		return kernel.Errorf(kernel.CodeConflict, "an administrator already exists: sign in with its token (praxium admin whoami), or issue a token for it at the server with praxium admin issue-token")
	}
	return nil
}

// claimBootstrap takes the claim and returns the version it has. A claim of another bootstrap that is not old yet
// refuses.
func (s *Service) claimBootstrap(ctx context.Context, name string) (int64, error) {
	data, version, err := s.store.Get(ctx, kindMeta, bootstrapKey)
	switch {
	case err == nil:
		old, derr := decodeRecord(data, func(r metaRec) int { return r.V })
		if derr != nil {
			return 0, derr
		}
		if !old.ClaimedAt.IsZero() && s.clock.Now().Sub(old.ClaimedAt) < claimTTL {
			return 0, kernel.Errorf(kernel.CodeConflict, "another bootstrap is running on this store; wait for it, then look again")
		}
	case kernel.CodeOf(err) != kernel.CodeNotFound:
		return 0, err
	}
	claim := metaRec{V: recordVersion, ClaimedAt: s.clock.Now(), Name: name}
	next, err := s.store.Put(ctx, kindMeta, bootstrapKey, encode(claim), version)
	if err != nil {
		if kernel.CodeOf(err) == kernel.CodeConflict {
			return 0, kernel.Errorf(kernel.CodeConflict, "another bootstrap is running on this store; wait for it, then look again")
		}
		return 0, err
	}
	return next, nil
}

// releaseBootstrap gives the claim back after a failure, as well as it can: the claim would run out anyway.
func (s *Service) releaseBootstrap(ctx context.Context, version int64) {
	_, _ = s.store.Put(ctx, kindMeta, bootstrapKey, encode(metaRec{V: recordVersion}), version)
}
