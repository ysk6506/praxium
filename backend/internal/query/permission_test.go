package query

import (
	"reflect"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// liar is a filter that misreports its floor: it says every visible object
// allows confidential values while it really caps them at open.
type liar struct{}

func (liar) Decide(Target) Decision   { return Decision{Visible: true, Cap: model.SensitivityOpen} }
func (liar) NeedsScopeKeys() bool     { return false }
func (liar) Floor() model.Sensitivity { return model.SensitivityConfidential }
func (liar) Exact() bool              { return true }

// [REQ-253]
func TestFindFiltersObjectsAndNeverMatchesOnAValueAboveTheCap(t *testing.T) {
	r := newRig(t, true)
	r.world()
	racks := NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "type", Ref: "Rack"}, {Kind: "type", Ref: "Site"}}})
	a, err := r.find(FindInput{Common: r.common()}, racks)
	if err != nil {
		t.Fatal(err)
	}
	if a.Meta.ResultComplete.Total != 6 || *a.Meta.Hidden.ReachedCount != 12 || a.Meta.Hidden.Passed {
		t.Fatalf("total %d hidden %+v", a.Meta.ResultComplete.Total, a.Meta.Hidden)
	}
	for _, c := range a.Data.CountsByType {
		if c.ObjectType == "PhysicalServer" {
			t.Fatalf("the count of a hidden type leaks: %+v", a.Data.CountsByType)
		}
	}
	presence, _ := r.find(FindInput{Common: r.common()}, NewGrants(r.reg, false, Grant{Types: []Selector{{Kind: "type", Ref: "Rack"}}}))
	if presence.Meta.Hidden.ReachedCount != nil || !presence.Meta.Hidden.Reached {
		t.Fatalf("a limited scope gets presence only: %+v", presence.Meta.Hidden)
	}
	// A filter that misreports its floor still cannot match on a value above the cap of the object.
	hit, err := r.find(FindInput{Common: r.common(), ObjectType: "PhysicalServer", Attributes: []AttrFilter{{Name: "model", Op: OpEquals, Values: []string{"M1"}}}}, liar{})
	if err != nil || hit.Meta.ResultComplete.Total != 0 {
		t.Fatalf("a value above the cap matched: %v %+v", err, hit.Meta.ResultComplete)
	}
	text, _ := r.find(FindInput{Common: r.common(), Text: "m1"}, liar{})
	if text.Meta.ResultComplete.Total != 0 {
		t.Fatal("text search read a value above the cap")
	}
	full, _ := r.find(FindInput{Common: r.common(), ObjectType: "PhysicalServer", Attributes: []AttrFilter{{Name: "model", Op: OpEquals, Values: []string{"M1"}}}}, AllowAll())
	if full.Meta.ResultComplete.Total != 12 {
		t.Fatalf("allow-all: %d", full.Meta.ResultComplete.Total)
	}
}

// [REQ-253]
func TestGrantsJudgeByLayerNamespaceAndScopeKeyConditions(t *testing.T) {
	label := func(t *testing.T, f *graph.Fixture) *model.Registry {
		return variantRegistry(t, f, func(p *model.Property) { p.ScopeKey = p.AttrNo == 35 })
	}
	r := newRigWith(t, label)
	b := r.world()
	rack, _ := r.reg.ObjectTypeByName("Rack")
	for i, o := range b.Objects {
		if o.Type == rack.ID {
			b.Objects[i].Attrs[35] = graph.Attr{Value: []string{"alpha", "beta"}[i%2], Grade: model.GradeFact, Basis: model.BasisConfigured}
		}
	}
	r.apply(b)
	alpha := NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "type", Ref: "Rack"}}, Conditions: map[string][]string{"domain_scope_label": {"alpha"}}})
	if !alpha.NeedsScopeKeys() || alpha.Floor() != model.SensitivityOpen {
		t.Fatalf("needs %v floor %s", alpha.NeedsScopeKeys(), alpha.Floor())
	}
	a, err := r.find(FindInput{Common: r.common(), ObjectType: "Rack"}, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if a.Meta.ResultComplete.Total != 2 || *a.Meta.Hidden.ReachedCount != 2 {
		t.Fatalf("racks of one label: total %d hidden %+v", a.Meta.ResultComplete.Total, a.Meta.Hidden)
	}
	rackType, _ := r.reg.ObjectTypeByName("Rack")
	cases := []struct {
		name string
		g    *Grants
		want bool
	}{
		{"layer", NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "layer", Ref: string(rackType.Layer)}}}), true},
		{"other layer", NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "layer", Ref: "nope"}}}), false},
		{"namespace", NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "namespace", Ref: string(rackType.Namespace)}}}), true},
		{"unknown selector", NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "colour", Ref: "red"}}}), false},
		{"unknown type name", NewGrants(r.reg, true, Grant{Types: []Selector{{Kind: "type", Ref: "Nope"}}}), false},
		{"no grants", NewGrants(r.reg, true), false},
	}
	for _, tc := range cases {
		if got := tc.g.Decide(Target{Type: rackType}).Visible; got != tc.want {
			t.Errorf("%s: visible %v, want %v", tc.name, got, tc.want)
		}
	}
	if NewGrants(r.reg, true).Floor() != model.SensitivityOpen {
		t.Error("no grants must have the lowest floor")
	}
	if got := NewGrants(r.reg, true, Grant{Max: model.SensitivityConfidential}, Grant{Max: model.SensitivityRestricted}).Floor(); got != model.SensitivityRestricted {
		t.Errorf("floor %s", got)
	}
}

func TestValueTextForms(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{{"a", "a"}, {int64(7), "7"}, {7, "7"}, {true, "true"}, {nil, ""}, {[]any{"x", int64(1)}, `["x",1]`}, {map[string]any{"k": "v"}, `{"k":"v"}`}, {make(chan int), ""}}
	for _, tc := range tests {
		if got := valueText(tc.in); got != tc.want {
			t.Errorf("valueText(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if !reflect.DeepEqual(SensitivityRank(model.SensitivityConfidential), 2) || SensitivityRank("") != 0 {
		t.Error("sensitivity ranks")
	}
}

func TestFingerprintFallsBackWhenAValueCannotBeHashed(t *testing.T) {
	if got := fingerprint("find", make(chan int)); got != "find" {
		t.Fatalf("fingerprint %q", got)
	}
	if a, b := fingerprint("find", "x"), fingerprint("find", "y"); a == b || len(a) != 16 {
		t.Fatalf("fingerprints %q %q", a, b)
	}
}
