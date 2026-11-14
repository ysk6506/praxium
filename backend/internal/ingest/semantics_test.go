package ingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
)

// attrNo is the number of an attribute of a type, by name.
func (r *rig) attrNo(typ, name string) int {
	r.t.Helper()
	ot, ok := r.runner.Reg.ObjectTypeByName(typ)
	if !ok {
		r.t.Fatalf("no type %s", typ)
	}
	for _, p := range r.runner.Reg.EffectiveProperties(ot.ID) {
		if p.Name == name {
			return p.AttrNo
		}
	}
	r.t.Fatalf("no attribute %s of %s", name, typ)
	return 0
}

// linksOf is the live links of an object by name.
func (r *rig) linksOf(name string) []graph.LinkRow {
	r.t.Helper()
	snap, err := r.store.Snapshot(context.Background(), graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		r.t.Fatal(err)
	}
	links, err := snap.LinksOf(context.Background(), r.rows()[name].ID, graph.Both)
	if err != nil {
		r.t.Fatal(err)
	}
	return links
}

func (r *rig) dataset(rep Report, id string) DatasetReport {
	r.t.Helper()
	for _, d := range rep.Datasets {
		if d.Dataset == id {
			return d
		}
	}
	r.t.Fatalf("no dataset %s", id)
	return DatasetReport{}
}

// [REQ-358]
func TestAWrongCellLeavesOutTheValueAndKeepsTheObjectAndItsLinks(t *testing.T) {
	r := loaded(t)
	r.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,tall\nRK2,rack-b,45\n"
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,sleeping\nSV2,sn200,Zorvane,RK2,off\n"
	rep := r.run()
	if rep.Objects != 6 || rep.Links != 2 {
		t.Fatalf("objects %d links %d, want 6 and 2: %v", rep.Objects, rep.Links, rep.ProblemCounts)
	}
	if rep.ProblemCounts[ProblemInvalidValue] != 2 || rep.ProblemCounts[ProblemLinkTarget] != 0 || rep.ProblemCounts[ProblemMissingIdentity] != 0 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
	rack := r.rows()["rack-a"]
	if _, ok := rack.Attrs[r.attrNo("Rack", "total_u")]; ok {
		t.Error("the wrong value was kept")
	}
	if _, ok := r.rows()["rack-b"].Attrs[r.attrNo("Rack", "total_u")]; !ok {
		t.Error("the good value of another row was lost")
	}
	if len(r.linksOf("rack-a")) != 1 {
		t.Error("the link to the rack with a wrong cell is not alive")
	}
	for _, p := range rep.Problems {
		if p.Kind == ProblemInvalidValue && (p.Line == 0 || !strings.Contains(p.Reason, "without this value")) {
			t.Errorf("the problem does not say the row and what was done: %+v", p)
		}
	}
	for _, id := range []string{"ds_racks", "ds_servers"} {
		d := r.dataset(rep, id)
		if !d.Balanced() || d.Problem != 0 || d.UsedWithProblem != 1 || d.CellsLeftOut != 1 || d.Used != 2 {
			t.Errorf("%s: %+v", id, d)
		}
	}
}

// [REQ-358]
func TestAValueThatTurnsWrongIsAsIfTheCellWereEmpty(t *testing.T) {
	r := loaded(t)
	r.run()
	no := r.attrNo("Rack", "total_u")
	if _, ok := r.rows()["rack-a"].Attrs[no]; !ok {
		t.Fatal("the first run gave no value")
	}
	r.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,tall\nRK2,rack-b,45\n"
	rep := r.run()
	if rep.ProblemCounts[ProblemInvalidValue] != 1 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
	if _, ok := r.rows()["rack-a"].Attrs[no]; ok {
		t.Error("the old value was held although the source no longer says it")
	}
	r.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,\nRK2,rack-b,45\n"
	r.run()
	if _, ok := r.rows()["rack-a"].Attrs[no]; ok {
		t.Error("an empty cell holds the old value, so a wrong cell and an empty one are not alike")
	}
}

// [REQ-358]
func TestAWrongCellOfAnIdentityKeyStillMakesNoObject(t *testing.T) {
	r := loaded(t)
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,sn200,,RK2,off\n"
	rep := r.run()
	if rep.ProblemCounts[ProblemMissingIdentity] != 1 || rep.ProblemCounts[ProblemInvalidValue] != 0 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
	if _, ok := r.rows()["SV2"]; ok {
		t.Error("an object was made from a row without a part of its key")
	}
	for _, p := range rep.Problems {
		if p.Kind == ProblemMissingIdentity && !strings.Contains(p.Reason, "part of what names the object") {
			t.Errorf("the problem does not say the column is part of the identity key: %q", p.Reason)
		}
	}
	if d := r.dataset(rep, "ds_servers"); !d.Balanced() || d.Problem != 1 || d.UsedWithProblem != 0 {
		t.Errorf("%+v", d)
	}
}

