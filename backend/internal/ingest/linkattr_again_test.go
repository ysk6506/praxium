package ingest

import (
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const laOneServer = "SV1,sn100,Zorvane,RK1,on,5,,,,\n"

// [REQ-435]
// When a value leaves the file the link loses it: the write of a link replaces all its attributes, as it does for an
// object, so a wrong cell also takes away the good value that was stored before. The run says so with a problem.
func TestALinkLosesAnAttributeWhenTheNextRunDoesNotGiveIt(t *testing.T) {
	r := laRig(t, laLink(laRackLink, laPos), laOneServer)
	r.run()
	have := func(want any) {
		t.Helper()
		a, ok := linkAttr(r, onlyLink(r, "SV1"), "position_u")
		if want == nil && ok || want != nil && a.Value != want {
			t.Fatalf("position_u %+v (%v), want %v", a, ok, want)
		}
	}
	have(int64(5))
	run := func(servers string) Report {
		r.clock.Advance(time.Hour)
		r.files["ds_servers"] = laServersHeader + servers
		return r.run()
	}
	if rep := run("SV1,sn100,Zorvane,RK1,on,,,,,\nSV2,sn200,Zorvane,RK2,off,,,,,\n"); !rep.Changed || len(rep.Problems) != 0 {
		t.Fatalf("an empty cell: %+v", rep)
	}
	have(nil)
	if rep := run("SV1,sn100,Zorvane,RK1,on,7,,,,\nSV2,sn200,Zorvane,RK2,off,,,,,\n"); !rep.Changed {
		t.Fatalf("the value came back: %+v", rep)
	}
	have(int64(7))
	rep := run("SV1,sn100,Zorvane,RK1,on,tall,,,,\nSV2,sn200,Zorvane,RK2,off,,,,,\n")
	if !rep.Changed || rep.Links != 2 || rep.ProblemCounts[ProblemInvalidValue] != 1 {
		t.Fatalf("a wrong cell: %+v", rep)
	}
	have(nil)
}

func conflictRig(t *testing.T, links string, mounts string) (*rig, Report) {
	t.Helper()
	r := laRig(t, links, "SV1,sn100,Zorvane,RK1,on,5,,,,\n")
	r.files["ds_mounts"] += mounts
	return r, r.run()
}

// [REQ-435]
// Two rows of one definition that make the same link: the later row, in the order of the natural key, is used and the
// attributes are not merged. The row that lost is reported with both sets of attributes when they differ.
func TestTwoRowsThatMakeTheSameLinkWithOtherAttributesLeaveTheLaterAndReportTheEarlier(t *testing.T) {
	r, rep := conflictRig(t, laLink(laMountDef, laPos+","+laFace), "M1,Zorvane,sn100,RK1,5,front\nM2,Zorvane,sn100,RK1,7,\n")
	l := onlyLink(r, "SV1")
	if pos, _ := linkAttr(r, l, "position_u"); pos.Value != int64(7) {
		t.Errorf("position_u %+v, want the one of the later row", pos)
	}
	if _, ok := linkAttr(r, l, "face"); ok {
		t.Error("the face of the earlier row was merged into the link of the later one")
	}
	got := problemsOf(rep, ProblemLinkConflict)
	if len(got) != 1 || rep.Links != 1 || len(rep.Problems) != 1 {
		t.Fatalf("problems %+v, links %d", rep.Problems, rep.Links)
	}
	p := got[0]
	for _, want := range []string{"MOUNTED_IN", "Zorvane", "RK1", "position_u=5 fact", "face=front fact", "position_u=7 fact", "l_mount_table", "row M2", "not merged"} {
		if !strings.Contains(strings.ToLower(p.Reason), strings.ToLower(want)) {
			t.Errorf("the reason lacks %q: %s", want, p.Reason)
		}
	}
	if p.Dataset != "ds_mounts" || p.Feed != "l_mount_table" || p.Key != "M1" || p.Line == 0 {
		t.Errorf("the problem is about the row that lost: %+v", p)
	}
	if d := dataset(r, rep, "ds_mounts"); d.Used != 2 || d.Problem != 0 || d.UsedWithProblem != 0 || !d.Balanced() {
		t.Errorf("both rows are used rows: %+v", d)
	}
}

// [REQ-435]
// Two definitions of the same link type that make the same link: the one written later in the mapping is used, also when
// it gives no attribute, and the attribute of the earlier one is gone. Same attributes, or none on both, are no problem.
func TestTwoDefinitionsThatMakeTheSameLinkLeaveTheLaterOneWhateverItGives(t *testing.T) {
	for name, c := range map[string]struct {
		links    string
		position any
		problems int
	}{
		"the later gives another value": {laLink(laRackLink, laPos) + "," + laLink(laMountDef, laPos), int64(9), 1},
		"the later gives none":          {laLink(laRackLink, laPos) + "," + laLink(laMountDef, ""), nil, 1},
		"the later gives the same":      {laLink(laRackLink, laPos) + "," + laLink(laMountDef, laPos), int64(5), 0},
		"neither gives any":             {laLink(laRackLink, "") + "," + laLink(laMountDef, ""), nil, 0},
	} {
		mounts := "M1,Zorvane,sn100,RK1,9,\n"
		if c.position == int64(5) {
			mounts = "M1,Zorvane,sn100,RK1,5,\n"
		}
		r, rep := conflictRig(t, c.links, mounts)
		a, ok := linkAttr(r, onlyLink(r, "SV1"), "position_u")
		if c.position == nil && ok || c.position != nil && a.Value != c.position {
			t.Errorf("%s: position_u %+v (%v), want %v", name, a, ok, c.position)
		}
		got := problemsOf(rep, ProblemLinkConflict)
		if len(got) != c.problems || len(rep.Problems) != c.problems {
			t.Errorf("%s: problems %+v", name, rep.Problems)
			continue
		}
		if c.problems == 1 && (got[0].Feed != "l_mounted" || !strings.Contains(got[0].Reason, "l_mount_table")) {
			t.Errorf("%s: the problem names the definition that was left and the one that was used: %+v", name, got[0])
		}
	}
}

// [REQ-435]
// A link that the cardinality of the model drops reports nothing about its cells: it is not made.
func TestADroppedLinkReportsNoWrongCell(t *testing.T) {
	r, rep := conflictRig(t, laLink(laMountDef, laPos), "M1,Zorvane,sn100,RK1,first,\nM2,Zorvane,sn100,RK2,second,\n")
	if rep.ProblemCounts[ProblemCardinality] != 1 || rep.ProblemCounts[ProblemInvalidValue] != 1 || len(rep.Problems) != 2 {
		t.Fatalf("problems %+v: the two links are one too many, and only the one that is made has a wrong cell to report", rep.Problems)
	}
	if d := dataset(r, rep, "ds_mounts"); d.CellsLeftOut != 1 || d.UsedWithProblem != 1 || !d.Balanced() {
		t.Errorf("%+v", d)
	}
}

// [REQ-435]
// A row that a rule filtered out and whose object is waited for still makes its links and the values of its cells are
// read, but nothing is reported about it, as for the attributes of an object (ADR-0052): a wrong cell leaves out its value only.
func TestTheLinkOfAFilteredRowIsMadeFromItsCellsAndReportsNothing(t *testing.T) {
	filtered := func(filter string) string {
		mp := strings.Replace(laMapping(laLink(laRackLink, laPos)), `{"feeds":[`, `{"refinements":[{"id":"r_servers","input":"ds_servers","ops":[`+filter+`]}],"feeds":[`, 1)
		return strings.Replace(mp, `"id":"f_server","dataset":"ds_servers"`, `"id":"f_server","dataset":"r_servers"`, 1)
	}
	r := laRig(t, laLink(laRackLink, laPos), "SV1,sn100,Zorvane,RK1,on,5,,,,\nSV2,sn200,Zorvane,RK2,off,6,,,,\n")
	useMapping(r, filtered(keepAll))
	r.run()
	useMapping(r, filtered(typoFilter))
	r.clock.Advance(time.Hour)
	r.files["ds_servers"] = laServersHeader + "SV1,sn100,Zorvane,RK1,on,tall,,,,\nSV2,sn200,Zorvane,RK2,off,8,,,,\n"
	rep := r.run()
	if len(rep.Problems) != 0 || rep.RetiredLinks != 0 || rep.Links != 2 {
		t.Fatalf("problems %+v, links %d, retired %d", rep.Problems, rep.Links, rep.RetiredLinks)
	}
	if _, ok := linkAttr(r, onlyLink(r, "SV1"), "position_u"); ok {
		t.Error("the wrong cell of a waiting row was kept")
	}
	if pos, _ := linkAttr(r, onlyLink(r, "SV2"), "position_u"); pos.Value != int64(8) {
		t.Errorf("the value of a waiting row was not read: %+v", pos)
	}
	if d := dataset(r, rep, "ds_servers"); d.Filtered != 2 || d.CellsLeftOut != 0 || !d.Balanced() {
		t.Errorf("%+v", d)
	}
}

// [REQ-435]
// The name of a link attribute is found by its old names, and an attribute that is deprecated is refused with what to
// write instead, as for an object (ADR-0066).
func TestLinkAttributeNamesFollowTheRulesOfObjectAttributes(t *testing.T) {
	defs, err := model.StandardDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	for i := range defs.LinkTypes {
		if defs.LinkTypes[i].Name != "MOUNTED_IN" {
			continue
		}
		for j := range defs.LinkTypes[i].Properties {
			switch p := &defs.LinkTypes[i].Properties[j]; p.Name {
			case "position_u":
				p.Name, p.Aliases = "slot_u", []string{"position_u"}
			case "side":
				next, err := kernel.ParseID[model.PropertyKind]("pr.mounted_in.face")
				if err != nil {
					t.Fatal(err)
				}
				p.Status, p.SupersededBy = model.StatusDeprecated, next
			}
		}
	}
	reg, err := model.NewRegistry(defs)
	if err != nil {
		t.Fatal(err)
	}
	src, _ := ParseSource(strings.NewReader(laSource))
	compile := func(attrs string) error {
		m, err := ParseMapping(strings.NewReader(laMapping(laLink(laRackLink, attrs))))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Compile(reg, src, m)
		return err
	}
	if err := compile(`{"attribute":"position_u","column":"position_u"}`); err != nil {
		t.Errorf("the old name no longer finds the attribute: %v", err)
	}
	if err := compile(`{"attribute":"slot_u","column":"position_u"},{"attribute":"position_u","column":"position_u"}`); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("the name and the old name of one attribute are one attribute: %v", err)
	}
	if err := compile(`{"attribute":"side","value":"full"}`); err == nil || !strings.Contains(err.Error(), "deprecated") {
		t.Errorf("a deprecated attribute: %v", err)
	}
}
