package identity

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-307]
func TestBootstrapMakesTheFirstAdministratorAndItsTokenOnce(t *testing.T) {
	r := idtNewRig(t)
	p, tok, err := r.svc.Bootstrap(r.ctx, "first.admin", "First Admin", 0)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if p.ID != "first.admin" || p.Kind != access.SubjectPerson || len(p.Roles) != 1 || p.Roles[0] != access.RoleAdmin {
		t.Fatalf("the administrator is %+v", p)
	}
	subject, err := r.svc.AuthenticateContext(r.ctx, tok.Secret)
	if err != nil || !subject.Holds(access.RoleAdmin) || subject.ID != "first.admin" || subject.TokenID != tok.Info.ID {
		t.Fatalf("the first token gives %+v, %v", subject, err)
	}
	if got := tok.Info.ExpiresAt.Sub(idtEpoch); got != DefaultTokenLifetime {
		t.Fatalf("the first token lives %s", got)
	}
	_, _, err = r.svc.Bootstrap(r.ctx, "second.admin", "", 0)
	idtWantCode(t, "a second bootstrap", err, kernel.CodeConflict)
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
	people, err := r.svc.ListPrincipals(r.ctx, access.SubjectPerson)
	if err != nil || len(people) != 1 {
		t.Fatalf("%d users after two bootstraps, want 1 (error %v)", len(people), err)
	}
}

// [REQ-307]
func TestBootstrapStaysRefusedWhileAnAdministratorExistsEvenWithoutAUsableToken(t *testing.T) {
	r := idtNewRig(t)
	_, tok, err := r.svc.Bootstrap(r.ctx, "root", "", 0)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := r.svc.RevokeToken(r.ctx, idtAdmin, tok.Info.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	r.advance(2 * MaxTokenLifetime)
	_, _, err = r.svc.Bootstrap(r.ctx, "other", "", 0)
	idtWantCode(t, "a bootstrap with no usable administrator token", err, kernel.CodeConflict)
}

// [REQ-307]
func TestBootstrapChecksTheNameBeforeItClaimsAnything(t *testing.T) {
	r := idtNewRig(t)
	for _, name := range []string{"", "Admin", "a b", strings.Repeat("a", 65), "-x", "(local operator)"} {
		if _, _, err := r.svc.Bootstrap(r.ctx, name, "", 0); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	if _, _, err := r.svc.Bootstrap(r.ctx, "ok", "", 30*time.Minute); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a lifetime of 30 minutes: %v", err)
	}
	if _, _, err := r.svc.Bootstrap(r.ctx, "ok", "", 0); err != nil {
		t.Fatalf("a good bootstrap after the refused ones: %v", err)
	}
}

// [REQ-307]
func TestAFailedBootstrapGivesItsClaimBackAndADeadOneRunsOut(t *testing.T) {
	r := idtNewRig(t)
	r.person("taken", access.RoleViewer)
	_, _, err := r.svc.Bootstrap(r.ctx, "taken", "", 0)
	idtWantCode(t, "a name that is used", err, kernel.CodeConflict)
	if _, _, err := r.svc.Bootstrap(r.ctx, "free", "", 0); err != nil {
		t.Fatalf("the retry after the failure: %v", err)
	}
	dead := idtNewRig(t)
	if _, err := dead.svc.claimBootstrap(dead.ctx, "dead"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	_, _, err = dead.svc.Bootstrap(dead.ctx, "other", "", 0)
	idtWantCode(t, "a bootstrap while another claim is fresh", err, kernel.CodeConflict)
	dead.advance(claimTTL)
	if _, _, err := dead.svc.Bootstrap(dead.ctx, "other", "", 0); err != nil {
		t.Fatalf("a bootstrap after the dead claim ran out: %v", err)
	}
}

// [REQ-307]
func TestOfBootstrapsAtTheSameMomentExactlyOneMakesTheAdministrator(t *testing.T) {
	r := idtNewRig(t)
	const runners = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < runners; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := r.svc.Bootstrap(r.ctx, fmt.Sprintf("admin-%d", i), "", 0)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if kernel.CodeOf(err) != kernel.CodeConflict {
				t.Errorf("runner %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	people, err := r.svc.ListPrincipals(r.ctx, access.SubjectPerson)
	if err != nil || wins != 1 || len(people) != 1 {
		t.Fatalf("%d bootstraps succeeded and %d administrators exist (error %v)", wins, len(people), err)
	}
}
