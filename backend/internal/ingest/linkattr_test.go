package ingest

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The world of the tests of link attributes (ADR-0066): the usual racks, servers and virtual machines, with the
// columns a link attribute is read from, a table of mounts that makes no object, hosts and cooling units.
var laSource = strings.NewReplacer(
	`{"name":"power_state"}]}`, `{"name":"power_state"},{"name":"position_u","optional":true},{"name":"face","optional":true},{"name":"cdu_id","optional":true},{"name":"cap_kw","optional":true},{"name":"ride_s","optional":true}]}`,
	`{"name":"vcpu","paths":["cpu"]}]}`, `{"name":"vcpu","paths":["cpu"]},{"name":"host_id","optional":true},{"name":"ha","optional":true}]},
 {"id":"ds_hosts","parser":"csv","natural_key":["host_id"],"columns":[{"name":"host_id"},{"name":"host_name"}]},
 {"id":"ds_cdus","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial_no"},{"name":"manufacturer"}]},
 {"id":"ds_mounts","parser":"csv","natural_key":["mount_id"],"columns":[{"name":"mount_id"},{"name":"mfr"},{"name":"serial_no"},{"name":"rack_asset_id"},{"name":"position_u"},{"name":"face","optional":true}]}`,
).Replace(sourceJSON)

const laFeeds = `,
 {"id":"f_host","dataset":"ds_hosts","object_type":"HypervisorHost","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"host_name"}],"identity":[{"scheme":"host_id","column":"host_id","priority":1}]},
 {"id":"f_vm_host","dataset":"ds_vms","object_type":"VirtualMachine","basis":"observed","attributes":[],
  "identity":[{"scheme":"vm_instance_uuid","column":"uuid","priority":1}]},
 {"id":"f_cdu","dataset":"ds_cdus","object_type":"Cdu","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"},{"attribute":"manufacturer","column":"manufacturer"},{"attribute":"serial_number","column":"serial_no"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial_no"}],"priority":1}]},
 {"id":"f_mount","dataset":"ds_mounts","object_type":"PhysicalServer","basis":"observed","attributes":[],
  "identity":[{"components":[{"property":"manufacturer","column":"mfr"},{"property":"serial_number","column":"serial_no"}],"priority":1}]}
`

// laMapping is the usual mapping with the feeds above and the link definitions given.
func laMapping(links string) string {
	return mappingJSON[:strings.Index(mappingJSON, "\n],\n\"links\":")] + laFeeds + `],"links":[` + links + `]}`
}

const (
	laRackLink = `{"id":"l_mounted","link_type":"MOUNTED_IN","from":{"feed":"f_server","columns":["rack_asset_id"]},"to":{"feed":"f_rack","columns":["asset_id"]},"condition":"same_key","basis":"declared"`
	laMountDef = `{"id":"l_mount_table","link_type":"MOUNTED_IN","from":{"feed":"f_mount","columns":["rack_asset_id"]},"to":{"feed":"f_rack","columns":["asset_id"]},"condition":"same_key","basis":"declared"`
	laCoolLink = `{"id":"l_cooled","link_type":"COOLED_BY","from":{"feed":"f_server","columns":["cdu_id"]},"to":{"feed":"f_cdu","columns":["asset_id"]},"condition":"same_key","basis":"observed"`
	laHostLink = `{"id":"l_runs_on","link_type":"RUNS_ON","from":{"feed":"f_vm_host","columns":["host_id"]},"to":{"feed":"f_host","columns":["host_id"]},"condition":"same_key","basis":"observed"`
)

// laLink is a link definition with the attributes written as JSON.
func laLink(def, attrs string) string {
	if attrs == "" {
		return def + "}"
	}
	return def + `,"attributes":[` + attrs + `]}`
}

const (
	laServersHeader = "asset_id,serial_no,manufacturer,rack_asset_id,power_state,position_u,face,cdu_id,cap_kw,ride_s\n"
	laCdus          = "asset_id,serial_no,manufacturer\nCD1,cs1,Kelmar\n"
	laHosts         = "host_id,host_name\nH1,host-one\n"
)

func laRig(t *testing.T, links string, servers string) *rig {
	t.Helper()
	r := newRig(t, laSource, laMapping(links))
	r.files["ds_racks"], r.files["ds_servers"], r.files["ds_vms"] = racksV1, laServersHeader+servers, vmsV1
	r.files["ds_cdus"], r.files["ds_hosts"], r.files["ds_mounts"] = laCdus, laHosts, "mount_id,mfr,serial_no,rack_asset_id,position_u,face\n"
	return r
}

