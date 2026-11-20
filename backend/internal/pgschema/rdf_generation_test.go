package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-785]
func TestRdfDiffFromGeneratorFiveAddsTheWaitsExpiredTheRowOfARecordTheProblemsThatHaveNoDefinitionAndTheIndexOfTheRowsReceivedAndNothingElse(t *testing.T) {
	next := igtPlan(t).Manifest
	old := next
	old.GeneratorVersion = 5
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"record_wait.expired":   `ALTER TABLE "s_7"."record_wait" ADD COLUMN IF NOT EXISTS expired boolean NOT NULL DEFAULT false`,
		"calc_record.page":      `ALTER TABLE "s_7"."calc_record" ADD COLUMN IF NOT EXISTS page integer`,
		"calc_record.row_no":    `ALTER TABLE "s_7"."calc_record" ADD COLUMN IF NOT EXISTS row_no integer`,
		"record_problem.def_no": `ALTER TABLE "s_7"."record_problem" ALTER COLUMN def_no DROP NOT NULL`,
		"calc_problem.def_no":   `ALTER TABLE "s_7"."calc_problem" ALTER COLUMN def_no DROP NOT NULL`,
	}
	want["stage_record_key"] = `CREATE INDEX IF NOT EXISTS "stage_record_key" ON "s_7"."stage_record" (dataset_no, key, page, row_no)`
	if len(delta.Steps) != len(want) {
		t.Fatalf("%d steps", len(delta.Steps))
	}
	for _, s := range delta.Steps {
		kind := pgschema.WhatColumn
		if s.Name == "stage_record_key" {
			kind = pgschema.WhatIndex
		}
		if s.Phase != pgschema.InTx || s.What != kind || want[s.Name] != s.SQL {
			t.Errorf("unexpected step %+v", s)
		}
	}
	if delta.Result.GeneratorVersion != pgschema.GeneratorVersion {
		t.Errorf("the generator to keep is %d", delta.Result.GeneratorVersion)
	}
	plan := igtPlan(t)
	for table, column := range map[string]string{"record_wait": "expired boolean NOT NULL DEFAULT false", "calc_record": "page integer,", "calc_wait": "expired boolean NOT NULL"} {
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
	if err != nil || len(delta.Steps) != 9 {
		t.Fatalf("%d steps, %v", len(delta.Steps), err)
	}
	for i, name := range []string{"stage_key.held", "stage_end.held", "stage_claim.held", "record_wait.expired"} {
		if delta.Steps[i].Name != name {
			t.Errorf("step %d is %s, not %s", i, delta.Steps[i].Name, name)
		}
	}
	same := next
	if d, err := pgschema.Diff(same, next); err != nil || len(d.Steps) != 0 {
		t.Errorf("the same generator needs no step: %d %v", len(d.Steps), err)
	}
}
