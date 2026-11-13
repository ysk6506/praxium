package query

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

func (r *rig) neighbors(in NeighborsInput, f ReadFilter) (Answer[NeighborsData], error) {
	if in.KG == "" {
		in.Common = r.common()
	}
	return r.eng.Neighbors(r.ctx, in, f)
}

func (r *rig) must(in NeighborsInput, f ReadFilter) Answer[NeighborsData] {
	r.t.Helper()
	a, err := r.neighbors(in, f)
	if err != nil {
		r.t.Fatalf("neighbors: %v", err)
	}
	return a
}

func peerNames(items []NeighborItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.PeerDisplayName)
	}
	sort.Strings(out)
	return out
}

// [REQ-254]
func TestNeighborsTableDriven(t *testing.T) {
	r := newRig(t, false)
	r.world()
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00").String()
	located, _ := r.reg.LinkTypeByName("LOCATED_IN")
	servers := []string{"Server SN-000000", "Server SN-000001", "Server SN-000002"}
	tests := []struct {
		name string
		in   NeighborsInput
		want []string
	}{
		{"both directions", NeighborsInput{ObjectID: rack}, append([]string{"Site S00"}, servers...)},
		{"out only", NeighborsInput{ObjectID: rack, Direction: DirOut}, []string{"Site S00"}},
		{"in only", NeighborsInput{ObjectID: rack, Direction: DirIn}, servers},
		{"link type", NeighborsInput{ObjectID: rack, LinkTypes: []string{"MOUNTED_IN"}}, servers},
		{"meaning", NeighborsInput{ObjectID: rack, Meanings: []string{string(located.Meaning)}, LinkTypes: []string{"LOCATED_IN"}}, []string{"Site S00"}},
		{"peer type", NeighborsInput{ObjectID: rack, PeerTypes: []string{"Site"}}, []string{"Site S00"}},
		{"two steps", NeighborsInput{ObjectID: rack, MaxSteps: 2}, append([]string{"Site S00", "Rack R01"}, servers...)},
		{"two steps, peer type", NeighborsInput{ObjectID: rack, MaxSteps: 2, PeerTypes: []string{"Rack"}}, []string{"Rack R01"}},
		{"no match", NeighborsInput{ObjectID: rack, Meanings: []string{"owned_by"}}, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Common = r.common()
			a := r.must(tc.in, AllowAll())
			want := append([]string{}, tc.want...)
			sort.Strings(want)
			if got := peerNames(a.Data.Items); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			total := 0
			for _, g := range a.Data.Groups {
				total += g.Count
			}
			if total != len(want) || a.Meta.ResultComplete.Total != len(want) {
				t.Fatalf("groups add up to %d, total %d, want %d", total, a.Meta.ResultComplete.Total, len(want))
			}
		})
	}
}

// [REQ-254]
func TestNeighborsGroupsAndStepsAreReported(t *testing.T) {
	r := newRig(t, false)
	r.world()
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00")
	a := r.must(NeighborsInput{ObjectID: rack.String(), MaxSteps: 2}, AllowAll())
	var viaSite, direct int
	for _, it := range a.Data.Items {
		if it.Step == 1 {
			direct++
			if it.ViaObjectID != nil {
				t.Fatalf("a first step has no via: %+v", it)
			}
		} else {
			viaSite++
			if it.ViaObjectID == nil || it.Step != 2 {
				t.Fatalf("a second step names where it came from: %+v", it)
			}
		}
	}
	if direct != 4 || viaSite != 1 {
		t.Fatalf("steps: %d direct, %d second", direct, viaSite)
	}
	if a.Data.Object.DisplayName != "Rack R00" || len(a.Data.Groups) == 0 || a.Data.Groups[0].PeerType == "" {
		t.Fatalf("groups %+v object %+v", a.Data.Groups, a.Data.Object)
	}
	summary := r.must(NeighborsInput{Common: Common{KG: "main"}, ObjectID: rack.String()}, AllowAll())
	if summary.Data.Items != nil || summary.Meta.ResultComplete.Total != 4 || len(summary.Data.Groups) != 2 {
		t.Fatalf("summary: %+v %+v", summary.Data, summary.Meta.ResultComplete)
	}
}

// [REQ-254]
func TestNeighborsPagesOfAHubRoundTrip(t *testing.T) {
	r := newRig(t, false)
	r.world()
	site := r.fx.SiteID("S00").String()
	whole := r.must(NeighborsInput{ObjectID: site, MaxSteps: 2}, AllowAll())
	var got []string
	in := NeighborsInput{Common: Common{KG: "main", Shape: ShapeItems, Limit: 3}, ObjectID: site, MaxSteps: 2}
	for i := 0; ; i++ {
		a := r.must(in, AllowAll())
		for _, it := range a.Data.Items {
			got = append(got, it.LinkID)
		}
		if a.Meta.ResultComplete.NextCursor == nil {
			break
		}
		if i > 10 || len(a.Data.Items) != 3 || !a.Meta.ResultComplete.Truncated {
			t.Fatalf("page %d: %+v", i, a.Meta.ResultComplete)
		}
		in.Cursor = *a.Meta.ResultComplete.NextCursor
	}
	var want []string
	for _, it := range whole.Data.Items {
		want = append(want, it.LinkID)
	}
	if !reflect.DeepEqual(got, want) || len(got) != 8 {
		t.Fatalf("pages\n%v\n%v", got, want)
	}
	in.Cursor = "x"
	if _, err := r.neighbors(in, AllowAll()); codeOf(err) != CodeCursorInvalid {
		t.Fatalf("bad cursor: %v", err)
	}
}

