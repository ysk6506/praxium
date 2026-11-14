package ingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
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

// serversFilteredBy is the mapping of the usual rig with a refinement on the servers whose filter is given.
func serversFilteredBy(filter string) string {
	mp := strings.Replace(mappingJSON, `{"feeds":[`, `{"refinements":[{"id":"r_servers","input":"ds_servers","ops":[`+filter+`]}],"feeds":[`, 1)
	return strings.Replace(mp, `"id":"f_server","dataset":"ds_servers"`, `"id":"f_server","dataset":"r_servers"`, 1)
}

func (r *rig) useMapping(mapping string) {
	r.t.Helper()
	m, err := ParseMapping(strings.NewReader(mapping))
	if err != nil {
		r.t.Fatal(err)
	}
	if r.plan, err = Compile(r.runner.Reg, r.plan.Source, m); err != nil {
		r.t.Fatal(err)
	}
}

const (
	keepAll    = `{"op":"filter","column":"power_state","not_in":["never"]}`
	typoFilter = `{"op":"filter","column":"power_state","in":["onn"]}`
)

// [REQ-360]
func TestAFilterTypoDoesNotCloseObjectsAtOnceButAfterTheWaitOfAVanishedRow(t *testing.T) {
	r := newRig(t, sourceJSON, serversFilteredBy(keepAll))
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	r.run()
	r.useMapping(serversFilteredBy(typoFilter))
	for i := 0; i < 2; i++ {
		r.clock.Advance(time.Hour)
		rep := r.run()
		if rep.RetiredObjects != 0 || rep.RetiredLinks != 0 || rep.WaitingObjects != 2 {
			t.Fatalf("run %d: retired %d objects %d links, waiting %d", i, rep.RetiredObjects, rep.RetiredLinks, rep.WaitingObjects)
		}
		if d := r.dataset(rep, "ds_servers"); d.Filtered != 2 || d.Used != 0 || !d.Balanced() {
			t.Fatalf("run %d: %+v", i, d)
		}
		if len(r.linksOf("SV1")) != 1 {
			t.Fatalf("run %d: the link of a waiting object was closed", i)
		}
	}
	r.clock.Advance(time.Hour)
	if rep := r.run(); rep.RetiredObjects != 0 {
		t.Fatalf("closed after three runs but before a day: %+v", rep)
	}
	r.clock.Advance(24 * time.Hour)
	rep := r.run()
	if rep.RetiredObjects != 2 || rep.RetiredLinks != 2 || rep.WaitingObjects != 0 {
		t.Fatalf("retired %d objects %d links, waiting %d", rep.RetiredObjects, rep.RetiredLinks, rep.WaitingObjects)
	}
	if _, ok := r.rows()["SV1"]; ok {
		t.Error("SV1 is still live")
	}
}

// [REQ-360]
func TestMendingTheFilterInTimeKeepsEverythingAndRestartsTheCount(t *testing.T) {
	r := newRig(t, sourceJSON, serversFilteredBy(keepAll))
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	first := r.run()
	r.useMapping(serversFilteredBy(typoFilter))
	for i := 0; i < 2; i++ {
		r.clock.Advance(time.Hour)
		r.run()
	}
	r.useMapping(serversFilteredBy(keepAll))
	r.clock.Advance(time.Hour)
	rep := r.run()
	if rep.RetiredObjects != 0 || rep.WaitingObjects != 0 || rep.GraphHash != first.GraphHash {
		t.Fatalf("%+v", rep)
	}
	r.useMapping(serversFilteredBy(typoFilter))
	for i := 0; i < 5; i++ {
		r.clock.Advance(time.Hour)
		if rep := r.run(); rep.RetiredObjects != 0 && i < 2 {
			t.Fatalf("the count did not start again: run %d retired %d", i, rep.RetiredObjects)
		}
	}
}

// [REQ-360]
func TestARowThatWasNeverAnObjectDoesNotBecomeOneByBeingFiltered(t *testing.T) {
	r := newRig(t, sourceJSON, serversFilteredBy(`{"op":"filter","column":"power_state","not_in":["off"]}`))
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	rep := r.run()
	if _, ok := r.rows()["SV2"]; ok || rep.Objects != 5 || rep.WaitingObjects != 0 {
		t.Fatalf("objects %d waiting %d", rep.Objects, rep.WaitingObjects)
	}
}

// [REQ-360]
func TestAFilteredRowIsNotRetiredWhereAVanishedRowIsNot(t *testing.T) {
	src := strings.Replace(sourceJSON, `{"id":"ds_servers","parser":"csv"`, `{"id":"ds_servers","deletion":"none","parser":"csv"`, 1)
	r := newRig(t, src, serversFilteredBy(keepAll))
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	r.run()
	r.useMapping(serversFilteredBy(typoFilter))
	for i := 0; i < 6; i++ {
		r.clock.Advance(48 * time.Hour)
		if rep := r.run(); rep.RetiredObjects != 0 {
			t.Fatalf("run %d retired %d objects of a dataset that never retires", i, rep.RetiredObjects)
		}
	}
}

