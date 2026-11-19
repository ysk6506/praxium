package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// expectedTables works out from the model alone how many tables a KG has: a table per type, a table of
// volatile values per type that has one beyond what every type has, and the fixed tables.
func expectedTables(t *testing.T, reg *model.Registry) (types, withState int) {
	t.Helper()
	all := reg.ObjectTypes()
	volatile := map[int]int{}
	per := make([]map[int]bool, len(all))
	for i, ot := range all {
		per[i] = map[int]bool{}
		for _, p := range reg.EffectiveProperties(ot.ID) {
			if p.EffectiveHistory() == model.HistoryVolatile {
				per[i][p.AttrNo] = true
				volatile[p.AttrNo]++
			}
		}
	}
	for i := range all {
		for no := range per[i] {
			if volatile[no] != len(all) {
				withState++
				break
			}
		}
	}
	return len(all), withState
}

func TestTheStandardModelHasAPlan(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	search, err := pgschema.DefaultSearchSet(reg)
	if err != nil {
		t.Fatal(err)
	}
	opts := pgschema.Options{KGNo: 1, Search: search, WithStats: true}
	plan, err := pgschema.Build(reg, opts)
	if err != nil {
		t.Fatal(err)
	}
	types, withState := expectedTables(t, reg)
	if want := types + withState + len(pgschema.FixedTables()); len(plan.Tables()) != want {
		t.Fatalf("%d tables, want %d (%d types, %d with state)", len(plan.Tables()), want, types, withState)
	}
	counts := map[string]int{}
	for _, s := range plan.Steps {
		counts[s.What]++
		if len(s.Name) > pgschema.MaxIdentBytes {
			t.Errorf("the name %q is %d bytes", s.Name, len(s.Name))
		}
		if s.SQL == "" || s.Phase != pgschema.InTx {
			t.Errorf("step %s: empty text or not in the transaction", s.Name)
		}
	}
	if counts[pgschema.WhatIndex] < len(search) || counts[pgschema.WhatStatistics] == 0 {
		t.Errorf("indexes %d for %d searched, statistics %d", counts[pgschema.WhatIndex], len(search), counts[pgschema.WhatStatistics])
	}
	t.Logf("types %d, with state %d, tables %d, indexes %d (search %d), statistics %d, statements %d",
		types, withState, len(plan.Tables()), counts[pgschema.WhatIndex], len(search), counts[pgschema.WhatStatistics], len(plan.Steps))
	if len(plan.Manifest.CommonState) < 2 {
		t.Errorf("the standard model has a lifecycle and an operating state in every type: %v", plan.Manifest.CommonState)
	}
}

func TestTheStandardPlanIsTheSameTwiceAndItsManifestFollowsItself(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	search, _ := pgschema.DefaultSearchSet(reg)
	opts := pgschema.Options{KGNo: 3, Search: search, WithStats: true}
	a, err := pgschema.Build(reg, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := pgschema.Build(reg, opts)
	var ta, tb strings.Builder
	for _, s := range a.Steps {
		ta.WriteString(s.SQL + ";\n")
	}
	for _, s := range b.Steps {
		tb.WriteString(s.SQL + ";\n")
	}
	if ta.String() != tb.String() {
		t.Fatal("two plans of the same input differ")
	}
	delta, err := pgschema.Diff(a.Manifest, b.Manifest)
	if err != nil || len(delta.Steps) != 0 {
		t.Fatalf("a manifest against itself: %v %v", delta.Steps, err)
	}
}