// [REQ-358]
func TestAnEmptyRequiredAttributeAndAWrongOneGiveTheSameObject(t *testing.T) {
	a, b := loaded(t), loaded(t)
	a.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,\nRK2,rack-b,45\n"
	b.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,tall\nRK2,rack-b,45\n"
	ra, rb := a.run(), b.run()
	if ra.ProblemCounts[ProblemInvalidValue] != 0 || rb.ProblemCounts[ProblemInvalidValue] != 1 {
		t.Fatalf("problems %v and %v", ra.ProblemCounts, rb.ProblemCounts)
	}
	if ra.GraphHash != rb.GraphHash {
		t.Error("an empty cell and a wrong one do not give the same graph")
	}
}

// [REQ-358]
func TestAWrongCellDoesNotRetireTheObjectOfARowThatWasGood(t *testing.T) {
	r := loaded(t)
	r.run()
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,sn200,Zorvane,RK2,sleeping\n"
	for i := 0; i < 5; i++ {
		r.clock.Advance(48 * time.Hour)
		if rep := r.run(); rep.RetiredObjects != 0 || rep.RetiredLinks != 0 {
			t.Fatalf("run %d retired %d objects and %d links", i, rep.RetiredObjects, rep.RetiredLinks)
		}
	}
}

// powerSheetRig is a second file for the servers that attaches the power state, with a key name of its own choice.
func powerSheetRig(t *testing.T, keyName string) *rig {
	t.Helper()
	i := strings.LastIndex(sourceJSON, `]}`)
	src := sourceJSON[:i] + `,
 {"id":"ds_power","parser":"csv","natural_key":["serial"],"columns":[{"name":"serial"},{"name":"maker"},{"name":"state"}]}]}`
	feed := `,{"id":"f_power","dataset":"ds_power","object_type":"PhysicalServer","basis":"observed",
  "attributes":[{"attribute":"power_state","column":"state"}],
  "identity":[{` + keyName + `"components":[{"property":"manufacturer","column":"maker"},{"property":"serial_number","column":"serial"}],"priority":1}]}`
	mp := strings.Replace(mappingJSON, `],
"links"`, feed+`],
"links"`, 1)
	r := newRig(t, src, mp)
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	r.files["ds_power"] = "serial,maker,state\nsn100,Zorvane,on\nsn200,Zorvane,off\n"
	return r
}

// [REQ-359]
func TestTheKeyNameTakesNoPartInWhichObjectARowIs(t *testing.T) {
	var hashes []string
	for _, name := range []string{``, `"key_name":"serial_key",`, `"key_name":"another",`} {
		r := powerSheetRig(t, name)
		rep := r.run()
		if rep.ProblemCounts[ProblemOrphanValue] != 0 || len(rep.ProblemCounts) != 0 || rep.Objects != 6 {
			t.Fatalf("key_name %q: objects %d problems %v", name, rep.Objects, rep.ProblemCounts)
		}
		hashes = append(hashes, rep.GraphHash)
	}
	if hashes[0] != hashes[1] || hashes[1] != hashes[2] {
		t.Errorf("the key name moved the graph: %v", hashes)
	}
}

// [REQ-359]
func TestAKeyNameThatOneFeedGivesAndAnotherLacksStillFindsTheObject(t *testing.T) {
	r := powerSheetRig(t, ``)
	r.plan.feeds[1].keys[0].name = "renamed_after_the_fact"
	if rep := r.run(); rep.ProblemCounts[ProblemOrphanValue] != 0 || rep.Objects != 6 {
		t.Fatalf("objects %d problems %v", rep.Objects, rep.ProblemCounts)
	}
}

// [REQ-362]
func TestInAndNotInSetAsideTheSpacesAtTheEnds(t *testing.T) {
	for name, c := range map[string]struct {
		op   Op
		cell string
		keep bool
	}{
		"in, space in the cell":      {Op{Op: OpFilter, Column: "c", In: []string{"on"}}, "on ", true},
		"in, space in the list":      {Op{Op: OpFilter, Column: "c", In: []string{" on"}}, "on", true},
		"in, a tab and a new line":   {Op{Op: OpFilter, Column: "c", In: []string{"on"}}, "\ton\n", true},
		"in, another value":          {Op{Op: OpFilter, Column: "c", In: []string{"on"}}, " off ", false},
		"in, spaces inside count":    {Op{Op: OpFilter, Column: "c", In: []string{"rack a"}}, "rack  a", false},
		"in, case counts":            {Op{Op: OpFilter, Column: "c", In: []string{"on"}}, "ON", false},
		"not_in, space in the cell":  {Op{Op: OpFilter, Column: "c", NotIn: []string{"45"}}, " 45 ", false},
		"not_in, space in the list":  {Op{Op: OpFilter, Column: "c", NotIn: []string{"45 "}}, "45", false},
		"not_in, another value":      {Op{Op: OpFilter, Column: "c", NotIn: []string{"45"}}, "46", true},
		"not_in, an empty cell kept": {Op{Op: OpFilter, Column: "c", NotIn: []string{"45"}}, "  ", true},
	} {
		if got := c.op.keep(c.cell); got != c.keep {
			t.Errorf("%s: keep(%q) = %t, want %t", name, c.cell, got, c.keep)
		}
	}
}
