package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

func stepSQL(p pgschema.Plan, name string) string {
	for _, s := range p.Steps {
		if s.Name == name {
			return s.SQL
		}
	}
	for _, s := range p.Deferred {
		if s.Name == name {
			return s.SQL
		}
	}
	return ""
}

func TestBuildMakesTheTablesOfASmallModel(t *testing.T) {
	reg := registry(t, smallDefs(t))
	plan, err := pgschema.Build(reg, pgschema.Options{KGNo: 7, WithStats: true,
		Search: pgschema.NewSearchSet(pgschema.SearchEntry{TypeNo: 1, AttrNo: 10}, pgschema.SearchEntry{TypeNo: 1, AttrNo: 11})})
	if err != nil {
		t.Fatal(err)
	}
	wantTables := append(pgschema.FixedTables(), "t_1", "s_1", "t_2")
	got := plan.Tables()
	if strings.Join(got, ",") != strings.Join(wantTables, ",") {
		t.Fatalf("tables %v, want %v", got, wantTables)
	}
	if plan.Steps[0].SQL != `CREATE SCHEMA "s_7"` {
		t.Fatalf("first step %q", plan.Steps[0].SQL)
	}
	want := map[string]string{
		"t_1": `CREATE TABLE "s_7"."t_1" (
    obj_no bigint NOT NULL PRIMARY KEY,
    ref text COLLATE "C" NOT NULL,
    name text NOT NULL,
    min_grade smallint NOT NULL,
    doc jsonb NOT NULL,
    source_refs jsonb NOT NULL DEFAULT '[]',
    content_hash bytea NOT NULL,
    row_hash bytea NOT NULL,
    seq_from bigint NOT NULL
) WITH (fillfactor = 85, autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_insert_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.01)`,
		"s_1": `CREATE TABLE "s_7"."s_1" (
    obj_no bigint NOT NULL PRIMARY KEY,
    a13 double precision,
    a13_at timestamptz,
    prov jsonb NOT NULL DEFAULT '{}'
) WITH (fillfactor = 70, autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000, autovacuum_analyze_scale_factor = 0.02)`,
		"object_state_a4": `CREATE INDEX "object_state_a4" ON "s_7"."object_state" (a4)`,
		"object_state_a7": `CREATE INDEX "object_state_a7" ON "s_7"."object_state" (a7)`,
		"link_src":        `CREATE INDEX "link_src" ON "s_7"."link" (src, type_no, dst)`,
		"link_dst":        `CREATE INDEX "link_dst" ON "s_7"."link" (dst, type_no, src)`,
		"t_1_a10":         `CREATE INDEX "t_1_a10" ON "s_7"."t_1" ((doc->'10'->>'v'))`,
		"t_1_a11":         `CREATE INDEX "t_1_a11" ON "s_7"."t_1" (((doc->'11'->>'v')::bigint))`,
		"t_2_st_1":        `CREATE STATISTICS "s_7"."t_2_st_1" (mcv) ON (doc->'1'->>'v'), ((doc->'20'->>'v')::numeric) FROM "s_7"."t_2"`,
	}
	for name, sql := range want {
		if g := stepSQL(plan, name); g != sql {
			t.Errorf("%s:\n got %s\nwant %s", name, g, sql)
		}
	}
	if g := stepSQL(plan, "t_1_st_1"); !strings.Contains(g, "(mcv) ON") || !strings.Contains(g, "public.praxium_ts(doc->'12'->>'v')") {
		t.Errorf("the statistics of the server: %s", g)
	}
	if stepSQL(plan, "t_1_a14") != "" || strings.Contains(stepSQL(plan, "t_1_st_1"), "'14'") == false {
		t.Errorf("a list has statistics but no index")
	}
	// the volatile attributes that every type has are in object_state, not in s_1.
	if !strings.Contains(stepSQL(plan, "object_state"), "a4 text,") || strings.Contains(stepSQL(plan, "s_1"), "a4 ") {
		t.Errorf("the common state is in the wrong table")
	}
	if len(plan.Deferred) != 0 || plan.Manifest.State != pgschema.StateOK {
		t.Errorf("with statistics nothing is deferred: %v %v", plan.Deferred, plan.Manifest.State)
	}
}

