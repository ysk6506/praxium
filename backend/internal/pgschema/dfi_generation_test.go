package pgschema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The generator version 14 (ADR-0127): the tables of the difference of the fifth step. Every helper of the tests starts with dfi.

func dfiStepsOf(t *testing.T, from, to int) []pgschema.Step {
	t.Helper()
	next := igtPlan(t).Manifest
	if next.GeneratorVersion < 14 {
		t.Fatalf("the generator of this build is %d", next.GeneratorVersion)
	}
	next.GeneratorVersion = to
	old := next
	old.GeneratorVersion = from
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	return delta.Steps
}

// [REQ-964]
func TestDfiDiffFromGeneratorThirteenToFourteenAddsTheTwoTablesOfTheDifferenceAndNothingElse(t *testing.T) {
	var got []string
	for _, s := range dfiStepsOf(t, 13, 14) {
		got = append(got, s.Name)
		if s.Phase != pgschema.InTx || s.What != pgschema.WhatTable || !strings.Contains(s.SQL, "to_regclass") ||
			!strings.Contains(s.SQL, "CREATE TABLE IF NOT EXISTS") || strings.Contains(s.SQL, "ALTER TABLE") {
			t.Errorf("%s is not a table made beside the others that can be made twice: %s", s.Name, s.SQL)
		}
	}
	if want := []string{pgschema.TableCalcDiffRow, pgschema.TableCalcDiffPlan}; !slices.Equal(got, want) {
		t.Errorf("the steps are %v, want %v", got, want)
	}
	names := map[string]bool{}
	for _, s := range dfiStepsOf(t, 12, 14) {
		names[s.Name] = true
	}
	for _, n := range []string{"calc_isolated", "calc_seat", "stage_ident_value", pgschema.TableCalcDiffRow, pgschema.TableCalcDiffPlan} {
		if !names[n] {
			t.Errorf("from 12 to 14 there is no step %s", n)
		}
	}
}

// [REQ-964]
func TestDfiTheTablesOfTheDifferenceAreCalcTablesKeyedByTheOrderOfTheDifference(t *testing.T) {
	calc := pgschema.ComputeTables()
	for _, name := range []string{pgschema.TableCalcDiffRow, pgschema.TableCalcDiffPlan} {
		if !slices.Contains(calc, name) {
			t.Errorf("%s is not a table of the computation", name)
		}
	}
	plan := igtPlan(t)
	for table, key := range map[string]string{
		pgschema.TableCalcDiffRow:  "PRIMARY KEY (kind, type_no, ref, tie)",
		pgschema.TableCalcDiffPlan: "PRIMARY KEY (kind, type_no)",
	} {
		found := false
		for _, s := range plan.Steps {
			if s.What == pgschema.WhatTable && s.Name == table {
				found = true
				if !strings.Contains(s.SQL, key) {
					t.Errorf("%s has not the key %s: %s", table, key, s.SQL)
				}
			}
		}
		if !found {
			t.Errorf("the plan makes no %s", table)
		}
	}
	if pgschema.CalcJobDiff != pgschema.CalcJobIsolate+1 {
		t.Errorf("the job of the difference is %d, after the isolation's %d", pgschema.CalcJobDiff, pgschema.CalcJobIsolate)
	}
}
