package model_test

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-101]
func TestClosedListsHaveTheDocumentedSizes(t *testing.T) {
	// The closed lists are closed: only a decision record changes their size.
	sizes := map[string][2]int{
		"namespaces": {len(model.Namespaces()), 4},
		"layers":     {len(model.Layers()), 10},
		"channels":   {len(model.Channels()), 6},
		"meanings":   {len(model.Meanings()), 11},
		"grades":     {len(model.Grades()), 3},
		"bases":      {len(model.Bases()), 4},
		"base kinds": {len(model.BaseKinds()), 10},
	}
	for name, got := range sizes {
		if got[0] != got[1] {
			t.Errorf("%s has %d values, want %d", name, got[0], got[1])
		}
	}
	if layers := model.Layers(); layers[9] != model.LayerSystem {
		t.Errorf("the tenth layer is %s, want system", layers[9])
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
		want bool
	}{
		{"namespace sys", model.Namespace("sys").Valid(), true},
		{"namespace team", model.Namespace("team").Valid(), false},
		{"layer cross_cutting", model.Layer("cross_cutting").Valid(), true},
		{"layer wireless", model.Layer("wireless").Valid(), false},
		{"[REQ-101] channel management", model.Channel("management").Valid(), true},
		{"[REQ-101] channel light", model.Channel("light").Valid(), false},
		{"[REQ-101] meaning record_only", model.Meaning("record_only").Valid(), true},
		{"[REQ-101] meaning depends_on", model.Meaning("depends_on").Valid(), false},
		{"grade assumed", model.Grade("assumed").Valid(), true},
		{"grade likely", model.Grade("likely").Valid(), false},
		{"basis derived", model.Basis("derived").Valid(), true},
		{"basis guessed", model.Basis("guessed").Valid(), false},
		{"[REQ-100] base kind quantity", model.BaseKind("quantity").Valid(), true},
		{"[REQ-100] base kind text", model.BaseKind("text").Valid(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.ok != tt.want {
				t.Fatalf("Valid() = %v, want %v", tt.ok, tt.want)
			}
		})
	}
}

