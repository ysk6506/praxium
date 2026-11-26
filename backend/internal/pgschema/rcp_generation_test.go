package pgschema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The generator version 8 (ADR-0115): the indexes the computation of the objects reads by.

// [REQ-941]
func TestRcpDiffFromGeneratorSevenToEightAddsTheTwoIndexesOfTheComputationOfTheObjectsAndNothingElse(t *testing.T) {
	// The generator is fixed at 8 here: the diff from 7 to 8 is about the two indexes whatever the generator of this build is.
	next := igtPlan(t).Manifest
	next.GeneratorVersion = 8
	old := next
	old.GeneratorVersion = 7
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range delta.Steps {
		got = append(got, s.Name)
		if s.Phase != pgschema.InTx || s.What != pgschema.WhatIndex {
			t.Errorf("%s is not an index made in the transaction", s.Name)
		}
		// made to be run twice, and a schema that has no such table is left as it is
		if !strings.Contains(s.SQL, "IF NOT EXISTS") || !strings.Contains(s.SQL, "to_regclass") {
			t.Errorf("%s cannot be run twice or on a schema without the table: %s", s.Name, s.SQL)
		}
	}
	if want := []string{"calc_ref_target", "calc_ask_pair"}; !slices.Equal(got, want) {
		t.Errorf("the steps are %v, want %v", got, want)
	}
}

// [REQ-941]
func TestRcpDiffFromGeneratorTwoToEightHasTheStepsOfEveryVersionAfterIt(t *testing.T) {
	next := igtPlan(t).Manifest
	old := next
	old.GeneratorVersion = 2
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range delta.Steps {
		names[s.Name] = true
	}
	for _, n := range []string{"calc_slot", "calc_ref_pair", "calc_ref_target", "calc_ask_pair"} {
		if !names[n] {
			t.Errorf("no step %s", n)
		}
	}
}

// [REQ-941]
func TestRcpTheKindsOfProblemTheFourthStepOwnsAreKindsOfTheClosedListAndNotTheOnesOfTheReceivingAndTheIdentification(t *testing.T) {
	owned := pgschema.StepFourProblemKinds()
	if !slices.Contains(owned, pgschema.ProblemSourceConflict) {
		t.Errorf("source_conflict is not owned: %v", owned)
	}
	for _, kind := range owned {
		if !slices.Contains(pgschema.ProblemKinds(), kind) {
			t.Errorf("%s is not in the closed list", kind)
		}
	}
	for _, kind := range []string{pgschema.ProblemDuplicateKey, pgschema.ProblemIdentityConflict, pgschema.ProblemContextParent, pgschema.ProblemInvalidValue, pgschema.ProblemMissingName} {
		if slices.Contains(owned, kind) {
			t.Errorf("%s is owned by the fourth step", kind)
		}
	}
	if pgschema.CalcJobRecompute == pgschema.CalcJobRawDiff || pgschema.CalcJobRecompute == pgschema.CalcJobIdentify || pgschema.CalcJobRecompute == pgschema.CalcJobExpand {
		t.Errorf("the job of the computation of the objects is the number of another job")
	}
}
