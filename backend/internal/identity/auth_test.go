package identity

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-308]
func TestTokenIsARandomSecretWithAPublicIdentifier(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, secret, ok := newTokenParts(kernel.NewToken)
		if !ok {
			t.Fatal("the system random source gave parts of the wrong shape")
		}
		raw := formatToken(id, secret)
		if len(raw) != 4+16+1+64 || !strings.HasPrefix(raw, "pxm_") || seen[raw] {
			t.Fatalf("token %q has the wrong shape or repeats", raw)
		}
		seen[raw] = true
		gotID, gotSecret, ok := parseToken(raw)
		if !ok || gotID != id || gotSecret != secret {
			t.Fatalf("token %q does not parse back", raw)
		}
	}
	for _, bad := range []string{"", "pxm_", strings.ToUpper(formatTokenFor("a")), "px_" + strings.Repeat("a", 81), formatTokenFor("a") + "0", strings.Replace(formatTokenFor("a"), "_", "-", 2), strings.Repeat("g", 85)} {
		if _, _, ok := parseToken(bad); ok {
			t.Errorf("%q parsed as a token", bad)
		}
	}
	if _, _, ok := newTokenParts(func(n int) string { return "ab" }); ok {
		t.Error("a random source with short output made a token")
	}
}

func formatTokenFor(c string) string {
	return formatToken(TokenPrefix+strings.Repeat(c, 16), strings.Repeat(c, 64))
}

// [REQ-308]
func TestOnlyTheSha256OfTheSecretBoundToItsIdentifierIsKept(t *testing.T) {
	id, secret := TokenPrefix+strings.Repeat("a", 16), strings.Repeat("b", 64)
	sum := sha256.Sum256([]byte("praxium.token.v1\x00" + id + "\x00" + secret))
	if got := hashSecret(id, secret); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %s is not the SHA-256 of the domain, the ID and the secret", got)
	}
	if hashSecret(TokenPrefix+strings.Repeat("c", 16), secret) == hashSecret(id, secret) {
		t.Fatal("the hash does not depend on the identifier")
	}
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok := r.token("ann")
	secretPart := tok.Secret[len(tok.Info.ID)+1:]
	stored := ""
	for _, kind := range []string{kindSubject, kindToken, kindMeta} {
		if err := r.store.Scan(r.ctx, kind, func(_ string, data []byte, _ int64) error {
			stored += string(data)
			return nil
		}); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	if strings.Contains(stored, secretPart) {
		t.Fatal("the secret is in the store")
	}
	if !strings.Contains(stored, hashSecret(tok.Info.ID, secretPart)) {
		t.Fatal("the hash is not in the store")
	}
}

// [REQ-308]
func TestTheSecretIsComparedWithTheStandardConstantTimeComparison(t *testing.T) {
	if reflect.ValueOf(constantTimeEqual).Pointer() != reflect.ValueOf(subtle.ConstantTimeCompare).Pointer() {
		t.Fatal("the comparison is not crypto/subtle.ConstantTimeCompare")
	}
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok := r.token("ann")
	calls := 0
	saved := constantTimeEqual
	constantTimeEqual = func(a, b []byte) int { calls++; return saved(a, b) }
	defer func() { constantTimeEqual = saved }()
	if _, err := r.svc.AuthenticateContext(r.ctx, tok.Secret); err != nil || calls != 1 {
		t.Fatalf("a good token: %d comparisons, error %v", calls, err)
	}
	unknown := formatToken(TokenPrefix+strings.Repeat("0", 16), strings.Repeat("1", 64))
	if _, err := r.svc.AuthenticateContext(r.ctx, unknown); kernel.CodeOf(err) != access.CodeTokenInvalid || calls != 2 {
		t.Fatalf("an unknown ID: %d comparisons, error %v (it must cost the same work as a wrong secret)", calls, err)
	}
}