func TestLowestGrade(t *testing.T) {
	// The grade of an answer is the lowest grade of the inputs it used.
	tests := []struct {
		name string
		in   []model.Grade
		want model.Grade
	}{
		{"facts only", []model.Grade{model.GradeFact, model.GradeFact}, model.GradeFact},
		{"an assumption among facts", []model.Grade{model.GradeFact, model.GradeAssumed, model.GradeFact}, model.GradeAssumed},
		{"an unknown among assumptions", []model.Grade{model.GradeAssumed, model.GradeUnknown}, model.GradeUnknown},
		{"no inputs", nil, model.GradeUnknown},
		{"only grades outside the list", []model.Grade{"likely"}, model.GradeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.Lowest(tt.in...); got != tt.want {
				t.Fatalf("Lowest(%v) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestNameForms(t *testing.T) {
	tests := []struct {
		name string
		form model.NameForm
		text string
		want bool
	}{
		{"[REQ-100] PascalCase type", model.FormPascal, "PhysicalServer", true},
		{"[REQ-100] short PascalCase type", model.FormPascal, "Ups", true},
		{"[REQ-100] snake_case as a type", model.FormPascal, "physical_server", false},
		{"[REQ-100] type starting with a digit", model.FormPascal, "2Server", false},
		{"[REQ-100] empty type", model.FormPascal, "", false},
		{"[REQ-100] UPPER_SNAKE link", model.FormUpperSnake, "HOSTED_ON", true},
		{"[REQ-100] single word link", model.FormUpperSnake, "OWNED", true},
		{"[REQ-100] lower case link", model.FormUpperSnake, "hosted_on", false},
		{"[REQ-100] link with a double underscore", model.FormUpperSnake, "HOSTED__ON", false},
		{"[REQ-100] snake_case property", model.FormSnake, "rack_unit", true},
		{"[REQ-100] property with a digit", model.FormSnake, "ipv4_address", true},
		{"[REQ-100] property with a capital", model.FormSnake, "rackUnit", false},
		{"[REQ-100] property ending with an underscore", model.FormSnake, "rack_", false},
		{"[REQ-100] a form that does not exist", model.NameForm(9), "rack", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.ValidName(tt.form, tt.text); got != tt.want {
				t.Fatalf("ValidName(%d, %q) = %v, want %v", tt.form, tt.text, got, tt.want)
			}
		})
	}
}

// [REQ-101]
func TestMeaningSpecsAreSound(t *testing.T) {
	// Each of the eleven meanings says which channel of the start depends on which channel of the end.
	specs := model.MeaningSpecs()
	if len(specs) != 11 {
		t.Fatalf("there are %d meaning specs, want 11", len(specs))
	}
	for i, spec := range specs {
		if spec.Meaning != model.Meanings()[i] {
			t.Errorf("spec %d is %s, want %s", i, spec.Meaning, model.Meanings()[i])
		}
		if got, ok := model.SpecOf(spec.Meaning); !ok || got.Meaning != spec.Meaning {
			t.Errorf("SpecOf(%s) = %v, %v", spec.Meaning, got, ok)
		}
		if spec.To != "" && !spec.To.Valid() || spec.From != "" && !spec.From.Valid() {
			t.Errorf("%s spreads along a channel outside the six: %s to %s", spec.Meaning, spec.From, spec.To)
		}
		if spec.Spreads() != (spec.To != "") {
			t.Errorf("%s: Spreads() disagrees with its channels", spec.Meaning)
		}
		if spec.FromParam != "" {
			param, ok := spec.Param(spec.FromParam)
			if !ok || param.Kind != model.ParamEnum {
				t.Errorf("%s takes its channel from %s, which is not an enum parameter", spec.Meaning, spec.FromParam)
			}
			for _, value := range param.Values {
				if c := model.Channel(value); !c.Valid() || c == model.ChannelService || c == model.ChannelManagement {
					t.Errorf("%s: resource %s is not one of power, cooling, network, storage", spec.Meaning, value)
				}
			}
		}
	}
	if _, ok := model.SpecOf("depends_on"); ok {
		t.Errorf("a meaning outside the closed list has a spec")
	}
	if spec, _ := model.SpecOf(model.MeaningRequires); len(spec.Params) != 2 {
		t.Errorf("requires has %d parameters, want strength and demand", len(spec.Params))
	}
	if _, ok := specs[0].Param("nothing"); ok {
		t.Errorf("a parameter that does not exist is found")
	}
}

// [REQ-101]
func TestMeaningSpecsAreCopies(t *testing.T) {
	// The closed list cannot be changed through what MeaningSpecs returns.
	specs := model.MeaningSpecs()
	specs[1].Params[0].Values[0] = "changed"
	specs[1].Params = nil
	again, _ := model.SpecOf(model.MeaningRequires)
	if len(again.Params) != 2 || again.Params[0].Values[0] != "hard" {
		t.Errorf("changing a returned spec changed the closed list: %+v", again.Params)
	}
}

// [REQ-101]
func TestNotSpreadingMeanings(t *testing.T) {
	// Only the meanings that carry state have channels: contained_in, owned_by and record_only do not.
	var quiet []model.Meaning
	for _, spec := range model.MeaningSpecs() {
		if !spec.Spreads() {
			quiet = append(quiet, spec.Meaning)
		}
	}
	want := []model.Meaning{model.MeaningContainedIn, model.MeaningOwnedBy, model.MeaningRecordOnly}
	if !reflect.DeepEqual(quiet, want) {
		t.Fatalf("meanings that do not spread: %v, want %v", quiet, want)
	}
}

func TestCodeCatalogueIsStable(t *testing.T) {
	// The codes of this unit are a contract: a code is never renamed or removed, and a new one is added here on purpose.
	want := []kernel.Code{
		"model_action_ref", "model_action_risk", "model_binding_constraint", "model_binding_duplicate", "model_binding_endpoint",
		"model_data_invalid", "model_duplicate_id", "model_enum_values", "model_identity_context", "model_identity_key", "model_identity_key_changed", "model_identity_key_volatile", "model_identity_set",
		"model_layer_namespace", "model_layer_not_inherited", "model_link_meaning", "model_link_volatile", "model_name_duplicate",
		"model_name_invalid", "model_name_reserved", "model_need", "model_not_in_closed_list", "model_not_standard", "model_number",
		"model_parent_cycle", "model_parent_namespace", "model_parent_unknown", "model_property_duplicate", "model_required_field",
		"model_scheme_unknown", "model_scope_key_limit", "model_scope_key_type", "model_scope_key_volatile", "model_superseded", "model_title_property",
		"model_trait_unknown", "model_unit", "model_value_invalid", "model_value_type_unknown",
	}
	got := model.Codes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Codes() = %v, want %v", got, want)
	}
	form := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	for _, code := range got {
		if !form.MatchString(string(code)) {
			t.Errorf("code %q is not snake_case", code)
		}
		if meaning, ok := model.CodeMeaning(code); !ok || meaning == "" {
			t.Errorf("code %q has no meaning", code)
		}
		if _, clash := code.Meaning(); clash {
			t.Errorf("code %q is also a code of the kernel", code)
		}
	}
	if _, ok := model.CodeMeaning("model_never_defined"); ok {
		t.Errorf("an unknown code has a meaning")
	}
}

func TestDefinitionsCloneSharesNothing(t *testing.T) {
	// A clone can be changed without changing the original.
	d := baseline()
	c := d.Clone()
	objectType(&c, "ot.server").Traits[0] = trID("tr.nothing")
	objectType(&c, "ot.server").Properties[0].Name = "changed"
	linkType(&c, "lt.runs_on").Bindings[0].ParamConstraints["failover_to"][0] = "ot.nothing"
	linkType(&c, "lt.powered_by").MeaningParams["resource"] = "cooling"
	actionType(&c, "ac.power_off").AppliesTo[0].Ref = "ot.nothing"
	c.Traits[0].Needs[0].Channel = model.ChannelCooling
	c.ValueTypes[0].Name = "changed"
	c.Schemes[0].Aliases = append(c.Schemes[0].Aliases, "changed")
	objectType(&c, "ot.asset").IdentityKeys[1].Components[0].Context = ltID("lt.nothing")
	c.Reserved.AttrNos = append(c.Reserved.AttrNos, 5)
	if errs := model.Check(d); len(errs) != 0 {
		t.Fatalf("changing a clone changed the original: %v", errs)
	}
	if empty := (model.Definitions{}).Clone(); empty.ObjectTypes != nil || empty.Traits != nil {
		t.Errorf("the clone of nothing is something: %+v", empty)
	}
}
