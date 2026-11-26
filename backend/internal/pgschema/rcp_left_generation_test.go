package pgschema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The generator version 9 (ADR-0119): the table of the objects the computation of the objects left.

// [REQ-971]
func TestRcpDiffFromGeneratorEightToNineAddsTheTableOfTheObjectsThatWereLeftAndNothingElse(t *testing.T) {
	next := igtPlan(t).Manifest
	if next.GeneratorVersion < 9 {
		t.Fatalf("the generator of this build is %d", next.GeneratorVersion)
	}
	next.GeneratorVersion = 9
	old := next
	old.GeneratorVersion = 8
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range delta.Steps {
		got = append(got, s.Name)
		if s.Phase != pgschema.InTx || s.What != pgschema.WhatTable {
			t.Errorf("%s is not a table made in the transaction", s.Name)
		}
		// made to be run twice, and a schema that has no tables of the computation is left as it is
		if !strings.Contains(s.SQL, "IF NOT EXISTS") || !strings.Contains(s.SQL, "to_regclass") {
			t.Errorf("%s cannot be run twice or on a schema without the tables: %s", s.Name, s.SQL)
		}
	}
	if want := []string{pgschema.TableCalcLeft}; !slices.Equal(got, want) {
		t.Errorf("the steps are %v, want %v", got, want)
	}
}

// [REQ-971]
func TestRcpTheTableOfTheObjectsThatWereLeftIsOneOfTheTablesOfTheComputationAndOfANewPlan(t *testing.T) {
	if !slices.Contains(pgschema.ComputeTables(), pgschema.TableCalcLeft) {
		t.Errorf("calc_left is not emptied with the tables of the computation: %v", pgschema.ComputeTables())
	}
	plan := igtPlan(t)
	if !slices.Contains(plan.Tables(), pgschema.TableCalcLeft) {
		t.Errorf("a new schema has no calc_left")
	}
	// from the first generators on, the table is among the steps
	next := plan.Manifest
	old := next
	old.GeneratorVersion = 2
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range delta.Steps {
		found = found || s.Name == pgschema.TableCalcLeft
	}
	if !found {
		t.Error("no step makes calc_left from generator 2")
	}
}

// [REQ-941]
func TestRcpTheKindsOfProblemTheFourthStepOwnsAreOnlyTheOnesItWritesAndEveryOneOfThemIsInTheClosedList(t *testing.T) {
	owned := pgschema.StepFourProblemKinds()
	if !slices.Equal(owned, []string{pgschema.ProblemSourceConflict}) {
		t.Errorf("the fourth step owns %v", owned)
	}
	for _, kind := range []string{pgschema.ProblemLinkTarget, pgschema.ProblemLinkAmbiguous, pgschema.ProblemCardinality} {
		if slices.Contains(owned, kind) {
			t.Errorf("%s is owned by the fourth step, which does not write it", kind)
		}
	}
}
