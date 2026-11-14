package httpapi_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// idhDirectory is a small directory in memory: this unit cannot import the identity unit, and what these tests check
// is the routes, not the rules of the directory (they are tested in the identity unit).
type idhDirectory struct {
	mu      sync.Mutex
	people  map[string]access.Principal
	tokens  map[string]access.TokenInfo
	secrets map[string]string
	fail    error
	broken  error
	next    int
}

func idhNewDirectory() *idhDirectory {
	d := &idhDirectory{people: map[string]access.Principal{}, tokens: map[string]access.TokenInfo{}, secrets: map[string]string{}}
	born := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	d.people["root"] = access.Principal{Kind: access.SubjectPerson, ID: "root", Roles: []access.Role{access.RoleAdmin}, CreatedAt: born}
	d.people["ada"] = access.Principal{Kind: access.SubjectPerson, ID: "ada", Roles: []access.Role{access.RoleViewer}, CreatedAt: born}
	d.idhAdd("root", "adm0000000000001", "idh-admin-secret-not-real", access.RoleAdmin)
	d.idhAdd("ada", "red0000000000001", "idh-reader-secret-not-real", access.RoleViewer)
	return d
}

func (d *idhDirectory) idhAdd(owner, id, secret string, role access.Role) {
	d.tokens[id] = access.TokenInfo{ID: id, Owner: owner, OwnerKind: access.SubjectPerson, Roles: []access.Role{role}, State: access.TokenActive,
		CreatedAt: time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2030, 4, 2, 0, 0, 0, 0, time.UTC)}
	d.secrets[secret] = id
}

func (d *idhDirectory) Authenticate(token string) (access.Subject, error) {
	return d.AuthenticateContext(context.Background(), token)
}

func (d *idhDirectory) AuthenticateContext(_ context.Context, token string) (access.Subject, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fail != nil {
		return access.Subject{}, d.fail
	}
	id, ok := d.secrets[token]
	if !ok {
		return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "the token is not known")
	}
	info := d.tokens[id]
	if info.State == access.TokenRevoked {
		return access.Subject{}, kernel.Errorf(access.CodeTokenRevoked, "the token is revoked")
	}
	return access.Subject{Kind: info.OwnerKind, ID: info.Owner, Roles: info.Roles, TokenID: id}, nil
}

func (d *idhDirectory) CreatePrincipal(_ context.Context, actor access.Subject, req access.PrincipalRequest) (access.Principal, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.broken != nil {
		return access.Principal{}, d.broken
	}
	if _, taken := d.people[req.Name]; taken {
		return access.Principal{}, kernel.Errorf(kernel.CodeConflict, "the name %s is used", req.Name)
	}
	if req.Name == "" {
		return access.Principal{}, kernel.Errorf(kernel.CodeInvalidValue, "a name is needed")
	}
	p := access.Principal{Kind: req.Kind, ID: req.Name, DisplayName: req.DisplayName, Roles: req.Roles, CreatedBy: actor.ID,
		CreatedAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	d.people[req.Name] = p
	return p, nil
}

func (d *idhDirectory) ListPrincipals(_ context.Context, kind access.SubjectKind) ([]access.Principal, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.broken != nil {
		return nil, d.broken
	}
	var out []access.Principal
	for _, p := range d.people {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out, nil
}

func (d *idhDirectory) DisablePrincipal(_ context.Context, _ access.Subject, kind access.SubjectKind, id string) (access.Principal, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.people[id]
	if !ok || p.Kind != kind {
		return access.Principal{}, kernel.Errorf(kernel.CodeNotFound, "no such principal")
	}
	p.Disabled = true
	d.people[id] = p
	return p, nil
}

func (d *idhDirectory) IssueToken(_ context.Context, actor access.Subject, req access.TokenRequest) (access.IssuedToken, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.broken != nil {
		return access.IssuedToken{}, d.broken
	}
	owner, ok := d.people[req.Owner]
	if !ok {
		return access.IssuedToken{}, kernel.Errorf(kernel.CodeNotFound, "no such owner")
	}
	d.next++
	id := "new" + strings.Repeat("0", 12) + string(rune('0'+d.next))
	secret := "pxm_" + id + "_idh-new-secret-not-real"
	roles := req.Roles
	if len(roles) == 0 {
		roles = owner.Roles
	}
	d.idhAdd(req.Owner, id, secret, roles[0])
	info := d.tokens[id]
	info.Roles, info.CreatedBy = roles, actor.ID
	d.tokens[id] = info
	d.secrets[secret] = id
	return access.IssuedToken{Info: info, Secret: secret}, nil
}

func (d *idhDirectory) ListTokens(_ context.Context, owner string) ([]access.TokenInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.broken != nil {
		return nil, d.broken
	}
	var out []access.TokenInfo
	for _, t := range d.tokens {
		if owner == "" || t.Owner == owner {
			out = append(out, t)
		}
	}
	return out, nil
}

func (d *idhDirectory) GetToken(_ context.Context, id string) (access.TokenInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.tokens[id]
	if !ok {
		return access.TokenInfo{}, kernel.Errorf(kernel.CodeNotFound, "no such token")
	}
	return t, nil
}

func (d *idhDirectory) RevokeToken(_ context.Context, actor access.Subject, id string) (access.TokenInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t := d.tokens[id]
	t.State, t.RevokedBy, t.RevokedAt = access.TokenRevoked, actor.ID, time.Date(2030, 1, 3, 0, 0, 0, 0, time.UTC)
	d.tokens[id] = t
	return t, nil
}

// idhLimiter blocks a key after two refusals and says so in its calls.
type idhLimiter struct {
	mu     sync.Mutex
	fails  map[string]int
	passes int
}

func (l *idhLimiter) Blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return 42 * time.Second, l.fails[key] >= 2
}
func (l *idhLimiter) Failed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key]++
}
func (l *idhLimiter) Succeeded(string) { l.mu.Lock(); l.passes++; l.mu.Unlock() }

const (
	idhAdminToken  = "idh-admin-secret-not-real"
	idhReaderToken = "idh-reader-secret-not-real"
)

type idhRig struct {
	t       *testing.T
	dir     *idhDirectory
	limiter *idhLimiter
	logs    *bytes.Buffer
	handler http.Handler
}

func idhNewRig(t *testing.T, proxy bool) *idhRig {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	r := &idhRig{t: t, dir: idhNewDirectory(), limiter: &idhLimiter{fails: map[string]int{}}, logs: &bytes.Buffer{}}
	srv, err := httpapi.New(httpapi.Config{
		Registry: reg, Authenticator: r.dir, Directory: r.dir, Limiter: r.limiter, TrustProxy: proxy,
		Clock: kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)), IDs: kernel.NewSequentialIDs(),
		Logger: slog.New(slog.NewJSONHandler(r.logs, nil)), Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.MarkStarted()
	r.handler = srv.Handler()
	return r
}

// send sends a request as the holder of a token ("" for none) with the headers given as pairs.
func (r *idhRig) idhSend(method, target, token, body string, headers ...string) reply {
	r.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:5555"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	flat := map[string]string{}
	for k, v := range rec.Header() {
		flat[strings.ToLower(k)] = v[0]
	}
	return reply{status: rec.Code, header: rec.Header(), body: rec.Body.Bytes(), headers: flat}
}
