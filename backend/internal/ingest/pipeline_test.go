package ingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

const (
	racksV1   = "asset_id,rack_name,total_u\nRK1,rack-a,42\nRK2,rack-b,45\n"
	serversV1 = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,sn200,Zorvane,RK2,off\n"
	vmsV1     = `{"instance_uuid":"vm-1","name":"vm-one","cpu":4}` + "\n" + `{"instance_uuid":"vm-2","name":"vm-two","cpu":2}` + "\n"
)

func loaded(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, sourceJSON, mappingJSON)
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	return r
}

// rows reads the live objects by name.
func (r *rig) rows() map[string]graph.ObjectRow {
	r.t.Helper()
	snap, err := r.store.Snapshot(context.Background(), graph.ReadPoint{KG: r.kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		r.t.Fatal(err)
	}
	all, err := snap.ScanObjects(context.Background(), graph.ObjectID{}, 1000)
	if err != nil {
		r.t.Fatal(err)
	}
	out := map[string]graph.ObjectRow{}
	for _, o := range all {
		out[o.Name] = o
	}
	return out
}

// [REQ-263]
func TestCompileRejectsWrongMappings(t *testing.T) {
	cases := map[string][2]string{
		"unknown object type": {`"object_type":"Rack"`, `"object_type":"Rackk"`},
		"unknown attribute":   {`"attribute":"total_u"`, `"attribute":"total_v"`},
		"unknown column":      {`"column":"rack_name"`, `"column":"rack_nam"`},
		"unknown scheme":      {`"scheme":"vm_instance_uuid"`, `"scheme":"vm_uuid_x"`},
		"unknown link type":   {`"link_type":"MOUNTED_IN"`, `"link_type":"MOUNTED_ON"`},
		"unknown field":       {`"creates_objects":true,"retires_objects":true,"basis":"declared"`, `"creates_object":true`},
	}
	reg := newRig(t, sourceJSON, mappingJSON).runner.Reg
	src, _ := ParseSource(strings.NewReader(sourceJSON))
	for name, c := range cases {
		text := strings.Replace(mappingJSON, c[0], c[1], 1)
		m, err := ParseMapping(strings.NewReader(text))
		if err == nil {
			_, err = Compile(reg, src, m)
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// [REQ-263]
func TestSourceDefinitionRejectsUnsupported(t *testing.T) {
	for name, text := range map[string]string{
		"adapter":    strings.Replace(sourceJSON, `"adapter":"file"`, `"adapter":"http"`, 1),
		"parser":     strings.Replace(sourceJSON, `"parser":"csv"`, `"parser":"xml"`, 1),
		"unknown":    strings.Replace(sourceJSON, `"name":"ledger"`, `"nme":"ledger"`, 1),
		"natual key": strings.Replace(sourceJSON, `"natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"rack_name"}`, `"columns":[{"name":"asset_id"},{"name":"rack_name"}`, 1),
	} {
		if _, err := ParseSource(strings.NewReader(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// [REQ-264]
func TestRowAccountingBalances(t *testing.T) {
	r := loaded(t)
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,,Zorvane,RK2,off\nSV3,sn3\nSV1,sn101,Zorvane,RK1,on\n"
	rep := r.run()
	for _, d := range rep.Datasets {
		if !d.Balanced() {
			t.Errorf("dataset %s does not balance: %+v", d.Dataset, d)
		}
	}
	var sv DatasetReport
	for _, d := range rep.Datasets {
		if d.Dataset == "ds_servers" {
			sv = d
		}
	}
	if sv.Received != 4 || sv.Unreadable != 1 || sv.Problem < 2 {
		t.Errorf("unexpected count %+v", sv)
	}
}

// [REQ-264]
func TestRawChangesCountAddedModifiedUnchanged(t *testing.T) {
	r := loaded(t)
	r.run()
	r.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,48\nRK2,rack-b,45\nRK3,rack-c,42\n"
	rep := r.run()
	for _, d := range rep.Datasets {
		if d.Dataset == "ds_racks" {
			if d.Raw.Added != 1 || d.Raw.Modified != 1 || d.Raw.Unchanged != 1 {
				t.Errorf("raw changes %+v", d.Raw)
			}
		}
	}
}

// [REQ-266]
func TestSecondRunChangesNothing(t *testing.T) {
	r := loaded(t)
	first := r.run()
	before := r.rows()
	again := r.run()
	if !first.NewVersion || again.NewVersion || again.Changed {
		t.Fatalf("first %+v again %+v", first.NewVersion, again)
	}
	if again.GraphHash != first.GraphHash {
		t.Error("graph hash moved")
	}
	for name, o := range r.rows() {
		if o.ContentHash != before[name].ContentHash || o.SeqFrom != before[name].SeqFrom {
			t.Errorf("%s changed", name)
		}
	}
}

// [REQ-266]
func TestOneChangedRowChangesOneObject(t *testing.T) {
	r := loaded(t)
	r.run()
	before := r.rows()
	r.files["ds_vms"] = strings.Replace(vmsV1, `"cpu":4`, `"cpu":8`, 1)
	rep := r.run()
	if !rep.NewVersion {
		t.Fatal("no new version")
	}
	changed := 0
	for name, o := range r.rows() {
		if o.ContentHash != before[name].ContentHash {
			changed++
			if name != "vm-one" {
				t.Errorf("%s changed", name)
			}
		}
	}
	if changed != 1 {
		t.Errorf("changed %d objects", changed)
	}
}

// [REQ-265]
func TestAttachFeedMergesTwoFiles(t *testing.T) {
	i := strings.LastIndex(sourceJSON, `]}`)
	src := sourceJSON[:i] + `,
 {"id":"ds_power","parser":"csv","natural_key":["serial"],"columns":[{"name":"serial"},{"name":"maker"},{"name":"state"},{"name":"label"}]}]}`
	feed := `,{"id":"f_power","dataset":"ds_power","object_type":"PhysicalServer","basis":"observed",
  "attributes":[{"attribute":"power_state","column":"state"},{"attribute":"name","column":"label","rank":5}],
  "identity":[{"components":[{"property":"manufacturer","column":"maker"},{"property":"serial_number","column":"serial"}],"priority":1}]}`
	mp := strings.Replace(mappingJSON, `],
"links"`, feed+`],
"links"`, 1)
	r := newRig(t, src, mp)
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	r.files["ds_power"] = "serial,maker,state,label\nsn100,Zorvane,off,other-name\nsn999,Zorvane,on,orphan\n"
	rep := r.run()
	if rep.Objects != 6 {
		t.Fatalf("two files made %d objects, want 6", rep.Objects)
	}
	if rep.ProblemCounts[ProblemSourceConflict] != 2 || rep.ProblemCounts[ProblemOrphanValue] != 1 {
		t.Errorf("problems %v", rep.ProblemCounts)
	}
	if _, ok := r.rows()["SV1"]; !ok {
		t.Error("the winning name was lost")
	}
}

// [REQ-268]
func TestBadRowsAreQuarantined(t *testing.T) {
	r := loaded(t)
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,,Zorvane,RK2,off\n"
	r.files["ds_vms"] = `{"instance_uuid":"vm-1","name":"vm-one","cpu":"many"}` + "\nnot json\n" + `{"instance_uuid":"vm-2","name":"vm-two","cpu":2}` + "\n"
	rep := r.run()
	if rep.ProblemCounts[ProblemMissingIdentity] != 1 || rep.ProblemCounts[ProblemInvalidValue] != 1 || rep.ProblemCounts[ProblemUnreadable] != 1 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
	rows := r.rows()
	if _, ok := rows["vm-one"]; ok {
		t.Error("a row with an invalid value was applied")
	}
	for _, want := range []string{"rack-a", "rack-b", "SV1", "vm-two"} {
		if _, ok := rows[want]; !ok {
			t.Errorf("%s was not applied", want)
		}
	}
	for _, p := range rep.Problems {
		if p.Reason == "" {
			t.Errorf("problem without a reason: %+v", p)
		}
	}
}

// [REQ-268]
func TestShapeChangeStopsTheDataset(t *testing.T) {
	r := loaded(t)
	r.run()
	r.files["ds_racks"] = "asset_id,rack_name\nRK1,rack-a\n"
	rep := r.run()
	if rep.ProblemCounts[ProblemShapeChanged] != 1 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
	if _, ok := r.rows()["rack-b"]; !ok {
		t.Error("a shape change retired objects")
	}
}

type incompleteFetcher struct {
	memFetcher
	short map[string]bool
}

func (f incompleteFetcher) Fetch(ctx context.Context, src string, ds Dataset) (Capture, error) {
	c, err := f.memFetcher.Fetch(ctx, src, ds)
	if f.short[ds.ID] {
		c.Complete, c.Reason = false, "limit"
	}
	return c, err
}

// [REQ-267]
func TestVanishedRowRetiresAfterThreeRunsAndADay(t *testing.T) {
	r := loaded(t)
	r.run()
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\n"
	step := func() Report { r.clock.Advance(time.Hour); return r.run() }
	for i := 0; i < 3; i++ {
		if rep := step(); rep.RetiredObjects != 0 {
			t.Fatalf("retired too early at %d", i)
		}
	}
	if _, ok := r.rows()["SV2"]; !ok {
		t.Fatal("SV2 gone before 24 hours")
	}
	r.clock.Advance(24 * time.Hour)
	rep := r.run()
	if rep.RetiredObjects != 1 || rep.RetiredLinks != 1 {
		t.Fatalf("retired %d objects %d links", rep.RetiredObjects, rep.RetiredLinks)
	}
	if _, ok := r.rows()["SV2"]; ok {
		t.Error("SV2 still live")
	}
}

// [REQ-267]
func TestIncompleteCaptureDoesNotCountAbsence(t *testing.T) {
	r := loaded(t)
	r.run()
	r.runner.Fetcher = incompleteFetcher{r.files, map[string]bool{"ds_servers": true}}
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\n"
	for i := 0; i < 5; i++ {
		r.clock.Advance(48 * time.Hour)
		rep := r.run()
		if rep.RetiredObjects != 0 || rep.ProblemCounts[ProblemIncomplete] != 1 {
			t.Fatalf("run %d: %+v", i, rep.ProblemCounts)
		}
	}
}

// [REQ-267]
func TestRowWithoutRetiringFeedStays(t *testing.T) {
	mp := strings.Replace(mappingJSON, `"id":"f_server","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":true,"retires_objects":true`, `"id":"f_server","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":true`, 1)
	r := newRig(t, sourceJSON, mp)
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	r.run()
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\n"
	for i := 0; i < 5; i++ {
		r.clock.Advance(48 * time.Hour)
		r.run()
	}
	if _, ok := r.rows()["SV2"]; !ok {
		t.Error("an object no retiring feed watched was retired")
	}
}

// [REQ-265]
func TestSameInputSameObjectIDs(t *testing.T) {
	a, b := loaded(t), loaded(t)
	a.run()
	b.run()
	for name, o := range a.rows() {
		if b.rows()[name].ID != o.ID {
			t.Errorf("%s id differs between two runs of the same input", name)
		}
	}
	_ = kernel.CodeNotFound
}

// [REQ-267]
func TestRetiredRowComesBackWithItsObject(t *testing.T) {
	r := loaded(t)
	r.run()
	id := r.rows()["SV2"].ID
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\n"
	for i := 0; i < 3; i++ {
		r.clock.Advance(10 * time.Hour)
		r.run()
	}
	r.clock.Advance(10 * time.Hour)
	r.run()
	if _, ok := r.rows()["SV2"]; ok {
		t.Fatal("not retired")
	}
	r.files["ds_servers"] = serversV1
	rep := r.run()
	back, ok := r.rows()["SV2"]
	if !ok || back.ID != id || rep.ProblemCounts[ProblemIdentityConflict] != 0 {
		t.Fatalf("back %v id %v problems %v", ok, back.ID == id, rep.ProblemCounts)
	}
}

// [REQ-263] [REQ-264]
func TestRefinementFiltersFillsAndReplacesBeforeTheFeed(t *testing.T) {
	mp := strings.Replace(mappingJSON, `{"feeds":[`, `{"refinements":[{"id":"r_racks","input":"ds_racks","ops":[
 {"op":"filter","column":"total_u","not_in":["45"]},
 {"op":"replace","column":"rack_name","function":"upper"},
 {"op":"replace","column":"rack_name","function":"map","map":{"RACK-A":"first-rack"}},
 {"op":"fill_empty","column":"total_u","value":"42"}]}],"feeds":[`, 1)
	mp = strings.Replace(mp, `"id":"f_rack","dataset":"ds_racks"`, `"id":"f_rack","dataset":"r_racks"`, 1)
	r := newRig(t, sourceJSON, mp)
	r.files["ds_racks"] = racksV1 + "RK3,rack-c,\n"
	r.files["ds_servers"], r.files["ds_vms"] = serversV1, vmsV1
	rep := r.run()
	rows := r.rows()
	if _, ok := rows["first-rack"]; !ok {
		t.Errorf("the map did not rename the rack: %v", rows)
	}
	if _, ok := rows["RACK-C"]; !ok {
		t.Error("the fill or the upper-casing was lost")
	}
	if _, ok := rows["rack-b"]; ok || rows["RACK-B"].ID != (graph.ObjectID{}) {
		t.Error("the filtered row was applied")
	}
	for _, d := range rep.Datasets {
		if !d.Balanced() {
			t.Errorf("%s does not balance: %+v", d.Dataset, d)
		}
	}
}

// [REQ-263]
func TestCompileRejectsWrongRefinements(t *testing.T) {
	reg := newRig(t, sourceJSON, mappingJSON).runner.Reg
	src, _ := ParseSource(strings.NewReader(sourceJSON))
	for name, op := range map[string]string{
		"unknown operation":  `{"op":"explode","column":"rack_name"}`,
		"unknown column":     `{"op":"filter","column":"nope","not_empty":true}`,
		"empty filter":       `{"op":"filter","column":"rack_name"}`,
		"fill without value": `{"op":"fill_empty","column":"rack_name"}`,
		"unknown function":   `{"op":"replace","column":"rack_name","function":"rot13"}`,
		"map without map":    `{"op":"replace","column":"rack_name","function":"map"}`,
	} {
		text := strings.Replace(mappingJSON, `{"feeds":[`, `{"refinements":[{"id":"r","input":"ds_racks","ops":[`+op+`]}],"feeds":[`, 1)
		m, err := ParseMapping(strings.NewReader(text))
		if err == nil {
			_, err = Compile(reg, src, m)
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// [REQ-263] [REQ-268]
func TestCellsOfEveryBaseKindAreConvertedOrReported(t *testing.T) {
	src := strings.Replace(sourceJSON, `{"name":"power_state"}]}`, `{"name":"power_state"},{"name":"watts"},{"name":"made"},{"name":"state"}]}`, 1)
	src = strings.Replace(src, `{"name":"vcpu","paths":["cpu"]}`, `{"name":"vcpu","paths":["cpu"]},{"name":"ha"}`, 1)
	mp := strings.Replace(mappingJSON, `{"attribute":"name","column":"asset_id"}]`, `{"attribute":"name","column":"asset_id"},{"attribute":"typical_power_w","column":"watts"},{"attribute":"production_date","column":"made"},{"attribute":"lifecycle_state","column":"state"}]`, 1)
	mp = strings.Replace(mp, `{"attribute":"vcpu_count","column":"vcpu"}`, `{"attribute":"vcpu_count","column":"vcpu"},{"attribute":"ha_protected","column":"ha"}`, 1)
	r := newRig(t, src, mp)
	r.files["ds_racks"] = racksV1
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state,watts,made,state\n" +
		"SV1,sn100,Zorvane,RK1,on,1500.5,2022-03-29T00:00:00Z,active\n" +
		"SV2,sn200,Zorvane,RK2,off,heavy,2022-03-29T00:00:00Z,active\n" +
		"SV3,sn300,Zorvane,RK2,off,100,last week,active\n" +
		"SV4,sn400,Zorvane,RK2,off,100,2022-03-29T00:00:00Z,no-such-state\n"
	r.files["ds_vms"] = `{"instance_uuid":"vm-1","name":"vm-one","cpu":4,"ha":true}` + "\n" + `{"instance_uuid":"vm-2","name":"vm-two","cpu":2,"ha":"maybe"}` + "\n"
	rep := r.run()
	if rep.ProblemCounts[ProblemInvalidValue] != 4 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
	rows := r.rows()
	if _, ok := rows["SV1"]; !ok {
		t.Error("the good row was lost")
	}
	if _, ok := rows["vm-one"]; !ok {
		t.Error("the boolean row was lost")
	}
}

// [REQ-265] [REQ-268]
func TestLinkProblemsAreReportedAndTheRestIsLinked(t *testing.T) {
	r := loaded(t)
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,sn200,Zorvane,RK9,off\n"
	rep := r.run()
	if rep.ProblemCounts[ProblemLinkTarget] != 1 || rep.Links != 1 {
		t.Fatalf("links %d problems %v", rep.Links, rep.ProblemCounts)
	}
}

// [REQ-264] [REQ-268]
func TestCSVOdditiesAreReadOrReported(t *testing.T) {
	r := loaded(t)
	r.files["ds_racks"] = "\xef\xbb\xbfasset_id,rack_name,total_u,extra\nRK1,\"rack, a\",42,x\nRK2,\"rack-b,45,y\n"
	rep := r.run()
	if _, ok := r.rows()["rack, a"]; !ok {
		t.Error("the byte order mark or the quoted comma broke the first row")
	}
	if rep.ProblemCounts[ProblemExtraColumn] != 1 || rep.ProblemCounts[ProblemUnreadable] == 0 {
		t.Errorf("problems %v", rep.ProblemCounts)
	}
}

// [REQ-265] [REQ-268]
func TestIdentifiersOfAFeedAreBoundAndAClashIsReported(t *testing.T) {
	src := strings.Replace(sourceJSON, `{"name":"vcpu","paths":["cpu"]}`, `{"name":"vcpu","paths":["cpu"]},{"name":"native","paths":["id"]}`, 1)
	mp := strings.Replace(mappingJSON, `"identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}]`, `"identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}],"identifiers":[{"scheme":"vm_native_id","column":"native"}]`, 1)
	r := newRig(t, src, mp)
	r.files["ds_racks"], r.files["ds_servers"] = racksV1, serversV1
	r.files["ds_vms"] = `{"instance_uuid":"vm-1","name":"vm-one","cpu":4,"id":"n-1"}` + "\n" + `{"instance_uuid":"vm-2","name":"vm-two","cpu":2,"id":"n-1"}` + "\n"
	rep := r.run()
	if rep.ProblemCounts[ProblemIdentityConflict] != 1 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
	if again := r.run(); again.Changed {
		t.Error("the second run changed the KG")
	}
	r.files["ds_vms"] = `{"instance_uuid":"vm-1","name":"vm-one","cpu":4,"id":"n-9"}` + "\n" + `{"instance_uuid":"vm-2","name":"vm-two","cpu":2,"id":"n-1"}` + "\n"
	if moved := r.run(); !moved.Changed || moved.ProblemCounts[ProblemIdentityConflict] != 0 {
		t.Errorf("a changed identifier was not applied: %+v", moved.ProblemCounts)
	}
}

// [REQ-265]
func TestSecondLinkThatBreaksCardinalityIsSetAside(t *testing.T) {
	src := strings.Replace(sourceJSON, `{"name":"power_state"}]}`, `{"name":"power_state"},{"name":"alt_rack"}]}`, 1)
	mp := strings.Replace(mappingJSON, `"links":[`, `"links":[{"id":"l_alt","link_type":"MOUNTED_IN","from":{"feed":"f_server","columns":["alt_rack"]},"to":{"feed":"f_rack","columns":["asset_id"]},"condition":"same_key"},`, 1)
	r := newRig(t, src, mp)
	r.files["ds_racks"], r.files["ds_vms"] = racksV1, vmsV1
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state,alt_rack\nSV1,sn100,Zorvane,RK1,on,RK2\nSV2,sn200,Zorvane,RK2,off,\n"
	rep := r.run()
	if rep.ProblemCounts[ProblemCardinality] != 1 || rep.Links != 2 {
		t.Fatalf("links %d problems %v", rep.Links, rep.ProblemCounts)
	}
}

// [REQ-263]
func TestLinkKeyTypeAndFilterTestsAreChecked(t *testing.T) {
	reg := newRig(t, sourceJSON, mappingJSON).runner.Reg
	src, _ := ParseSource(strings.NewReader(sourceJSON))
	for name, text := range map[string]string{
		"a key type of the model": strings.Replace(mappingJSON, `"condition":"same_key","basis":"declared"`, `"condition":"same_key","key_type":"serial_number"`, 1),
		"a filter with in":        strings.Replace(mappingJSON, `{"feeds":[`, `{"refinements":[{"id":"r","input":"ds_racks","ops":[{"op":"filter","column":"total_u","in":["42"]}]}],"feeds":[`, 1),
	} {
		m, err := ParseMapping(strings.NewReader(text))
		if err == nil {
			_, err = Compile(reg, src, m)
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, text := range map[string]string{
		"an unknown key type":     strings.Replace(mappingJSON, `"condition":"same_key","basis":"declared"`, `"condition":"same_key","key_type":"nope"`, 1),
		"a condition not built":   strings.Replace(mappingJSON, `"condition":"same_key"`, `"condition":"contains"`, 1),
		"an unknown link feed":    strings.Replace(mappingJSON, `"from":{"feed":"f_server"`, `"from":{"feed":"f_none"`, 1),
		"an unknown link column":  strings.Replace(mappingJSON, `"columns":["rack_asset_id"]`, `"columns":["nope"]`, 1),
		"an unknown identifier":   strings.Replace(mappingJSON, `"identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}]`, `"identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}],"identifiers":[{"scheme":"nope","column":"uuid"}]`, 1),
		"a feed of no dataset":    strings.Replace(mappingJSON, `"dataset":"ds_vms"`, `"dataset":"ds_none"`, 1),
		"an identity of no key":   strings.Replace(mappingJSON, `"components":[{"property":"asset_tag","column":"asset_id"}]`, `"components":[{"property":"name","column":"asset_id"}]`, 1),
		"an identity of no cell":  strings.Replace(mappingJSON, `"column":"uuid","priority":1`, `"column":"nope","priority":1`, 1),
		"a base kind not handled": strings.Replace(mappingJSON, `{"attribute":"vcpu_count","column":"vcpu"}`, `{"attribute":"labels","column":"vcpu"}`, 1),
	} {
		m, err := ParseMapping(strings.NewReader(text))
		if err == nil {
			_, err = Compile(reg, src, m)
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// [REQ-263]
func TestSourceDefinitionRefusesWhatThisBuildLacks(t *testing.T) {
	for name, text := range map[string]string{
		"a stream":           strings.Replace(sourceJSON, `"id":"ds_racks"`, `"id":"ds_racks","receive_mode":"stream"`, 1),
		"a derived dataset":  strings.Replace(sourceJSON, `"id":"ds_racks"`, `"id":"ds_racks","kind":"refined"`, 1),
		"another deletion":   strings.Replace(sourceJSON, `"id":"ds_racks"`, `"id":"ds_racks","deletion":"tombstone"`, 1),
		"two documents":      sourceJSON + sourceJSON,
		"a repeated id":      strings.Replace(sourceJSON, `"id":"ds_servers"`, `"id":"ds_racks"`, 1),
		"a key not a column": strings.Replace(sourceJSON, `"natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"rack_name"}`, `"natural_key":["nope"],"columns":[{"name":"asset_id"},{"name":"rack_name"}`, 1),
		"no columns":         strings.Replace(sourceJSON, `"columns":[{"name":"asset_id"},{"name":"rack_name"},{"name":"total_u"}]`, `"columns":[]`, 1),
	} {
		if _, err := ParseSource(strings.NewReader(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// [REQ-263] [REQ-265]
func TestHostsLinkToServersByNormalisedSerial(t *testing.T) {
	i := strings.LastIndex(sourceJSON, `]}`)
	src := sourceJSON[:i] + `,{"id":"ds_hosts","parser":"json_lines","natural_key":["host_id"],"columns":[
 {"name":"host_id"},{"name":"hostname","paths":["name"]},{"name":"serial","paths":["hardware.serial","hw.0.serial"]},{"name":"state","paths":["connection"],"optional":true}]}]}`
	host := `,{"id":"f_host","dataset":"ds_hosts","object_type":"HypervisorHost","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"hostname"},{"attribute":"hostname","column":"hostname"}],
  "identity":[{"scheme":"host_id","column":"host_id","priority":1}]}`
	mp := strings.Replace(mappingJSON, `],
"links"`, host+`],
"links"`, 1)
	mp = strings.Replace(mp, `"links":[`, `"links":[{"id":"l_hs","link_type":"RUNS_ON","from":{"feed":"f_host","columns":["serial"]},"to":{"feed":"f_server","columns":["serial"]},"condition":"same_key","key_type":"serial_number"},`, 1)
	r := newRig(t, src, mp)
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, serversV1, vmsV1
	r.files["ds_hosts"] = `{"host_id":"h1","name":"hv-a","hardware":{"serial":" SN100 "}}` + "\n" +
		`{"host_id":"h2","name":"hv-b","hw":[{"serial":"sn200"}]}` + "\n" +
		`{"host_id":"h3","name":"hv-c","hardware":{"serial":null}}` + "\n" +
		`{"host_id":7,"name":true,"hardware":{"serial":"sn100"}}` + "\n"
	rep := r.run()
	if rep.Links < 4 {
		t.Fatalf("links %d, problems %v", rep.Links, rep.ProblemCounts)
	}
	if _, ok := r.rows()["hv-c"]; !ok {
		t.Error("a host without a serial was lost")
	}
}
