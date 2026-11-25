package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of generator version 5 (ADR-0101): the name indexes of the tables of the types and the move of the volatile values into their
// columns. Their names start with TestVm.

// [REQ-761]
func TestVmEveryTableOfATypeHasTheTwoNameIndexesAndNoIndexOnTheReference(t *testing.T) {
	plan, err := pgschema.Build(registry(t, smallDefs(t)), pgschema.Options{KGNo: 7, WithStats: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"t_1_name":    `CREATE INDEX "t_1_name" ON "s_7"."t_1" (name)`,
		"t_1_name_ci": `CREATE INDEX "t_1_name_ci" ON "s_7"."t_1" (lower(name) text_pattern_ops)`,
		"t_2_name":    `CREATE INDEX "t_2_name" ON "s_7"."t_2" (name)`,
		"t_2_name_ci": `CREATE INDEX "t_2_name_ci" ON "s_7"."t_2" (lower(name) text_pattern_ops)`,
	}
	for name, sql := range want {
		if got := stepSQL(plan, name); got != sql {
			t.Errorf("%s:\n got %s\nwant %s", name, got, sql)
		}
	}
	for _, st := range plan.Steps {
		if st.What == pgschema.WhatIndex && strings.HasPrefix(st.Name, "t_") && strings.Contains(st.SQL, "(ref") {
			t.Errorf("an index on the reference in a table of a type: %s", st.SQL)
		}
	}
	if pgschema.NameIndex(45) != "t_45_name" || pgschema.NameIndexFold(45) != "t_45_name_ci" {
		t.Errorf("the names of the indexes: %s %s", pgschema.NameIndex(45), pgschema.NameIndexFold(45))
	}
}

// [REQ-763]
func TestVmAKGOfGeneratorFourIsFollowedToFiveByMovingTheValuesAndBuildingTheNameIndexesAfterwards(t *testing.T) {
	next := manifestOf(t, smallDefs(t))
	next.GeneratorVersion = 5 // the steps of version 5 alone: what a later version adds is in the tests of that version
	old := next
	old.GeneratorVersion = 4
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tx data object_state", "tx data s_1",
		"concurrent index t_1_name", "concurrent index t_1_name_ci", "concurrent index t_2_name", "concurrent index t_2_name_ci"}
	if got := summary(delta.Steps); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("steps %v, want %v", got, want)
	}
	for _, s := range delta.Steps {
		switch {
		case s.What == pgschema.WhatIndex && !strings.HasPrefix(s.SQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "):
			t.Errorf("an index that is not built concurrently, and again without harm: %s", s.SQL)
		case s.What == pgschema.WhatData && !strings.HasPrefix(s.SQL, `UPDATE "s_5"."`+s.Name+`" AS t SET `):
			t.Errorf("a data step that is not an UPDATE of its table: %s", s.SQL)
		}
	}
	state := stepOf(delta.Steps, "object_state")
	for _, part := range []string{
		`a4 = CASE WHEN jsonb_exists(t.prov->'4', 'v') THEN (t.prov->'4'->>'v') ELSE t.a4 END`,
		`THEN public.praxium_ts(t.prov->'7'->>'s') END ELSE t.a7_at END`,
		`jsonb_build_object('t', x.value->'s')`,
		`WHERE EXISTS (SELECT 1 FROM jsonb_each(t.prov) AS y WHERE jsonb_typeof(y.value) = 'object' AND (jsonb_exists(y.value, 'v') OR jsonb_exists(y.value, 's')))`,
	} {
		if !strings.Contains(state, part) {
			t.Errorf("the statement of the common table lacks %s:\n%s", part, state)
		}
	}
	if own := stepOf(delta.Steps, "s_1"); !strings.Contains(own, `a13 = CASE WHEN jsonb_exists(t.prov->'13', 'v') THEN ((t.prov->'13'->>'v')::double precision) ELSE t.a13 END`) {
		t.Errorf("the statement of the table of the server does not cast a number:\n%s", own)
	}
	if delta.Result.GeneratorVersion != next.GeneratorVersion {
		t.Errorf("the generator to keep is %d", delta.Result.GeneratorVersion)
	}
}

// [REQ-763]
func TestVmAKGOfGeneratorThreeIsFollowedThroughFourToFiveInOrder(t *testing.T) {
	next := manifestOf(t, smallDefs(t))
	next.GeneratorVersion = 5 // the steps of version 5 alone: what a later version adds is in the tests of that version
	old := next
	old.GeneratorVersion = 3
	delta, err := pgschema.Diff(old, next)
	if err != nil {
		t.Fatal(err)
	}
	got := summary(delta.Steps)
	want := []string{"tx column stage_key.held", "tx column stage_end.held", "tx column stage_claim.held", "tx data object_state", "tx data s_1",
		"concurrent index t_1_name", "concurrent index t_1_name_ci", "concurrent index t_2_name", "concurrent index t_2_name_ci"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("steps %v, want %v", got, want)
	}
}

// [REQ-761]
func TestVmATypeThatIsNewInTheDiffGetsItsIndexesWithItsTableAndTheOldTypesGetThemAfterwards(t *testing.T) {
	d := smallDefs(t)
	old := manifestOf(t, d)
	old.GeneratorVersion = 4
	d.ObjectTypes = append(d.ObjectTypes, objType(t, "ot.site", 3, "Site", []string{"tr.common"}, prop(t, "pr.site.code", "code", 30, "vt.string", model.ChangeSlow)))
	delta, err := pgschema.Diff(old, manifestOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range summary(delta.Steps) {
		if strings.Contains(s, "name") {
			got = append(got, s)
		}
	}
	want := []string{"tx index t_3_name", "tx index t_3_name_ci", "concurrent index t_1_name", "concurrent index t_1_name_ci", "concurrent index t_2_name", "concurrent index t_2_name_ci"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("name indexes %v, want %v", got, want)
	}
}

func stepOf(steps []pgschema.Step, name string) string {
	for _, s := range steps {
		if s.Name == name {
			return s.SQL
		}
	}
	return ""
}

// [REQ-763]
func TestVmAValueThatHasNoColumnKindStopsTheFollowRatherThanMovingItWrong(t *testing.T) {
	next := manifestOf(t, smallDefs(t))
	old := next
	old.GeneratorVersion = 4
	old.CommonState = append([]pgschema.Column(nil), next.CommonState...)
	old.CommonState[0].Kind = "no-such-kind"
	if _, err := pgschema.Diff(old, next); err == nil {
		t.Error("the common table with a column of an unknown kind was followed")
	}
	old = next
	old.GeneratorVersion = 4
	old.Types = append([]pgschema.TypeSummary(nil), next.Types...)
	for i := range old.Types {
		if len(old.Types[i].State) > 0 {
			old.Types[i].State = append([]pgschema.Column(nil), old.Types[i].State...)
			old.Types[i].State[0].Kind = "no-such-kind"
		}
	}
	if _, err := pgschema.Diff(old, next); err == nil {
		t.Error("the table of a type with a column of an unknown kind was followed")
	}
}
