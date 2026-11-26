package pgschema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-951] [REQ-957]
func TestExpDiffFromGeneratorSevenToEightAddsTheTableOfTheMarksAndTheTableOfTheKeysTheExpansionReadAndAltersNoTable(t *testing.T) {
	next := igtPlan(t).Manifest
	next.GeneratorVersion = 8
	old := next
	old.GeneratorVersion = 7
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"object_retire", "calc_xkey"}
	var got []string
	for _, s := range delta.Steps {
		got = append(got, s.Name)
		if s.Phase != pgschema.InTx {
			t.Errorf("%s is not in the transaction", s.Name)
		}
		// Every step is made to be run twice: a schema made by this build has all of it.
		if !strings.Contains(s.SQL, "IF NOT EXISTS") {
			t.Errorf("%s cannot be run twice: %s", s.Name, s.SQL)
		}
		// the adjustment makes tables beside the others and takes no table that a reader may have open
		if strings.Contains(s.SQL, "ALTER TABLE") || s.What != pgschema.WhatTable {
			t.Errorf("%s is not a table made beside the others: %s", s.Name, s.SQL)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the steps are %v, want %v", got, want)
	}
	if delta.Result.GeneratorVersion != 8 {
		t.Errorf("the generator to keep is %d", delta.Result.GeneratorVersion)
	}
}

// [REQ-951]
func TestExpTheSchemaOfThisBuildHasTheTableOfTheMarksAndTheTableOfTheKeysTheExpansionReadForTheKnowledgeGraph(t *testing.T) {
	plan := igtPlan(t)
	if plan.Manifest.GeneratorVersion != pgschema.GeneratorVersion {
		t.Fatalf("the plan is of generator %d, the build is %d", plan.Manifest.GeneratorVersion, pgschema.GeneratorVersion)
	}
	var marks, xkey bool
	for _, s := range plan.Steps {
		marks = marks || s.Name == pgschema.TableObjectRetire
		xkey = xkey || s.Name == pgschema.TableCalcXKey
	}
	if !marks || !xkey {
		t.Errorf("a new schema has the table of the marks: %t, and the table of the keys: %t", marks, xkey)
	}
	// the table of the marks is kept between runs, the table of the keys is emptied with the tables of the computation
	if !slices.Contains(pgschema.IngestTables(), pgschema.TableObjectRetire) || slices.Contains(pgschema.ComputeTables(), pgschema.TableObjectRetire) || !slices.Contains(pgschema.ComputeTables(), pgschema.TableCalcXKey) {
		t.Errorf("the tables of the ingestion: %v, of the computation: %v", pgschema.IngestTables(), pgschema.ComputeTables())
	}
}
