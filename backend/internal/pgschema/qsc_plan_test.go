package pgschema_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

const qscToken = "0123456789ab"

// qscStandardPlan is the plan of the standard model with the query schema.
func qscStandardPlan(t *testing.T) pgschema.Plan {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	search, err := pgschema.DefaultSearchSet(reg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := pgschema.Build(reg, pgschema.Options{KGNo: 7, Search: search, InstallToken: qscToken})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// [REQ-572]
func TestQscThePlanOfTheStandardModelHasTheStepsOfTheQuerySchemaAndTheRolesOnlyWhenItIsGivenAToken(t *testing.T) {
	plan := qscStandardPlan(t)
	kinds := map[string]int{}
	for _, s := range plan.Query {
		kinds[s.What]++
		if s.Phase != pgschema.InTx {
			t.Errorf("the step %s %s runs outside the transaction", s.What, s.Name)
		}
	}
	if kinds[pgschema.WhatGrant] != 3 || kinds[pgschema.WhatSchema] < 6 || kinds[pgschema.WhatView] == 0 || kinds[pgschema.WhatRows] < 4 {
		t.Errorf("the steps by kind: %v", kinds)
	}
	again := qscStandardPlan(t)
	if len(again.Query) != len(plan.Query) {
		t.Fatalf("the plan has %d steps, and %d the second time", len(plan.Query), len(again.Query))
	}
	for i := range plan.Query {
		if plan.Query[i] != again.Query[i] {
			t.Fatalf("the step %d (%s %s) is not the same the second time: the order of the statements depends on the run", i, plan.Query[i].What, plan.Query[i].Name)
		}
	}
	if got, want := len(plan.Roles), 3; got != want {
		t.Errorf("%d roles", got)
	}
	for i, grade := range pgschema.QueryGrades() {
		if plan.Roles[i] != pgschema.QueryRoleName(qscToken, 7, grade) {
			t.Errorf("the role %d is %s", i, plan.Roles[i])
		}
	}
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	without, err := pgschema.Build(reg, pgschema.Options{KGNo: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(without.Query) != 0 || len(without.Roles) != 0 {
		t.Errorf("a plan without a token has %d steps of the query schema and %d roles", len(without.Query), len(without.Roles))
	}
	if _, err := pgschema.Build(reg, pgschema.Options{KGNo: 7, InstallToken: "not-a-token"}); err == nil {
		t.Error("a token that is not hexadecimal is accepted")
	}
}
