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
	src := strings.Replace(sourceJSON, `]}`, `,
 {"id":"ds_power","parser":"csv","natural_key":["serial"],"columns":[{"name":"serial"},{"name":"maker"},{"name":"state"},{"name":"label"}]}]}`, 1)
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
	if rep.ProblemCounts[ProblemSourceConflict] != 1 || rep.ProblemCounts[ProblemOrphanValue] != 1 {
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
	for _, want := range []string{"rack-a", "rack-b", "SV1", "vm-one", "vm-two"} {
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
