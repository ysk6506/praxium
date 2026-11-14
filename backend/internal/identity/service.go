package identity

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The lifetimes of a token (ADR-0044). Every token expires: a token that never expires is a leak that never ends.
const (
	// DefaultTokenLifetime is the lifetime when the request names none.
	DefaultTokenLifetime = 90 * 24 * time.Hour
	// MaxTokenLifetime is the longest lifetime a token can be given.
	MaxTokenLifetime = 365 * 24 * time.Hour
	// lastUsedEvery is how often the last use of a token is written: a request does not write every time.
	lastUsedEvery = time.Minute
	// storeTimeout bounds one authentication for the authenticators that take no context.
	storeTimeout = 10 * time.Second
	// updateTries is how often a change of a record is tried again when another writer got in first.
	updateTries = 3
)

// OfflineActor is the actor of what an operator does at the server, with the store and no API: the bootstrap and
// the issue of a token on the command line. Its ID is not a valid name, so no principal can be called that.
var OfflineActor = access.Subject{Kind: access.SubjectPerson, ID: "(local operator)"}

// Config is what a Service needs from the outside.
type Config struct {
	Store Store
	Clock kernel.Clock
	// Random gives n random bytes as 2n hex characters; the program passes kernel.NewToken.
	Random func(n int) string
	// Log receives the audit events: one structured line each, never a secret.
	Log *slog.Logger
}

// Service is the built-in authentication: it authenticates tokens and manages principals and tokens. It is safe for
// concurrent use; every change goes through the version check of the Store.
type Service struct {
	store  Store
	clock  kernel.Clock
	random func(n int) string
	log    *slog.Logger
}

var (
	_ access.Directory            = (*Service)(nil)
	_ access.ContextAuthenticator = (*Service)(nil)
)

// New builds the service.
func New(cfg Config) (*Service, error) {
	if cfg.Store == nil || cfg.Clock == nil || cfg.Random == nil || cfg.Log == nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the identity service needs a store, a clock, a random source and a log")
	}
	return &Service{store: cfg.Store, clock: cfg.Clock, random: cfg.Random, log: cfg.Log}, nil
}

// audit writes one line of the audit log. Callers pass IDs and names only: no token, no secret, no hash.
func (s *Service) audit(event string, actor access.Subject, attrs ...any) {
	base := []any{"audit", true, "event", event, "actor", actor.ID, "actor_kind", string(actor.Kind)}
	s.log.Info("audit", append(base, attrs...)...)
}

// Authenticate is AuthenticateContext with a time limit, for callers that have no context.
func (s *Service) Authenticate(token string) (access.Subject, error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	return s.AuthenticateContext(ctx, token)
}

// AuthenticateContext turns a presented token into the subject that holds it. A token that is not of the shape, is
// unknown, has another secret, is revoked, has expired, or belongs to a disabled principal is refused; the message
// names the token ID where it is known and never the secret. A failure of the store is not a refusal: it is returned
// with the code of the store's error, so the server answers that it cannot tell, not that the token is bad.
func (s *Service) AuthenticateContext(ctx context.Context, raw string) (access.Subject, error) {
	id, secret, ok := parseToken(raw)
	if !ok {
		return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "the text is not an access token")
	}
	rec, version, err := s.loadToken(ctx, id)
	if err != nil {
		if kernel.CodeOf(err) == kernel.CodeNotFound {
			// Do the work of a real check anyway, so an unknown ID does not answer faster than a wrong secret.
			secretMatches(id, secret, hashSecret("", ""))
			return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "no token %s", id)
		}
		return access.Subject{}, err
	}
	if !secretMatches(id, secret, rec.Hash) {
		return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "the secret of token %s is wrong", id)
	}
	now := s.clock.Now()
	switch rec.state(now) {
	case access.TokenRevoked:
		return access.Subject{}, kernel.Errorf(access.CodeTokenRevoked, "token %s is revoked", id)
	case access.TokenExpired:
		return access.Subject{}, kernel.Errorf(access.CodeTokenExpired, "token %s has expired", id)
	}
	subject, err := s.subjectOf(ctx, id, rec)
	if err != nil {
		return access.Subject{}, err
	}
	s.touch(ctx, id, rec, version, now)
	return subject, nil
}

// subjectOf finds the owner of a token and the roles the token holds now: the roles of the token that the owner
// still has, so taking a role from an owner takes it from every token at once.
func (s *Service) subjectOf(ctx context.Context, id string, rec tokenRec) (access.Subject, error) {
	data, _, err := s.store.Get(ctx, kindSubject, rec.Owner)
	if err != nil {
		if kernel.CodeOf(err) == kernel.CodeNotFound {
			return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "token %s has no owner", id)
		}
		return access.Subject{}, err
	}
	owner, err := decodeSubject(data)
	if err != nil {
		return access.Subject{}, err
	}
	if owner.Disabled {
		return access.Subject{}, kernel.Errorf(access.CodeSubjectDisabled, "the owner of token %s is disabled", id)
	}
	var roles []access.Role
	for _, r := range rolesOf(rec.Roles) {
		if slices.Contains(owner.Roles, string(r)) {
			roles = append(roles, r)
		}
	}
	if len(roles) == 0 {
		return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "token %s holds no role its owner has", id)
	}
	return access.Subject{Kind: access.SubjectKind(owner.Kind), ID: rec.Owner, DisplayName: owner.DisplayName, Roles: roles, TokenID: id}, nil
}

// touch writes the last use of a token, at most once a minute and only as well as it can: a request never fails
// because this write did.
func (s *Service) touch(ctx context.Context, id string, rec tokenRec, version int64, now time.Time) {
	if !rec.LastUsedAt.IsZero() && now.Sub(rec.LastUsedAt) < lastUsedEvery {
		return
	}
	rec.LastUsedAt = now.Truncate(time.Minute)
	if _, err := s.store.Put(ctx, kindToken, id, encode(rec), version); err != nil && kernel.CodeOf(err) != kernel.CodeConflict {
		s.log.Warn("the last use of a token could not be written", "token_id", id, "code", string(kernel.CodeOf(err)))
	}
}

func (s *Service) loadToken(ctx context.Context, id string) (tokenRec, int64, error) {
	data, version, err := s.store.Get(ctx, kindToken, id)
	if err != nil {
		return tokenRec{}, 0, err
	}
	rec, err := decodeToken(data)
	return rec, version, err
}

func (s *Service) loadSubject(ctx context.Context, name string) (subjectRec, int64, error) {
	data, version, err := s.store.Get(ctx, kindSubject, name)
	if err != nil {
		return subjectRec{}, 0, err
	}
	rec, err := decodeSubject(data)
	return rec, version, err
}
