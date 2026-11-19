package pgschema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

func TestManifestHashDoesNotDependOnTheOrderOfTheInput(t *testing.T) {
	a := smallDefs(t)
	b := smallDefs(t)
	slices.Reverse(b.ObjectTypes)
	for i := range b.ObjectTypes {
		slices.Reverse(b.ObjectTypes[i].Properties)
	}
	ha, err := manifestOf(t, a).Hash()
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := manifestOf(t, b).Hash()
	if ha != hb {
		t.Fatal("the order of the definitions moved the hash")
	}
}

func TestManifestHashMovesWithTheShapeButNotWithTheState(t *testing.T) {
	base := manifestOf(t, smallDefs(t))
	h0, _ := base.Hash()
	pending := base
	pending.State = pgschema.StateStatsPending
	pending.KGNo = 99
	if h, _ := pending.Hash(); h != h0 {
		t.Fatal("state and KG number are not part of the hash")
	}
	d := smallDefs(t)
	typeOf(&d, "ot.rack").Properties[0].ValueType = mustID[modelVT](t, "vt.integer")
	typeOf(&d, "ot.rack").Properties[0].Unit = ""
	if h, _ := manifestOf(t, d).Hash(); h == h0 {
		t.Fatal("a change of a value kind must move the hash")
	}
	if h, _ := manifestOf(t, smallDefs(t), pgschema.SearchEntry{TypeNo: 1, AttrNo: 10}).Hash(); h == h0 {
		t.Fatal("the search set must move the hash")
	}
}

func TestManifestRoundTripsAndKnowsItsTables(t *testing.T) {
	m := manifestOf(t, smallDefs(t))
	raw, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}
	back, err := pgschema.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	h1, _ := m.Hash()
	h2, _ := back.Hash()
	if h1 != h2 || back.KGNo != m.KGNo || back.State != m.State {
		t.Fatal("the manifest changed on the way")
	}
	for _, name := range []string{"object", "t_1", "s_1", "t_2", "stage_record", "calc_bind", "ingest_diff"} {
		if !back.Knows(name) {
			t.Errorf("%s must be known", name)
		}
	}
	for _, name := range []string{"s_2", "t_3", "stray", "staged"} {
		if back.Knows(name) {
			t.Errorf("%s must be a stranger", name)
		}
	}
	if _, err := pgschema.ParseManifest([]byte("{")); err == nil {
		t.Error("damaged text must be refused")
	}
}

func TestNamesAreBuiltFromNumbersAndQuoted(t *testing.T) {
	if pgschema.SchemaName(12) != "s_12" || pgschema.TypeTable(105) != "t_105" || pgschema.StateTable(105) != "s_105" {
		t.Fatal("names")
	}
	if pgschema.StateColumn(481) != "a481" || pgschema.StateColumnAt(481) != "a481_at" {
		t.Fatal("columns")
	}
	if got := pgschema.QuoteIdent(`a"b`); got != `"a""b"` {
		t.Fatalf("quote %s", got)
	}
	if pgschema.Qualified("s_1", "t_2") != `"s_1"."t_2"` {
		t.Fatal("qualified")
	}
	if !strings.HasPrefix(pgschema.StatsName(1, 2), "t_1_st_") {
		t.Fatal("stats name")
	}
}

func TestEveryValueKindHasAColumnTypeAndAnExpression(t *testing.T) {
	for _, kind := range kinds() {
		if _, err := pgschema.ColumnType(kind); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
		e, err := pgschema.Expr("t.doc", 7, kind)
		if err != nil || !strings.HasPrefix(e, "(") || !strings.Contains(e, "t.doc->'7'") {
			t.Errorf("%s: %q %v", kind, e, err)
		}
	}
	if _, err := pgschema.Expr("doc", 1, "bogus"); err == nil {
		t.Error("an unknown kind must be refused")
	}
	if _, err := pgschema.ColumnType("bogus"); err == nil {
		t.Error("an unknown kind must be refused")
	}
	if e, _ := pgschema.Expr("doc", 3, "time"); e != "(public.praxium_ts(doc->'3'->>'v'))" {
		t.Errorf("time %s", e)
	}
}