// linkAttr is the attribute of a link by its name.
func linkAttr(r *rig, l graph.LinkRow, name string) (graph.Attr, bool) {
	r.t.Helper()
	for _, lt := range r.runner.Reg.LinkTypes() {
		if lt.LinkTypeNo != l.TypeNo {
			continue
		}
		for _, p := range lt.Properties {
			if p.Name == name {
				a, ok := l.Attrs[p.AttrNo]
				return a, ok
			}
		}
	}
	r.t.Fatalf("no link attribute %s", name)
	return graph.Attr{}, false
}

func onlyLink(r *rig, name string) graph.LinkRow {
	r.t.Helper()
	links := linksOf(r, name)
	if len(links) != 1 {
		r.t.Fatalf("links of %s: %+v, want one", name, links)
	}
	return links[0]
}

const (
	laPos  = `{"attribute":"position_u","column":"position_u"}`
	laFace = `{"attribute":"face","column":"face"}`
)

// [REQ-435]
// A link definition fills the attributes of its link type from a column of the From feed's row or with a fixed value.
// A cell has the grade and the basis of the link; a fixed value is assumed and has the basis of the link. The link has
// no time of observation, and a second run of the same files changes nothing.
func TestALinkTakesItsAttributesFromAColumnAndFromAFixedValue(t *testing.T) {
	r := laRig(t, laLink(laRackLink, laPos+`,{"attribute":"face","value":"front"}`), "SV1,sn100,Zorvane,RK1,on,5,,,,\nSV2,sn200,Zorvane,RK2,off,,,,,\n")
	rep := r.run()
	if len(rep.Problems) != 0 || rep.Links != 2 {
		t.Fatalf("links %d, problems %+v", rep.Links, rep.Problems)
	}
	l := onlyLink(r, "SV1")
	pos, ok := linkAttr(r, l, "position_u")
	if !ok || pos.Value != int64(5) || pos.Grade != model.GradeFact || pos.Basis != model.BasisDeclared || pos.SourceTime != "" {
		t.Errorf("position_u %+v", pos)
	}
	face, ok := linkAttr(r, l, "face")
	if !ok || face.Value != "front" || face.Grade != model.GradeAssumed || face.Basis != model.BasisDeclared || face.SourceTime != "" {
		t.Errorf("face %+v: a fixed value is assumed and keeps the basis of the link", face)
	}
	if l.Grade != model.GradeFact {
		t.Errorf("the grade of the link changed: %s", l.Grade)
	}
	if _, ok := linkAttr(r, onlyLink(r, "SV2"), "position_u"); ok {
		t.Error("an empty cell gave a value")
	}
	if again := r.run(); again.Changed || again.NewVersion || again.GraphHash != rep.GraphHash {
		t.Errorf("the second run of the same files changed the graph: %+v", again)
	}
}

// [REQ-435]
// The attribute is read from the From feed's row and the link goes to the object the key says. A table of relations that
// makes no object is a feed of the From type that creates nothing.
func TestALinkAttributeCanBeReadFromARelationTableThatMakesNoObject(t *testing.T) {
	r := laRig(t, laLink(laMountDef, laPos+","+laFace), "SV1,sn100,Zorvane,RK1,on,,,,,\n")
	r.files["ds_mounts"] += "M1,Zorvane,sn100,RK2,12,rear\n"
	rep := r.run()
	if len(rep.Problems) != 0 {
		t.Fatalf("problems %+v", rep.Problems)
	}
	l := onlyLink(r, "SV1")
	if pos, _ := linkAttr(r, l, "position_u"); pos.Value != int64(12) {
		t.Errorf("position_u %+v", pos)
	}
	if face, _ := linkAttr(r, l, "face"); face.Value != "rear" {
		t.Errorf("face %+v", face)
	}
	if d := dataset(r, rep, "ds_mounts"); d.Used != 1 || !d.Balanced() {
		t.Errorf("mounts %+v", d)
	}
}

// [REQ-435]
// A value of a feed marked approximate is assumed, so the link and the attribute read from its cell are assumed.
func TestALinkAttributeOfAnApproximateFeedIsAssumed(t *testing.T) {
	r := laRig(t, laLink(laRackLink, laPos), "SV1,sn100,Zorvane,RK1,on,5,,,,\n")
	useMapping(r, strings.Replace(laMapping(laLink(laRackLink, laPos)), `"id":"f_server",`, `"approximate":true,"id":"f_server",`, 1))
	r.run()
	l := onlyLink(r, "SV1")
	if pos, _ := linkAttr(r, l, "position_u"); pos.Grade != model.GradeAssumed || l.Grade != model.GradeAssumed {
		t.Errorf("link %s, attribute %+v", l.Grade, pos)
	}
}

