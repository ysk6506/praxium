package pgschema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The generator version 13 (ADR-0123): the tables and the indexes of the fifth step, which takes out of the staged rows what the KG
// cannot have. Every helper of the tests starts with iso.

func isoStepsOf(t *testing.T, from, to int) []pgschema.Step {
	t.Helper()
	next := igtPlan(t).Manifest
	if next.GeneratorVersion < 13 {
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

// [REQ-850]
func TestIsoDiffFromGeneratorTwelveToThirteenAddsTheTwoTablesAndTheTwoIndexesAndNothingElse(t *testing.T) {
	var got []string
	for _, s := range isoStepsOf(t, 12, 13) {
		got = append(got, s.Name)
		if s.Phase != pgschema.InTx || !strings.Contains(s.SQL, "to_regclass") {
			t.Errorf("%s is not a step of the transaction that leaves a schema without the table as it is: %s", s.Name, s.SQL)
		}
		switch s.What {
		case pgschema.WhatTable:
			if !strings.Contains(s.SQL, "CREATE TABLE IF NOT EXISTS") || strings.Contains(s.SQL, "ALTER TABLE") {
				t.Errorf("%s is not a table made to be run twice: %s", s.Name, s.SQL)
			}
		case pgschema.WhatIndex:
			if !strings.Contains(s.SQL, "IF NOT EXISTS") || strings.Contains(s.SQL, "ALTER TABLE") {
				t.Errorf("%s cannot be run twice: %s", s.Name, s.SQL)
			}
		default:
			t.Errorf("%s is a step of %v", s.Name, s.What)
		}
	}
	want := []string{"calc_isolated", "calc_seat", "stage_ident_value", "stage_link_ref"}
	if !slices.Equal(got, want) {
		t.Errorf("the steps are %v, want %v", got, want)
	}
	// the steps of version 12 are the same as they were: the version 12 alone gives what it gave
	var twelve []string
	for _, s := range isoStepsOf(t, 11, 12) {
		twelve = append(twelve, s.Name)
	}
	if !slices.Equal(twelve, []string{"calc_tokey", "calc_linkwin", "calc_linklost", "calc_end_key", "calc_linklost_ref"}) {
		t.Errorf("the steps of version 12 are %v", twelve)
	}
}

// [REQ-850]
func TestIsoDiffFromGeneratorElevenToThirteenHasTheStepsOfBothVersions(t *testing.T) {
	names := map[string]bool{}
	for _, s := range isoStepsOf(t, 11, 13) {
		names[s.Name] = true
	}
	for _, n := range []string{"calc_tokey", "calc_linkwin", "calc_isolated", "calc_seat", "stage_ident_value", "stage_link_ref"} {
		if !names[n] {
			t.Errorf("no step %s", n)
		}
	}
}

// [REQ-850]
func TestIsoTheTablesOfTheFifthStepAreCalcTablesAndTheirKeysAreWhatIsTakenAndThePlace(t *testing.T) {
	calc := pgschema.ComputeTables()
	for _, name := range []string{pgschema.TableCalcIsolated, pgschema.TableCalcSeat} {
		if !slices.Contains(calc, name) {
			t.Errorf("%s is not a table of the computation", name)
		}
	}
	plan := igtPlan(t)
	for table, key := range map[string]string{
		pgschema.TableCalcIsolated: "PRIMARY KEY (kind, entity_no, scheme, value, scope)",
		pgschema.TableCalcSeat:     "PRIMARY KEY (side, end_no, bind_id)",
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
}

// [REQ-851]
func TestIsoTheKindsOfProblemTheFifthStepWritesAreInTheClosedListAndNoneIsNew(t *testing.T) {
	owned := pgschema.StepFiveProblemKinds()
	want := []string{pgschema.ProblemIdentityConflict, pgschema.ProblemLinkTarget, pgschema.ProblemCardinality}
	if !slices.Equal(owned, want) {
		t.Errorf("the fifth step writes %v, want %v", owned, want)
	}
	for _, kind := range owned {
		if !slices.Contains(pgschema.ProblemKinds(), kind) {
			t.Errorf("%s is not in the closed list", kind)
		}
	}
	if len(pgschema.ProblemKinds()) != 17 {
		t.Errorf("the closed list has %d kinds", len(pgschema.ProblemKinds()))
	}
	// the cardinality was not the fourth step's and is the fifth step's now
	if slices.Contains(pgschema.StepFourProblemKinds(), pgschema.ProblemCardinality) {
		t.Error("the fourth step owns the cardinality")
	}
}