// [REQ-254]
func TestNeighborsRefusesBadInputs(t *testing.T) {
	r := newRig(t, false)
	r.world()
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00").String()
	tests := []struct {
		name string
		in   NeighborsInput
		code kernel.Code
	}{
		{"steps too many", NeighborsInput{ObjectID: rack, MaxSteps: 4}, CodeInput},
		{"steps negative", NeighborsInput{ObjectID: rack, MaxSteps: -1}, CodeInput},
		{"direction", NeighborsInput{ObjectID: rack, Direction: "up"}, CodeInput},
		{"link type", NeighborsInput{ObjectID: rack, LinkTypes: []string{"NOPE"}}, CodeInput},
		{"meaning", NeighborsInput{ObjectID: rack, Meanings: []string{"nope"}}, CodeInput},
		{"peer type", NeighborsInput{ObjectID: rack, PeerTypes: []string{"Nope"}}, CodeInput},
		{"unknown object", NeighborsInput{ObjectID: "obj_00000000000000000000000000000000"}, CodeObjectNotFound},
		{"not an id", NeighborsInput{ObjectID: "?"}, CodeObjectNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := r.neighbors(tc.in, AllowAll()); codeOf(err) != tc.code {
				t.Fatalf("code %q (%v), want %q", codeOf(err), err, tc.code)
			}
		})
	}
}

// [REQ-253] [REQ-254]
func TestNeighborsLeaveHiddenObjectsOutAndCountThem(t *testing.T) {
	r := newRig(t, false)
	r.world()
	rack := r.fx.RackID(r.fx.SiteID("S00"), "R00").String()
	noServers := func(exact bool) ReadFilter {
		return NewGrants(r.reg, exact, Grant{Types: []Selector{{Kind: "type", Ref: "Rack"}, {Kind: "type", Ref: "Site"}}})
	}
	a := r.must(NeighborsInput{ObjectID: rack}, noServers(true))
	if got := peerNames(a.Data.Items); !reflect.DeepEqual(got, []string{"Site S00"}) {
		t.Fatalf("visible peers %v", got)
	}
	if a.Meta.Hidden.ReachedCount == nil || *a.Meta.Hidden.ReachedCount != 3 || *a.Meta.Hidden.PassedCount != 0 || a.Meta.ResultComplete.Total != 1 {
		t.Fatalf("hidden %+v total %d", a.Meta.Hidden, a.Meta.ResultComplete.Total)
	}
	limited := r.must(NeighborsInput{ObjectID: rack}, noServers(false))
	if limited.Meta.Hidden.ReachedCount != nil || !limited.Meta.Hidden.Reached || limited.Meta.Hidden.Passed {
		t.Fatalf("a limited scope gets presence only: %+v", limited.Meta.Hidden)
	}
	// Walking two steps from a server goes through the hidden rack to the site and the other servers.
	server := r.fx.ServerID("SN-000000").String()
	through := r.must(NeighborsInput{ObjectID: server, MaxSteps: 2}, NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "type", Ref: "Site"}, {Kind: "type", Ref: "PhysicalServer"}}}))
	if got := peerNames(through.Data.Items); !reflect.DeepEqual(got, []string{"Server SN-000001", "Server SN-000002", "Site S00"}) {
		t.Fatalf("through the hidden rack: %v", got)
	}
	if *through.Meta.Hidden.PassedCount != 1 || *through.Meta.Hidden.ReachedCount != 0 {
		t.Fatalf("hidden %+v", through.Meta.Hidden)
	}
	for _, it := range through.Data.Items {
		if it.Step == 2 && (it.ViaObjectID != nil || !it.ViaHidden) {
			t.Fatalf("a step through a hidden object must not name it: %+v", it)
		}
	}
	if _, err := r.neighbors(NeighborsInput{ObjectID: server}, noServers(true)); codeOf(err) != CodeObjectNotFound {
		t.Fatalf("neighbors of a hidden object: %v", err)
	}
}

func TestNewRefusesMissingParts(t *testing.T) {
	if _, err := New(nil, nil, nil); codeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("missing parts: %v", err)
	}
}

func TestCodesAreDescribedAndSorted(t *testing.T) {
	codes := Codes()
	if len(codes) == 0 || !sort.SliceIsSorted(codes, func(i, j int) bool { return codes[i] < codes[j] }) {
		t.Fatalf("codes %v", codes)
	}
	for _, c := range codes {
		if m, ok := CodeMeaning(c); !ok || m == "" {
			t.Errorf("code %s has no meaning", c)
		}
	}
	if _, ok := CodeMeaning("nope"); ok {
		t.Error("a code outside the unit has a meaning")
	}
}