func TestBuildWithoutStatisticsDefersThemAndKeepsTheHash(t *testing.T) {
	reg := registry(t, smallDefs(t))
	with, err := pgschema.Build(reg, pgschema.Options{KGNo: 1, WithStats: true})
	if err != nil {
		t.Fatal(err)
	}
	without, err := pgschema.Build(reg, pgschema.Options{KGNo: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(without.Deferred) == 0 || without.Manifest.State != pgschema.StateStatsPending {
		t.Fatalf("statistics must be deferred and pending: %d %q", len(without.Deferred), without.Manifest.State)
	}
	if len(with.Steps) != len(without.Steps)+len(without.Deferred) {
		t.Fatalf("the statements must be the same set: %d vs %d+%d", len(with.Steps), len(without.Steps), len(without.Deferred))
	}
	h1, _ := with.Manifest.Hash()
	h2, _ := without.Manifest.Hash()
	if h1 != h2 {
		t.Fatal("the state of the statistics and the KG number must not move the hash")
	}
}

func TestBuildRefusesWhatItCannotServe(t *testing.T) {
	reg := registry(t, smallDefs(t))
	cases := map[string]pgschema.Options{
		"no KG number":          {},
		"unknown type":          {KGNo: 1, Search: pgschema.SearchSet{{TypeNo: 9, AttrNo: 10}}},
		"unknown attribute":     {KGNo: 1, Search: pgschema.SearchSet{{TypeNo: 1, AttrNo: 99}}},
		"volatile attribute":    {KGNo: 1, Search: pgschema.SearchSet{{TypeNo: 1, AttrNo: 13}}},
		"list has no index":     {KGNo: 1, Search: pgschema.SearchSet{{TypeNo: 1, AttrNo: 14}}},
		"an attribute of other": {KGNo: 1, Search: pgschema.SearchSet{{TypeNo: 2, AttrNo: 10}}},
	}
	for name, opts := range cases {
		if _, err := pgschema.Build(reg, opts); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := pgschema.Build(nil, pgschema.Options{KGNo: 1}); err == nil {
		t.Error("a nil registry must be refused")
	}
}

func TestTheSameInputGivesTheSameText(t *testing.T) {
	reg := registry(t, smallDefs(t))
	a, _ := pgschema.Build(reg, pgschema.Options{KGNo: 3, WithStats: true})
	b, _ := pgschema.Build(reg, pgschema.Options{KGNo: 3, WithStats: true})
	if len(a.Steps) != len(b.Steps) {
		t.Fatal("different lengths")
	}
	for i := range a.Steps {
		if a.Steps[i] != b.Steps[i] {
			t.Fatalf("step %d differs", i)
		}
	}
}

func TestTheDefaultSearchSetIsTheIdentityKeyProperties(t *testing.T) {
	reg := registry(t, smallDefs(t))
	set, err := pgschema.DefaultSearchSet(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 1 || set[0] != (pgschema.SearchEntry{TypeNo: 1, AttrNo: 10}) {
		t.Fatalf("set %v", set)
	}
}

func TestSearchSetIsOrderFree(t *testing.T) {
	a := pgschema.NewSearchSet(pgschema.SearchEntry{TypeNo: 2, AttrNo: 1}, pgschema.SearchEntry{TypeNo: 1, AttrNo: 5}, pgschema.SearchEntry{TypeNo: 1, AttrNo: 5})
	b := pgschema.NewSearchSet(pgschema.SearchEntry{TypeNo: 1, AttrNo: 5}, pgschema.SearchEntry{TypeNo: 2, AttrNo: 1})
	ha, _ := a.Hash()
	hb, _ := b.Hash()
	if len(a) != 2 || ha != hb {
		t.Fatalf("%v %v", a, b)
	}
}

func TestModelWithoutCommonStateStillHasAStateTable(t *testing.T) {
	d := smallDefs(t)
	d.Traits = nil
	d.ObjectTypes = d.ObjectTypes[:1]
	d.ObjectTypes[0].Traits = nil
	d.ObjectTypes[0].IdentityKeys = nil
	reg := registry(t, d)
	plan, err := pgschema.Build(reg, pgschema.Options{KGNo: 1})
	if err != nil {
		t.Fatal(err)
	}
	// with one type, every volatile attribute of it is common.
	if !strings.Contains(stepSQL(plan, "object_state"), "a13 double precision") {
		t.Fatalf("%s", stepSQL(plan, "object_state"))
	}
	_ = model.OrgNumberBase
}
