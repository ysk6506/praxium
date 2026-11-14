package ingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const sourceJSON = `{"id":"src_ledger","name":"ledger","adapter":"file","datasets":[
 {"id":"ds_racks","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"rack_name"},{"name":"total_u"}]},
 {"id":"ds_servers","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial","paths":["serial_number","serial_no"]},{"name":"manufacturer"},{"name":"rack_asset_id"},{"name":"power_state"}]},
 {"id":"ds_vms","parser":"json_lines","natural_key":["uuid"],"columns":[{"name":"uuid","paths":["instance_uuid"]},{"name":"name"},{"name":"vcpu","paths":["cpu"]}]}
]}`

const mappingJSON = `{"feeds":[
 {"id":"f_rack","dataset":"ds_racks","object_type":"Rack","creates_objects":true,"retires_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack_name"},{"attribute":"total_u","column":"total_u"},{"attribute":"asset_tag","column":"asset_id"}],
  "identity":[{"key_name":"asset_tag","components":[{"property":"asset_tag","column":"asset_id"}],"priority":1}]},
 {"id":"f_server","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":true,"retires_objects":true,"basis":"observed",
  "attributes":[{"attribute":"serial_number","column":"serial"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"power_state","column":"power_state"},{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_vm","dataset":"ds_vms","object_type":"VirtualMachine","creates_objects":true,"retires_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"name"},{"attribute":"vcpu_count","column":"vcpu"}],
  "identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}]}
],
"links":[{"id":"l_mounted","link_type":"MOUNTED_IN","from":{"feed":"f_server","columns":["rack_asset_id"]},"to":{"feed":"f_rack","columns":["asset_id"]},"condition":"same_key","basis":"declared"}]}`

type memFetcher map[string]string

func (m memFetcher) Fetch(_ context.Context, _ string, ds Dataset) (Capture, error) {
	data, ok := m[ds.ID]
	if !ok {
		return Capture{}, kernel.Errorf(kernel.CodeNotFound, "not given")
	}
	h := kernel.HashBytes([]byte(data))
	return Capture{Dataset: ds.ID, Data: []byte(data), Hash: h.String(), Complete: true}, nil
}

type rig struct {
	t      *testing.T
	store  *graph.ScratchStore
	kg     graph.KG
	runner Runner
	plan   *Plan
	clock  *kernel.ManualClock
	files  memFetcher
}

func newRig(t *testing.T, src, mapping string) *rig {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSource(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMapping(strings.NewReader(mapping))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Compile(reg, s, m)
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := kernel.NewSequentialIDs()
	store := graph.NewScratchStore()
	kgid, _ := kernel.NewID[graph.KGKind](ids, "kg")
	kg, err := store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "t", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	files := memFetcher{}
	return &rig{t: t, store: store, kg: kg, plan: plan, clock: clock, files: files,
		runner: Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, Fetcher: files, States: NewMemStateStore()}}
}

func (r *rig) run() Report {
	r.t.Helper()
	rep, err := r.runner.Run(context.Background(), r.plan, r.kg.ID)
	if err != nil {
		r.t.Fatalf("run: %v", err)
	}
	return rep
}

func TestFirstRun(t *testing.T) {
	r := newRig(t, sourceJSON, mappingJSON)
	r.files["ds_racks"] = "asset_id,rack_name,total_u\nRK1,rack-a,42\nRK2,rack-b,45\n"
	r.files["ds_servers"] = "asset_id,serial_no,manufacturer,rack_asset_id,power_state\nSV1,sn100,Zorvane,RK1,on\nSV2,sn200,Zorvane,RK2,off\n"
	r.files["ds_vms"] = `{"instance_uuid":"vm-1","name":"vm-one","cpu":4}` + "\n" + `{"instance_uuid":"vm-2","name":"vm-two","cpu":null}` + "\n"
	rep := r.run()
	t.Logf("%+v", rep)
	if rep.Objects != 6 || rep.Links != 2 {
		t.Fatalf("objects %d links %d", rep.Objects, rep.Links)
	}
	rep2 := r.run()
	t.Logf("%+v", rep2)
	if rep2.Changed || rep2.NewVersion {
		t.Fatal("second run changed")
	}
}