// [REQ-435]
// Every kind of value a flat file cell can give is read the way an attribute of an object is: an integer, a boolean, a
// number, a quantity and an enum. A cell of spaces is an empty cell, and a wrong cell leaves out its value only: the link is
// made, the row is used with a problem, and the cell is counted in cells_left_out with those of the objects.
func TestLinkAttributesAreReadByTheKindOfTheirValueAndAWrongCellLeavesOutOnlyItsValue(t *testing.T) {
	links := laLink(laRackLink, laPos+","+laFace) + "," + laLink(laCoolLink, `{"attribute":"capacity_kw","column":"cap_kw"},{"attribute":"ride_through_s","column":"ride_s"}`) +
		"," + laLink(laHostLink, `{"attribute":"ha_protected","column":"ha"}`)
	r := laRig(t, links, "SV1,sn100,Zorvane,RK1,on,5,Front,CD1,12.5,30\nSV2,sn200,Zorvane,RK2,off,  ,rear,CD1,lots,\n")
	r.files["ds_vms"] = `{"instance_uuid":"vm-1","name":"vm-one","cpu":4,"host_id":"H1","ha":true}` + "\n" + `{"instance_uuid":"vm-2","name":"vm-two","cpu":2,"host_id":"H1","ha":"maybe"}` + "\n"
	rep := r.run()
	sv1, sv2 := linksOf(r, "SV1"), linksOf(r, "SV2")
	if len(sv1) != 2 || len(sv2) != 2 || rep.Links != 6 {
		t.Fatalf("links %d %d, in all %d: a wrong cell must not drop a link", len(sv1), len(sv2), rep.Links)
	}
	byType := func(ls []graph.LinkRow, no int) graph.LinkRow {
		for _, l := range ls {
			if l.TypeNo == no {
				return l
			}
		}
		t.Fatalf("no link of type %d in %+v", no, ls)
		return graph.LinkRow{}
	}
	mounted, _ := r.runner.Reg.LinkTypeByName("MOUNTED_IN")
	cooled, _ := r.runner.Reg.LinkTypeByName("COOLED_BY")
	m1 := byType(sv1, mounted.LinkTypeNo)
	if pos, _ := linkAttr(r, m1, "position_u"); pos.Value != int64(5) {
		t.Errorf("position_u %+v", pos)
	}
	if _, ok := linkAttr(r, m1, "face"); ok {
		t.Error(`the cell "Front" is none of the values of face and was kept`)
	}
	c1 := byType(sv1, cooled.LinkTypeNo)
	if cap, _ := linkAttr(r, c1, "capacity_kw"); cap.Value != "12.5" {
		t.Errorf("capacity_kw %+v", cap)
	}
	if ride, _ := linkAttr(r, c1, "ride_through_s"); ride.Value != "30" {
		t.Errorf("ride_through_s %+v", ride)
	}
	m2 := byType(sv2, mounted.LinkTypeNo)
	if _, ok := linkAttr(r, m2, "position_u"); ok {
		t.Error("a cell of spaces gave a value")
	}
	if face, _ := linkAttr(r, m2, "face"); face.Value != "rear" {
		t.Errorf("face %+v", face)
	}
	if _, ok := linkAttr(r, byType(sv2, cooled.LinkTypeNo), "capacity_kw"); ok {
		t.Error(`the cell "lots" was kept as a quantity`)
	}
	if ha, _ := linkAttr(r, onlyLink(r, "vm-one"), "ha_protected"); ha.Value != true {
		t.Errorf("ha_protected %+v", ha)
	}
	if _, ok := linkAttr(r, onlyLink(r, "vm-two"), "ha_protected"); ok {
		t.Error(`the cell "maybe" was kept as a boolean`)
	}
	assertLeftOut(t, rep)
}

// assertLeftOut checks the three wrong cells of the test above: the problems say the link definition, the place of the
// row and what was done, and the row count and the count of cells agree with them.
func assertLeftOut(t *testing.T, rep Report) {
	t.Helper()
	bad := problemsOf(rep, ProblemInvalidValue)
	if len(bad) != 3 || len(rep.Problems) != 3 {
		t.Fatalf("problems %+v, want the 3 wrong cells and nothing else", rep.Problems)
	}
	for _, p := range bad {
		if p.Feed == "" || p.Key == "" || p.Line == 0 || !strings.Contains(p.Reason, "the link is made without this value") {
			t.Errorf("the problem does not say the definition, the row and what was done: %+v", p)
		}
	}
	for ds, want := range map[string][2]int{"ds_servers": {2, 2}, "ds_vms": {1, 1}} {
		var d DatasetReport
		for _, x := range rep.Datasets {
			if x.Dataset == ds {
				d = x
			}
		}
		if d.CellsLeftOut != want[0] || d.UsedWithProblem != want[1] || d.Used != 2 || d.Problem != 0 || !d.Balanced() {
			t.Errorf("%s: cells left out %d, used with problem %d, %+v", ds, d.CellsLeftOut, d.UsedWithProblem, d)
		}
	}
}