// [REQ-308]
func TestATokenThatIsNotGoodIsRefusedWithoutSayingWhichPartIsWrong(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok := r.token("ann")
	flipped := tok.Secret[:len(tok.Secret)-1] + map[bool]string{true: "1", false: "0"}[strings.HasSuffix(tok.Secret, "0")]
	for name, raw := range map[string]string{
		"a wrong last character": flipped, "an empty text": "", "a text that is no token": "hello",
		"an unknown identifier": formatToken(TokenPrefix+strings.Repeat("9", 16), strings.Repeat("a", 64)),
		"the identifier alone":  tok.Info.ID,
	} {
		_, err := r.svc.AuthenticateContext(r.ctx, raw)
		if kernel.CodeOf(err) != access.CodeTokenInvalid {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if secret := tok.Secret[len(tok.Info.ID)+1:]; strings.Contains(err.Error(), secret) || (raw != "" && strings.Contains(err.Error(), raw) && raw != tok.Info.ID) {
			t.Errorf("%s: the error repeats what was presented: %v", name, err)
		}
	}
}

// [REQ-308]
func TestAFailingStoreIsNotAnInvalidToken(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok := r.token("ann")
	r.store.breakWith(kernel.Errorf(kernel.CodeInternal, "the database is away"))
	_, err := r.svc.AuthenticateContext(r.ctx, tok.Secret)
	idtWantCode(t, "a token while the store fails", err, kernel.CodeInternal)
	r.store.breakWith(nil)
	if _, err := r.svc.Authenticate(tok.Secret); err != nil {
		t.Fatalf("the token after the store came back: %v", err)
	}
}

// [REQ-309]
func TestATokenWorksUntilItExpires(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok, err := r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: "ann", Lifetime: 24 * time.Hour, Description: "a day"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !tok.Info.ExpiresAt.Equal(idtEpoch.Add(24*time.Hour)) || tok.Info.State != access.TokenActive || tok.Info.Description != "a day" {
		t.Fatalf("the token is %+v", tok.Info)
	}
	r.advance(24*time.Hour - time.Second)
	if _, err := r.svc.Authenticate(tok.Secret); err != nil {
		t.Fatalf("one second before the end: %v", err)
	}
	r.advance(time.Second)
	_, err = r.svc.Authenticate(tok.Secret)
	idtWantCode(t, "a token at its end", err, access.CodeTokenExpired)
	info, err := r.svc.GetToken(r.ctx, tok.Info.ID)
	if err != nil || info.State != access.TokenExpired {
		t.Fatalf("the expired token is listed as %+v (%v)", info, err)
	}
}

// [REQ-309]
func TestEveryTokenExpiresAndTheLifetimeHasLimits(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	def := r.token("ann")
	if def.Info.ExpiresAt.Sub(idtEpoch) != 90*24*time.Hour {
		t.Fatalf("the default lifetime is %s", def.Info.ExpiresAt.Sub(idtEpoch))
	}
	for _, lt := range []time.Duration{time.Minute, -time.Hour, MaxTokenLifetime + time.Hour} {
		_, err := r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: "ann", Lifetime: lt})
		idtWantCode(t, "a lifetime of "+lt.String(), err, kernel.CodeInvalidValue)
	}
	if _, err := r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: "ann", Lifetime: MaxTokenLifetime}); err != nil {
		t.Fatalf("the longest lifetime: %v", err)
	}
}

// [REQ-309]
func TestARevokedTokenIsRefusedAtOnceAndStaysListed(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok, other := r.token("ann"), r.token("ann")
	if _, err := r.svc.Authenticate(tok.Secret); err != nil {
		t.Fatalf("before: %v", err)
	}
	info, err := r.svc.RevokeToken(r.ctx, idtAdmin, tok.Info.ID)
	if err != nil || info.State != access.TokenRevoked || info.RevokedBy != "root" || !info.RevokedAt.Equal(idtEpoch) {
		t.Fatalf("revoke: %+v, %v", info, err)
	}
	_, err = r.svc.Authenticate(tok.Secret)
	idtWantCode(t, "the revoked token", err, access.CodeTokenRevoked)
	if _, err := r.svc.Authenticate(other.Secret); err != nil {
		t.Fatalf("a token that was not revoked: %v", err)
	}
	again, err := r.svc.RevokeToken(r.ctx, access.Subject{ID: "someone-else"}, tok.Info.ID)
	if err != nil || again.RevokedBy != "root" {
		t.Fatalf("a second revoke changed the record: %+v, %v", again, err)
	}
	all, err := r.svc.ListTokens(r.ctx, "ann")
	if err != nil || len(all) != 2 {
		t.Fatalf("the tokens of ann: %d, %v", len(all), err)
	}
	_, err = r.svc.RevokeToken(r.ctx, idtAdmin, TokenPrefix+strings.Repeat("0", 16))
	idtWantCode(t, "revoke of an unknown token", err, kernel.CodeNotFound)
}

// [REQ-309]
func TestTheLastUseIsWrittenAtMostOnceAMinute(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok := r.token("ann")
	before := r.store.writes(kindToken)
	for i := 0; i < 5; i++ {
		r.advance(10 * time.Second)
		if _, err := r.svc.Authenticate(tok.Secret); err != nil {
			t.Fatalf("use %d: %v", i, err)
		}
	}
	// The first use writes; the next four are within a minute of it.
	if got := r.store.writes(kindToken) - before; got != 1 {
		t.Fatalf("%d writes for five uses within a minute, want 1", got)
	}
	info, err := r.svc.GetToken(r.ctx, tok.Info.ID)
	if err != nil || !info.LastUsedAt.Equal(idtEpoch.Add(10*time.Second).Truncate(time.Minute)) {
		t.Fatalf("last used %s", info.LastUsedAt)
	}
	r.advance(time.Minute)
	if _, err := r.svc.Authenticate(tok.Secret); err != nil {
		t.Fatalf("a use after a minute: %v", err)
	}
	if got := r.store.writes(kindToken) - before; got != 2 {
		t.Fatalf("%d writes after a minute passed, want 2", got)
	}
}
