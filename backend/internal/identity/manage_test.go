package identity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-310]
func TestPrincipalsHaveUniqueNamesAndRolesThatFitTheirKind(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer, access.RoleEditor)
	_, err := r.svc.CreatePrincipal(r.ctx, idtAdmin, access.PrincipalRequest{Kind: access.SubjectSystemAccount, Name: "ann", Roles: []access.Role{access.RoleViewer}})
	idtWantCode(t, "the name of a user used for a service account", err, kernel.CodeConflict)
	tests := []struct {
		name string
		req  access.PrincipalRequest
	}{
		{"a person with the role ingester", access.PrincipalRequest{Kind: access.SubjectPerson, Name: "p1", Roles: []access.Role{access.RoleIngester}}},
		{"a service account with the role admin", access.PrincipalRequest{Kind: access.SubjectSystemAccount, Name: "s1", Roles: []access.Role{access.RoleAdmin}}},
		{"no role", access.PrincipalRequest{Kind: access.SubjectPerson, Name: "p2"}},
		{"an unknown role", access.PrincipalRequest{Kind: access.SubjectPerson, Name: "p3", Roles: []access.Role{"root"}}},
		{"an unknown kind", access.PrincipalRequest{Kind: "robot", Name: "p4", Roles: []access.Role{access.RoleViewer}}},
		{"a display name with a control character", access.PrincipalRequest{Kind: access.SubjectPerson, Name: "p5", DisplayName: "a\nb", Roles: []access.Role{access.RoleViewer}}},
		{"a description that is too long", access.PrincipalRequest{Kind: access.SubjectPerson, Name: "p6", Description: strings.Repeat("x", 257), Roles: []access.Role{access.RoleViewer}}},
	}
	for _, tt := range tests {
		_, err := r.svc.CreatePrincipal(r.ctx, idtAdmin, tt.req)
		idtWantCode(t, tt.name, err, kernel.CodeInvalidValue)
	}
	feed, err := r.svc.CreatePrincipal(r.ctx, idtAdmin, access.PrincipalRequest{Kind: access.SubjectSystemAccount, Name: "rack-feed", Roles: []access.Role{access.RoleIngester}})
	if err != nil || feed.Kind != access.SubjectSystemAccount || feed.Roles[0] != access.RoleIngester {
		t.Fatalf("an ingester service account: %+v, %v", feed, err)
	}
	people, err := r.svc.ListPrincipals(r.ctx, access.SubjectPerson)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	accounts, err := r.svc.ListPrincipals(r.ctx, access.SubjectSystemAccount)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(people) != 1 || people[0].Roles[0] != access.RoleViewer || people[0].Roles[1] != access.RoleEditor || len(accounts) != 1 {
		t.Fatalf("lists: %+v %+v", people, accounts)
	}
}

// [REQ-310]
func TestATokenCanOnlyNarrowTheRolesOfItsOwner(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer, access.RoleEditor)
	_, err := r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: "ann", Roles: []access.Role{access.RoleAdmin}})
	idtWantCode(t, "a token with a role the owner lacks", err, kernel.CodeInvalidValue)
	_, err = r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: "ann", Roles: []access.Role{"root"}})
	idtWantCode(t, "a token with an unknown role", err, kernel.CodeInvalidValue)
	_, err = r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: "nobody"})
	idtWantCode(t, "a token for nobody", err, kernel.CodeNotFound)
	narrow := r.token("ann", access.RoleViewer)
	subject, err := r.svc.Authenticate(narrow.Secret)
	if err != nil || len(subject.Roles) != 1 || subject.Roles[0] != access.RoleViewer {
		t.Fatalf("the narrow token gives %+v, %v", subject, err)
	}
	whole := r.token("ann")
	if subject, err = r.svc.Authenticate(whole.Secret); err != nil || len(subject.Roles) != 2 {
		t.Fatalf("a token with no roles named gives %v, want both roles of the owner", subject.Roles)
	}
}

// [REQ-310]
func TestTakingARoleFromTheOwnerTakesItFromItsTokens(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer, access.RoleEditor)
	tok := r.token("ann")
	data, version, err := r.store.Get(r.ctx, kindSubject, "ann")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	rec, err := decodeSubject(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec.Roles = []string{"viewer"}
	if _, err := r.store.Put(r.ctx, kindSubject, "ann", encode(rec), version); err != nil {
		t.Fatalf("put: %v", err)
	}
	subject, err := r.svc.Authenticate(tok.Secret)
	if err != nil || len(subject.Roles) != 1 || subject.Roles[0] != access.RoleViewer {
		t.Fatalf("the token after the change: %+v, %v", subject, err)
	}
	rec.Roles = []string{"auditor"}
	if _, err := r.store.Put(r.ctx, kindSubject, "ann", encode(rec), version+1); err != nil {
		t.Fatalf("put: %v", err)
	}
	_, err = r.svc.Authenticate(tok.Secret)
	idtWantCode(t, "a token whose roles the owner no longer has", err, access.CodeTokenInvalid)
}

