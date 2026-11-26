package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// qscRefused builds the plan of the small model after a change of its definitions and says the text of the refusal.
func qscRefused(t *testing.T, change func(d *model.Definitions)) string {
	t.Helper()
	d := smallDefs(t)
	change(&d)
	_, err := pgschema.Build(registry(t, d), pgschema.Options{KGNo: 3, InstallToken: qscToken})
	if err == nil {
		t.Fatal("the plan was made")
	}
	return err.Error()
}

// [REQ-574]
func TestQscAModelThatCannotBeShownAsViewsIsRefusedWhenThePlanIsMade(t *testing.T) {
	cases := map[string]struct {
		change func(d *model.Definitions)
		want   string
	}{
		"a type named like a view of every schema": {func(d *model.Definitions) { d.ObjectTypes[1].Name = "Object" }, "taken by another view"},
		"two types with the same view":             {func(d *model.Definitions) { d.ObjectTypes[1].Name = "SERVER" }, "taken by another view"},
		"a property named like a column of every view": {func(d *model.Definitions) {
			d.ObjectTypes[0].Properties[0].Name = "id"
		}, "twice"},
		"a property named like the time column of another": {func(d *model.Definitions) {
			d.ObjectTypes[0].Properties[0].Name = "temp_at"
		}, "twice"},
		"a name too long for PostgreSQL": {func(d *model.Definitions) {
			d.ObjectTypes[0].Properties[0].Name = strings.Repeat("x", pgschema.MaxIdentBytes+1)
		}, "cannot keep"},
		"a description with a NUL byte": {func(d *model.Definitions) { d.ObjectTypes[0].Description = "a\x00b" }, "can keep"},
		"a description of a property with a NUL byte": {func(d *model.Definitions) {
			d.ObjectTypes[0].Properties[0].Description = "a\x00b"
		}, "can keep"},
	}
	for name, c := range cases {
		if got := qscRefused(t, c.change); !strings.Contains(got, c.want) {
			t.Errorf("%s: %s", name, got)
		}
	}
}

// [REQ-573] [REQ-626]
func TestQscTheStatementsOfAGradeNameNoAttributeOfAHigherGradeAndTheRowsOfTheDescriptionAreCutByNumber(t *testing.T) {
	d := smallDefs(t)
	d.ObjectTypes[0].Properties[1].Sensitivity = model.SensitivityConfidential // cpus, number 11
	plan, err := pgschema.Build(registry(t, d), pgschema.Options{KGNo: 3, InstallToken: qscToken})
	if err != nil {
		t.Fatal(err)
	}
	sql := func(schema, name string) string {
		for _, s := range plan.Query {
			if s.Name == schema+"."+name {
				return s.SQL
			}
		}
		return ""
	}
	open := sql(pgschema.QuerySchemaName(3, model.SensitivityOpen, false), "server")
	top := sql(pgschema.QuerySchemaName(3, model.SensitivityConfidential, false), "server")
	if open == "" || top == "" {
		t.Fatalf("the view of the server is missing: open %v, confidential %v", open != "", top != "")
	}
	if strings.Contains(open, `"cpus"`) || !strings.Contains(top, `"cpus"`) {
		t.Errorf("the column of an attribute of the highest grade: open %v, confidential %v", strings.Contains(open, `"cpus"`), strings.Contains(top, `"cpus"`))
	}
	if sql(pgschema.QuerySchemaName(3, model.SensitivityRestricted, false), "server") != "" {
		t.Error("the restricted grade makes a view that is the same as that of the open grade")
	}
	attr := sql(pgschema.QuerySchemaName(3, model.SensitivityOpen, false), "attribute")
	if !strings.Contains(attr, "<> ALL (ARRAY[11])") {
		t.Errorf("the view of the attributes does not leave the number 11 out: %s", attr)
	}
}

// [REQ-573]
func TestQscTheStatementsOfTheRolesMakeAnInertRoleLetTheExecutorSwitchToItAndDropThemAll(t *testing.T) {
	role := pgschema.QueryRoleName(qscToken, 7, model.SensitivityRestricted)
	create := pgschema.CreateRoleSQL(role)
	for _, want := range []string{`"` + role + `"`, "NOLOGIN", "NOSUPERUSER", "NOCREATEROLE", "NOINHERIT", "NOBYPASSRLS"} {
		if !strings.Contains(create, want) {
			t.Errorf("the statement that makes a role has no %s: %s", want, create)
		}
	}
	if member := pgschema.MemberSQL(role); !strings.Contains(member, "SET TRUE") || !strings.Contains(member, "INHERIT FALSE") {
		t.Errorf("the executor may not switch to the role: %s", member)
	}
	drops := pgschema.DropRolesSQL(qscToken, 7)
	if len(drops) != 3 || !strings.Contains(strings.Join(drops, ";"), role) {
		t.Errorf("the roles that are dropped: %v", drops)
	}
}
