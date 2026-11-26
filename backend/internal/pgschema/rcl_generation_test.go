package pgschema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The generator version 12 (ADR-0122): the tables and the indexes the computation of the links reads and writes, and the kind of problem
// it adds. Every helper of the tests starts with rcl.

func rclStepsOfTwelve(t *testing.T) []pgschema.Step {
	t.Helper()
	next := igtPlan(t).Manifest
	if next.GeneratorVersion < 12 {
		t.Fatalf("the generator of this build is %d", next.GeneratorVersion)
	}
	next.GeneratorVersion = 12 // the steps of version 12 alone
	old := next
	old.GeneratorVersion = 11
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	return delta.Steps
}

// [REQ-877]
func TestRclDiffFromGeneratorElevenToTwelveAddsTheThreeTablesAndTheTwoIndexesAndNothingElse(t *testing.T) {
	var got []string
	for _, s := range rclStepsOfTwelve(t) {
		got = append(got, s.Name)
		if s.Phase != pgschema.InTx || !strings.Contains(s.SQL, "to_regclass") {
			t.Errorf("%s is not a step of the transaction that leaves a schema without the table as it is: %s", s.Name, s.SQL)
		}
		switch s.What {
		case pgschema.WhatTable:
			// made to be run twice, and no table that the readers use is changed
			if !strings.Contains(s.SQL, "CREATE TABLE IF NOT EXISTS") || strings.Contains(s.SQL, "ALTER TABLE") {
				t.Errorf("%s is not a table made to be run twice: %s", s.Name, s.SQL)
			}
		case pgschema.WhatIndex:
			if !strings.Contains(s.SQL, "IF NOT EXISTS") {
				t.Errorf("%s cannot be run twice: %s", s.Name, s.SQL)
			}
		default:
			t.Errorf("%s is a step of %v", s.Name, s.What)
		}
	}
	want := []string{"calc_tokey", "calc_linkwin", "calc_linklost", "calc_end_key", "calc_linklost_ref"}
	if !slices.Equal(got, want) {
		t.Errorf("the steps are %v, want %v", got, want)
	}
}

// [REQ-877]
func TestRclDiffFromGeneratorTenToTwelveHasTheStepsOfBothVersions(t *testing.T) {
	next := igtPlan(t).Manifest
	old := next
	old.GeneratorVersion = 10
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range delta.Steps {
		names[s.Name] = true
	}
	for _, n := range []string{"calc_tokey", "calc_linkwin", "calc_linklost", "calc_end_key"} {
		if !names[n] {
			t.Errorf("no step %s", n)
		}
	}
}

// [REQ-877]
func TestRclTheTablesOfTheComputationOfTheLinksAreCalcTablesAndAreEmptiedWithTheOthers(t *testing.T) {
	calc := pgschema.ComputeTables()
	for _, name := range []string{pgschema.TableCalcToKey, pgschema.TableCalcLinkWin, pgschema.TableCalcLinkLost} {
		if !slices.Contains(calc, name) {
			t.Errorf("%s is not a table of the computation", name)
		}
	}
}

// [REQ-876]
func TestRclTheKindsOfProblemTheFourthStepOwnsAreTheOnesTheLinksMakeAndTheClosedListHasThem(t *testing.T) {
	owned := pgschema.StepFourProblemKinds()
	for _, kind := range []string{pgschema.ProblemSourceConflict, pgschema.ProblemLinkTarget, pgschema.ProblemLinkAmbiguous} {
		if !slices.Contains(owned, kind) {
			t.Errorf("%s is not owned: %v", kind, owned)
		}
		if !slices.Contains(pgschema.ProblemKinds(), kind) {
			t.Errorf("%s is not in the closed list", kind)
		}
	}
	// the cardinality is the fifth step's, and the cells that gave no value are said with the kind the objects use
	for _, kind := range []string{pgschema.ProblemCardinality, pgschema.ProblemInvalidValue} {
		if slices.Contains(owned, kind) {
			t.Errorf("%s is owned by the fourth step", kind)
		}
	}
}