// [REQ-310]
func TestADisabledPrincipalsTokensAreRefusedAndTheLastAdministratorStays(t *testing.T) {
	r := idtNewRig(t)
	r.person("root1", access.RoleAdmin)
	r.person("ann", access.RoleViewer)
	tok := r.token("ann")
	p, err := r.svc.DisablePrincipal(r.ctx, idtAdmin, access.SubjectPerson, "ann")
	if err != nil || !p.Disabled || !p.DisabledAt.Equal(idtEpoch) {
		t.Fatalf("disable: %+v, %v", p, err)
	}
	_, err = r.svc.Authenticate(tok.Secret)
	idtWantCode(t, "the token of a disabled user", err, access.CodeSubjectDisabled)
	_, err = r.svc.IssueToken(r.ctx, idtAdmin, access.TokenRequest{Owner: "ann"})
	idtWantCode(t, "a new token for a disabled user", err, kernel.CodeConflict)
	if again, err := r.svc.DisablePrincipal(r.ctx, idtAdmin, access.SubjectPerson, "ann"); err != nil || !again.Disabled {
		t.Fatalf("disabling twice: %+v, %v", again, err)
	}
	_, err = r.svc.DisablePrincipal(r.ctx, idtAdmin, access.SubjectSystemAccount, "root1")
	idtWantCode(t, "a user disabled as a service account", err, kernel.CodeNotFound)
	_, err = r.svc.DisablePrincipal(r.ctx, idtAdmin, access.SubjectPerson, "nobody")
	idtWantCode(t, "an unknown user", err, kernel.CodeNotFound)
	_, err = r.svc.DisablePrincipal(r.ctx, idtAdmin, access.SubjectPerson, "root1")
	idtWantCode(t, "the last administrator", err, kernel.CodeConflict)
	r.person("root2", access.RoleAdmin)
	if _, err := r.svc.DisablePrincipal(r.ctx, idtAdmin, access.SubjectPerson, "root1"); err != nil {
		t.Fatalf("an administrator when another exists: %v", err)
	}
}

// [REQ-310]
func TestAStoredRecordOfAnotherVersionIsRefused(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	data, version, err := r.store.Get(r.ctx, kindSubject, "ann")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	doc["v"] = 2
	bumped, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := r.store.Put(r.ctx, kindSubject, "ann", bumped, version); err != nil {
		t.Fatalf("put: %v", err)
	}
	_, err = r.svc.ListPrincipals(r.ctx, access.SubjectPerson)
	idtWantCode(t, "a record of version 2", err, kernel.CodeUnsupported)
	if err := r.store.Scan(r.ctx, kindSubject, func(string, []byte, int64) error { return nil }); err != nil {
		t.Fatalf("scan: %v", err)
	}
}

// [REQ-309]
func TestTokensAreListedByOwnerWithTheKindOfTheOwner(t *testing.T) {
	r := idtNewRig(t)
	r.person("ann", access.RoleViewer)
	if _, err := r.svc.CreatePrincipal(r.ctx, idtAdmin, access.PrincipalRequest{Kind: access.SubjectSystemAccount, Name: "feed", Roles: []access.Role{access.RoleIngester}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	r.token("ann")
	r.token("ann")
	r.token("feed")
	all, err := r.svc.ListTokens(r.ctx, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("all tokens: %d, %v", len(all), err)
	}
	feed, err := r.svc.ListTokens(r.ctx, "feed")
	if err != nil || len(feed) != 1 || feed[0].OwnerKind != access.SubjectSystemAccount || feed[0].Roles[0] != access.RoleIngester {
		t.Fatalf("the tokens of feed: %+v, %v", feed, err)
	}
	if none, err := r.svc.ListTokens(r.ctx, "nobody"); err != nil || len(none) != 0 {
		t.Fatalf("the tokens of nobody: %+v, %v", none, err)
	}
	if _, err := r.svc.GetToken(r.ctx, "pxm_nope"); kernel.CodeOf(err) != kernel.CodeNotFound {
		t.Fatalf("an unknown token: %v", err)
	}
}
