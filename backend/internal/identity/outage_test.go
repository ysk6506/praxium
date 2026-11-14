package identity

import (
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-314]
func TestEveryCallAnswersAnErrorWhenTheStoreCannot(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	tok := r.token("ann", access.RoleViewer)
	r.store.breakWith(kernel.Errorf(kernel.CodeInternal, "the store is down"))
	calls := map[string]func() error{
		"create": func() error {
			_, err := r.svc.CreatePrincipal(r.ctx, idtAdmin, access.PrincipalRequest{Kind: access.SubjectPerson, Name: "bob", Roles: []access.Role{access.RoleViewer}})
			return err
		},
		"list users": func() error { _, err := r.svc.ListPrincipals(r.ctx, access.SubjectPerson); return err },
		"disable": func() error {
			_, err := r.svc.DisablePrincipal(r.ctx, idtAdmin, access.SubjectPerson, "ann")
			return err
		},
		"issue": func() error {
			_, err := r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: "ann"})
			return err
		},
		"list tokens": func() error { _, err := r.svc.ListTokens(r.ctx, ""); return err },
		"get a token": func() error { _, err := r.svc.GetToken(r.ctx, tok.Info.ID); return err },
		"revoke":      func() error { _, err := r.svc.RevokeToken(r.ctx, idtAdmin, tok.Info.ID); return err },
		"bootstrap": func() error {
			_, _, err := r.svc.Bootstrap(r.ctx, "root", "Root", 24*time.Hour)
			return err
		},
		"has an administrator": func() error { _, err := r.svc.HasAdministrator(r.ctx); return err },
		"authenticate":         func() error { _, err := r.svc.Authenticate(tok.Secret); return err },
	}
	for name, call := range calls {
		err := call()
		if err == nil {
			t.Errorf("%s: no error while the store is down", name)
			continue
		}
		if kernel.CodeOf(err) == access.CodeTokenInvalid {
			t.Errorf("%s: an outage must not look like a refused token: %v", name, err)
		}
	}
	if r.store.writes("token") != 1 || r.store.writes("subject") != 1 {
		t.Errorf("a store that is down was written to: tokens %d, subjects %d", r.store.writes("token"), r.store.writes("subject"))
	}
}
