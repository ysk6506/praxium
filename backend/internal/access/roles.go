package access

import "slices"

// Role is one of the eight roles of the product. Roles are fixed: no one adds
// or edits a role (access-control.md section 4).
type Role string

// The eight roles.
const (
	RoleViewer       Role = "viewer"
	RoleEditor       Role = "editor"
	RoleApprover     Role = "approver"
	RoleOperator     Role = "operator"
	RoleModelSteward Role = "model_steward"
	RoleAuditor      Role = "auditor"
	RoleAdmin        Role = "admin"
	RoleIngester     Role = "ingester"
)

// Roles returns the eight roles in the order of the design.
func Roles() []Role {
	return []Role{
		RoleViewer, RoleEditor, RoleApprover, RoleOperator,
		RoleModelSteward, RoleAuditor, RoleAdmin, RoleIngester,
	}
}

// Valid reports whether r is one of the eight roles.
func (r Role) Valid() bool { return slices.Contains(Roles(), r) }

// IncludesRead reports whether the role contains the read of its scope: every
// role does but the ingester, which may only push data.
func (r Role) IncludesRead() bool { return r.Valid() && r != RoleIngester }

// ParseRole accepts exactly the eight role names.
func ParseRole(name string) (Role, bool) {
	r := Role(name)
	return r, r.Valid()
}

// Ability is one row of the role table: a kind of thing a role may do.
type Ability string

// The abilities of the role table of access-control.md section 4, in its order.
const (
	AbilityRead             Ability = "read"
	AbilityReadDefinitions  Ability = "read_definitions"
	AbilityInspectSources   Ability = "inspect_sources"
	AbilityBulkRead         Ability = "bulk_read"
	AbilityEditDefinitions  Ability = "edit_definitions"
	AbilityApprove          Ability = "approve_proposals"
	AbilityRunSources       Ability = "run_sources"
	AbilityUploadFiles      Ability = "upload_files"
	AbilityPushData         Ability = "push_data"
	AbilityReadTypeUsage    Ability = "read_type_usage"
	AbilityClassifyReports  Ability = "classify_reports"
	AbilityConfirmGuards    Ability = "confirm_guards"
	AbilityPinServing       Ability = "pin_serving"
	AbilityApproveOrgTypes  Ability = "approve_org_types"
	AbilityReadAudit        Ability = "read_audit"
	AbilityReadGrants       Ability = "read_grants"
	AbilityManageGrants     Ability = "manage_grants"
	AbilityManageSettings   Ability = "manage_settings"
	AbilityApproveLoosening Ability = "approve_loosening"
)

// abilityTable says which roles hold each ability. The read rows hold for all
// roles but the ingester; bulk_read is the separate permit that goes with a
// viewer grant.
var abilityTable = []struct {
	ability Ability
	roles   []Role
}{
	{AbilityRead, []Role{RoleViewer, RoleEditor, RoleApprover, RoleOperator, RoleModelSteward, RoleAuditor, RoleAdmin}},
	{AbilityReadDefinitions, []Role{RoleViewer, RoleEditor, RoleApprover, RoleOperator, RoleModelSteward, RoleAuditor, RoleAdmin}},
	{AbilityInspectSources, []Role{RoleEditor, RoleOperator, RoleAdmin}},
	{AbilityBulkRead, []Role{RoleViewer}},
	{AbilityEditDefinitions, []Role{RoleEditor}},
	{AbilityApprove, []Role{RoleApprover}},
	{AbilityRunSources, []Role{RoleOperator}},
	{AbilityUploadFiles, []Role{RoleEditor, RoleOperator}},
	{AbilityPushData, []Role{RoleIngester}},
	{AbilityReadTypeUsage, []Role{RoleViewer, RoleModelSteward}},
	{AbilityClassifyReports, []Role{RoleOperator}},
	{AbilityConfirmGuards, []Role{RoleOperator}},
	{AbilityPinServing, []Role{RoleOperator, RoleAdmin}},
	{AbilityApproveOrgTypes, []Role{RoleModelSteward}},
	{AbilityReadAudit, []Role{RoleAuditor}},
	{AbilityReadGrants, []Role{RoleAuditor, RoleAdmin}},
	{AbilityManageGrants, []Role{RoleAdmin}},
	{AbilityManageSettings, []Role{RoleAdmin}},
	{AbilityApproveLoosening, []Role{RoleAdmin}},
}

// Abilities returns every ability in the order of the role table.
func Abilities() []Ability {
	out := make([]Ability, len(abilityTable))
	for i, row := range abilityTable {
		out[i] = row.ability
	}
	return out
}

// RolesFor returns the roles that hold an ability, in the order of the roles.
func RolesFor(a Ability) []Role {
	for _, row := range abilityTable {
		if row.ability == a {
			return slices.Clone(row.roles)
		}
	}
	return nil
}

// AbilitiesOf returns the abilities a role holds, in the order of the table.
func AbilitiesOf(r Role) []Ability {
	var out []Ability
	for _, row := range abilityTable {
		if slices.Contains(row.roles, r) {
			out = append(out, row.ability)
		}
	}
	return out
}
