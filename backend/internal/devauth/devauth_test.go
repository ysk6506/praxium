package devauth

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

const fakeToken = "test-token-not-real-0001"

// [REQ-148]
func TestNewRefusesWhatWouldBeAnOpenDoor(t *testing.T) {
	good := Config{Mode: "development", Token: fakeToken, Roles: []access.Role{access.RoleViewer}}
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{"production", func(c *Config) { c.Mode = "production" }},
		{"production in capitals", func(c *Config) { c.Mode = " Production " }},
		{"no token", func(c *Config) { c.Token = "" }},
		{"short token", func(c *Config) { c.Token = "short" }},
		{"no role", func(c *Config) { c.Roles = nil }},
		{"unknown role", func(c *Config) { c.Roles = []access.Role{"root"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := good
			tt.change(&cfg)
			if _, err := New(cfg); err == nil || kernel.CodeOf(err) != kernel.CodeInvalidValue {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if _, err := New(good); err != nil {
		t.Fatal(err)
	}
}

// [REQ-148]
func TestAuthenticateAcceptsExactlyOneToken(t *testing.T) {
	a, err := New(Config{Mode: "development", Token: fakeToken, SubjectID: "dev-1", Roles: []access.Role{access.RoleEditor}})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := a.Authenticate(fakeToken)
	if err != nil || subject.ID != "dev-1" || !subject.Holds(access.RoleEditor) {
		t.Fatalf("subject = %+v, err = %v", subject, err)
	}
	subject.Roles[0] = access.RoleAdmin
	again, _ := a.Authenticate(fakeToken)
	if !again.Holds(access.RoleEditor) {
		t.Fatal("a caller changed the roles of the authenticator")
	}
	for _, bad := range []string{"", fakeToken + "x", fakeToken[:len(fakeToken)-1], "Bearer " + fakeToken} {
		if _, err := a.Authenticate(bad); err == nil || kernel.CodeOf(err) != access.CodeTokenInvalid {
			t.Errorf("token %q: err = %v", bad, err)
		}
	}
	defaulted, _ := New(Config{Mode: "development", Token: fakeToken, Roles: []access.Role{access.RoleViewer}})
	if s, _ := defaulted.Authenticate(fakeToken); s.ID != "dev-user" {
		t.Fatalf("default id = %q", s.ID)
	}
}
