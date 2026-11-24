package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

func manifestOf(t *testing.T, d model.Definitions, search ...pgschema.SearchEntry) pgschema.Manifest {
	t.Helper()
	plan, err := pgschema.Build(registry(t, d), pgschema.Options{KGNo: 5, WithStats: true, Search: pgschema.NewSearchSet(search...)})
	if err != nil {
		t.Fatal(err)
	}
	return plan.Manifest
}

func typeOf(d *model.Definitions, id string) *model.ObjectType {
	for i := range d.ObjectTypes {
		if d.ObjectTypes[i].ID.String() == id {
			return &d.ObjectTypes[i]
		}
	}
	panic("no type " + id)
}

func summary(steps []pgschema.Step) []string {
	var out []string
	for _, s := range steps {
		phase := "tx"
		if s.Phase == pgschema.Concurrent {
			phase = "concurrent"
		}
		out = append(out, phase+" "+s.What+" "+s.Name)
	}
	return out
}

func TestDiffFollowsAdditions(t *testing.T) {
	srv := pgschema.SearchEntry{TypeNo: 1, AttrNo: 10}
	tests := []struct {
		name   string
		change func(d *model.Definitions)
		search []pgschema.SearchEntry
		want   []string
		sql    map[string]string
	}{
		{name: "nothing changed", change: func(d *model.Definitions) {}, want: nil},
		{name: "a rename and a label change touch no table", change: func(d *model.Definitions) {
			typeOf(d, "ot.server").Name = "Machine"
			typeOf(d, "ot.server").Properties[0].Name = "serial_no"
			typeOf(d, "ot.server").Description = "a new label"
		}, want: nil},
		{name: "a new type has tables, indexes and statistics", change: func(d *model.Definitions) {
			d.ObjectTypes = append(d.ObjectTypes, objType(t, "ot.site", 3, "Site", []string{"tr.common"},
				prop(t, "pr.site.code", "code", 30, "vt.string", model.ChangeSlow),
				prop(t, "pr.site.load", "load", 31, "vt.number", model.ChangeDynamic)))
		}, want: []string{"tx table t_3", "tx table s_3", "tx index t_3_name", "tx index t_3_name_ci", "tx statistics t_3_st_1"}},
		{name: "a document attribute adds statistics and no column", change: func(d *model.Definitions) {
			p := typeOf(d, "ot.rack")
			p.Properties = append(p.Properties, prop(t, "pr.rack.floor", "floor", 21, "vt.integer", model.ChangeSlow))
		}, want: []string{"tx statistics t_2_st_21"}},
		{name: "a volatile attribute is a column that may be null", change: func(d *model.Definitions) {
			p := typeOf(d, "ot.server")
			p.Properties = append(p.Properties, prop(t, "pr.server.fan", "fan", 15, "vt.integer", model.ChangeDynamic))
		}, want: []string{"tx column s_1"}, sql: map[string]string{"s_1": `ALTER TABLE "s_5"."s_1" ADD COLUMN a15 bigint, ADD COLUMN a15_at timestamptz`}},
		{name: "a first volatile attribute makes the table", change: func(d *model.Definitions) {
			p := typeOf(d, "ot.rack")
			p.Properties = append(p.Properties, prop(t, "pr.rack.temp", "temp", 22, "vt.number", model.ChangeDynamic))
		}, want: []string{"tx table s_2"}},
		{name: "a search mark is a concurrent index", change: func(d *model.Definitions) {},
			search: []pgschema.SearchEntry{srv}, want: []string{"concurrent index t_1_a10"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := manifestOf(t, smallDefs(t))
			d := smallDefs(t)
			tt.change(&d)
			delta, err := pgschema.Diff(old, manifestOf(t, d, tt.search...))
			if err != nil {
				t.Fatal(err)
			}
			got := summary(delta.Steps)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("steps %v, want %v", got, tt.want)
			}
			for name, sql := range tt.sql {
				found := false
				for _, s := range delta.Steps {
					if s.Name == name && s.SQL == sql {
						found = true
					}
				}
				if !found {
					t.Errorf("no step %s with %s", name, sql)
				}
			}
			if delta.Result.State != pgschema.StateOK {
				t.Errorf("state %q", delta.Result.State)
			}
		})
	}
}

func TestDiffDropsAnIndexWhenTheMarkGoes(t *testing.T) {
	srv := pgschema.SearchEntry{TypeNo: 1, AttrNo: 10}
	old := manifestOf(t, smallDefs(t), srv)
	delta, err := pgschema.Diff(old, manifestOf(t, smallDefs(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Steps) != 1 || delta.Steps[0].SQL != `DROP INDEX CONCURRENTLY IF EXISTS "s_5"."t_1_a10"` || delta.Steps[0].Phase != pgschema.Concurrent {
		t.Fatalf("%+v", delta.Steps)
	}
}

func TestDiffKeepsTheGroupsOfTheStatisticsThatExist(t *testing.T) {
	old := manifestOf(t, smallDefs(t))
	d := smallDefs(t)
	p := typeOf(&d, "ot.rack")
	p.Properties = append(p.Properties, prop(t, "pr.rack.floor", "floor", 21, "vt.integer", model.ChangeSlow))
	delta, err := pgschema.Diff(old, manifestOf(t, d))
	if err != nil {
		t.Fatal(err)
	}
	for _, ts := range delta.Result.Types {
		if ts.TypeNo == 2 && (len(ts.Stats) != 2 || len(ts.Stats[0]) != 2 || len(ts.Stats[1]) != 1 || ts.Stats[1][0] != 21) {
			t.Fatalf("groups %v", ts.Stats)
		}
	}
	// the hash does not look at the groups: a schema grown by steps equals one made at once.
	h1, _ := delta.Result.Hash()
	h2, _ := manifestOf(t, d).Hash()
	if h1 != h2 {
		t.Fatal("the grouping of the statistics moved the hash")
	}
}

func TestDiffRefusesWhatIsNotAnAddition(t *testing.T) {
	tests := map[string]func(d *model.Definitions){
		"a type is gone":       func(d *model.Definitions) { d.ObjectTypes = d.ObjectTypes[:1] },
		"a document attr gone": func(d *model.Definitions) { p := typeOf(d, "ot.server"); p.Properties = p.Properties[:2] },
		"a value kind changed": func(d *model.Definitions) {
			typeOf(d, "ot.server").Properties[1].ValueType = mustID[model.ValueTypeKind](t, "vt.string")
		},
		"a volatile attr moved": func(d *model.Definitions) { typeOf(d, "ot.server").Properties[1].ChangeClass = model.ChangeDynamic },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			d := smallDefs(t)
			change(&d)
			_, err := pgschema.Diff(manifestOf(t, smallDefs(t)), manifestOf(t, d))
			if err == nil || kernel.CodeOf(err) != pgschema.CodeNotAdditive {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func TestDiffRefusesAChangedCommonStateGeneratorAndKG(t *testing.T) {
	old := manifestOf(t, smallDefs(t))
	a := old
	a.CommonState = a.CommonState[:1]
	b := old
	b.GeneratorVersion++
	c := old
	c.KGNo++
	for name, next := range map[string]pgschema.Manifest{"common": a, "generator": b, "kg": c} {
		if _, err := pgschema.Diff(old, next); err == nil || kernel.CodeOf(err) != pgschema.CodeNotAdditive {
			t.Errorf("%s: %v", name, err)
		}
	}
}
