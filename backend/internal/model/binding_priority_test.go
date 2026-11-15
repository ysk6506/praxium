package model_test

import (
	_ "embed"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

//go:embed testdata/binding_winners.txt
var bindingWinnersWant string

const bindingWinnersFile = "testdata/binding_winners.txt"

// bindingWinners lists every (link type, start type, end type) that more than one binding fits, with the binding
// the old rule (the lowest id) took and the one the priority takes now, and gives the number of such triples. To
// make the file again (only when a decision changes the model, never to pass a check):
//
//	cd backend/internal/model && go test . -run TestStandardWinnersThatChangedAreKept -print-binding-answers -v |
//	  sed -n '/^BEGIN-TABLE$/,/^END-TABLE$/p' | sed '1d;$d' > testdata/binding_winners.txt
func bindingWinners(r *model.Registry) (string, int) {
	var lines []string
	multi := 0
	for _, from := range r.ObjectTypes() {
		for _, to := range r.ObjectTypes() {
			byLink := map[model.LinkTypeID][]model.BindingID{}
			for _, bl := range r.BindingsFor(from.ID, to.ID) {
				byLink[bl.LinkType] = append(byLink[bl.LinkType], bl.Binding.ID)
			}
			for lt, ids := range byLink {
				if len(ids) < 2 {
					continue
				}
				multi++
				oldWinner := ids[0]
				for _, id := range ids {
					if id.String() < oldWinner.String() {
						oldWinner = id
					}
				}
				b, _ := r.BindingFor(from.ID, to.ID, lt)
				if ids[0] != b.ID {
					lines = append(lines, fmt.Sprintf("%s %s -> %s: BindingsFor lists %s first but BindingFor gives %s", lt, from.Name, to.Name, ids[0], b.ID))
				}
				if b.ID != oldWinner {
					l, _ := r.LinkType(lt)
					lines = append(lines, fmt.Sprintf("%s %s -> %s: %s, was %s", l.Name, from.Name, to.Name, b.ID, oldWinner))
				}
			}
		}
	}
	sort.Strings(lines)
	return fmt.Sprintf("pairs with more than one binding: %d\nwinner changed: %d\n%s\n", multi, len(lines), strings.Join(lines, "\n")), multi
}

// [REQ-421]
func TestStandardWinnersThatChangedAreKept(t *testing.T) {
	// The priority changes which binding wins for a known set of pairs, and only for them.
	got, _ := bindingWinners(loadStd(t))
	if *printBindingAnswers {
		printTable(got)
		return
	}
	if got != bindingWinnersWant {
		t.Errorf("the winners differ from %s:\n%s", bindingWinnersFile, got)
	}
}

// [REQ-421]
func TestTheNarrowerBindingWinsTheExamplesOfTheDesign(t *testing.T) {
	// The three examples of model-links.md 1: the end that is a type beats a trait, a deeper type beats its parent.
	r := loadStd(t)
	for _, c := range []struct{ from, to, link, want string }{
		{"Chiller", "PowerRedundancyGroup", "POWERED_BY", "bd.powered_by.trait_powered.power_redundancy_group"},
		{"StorageArray", "SanSwitch", "UPLINKED_VIA", "bd.uplinked_via.storage_array.san_switch"},
		{"VirtualMachine", "VirtualizationManager", "MANAGED_BY", "bd.managed_by.virtual_machine.virtualization_manager"},
	} {
		from, _ := r.ObjectTypeByName(c.from)
		to, _ := r.ObjectTypeByName(c.to)
		lt, _ := r.LinkTypeByName(c.link)
		b, ok := r.BindingFor(from.ID, to.ID, lt.ID)
		if !ok || b.ID.String() != c.want {
			t.Errorf("%s %s %s = %v %v, want %s", c.from, c.link, c.to, b.ID, ok, c.want)
		}
	}
}

// withBindings adds a link type with the given bindings to the baseline definitions.
func withBindings(name string, ns model.Namespace, bindings ...model.Binding) func(d *model.Definitions) {
	return func(d *model.Definitions) {
		d.LinkTypes = append(d.LinkTypes, model.LinkType{ID: ltID("lt." + strings.ToLower(name)), Name: name, Namespace: ns,
			Meaning: model.MeaningContainedIn, Status: model.StatusActive, Bindings: bindings})
	}
}

// [REQ-421]
func TestBindingsWithTheSameScoreGoByIdWhateverTheOrder(t *testing.T) {
	// A trait end and another trait end score the same, so the lower binding id wins; the order the bindings are
	// written in, or listed in, changes nothing.
	a := model.Binding{ID: bdID("bd.tie.a"), From: traitEnd("tr.powered"), To: typeEnd("ot.rack"), Cardinality: model.CardManyToMany}
	b := model.Binding{ID: bdID("bd.tie.b"), From: traitEnd("tr.rack_mountable"), To: typeEnd("ot.rack"), Cardinality: model.CardManyToMany}
	for _, order := range [][]model.Binding{{a, b}, {b, a}} {
		r := newBaseline(t, withBindings("TIE", model.NamespaceStd, order...))
		got, _ := r.BindingFor(otID("ot.server"), otID("ot.rack"), ltID("lt.tie"))
		if got.ID.String() != "bd.tie.a" {
			t.Errorf("the winner is %s, want bd.tie.a", got.ID)
		}
	}
}

// [REQ-421]
func TestTheSameRuleHoldsForBindingsAnOrganisationAdds(t *testing.T) {
	// A link type of the organisation is checked and chosen like a standard one: the narrow binding beats the one
	// that covers any object, whatever the ids say.
	wide := model.Binding{ID: bdID("bd.org_link.a_any.rack"), From: model.Endpoint{Kind: model.EndpointAny}, To: typeEnd("ot.rack"), Cardinality: model.CardManyToMany}
	narrow := model.Binding{ID: bdID("bd.org_link.z_acme.rack"), From: typeEnd("ot.acme_server"), To: typeEnd("ot.rack"), Cardinality: model.CardManyToOne}
	r := newBaseline(t, withBindings("ORG_LINK", model.NamespaceOrg, wide, narrow))
	got, _ := r.BindingFor(otID("ot.acme_server"), otID("ot.rack"), ltID("lt.org_link"))
	if got.ID != narrow.ID {
		t.Errorf("the winner is %s, want the binding of the type", got.ID)
	}
	if other, _ := r.BindingFor(otID("ot.server"), otID("ot.rack"), ltID("lt.org_link")); other.ID != wide.ID {
		t.Errorf("a type the narrow binding does not cover takes %s", other.ID)
	}
}

func partOfWith(c map[string][]string, defaults map[string]string) func(d *model.Definitions) {
	return func(d *model.Definitions) {
		b := &linkType(d, "lt.part_of").Bindings[0]
		b.ParamConstraints, b.ParamDefaults = c, defaults
	}
}

// [REQ-421]
func TestParamDefaultsAreChecked(t *testing.T) {
	// A default is one of the values the binding allows, names an enum parameter, and is required when the allowed
	// values leave out the default of the meaning.
	two := map[string][]string{"whole_needs_part": {"hard", "soft"}}
	for name, c := range map[string]struct {
		damage func(d *model.Definitions)
		ok     bool
	}{
		"a default among several allowed values":              {partOfWith(two, map[string]string{"whole_needs_part": "soft"}), true},
		"several allowed values without default":              {partOfWith(two, nil), false},
		"a default outside the allowed values":                {partOfWith(two, map[string]string{"whole_needs_part": "none"}), false},
		"a default for a parameter that is no enum":           {partOfWith(two, map[string]string{"whole_needs_part": "soft", "failover_to": "ot.cluster"}), false},
		"allowed values that keep the default of the meaning": {partOfWith(map[string][]string{"whole_needs_part": {"hard", "none"}}, nil), true},
	} {
		d := baseline()
		c.damage(&d)
		errs := model.Check(d)
		if c.ok && len(errs) != 0 || !c.ok && !hasCode(errs, model.CodeBindingConstraint) {
			t.Errorf("%s: %v", name, errs)
		}
	}
}

// [REQ-421]
func TestResolveParamsFollowsTheOrderOfTheValues(t *testing.T) {
	// 1 the link type, 2 the link (not given yet), 3 the default of the binding, 4 the only allowed value, 5 the
	// default of the meaning.
	lt := model.LinkType{Meaning: model.MeaningRequires}
	bind := func(c map[string][]string, d map[string]string) model.Binding {
		return model.Binding{ParamConstraints: c, ParamDefaults: d}
	}
	soft := map[string][]string{"strength": {"soft"}}
	both := map[string][]string{"strength": {"hard", "soft"}}
	for name, c := range map[string]struct {
		lt      model.LinkType
		b       model.Binding
		written map[string]string
		want    string
	}{
		"1 the link type fixes it":                        {model.LinkType{MeaningParams: map[string]string{"strength": "soft"}}, bind(both, map[string]string{"strength": "hard"}), map[string]string{"strength": "hard"}, "soft"},
		"2 the link states an allowed value":              {lt, bind(both, map[string]string{"strength": "hard"}), map[string]string{"strength": "soft"}, "soft"},
		"2 a value outside the allowed ones is not taken": {lt, bind(soft, nil), map[string]string{"strength": "hard"}, "soft"},
		"3 the default of the binding":                    {lt, bind(both, map[string]string{"strength": "soft"}), nil, "soft"},
		"4 the only allowed value":                        {lt, bind(soft, nil), nil, "soft"},
		"5 the default of the meaning":                    {lt, bind(nil, nil), nil, "hard"},
		"5 the default of the meaning among several":      {lt, bind(both, nil), nil, "hard"},
	} {
		if got := model.ResolveParams(c.lt, c.b, c.written).Strength; got != c.want {
			t.Errorf("%s: strength %q, want %q", name, got, c.want)
		}
	}
	if p := model.ResolveParams(lt, bind(nil, nil), nil); p.WholeNeedsPart != "none" || p.Resource != "" || len(p.FailoverTo) != 0 {
		t.Errorf("nothing says anything: %+v", p)
	}
	if p := model.ResolveParams(lt, bind(map[string][]string{"whole_needs_part": {"hard", "soft"}, "failover_to": {"ot.cluster"}}, map[string]string{"whole_needs_part": "soft"}), nil); p.WholeNeedsPart != "soft" || !slices.Equal(p.FailoverTo, []string{"ot.cluster"}) {
		t.Errorf("whole_needs_part and failover_to: %+v", p)
	}
}

// [REQ-421]
func TestTheStandardModelReadsVirtualDiskPartOfVirtualMachineAsSoft(t *testing.T) {
	// The default that keeps the old reading of the binding that allows hard and soft.
	r := loadStd(t)
	disk, _ := r.ObjectTypeByName("VirtualDisk")
	vm, _ := r.ObjectTypeByName("VirtualMachine")
	lt, _ := r.LinkTypeByName("PART_OF")
	p, ok := r.ParamsFor(disk.ID, vm.ID, lt.ID, nil)
	if !ok || p.WholeNeedsPart != "soft" {
		t.Errorf("VirtualDisk PART_OF VirtualMachine = %+v %v, want soft", p, ok)
	}
}
