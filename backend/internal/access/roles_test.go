package access

import "testing"

// [REQ-229]
func TestRoleTableGrantsEachAbilityToItsRolesOnly(t *testing.T) {
	if len(Roles()) != 8 {
		t.Fatalf("%d roles, want eight", len(Roles()))
	}
	tests := []struct {
		role    Role
		ability Ability
		want    bool
	}{
		{RoleViewer, AbilityRead, true},
		{RoleIngester, AbilityRead, false},
		{RoleIngester, AbilityPushData, true},
		{RoleViewer, AbilityPushData, false},
		{RoleEditor, AbilityEditDefinitions, true},
		{RoleAdmin, AbilityEditDefinitions, false},
		{RoleApprover, AbilityApprove, true},
		{RoleEditor, AbilityApprove, false},
		{RoleAuditor, AbilityReadAudit, true},
		{RoleAdmin, AbilityManageGrants, true},
		{RoleOperator, AbilityManageGrants, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.role)+"/"+string(tt.ability), func(t *testing.T) {
			d := RoleTable{}.Decide(Subject{Roles: []Role{tt.role}}, tt.ability, Target{})
			if d.Allowed != tt.want {
				t.Fatalf("allowed = %v", d.Allowed)
			}
			if !d.Allowed && d.ReasonCode != ReasonForbiddenRole {
				t.Fatalf("reason = %q", d.ReasonCode)
			}
		})
	}
}

// [REQ-229]
func TestRolesAndAbilitiesAgreeWithEachOther(t *testing.T) {
	for _, a := range Abilities() {
		roles := RolesFor(a)
		if len(roles) == 0 {
			t.Errorf("ability %s has no role", a)
		}
		for _, r := range roles {
			found := false
			for _, own := range AbilitiesOf(r) {
				found = found || own == a
			}
			if !found {
				t.Errorf("%s holds %s by RolesFor but not by AbilitiesOf", r, a)
			}
		}
	}
	for _, r := range Roles() {
		if r.IncludesRead() != (r != RoleIngester) {
			t.Errorf("IncludesRead(%s) = %v", r, r.IncludesRead())
		}
		parsed, ok := ParseRole(string(r))
		if !ok || parsed != r {
			t.Errorf("ParseRole(%q) = %q %v", r, parsed, ok)
		}
	}
	if _, ok := ParseRole("root"); ok {
		t.Fatal("an unknown role was parsed")
	}
	if Role("root").Valid() || Role("root").IncludesRead() {
		t.Fatal("an unknown role is valid")
	}
}

// [REQ-229]
func TestSubjectHoldsRoles(t *testing.T) {
	s := Subject{Roles: []Role{RoleViewer, RoleEditor}}
	if !s.Holds(RoleViewer) || s.Holds(RoleAdmin) {
		t.Fatal("Holds is wrong")
	}
	if !s.HoldsAny(RoleAdmin, RoleEditor) || s.HoldsAny(RoleAdmin, RoleAuditor) || s.HoldsAny() {
		t.Fatal("HoldsAny is wrong")
	}
}