// [REQ-360]
func TestAVanishedRowAndAFilteredRowAreWaitingObjectsInTheSameCount(t *testing.T) {
	r := loaded(t)
	r.run()
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\n"
	r.clock.Advance(time.Hour)
	if rep := r.run(); rep.WaitingObjects != 1 || rep.RetiredObjects != 0 {
		t.Fatalf("waiting %d retired %d", rep.WaitingObjects, rep.RetiredObjects)
	}
}

func withRowLimit(n string) string {
	return strings.Replace(sourceJSON, `"adapter":"file",`, `"adapter":"file","limits":{"max_rows_per_dataset":`+n+`},`, 1)
}

// [REQ-361]
func TestAFileWithMoreRowsThanTheLimitIsAnIncompleteCaptureAndUsesNothing(t *testing.T) {
	r := newRig(t, withRowLimit("2"), mappingJSON)
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	if rep := r.run(); rep.ProblemCounts[ProblemIncomplete] != 0 || rep.Objects != 6 {
		t.Fatalf("a file with exactly the limit: objects %d problems %v", rep.Objects, rep.ProblemCounts)
	}
	r.files["ds_racks"] = racksV1 + "RK3,rack-c,42\n"
	r.files["ds_vms"] = vmsV1 + `{"instance_uuid":"vm-3","name":"vm-three","cpu":1}` + "\n"
	r.clock.Advance(100 * time.Hour)
	rep := r.run()
	if rep.ProblemCounts[ProblemIncomplete] != 2 || rep.RetiredObjects != 0 {
		t.Fatalf("problems %v retired %d", rep.ProblemCounts, rep.RetiredObjects)
	}
	for _, p := range rep.Problems {
		if p.Kind == ProblemIncomplete && !strings.Contains(p.Reason, "more than 2 data rows") || p.Kind == ProblemIncomplete && !strings.Contains(p.Reason, "max_rows_per_dataset") {
			t.Errorf("the reason does not say the limit: %q", p.Reason)
		}
	}
	if _, ok := r.rows()["rack-c"]; ok {
		t.Error("rows of a file over the limit were used")
	}
	if d := r.dataset(rep, "ds_racks"); d.Read != 0 || !d.Collected || d.Applied {
		t.Errorf("%+v", d)
	}
}

// [REQ-361]
func TestUnreadableLinesCountForTheRowLimit(t *testing.T) {
	r := newRig(t, withRowLimit("2"), mappingJSON)
	r.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,42\nRK2,rack-b\nRK3,rack-c,42\n"
	if rep := r.run(); rep.ProblemCounts[ProblemIncomplete] != 1 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
}

// [REQ-361]
func TestTheRowLimitHasADefaultLikeTheSheetsAndCannotBeNegative(t *testing.T) {
	var l *Limits
	if rowLimit(l) != DefaultMaxRowsPerSheet || rowLimit(&Limits{}) != 200_000 || rowLimit(&Limits{MaxRowsPerDataset: 7}) != 7 {
		t.Error("the default or the setting is not what the sheets have")
	}
	if _, err := ParseSource(strings.NewReader(withRowLimit("-1"))); err == nil {
		t.Error("a negative limit was accepted")
	}
	tab := ParseLimited(Dataset{Parser: ParserCSV, NaturalKey: []string{"a"}, Columns: []Column{{Name: "a", Paths: []string{"a"}}}}, []byte("a\n1\n2\n3\n4\n"), 2)
	if !tab.OverRows || len(tab.Rows) > 3 {
		t.Errorf("the reading did not stop near the limit: %d rows, over %t", len(tab.Rows), tab.OverRows)
	}
}

// [REQ-365]
func TestALinkIsAsCertainAsTheLowestFeedItWasMadeFrom(t *testing.T) {
	for name, c := range map[string]struct {
		from, to string
		want     model.Grade
	}{
		"neither feed approximate": {`"id":"f_server",`, `"id":"f_rack",`, model.GradeFact},
		"the from feed":            {`"approximate":true,"id":"f_server",`, `"id":"f_rack",`, model.GradeAssumed},
		"the to feed":              {`"id":"f_server",`, `"approximate":true,"id":"f_rack",`, model.GradeAssumed},
	} {
		mp := strings.Replace(mappingJSON, `"id":"f_server",`, c.from, 1)
		mp = strings.Replace(mp, `"id":"f_rack",`, c.to, 1)
		r := newRig(t, sourceJSON, mp)
		r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
		r.run()
		links := r.linksOf("SV1")
		if len(links) != 1 || links[0].Grade != c.want {
			t.Errorf("%s: links %+v, want grade %s", name, links, c.want)
		}
	}
}
