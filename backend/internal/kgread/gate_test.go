package kgread_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/kgread"
)

const goodToken = "good-token-not-real-0001"

// answerAuthn answers a token with a fixed result.
type answerAuthn struct{ err error }

func (a answerAuthn) Authenticate(token string) (access.Subject, error) {
	if a.err != nil {
		return access.Subject{}, a.err
	}
	if token != goodToken {
		return access.Subject{}, kernel.Errorf(access.CodeTokenInvalid, "not known")
	}
	return access.Subject{Kind: access.SubjectPerson, ID: "ada", Roles: []access.Role{access.RoleViewer}}, nil
}

// ctxAuthn is an authenticator that takes the context of the call.
type ctxAuthn struct {
	answerAuthn
	called bool
}

func (c *ctxAuthn) AuthenticateContext(_ context.Context, token string) (access.Subject, error) {
	c.called = true
	return c.Authenticate(token)
}

// blockList is a limiter with a fixed set of blocked keys that counts the failures.
type blockList struct {
	blocked map[string]bool
	fails   map[string]int
}

func (b *blockList) Blocked(key string) (time.Duration, bool) {
	if b.blocked[key] {
		return 90 * time.Second, true
	}
	return 0, false
}

func (b *blockList) Failed(key string) {
	b.fails[key]++
	if b.fails[key] >= 2 {
		b.blocked[key] = true
	}
}

func newGate(authn access.Authenticator, limiter access.FailureLimiter) (kgread.Gate, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	return kgread.Gate{Authn: authn, Authz: access.RoleTable{}, Limiter: limiter, Log: slog.New(slog.NewJSONHandler(logs, nil))}, logs
}

var who = kgread.Caller{Key: "203.0.113.9", Path: "/v1/kgs", Trace: "trace-1"}

func TestGateAuthenticatesAGoodToken(t *testing.T) {
	ctxA := &ctxAuthn{}
	for _, authn := range []access.Authenticator{answerAuthn{}, ctxA} {
		g, _ := newGate(authn, nil)
		subject, p := g.Authenticate(context.Background(), who, "bearer "+goodToken)
		if p != nil || subject.ID != "ada" {
			t.Fatalf("subject %+v problem %+v", subject, p)
		}
	}
	if !ctxA.called {
		t.Fatal("an authenticator that takes the context must be asked with it")
	}
}

func TestGateRefusesWhatIsNotAGoodToken(t *testing.T) {
	cases := []struct {
		name, credential, code string
		authn                  access.Authenticator
	}{
		{"missing", "", "auth.token_missing", answerAuthn{}},
		{"wrong scheme", "Basic abc", "auth.token_invalid", answerAuthn{}},
		{"empty token", "Bearer ", "auth.token_invalid", answerAuthn{}},
		{"unknown token", "Bearer nope", "auth.token_invalid", answerAuthn{}},
		{"no authenticator", "Bearer " + goodToken, "auth.token_invalid", nil},
		{"expired", "Bearer " + goodToken, "auth.token_expired", answerAuthn{err: kernel.Errorf(access.CodeTokenExpired, "ran out")}},
		{"revoked", "Bearer " + goodToken, "auth.token_invalid", answerAuthn{err: kernel.Errorf(access.CodeTokenRevoked, "revoked")}},
		{"cannot answer", "Bearer " + goodToken, "server.unavailable", answerAuthn{err: errors.New("directory down")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, logs := newGate(tc.authn, nil)
			_, p := g.Authenticate(context.Background(), who, tc.credential)
			if p == nil || p.Code != tc.code {
				t.Fatalf("problem %+v, want %s", p, tc.code)
			}
			if strings.Contains(logs.String(), goodToken) {
				t.Fatalf("the token reached the log: %s", logs.String())
			}
		})
	}
}

func TestGateCountsRefusalsAndBlocksTheAddress(t *testing.T) {
	limiter := &blockList{blocked: map[string]bool{}, fails: map[string]int{}}
	g, logs := newGate(answerAuthn{}, limiter)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, p := g.Authenticate(ctx, who, "Bearer nope"); p == nil || p.Code != "auth.token_invalid" {
			t.Fatalf("refusal %d: %+v", i, p)
		}
	}
	if !strings.Contains(logs.String(), `"event":"auth_failed"`) || !strings.Contains(logs.String(), `"event":"auth_blocked"`) ||
		!strings.Contains(logs.String(), `"remote":"203.0.113.9"`) || !strings.Contains(logs.String(), `"trace_id":"trace-1"`) {
		t.Fatalf("the audit lines are wrong: %s", logs.String())
	}
	_, p := g.Authenticate(ctx, who, "Bearer nope")
	if p == nil || p.Code != "rate_limited.requests" || p.RetryAfter != 91 || p.Params["retry_after_seconds"] != 91 {
		t.Fatalf("a refusal from a blocked address: %+v", p)
	}
	if limiter.fails[who.Key] != 2 {
		t.Fatalf("a refusal while blocked was counted: %v", limiter.fails)
	}
	if subject, p := g.Authenticate(ctx, who, "Bearer "+goodToken); p != nil || subject.ID != "ada" {
		t.Fatalf("a good token from a blocked address must pass: %+v", p)
	}
}

func TestGateAuthorizesByRole(t *testing.T) {
	g, _ := newGate(answerAuthn{}, nil)
	viewer := access.Subject{ID: "ada", Roles: []access.Role{access.RoleViewer}}
	nobody := access.Subject{ID: "x"}
	cases := []struct {
		subject access.Subject
		roles   []string
		allowed bool
	}{
		{viewer, []string{kgread.RoleRead}, true},
		{nobody, []string{kgread.RoleRead}, false},
		{nobody, []string{kgread.RoleAuthenticated}, true},
		{viewer, []string{"admin"}, false},
		{viewer, []string{"admin", "viewer"}, true},
		{viewer, []string{"no-such-role"}, false},
	}
	for _, tc := range cases {
		p := g.Authorize(tc.subject, tc.roles)
		if tc.allowed != (p == nil) {
			t.Fatalf("%v for %v: problem %+v", tc.roles, tc.subject.Roles, p)
		}
		if p != nil && (p.Code != "permission.role_missing" || p.Params["required"] == nil) {
			t.Fatalf("the problem of a missing role: %+v", p)
		}
	}
}
