package pgstore

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of what the query schema does when something goes wrong (ADR-0113). Their names start with Qsc.

// qscBrokenQuery is a planner whose query schema has a statement that PostgreSQL refuses.
func qscBrokenQuery(m SchemaModel, kgNo int64) (pgschema.Plan, error) {
	p, err := buildPlan(m, kgNo)
	if err != nil {
		return p, err
	}
	p.Query = append(p.Query, pgschema.Step{Phase: pgschema.InTx, What: pgschema.WhatView, Name: "broken", SQL: "CREATE VIEW broken_view AS SELECT * FROM no_such_table"})
	return p, nil
}

// [REQ-573]
func TestQscAStatementOfTheQuerySchemaThatFailsLeavesTheKGAsItWasForTheMakingAndForTheAdjustment(t *testing.T) {
	st, admin := sxOpen(t)
	ctx := context.Background()
	if _, err := st.createStorageSchema(ctx, sxKG(t, 5), qscModel(t), qscBrokenQuery); err == nil {
		t.Fatal("a KG was made with a query schema that cannot be made")
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM kg_schema_state`); n != 0 {
		t.Errorf("%d records are left by the making that failed", n)
	}
	if _, err := st.CreateStorageSchema(ctx, sxKG(t, 5), qscModel(t)); err != nil {
		t.Fatal(err)
	}
	added := sxProp(t, "pr.machine.rack", "rack", 22, "vt.string", model.ChangeSlow)
	if _, err := st.adjustStorageSchema(ctx, sxKG(t, 5), qscModelNamed(t, "serial", added), qscBrokenQuery); err == nil {
		t.Fatal("a KG was adjusted with a query schema that cannot be made")
	}
	if _, err := st.AdjustStorageSchema(ctx, sxKG(t, 5), qscModelNamed(t, "serial", added)); err != nil {
		t.Fatalf("the adjustment that failed left the KG so that the next cannot be done: %v", err)
	}
}

// [REQ-573]
func TestQscATokenThatIsNotOneOfTheInstallationRefusesEveryWayToMakeAdjustOrDropTheQuerySchema(t *testing.T) {
	st, admin := sxOpen(t)
	ctx := context.Background()
	if _, err := st.CreateStorageSchema(ctx, sxKG(t, 1), qscModel(t)); err != nil {
		t.Fatal(err)
	}
	sxRun(t, admin, `ALTER TABLE install_token DROP CONSTRAINT install_token_token_check`)
	sxRun(t, admin, `UPDATE install_token SET token = 'NOT-A-TOKEN'`)
	added := sxProp(t, "pr.machine.rack", "rack", 22, "vt.string", model.ChangeSlow)
	if _, err := st.CreateStorageSchema(ctx, sxKG(t, 2), qscModel(t)); err == nil {
		t.Error("a KG is made with a token that is not one")
	}
	if _, err := st.AdjustStorageSchema(ctx, sxKG(t, 1), qscModelNamed(t, "serial", added)); err == nil {
		t.Error("a KG is adjusted with a token that is not one")
	}
	if err := st.DropStorageSchema(ctx, sxKG(t, 1)); err == nil {
		t.Error("a schema is dropped with a token that is not one")
	}
	if err := st.DeleteKG(ctx, sxKG(t, 1)); err == nil {
		t.Error("a KG is deleted with a token that is not one")
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'q\_%'`); n == 0 {
		t.Error("the query schemas went away with the drop that was refused")
	}
}

// [REQ-573]
func TestQscTheStepsOfTheQuerySchemaFailInATransactionThatIsOverAndAPlanWithoutThemDoesNothing(t *testing.T) {
	_, admin := sxOpen(t)
	ctx := context.Background()
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runQuerySteps(ctx, tx, pgschema.Plan{}); err != nil {
		t.Errorf("a plan without a query schema: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstallToken(ctx, tx); err == nil {
		t.Error("the token is read in a transaction that is over")
	}
	if err := ensureQueryRoles(ctx, tx, []string{"pq_qsc_none"}); err == nil {
		t.Error("the roles are made in a transaction that is over")
	}
	if err := dropQuerySchema(ctx, tx, 9); err == nil {
		t.Error("the schemas are dropped in a transaction that is over")
	}
	if err := runQuerySteps(ctx, tx, pgschema.Plan{Query: []pgschema.Step{{SQL: "SELECT 1"}}, Roles: []string{"pq_qsc_none"}}); err == nil {
		t.Error("the steps run in a transaction that is over")
	}
}
