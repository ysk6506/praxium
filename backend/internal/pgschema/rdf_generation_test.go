package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-785]
func TestRdfDiffFromGeneratorFiveAddsTheWaitsExpiredTheRowOfARecordTheProblemsThatHaveNoDefinitionTheOwnerOfTheComputationAndTheIndexOfTheRowsReceivedAndNothingElse(t *testing.T) {
	next := igtPlan(t).Manifest
	old := next
	old.GeneratorVersion = 5
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"record_wait.expired":   `ALTER TABLE IF EXISTS "s_7"."record_wait" ADD COLUMN IF NOT EXISTS expired boolean NOT NULL DEFAULT false`,
		"calc_record.page":      `ALTER TABLE IF EXISTS "s_7"."calc_record" ADD COLUMN IF NOT EXISTS page integer`,
		"calc_record.row_no":    `ALTER TABLE IF EXISTS "s_7"."calc_record" ADD COLUMN IF NOT EXISTS row_no integer`,
		"record_problem.def_no": `ALTER TABLE IF EXISTS "s_7"."record_problem" ALTER COLUMN def_no DROP NOT NULL`,
		"calc_problem.def_no":   `ALTER TABLE IF EXISTS "s_7"."calc_problem" ALTER COLUMN def_no DROP NOT NULL`,
	}
	want["calc_owner"] = `DO $$ BEGIN IF to_regclass('"s_7"."calc_record"') IS NOT NULL THEN CREATE TABLE IF NOT EXISTS "s_7"."calc_owner" (
    run_no bigint NOT NULL PRIMARY KEY
); END IF; END $$`
	want["stage_record_key"] = `DO $$ BEGIN IF to_regclass('"s_7"."stage_record"') IS NOT NULL THEN CREATE INDEX IF NOT EXISTS "stage_record_key" ON "s_7"."stage_record" (dataset_no, key, page, row_no); END IF; END $$`
	if len(delta.Steps) != len(want) {
		t.Fatalf("%d steps", len(delta.Steps))
	}
	for _, s := range delta.Steps {
		kind := pgschema.WhatColumn
		switch s.Name {
		case "stage_record_key":
			kind = pgschema.WhatIndex
		case "calc_owner":
			kind = pgschema.WhatTable
		}
		if s.Phase != pgschema.InTx || s.What != kind || want[s.Name] != s.SQL {
			t.Errorf("unexpected step %+v", s)
		}
	}
	if delta.Result.GeneratorVersion != pgschema.GeneratorVersion {
		t.Errorf("the generator to keep is %d", delta.Result.GeneratorVersion)
	}
	plan := igtPlan(t)
	for table, column := range map[string]string{"record_wait": "expired boolean NOT NULL DEFAULT false", "calc_record": "page integer,", "calc_wait": "expired boolean NOT NULL", "calc_owner": "run_no bigint NOT NULL PRIMARY KEY"} {
		if !strings.Contains(stepSQL(plan, table), column) {
			t.Errorf("a new KG has no %q in %s: %s", column, table, stepSQL(plan, table))
		}
	}
	for _, table := range []string{"record_problem", "calc_problem"} {
		if strings.Contains(stepSQL(plan, table), "def_no integer NOT NULL") || !strings.Contains(stepSQL(plan, table), "def_no integer,") {
			t.Errorf("a problem of a new KG may have no definition in %s: %s", table, stepSQL(plan, table))
		}
	}
}

// [REQ-785]
func TestRdfDiffFromGeneratorThreeToTheNewestIsTheStepsOfFourAndThenSix(t *testing.T) {
	next := igtPlan(t).Manifest
	old := next
	old.GeneratorVersion = 3
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range delta.Steps {
		if s.Phase == pgschema.InTx {
			names = append(names, s.Name)
		}
	}
	// the steps of four, then those of five (which move the values), then the six of this change, in one transaction
	want := []string{"stage_key.held", "stage_end.held", "stage_claim.held"}
	for i, name := range want {
		if i >= len(names) || names[i] != name {
			t.Fatalf("step %d is not %s: %v", i, name, names)
		}
	}
	tail := []string{"record_wait.expired", "calc_record.page", "calc_record.row_no", "record_problem.def_no", "calc_problem.def_no", "calc_owner", "stage_record_key"}
	if len(names) < len(tail) || strings.Join(names[len(names)-len(tail):], "|") != strings.Join(tail, "|") {
		t.Errorf("the steps of six are not last of the steps of a transaction: %v", names)
	}
	same := next
	if d, err := pgschema.Diff(same, next); err != nil || len(d.Steps) != 0 {
		t.Errorf("the same generator needs no step: %d %v", len(d.Steps), err)
	}
}
