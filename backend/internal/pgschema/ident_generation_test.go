package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-880]
func TestIdnDiffFromGeneratorSixToSevenAddsTheHeldColumnsTheFeedOfABindTheFourWorkingTablesAndTheirIndexesAndNothingElse(t *testing.T) {
	next := igtPlan(t).Manifest
	next.GeneratorVersion = 7
	old := next
	old.GeneratorVersion = 6
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"calc_key.held", "calc_end.held", "calc_claim.held", "calc_bind.feed_no",
		"calc_slot", "calc_label", "calc_klabel", "calc_ask", "calc_newobj",
		"stage_key_page", "stage_end_page", "stage_claim_page", "calc_record_row", "calc_key_rid", "calc_end_rid", "calc_claim_rid",
		"calc_claim_value", "calc_bind_pair", "calc_newkey_key", "calc_slot_pid", "calc_slot_key", "calc_label_pid", "calc_klabel_key", "calc_label_label",
		"calc_newobj_ref", "calc_ref_pair",
	}
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
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the steps are %v, want %v", got, want)
	}
	if delta.Result.GeneratorVersion != 7 {
		t.Errorf("the generator to keep is %d", delta.Result.GeneratorVersion)
	}
}

// [REQ-880]
func TestIdnDiffFromGeneratorTwoToSevenHasTheStepsOfEveryVersionAfterIt(t *testing.T) {
	next := igtPlan(t).Manifest
	next.GeneratorVersion = 7
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
	for _, n := range []string{"calc_slot", "calc_bind.feed_no", "calc_ref_pair", "stage_record_key"} {
		if !names[n] {
			t.Errorf("no step %s", n)
		}
	}
}
