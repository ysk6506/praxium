package model_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

func enumProp() model.Property {
	p := orgProp("server.grade", "grade", "vt.enum", 100001)
	p.EnumValues = []string{"gold", "silver"}
	return p
}

// [REQ-347]
func TestClassifyOverlay(t *testing.T) {
	// A table of changes and how each is received: an addition, a compatible change or a breaking one.
	base := orgProp("server.dept", "dept", "vt.string", 100002)
	tests := []struct {
		name    string
		prev    model.Property
		change  func(p *model.Property)
		want    model.ChangeKind
		field   string
		nothing bool
	}{
		{"display names", base, func(p *model.Property) { p.DisplayNames = map[string]string{"ko": "부서"} }, model.ChangeCompatible, "display_names", false},
		{"description", base, func(p *model.Property) { p.Description = "who owns it" }, model.ChangeCompatible, "description", false},
		{"aliases", base, func(p *model.Property) { p.Aliases = []string{"org.owner_dept"} }, model.ChangeCompatible, "aliases", false},
		{"rename keeping the old name", base, func(p *model.Property) { p.Name, p.Aliases = "org.department", []string{"org.dept"} }, model.ChangeCompatible, "name", false},
		{"rename losing the old name", base, func(p *model.Property) { p.Name = "org.department" }, model.ChangeBreaking, "name", false},
		{"enum value added", enumProp(), func(p *model.Property) { p.EnumValues = []string{"silver", "gold", "bronze"} }, model.ChangeCompatible, "enum_values", false},
		{"enum value removed", enumProp(), func(p *model.Property) { p.EnumValues = []string{"gold"} }, model.ChangeBreaking, "enum_values", false},
		{"enum values reordered", enumProp(), func(p *model.Property) { p.EnumValues = []string{"silver", "gold"} }, "", "", true},
		{"sensitivity up", base, func(p *model.Property) { p.Sensitivity = model.SensitivityRestricted }, model.ChangeCompatible, "sensitivity", false},
		{"sensitivity down", withSensitivity(base, model.SensitivityConfidential), func(p *model.Property) { p.Sensitivity = model.SensitivityRestricted }, model.ChangeBreaking, "sensitivity", false},
		{"open and empty are one", base, func(p *model.Property) { p.Sensitivity = model.SensitivityOpen }, "", "", true},
		{"deprecated", base, func(p *model.Property) { p.Status = model.StatusDeprecated }, model.ChangeCompatible, "status", false},
		{"value type", base, func(p *model.Property) { p.ValueType = vtID("vt.integer") }, model.ChangeBreaking, "value_type", false},
		{"unit", withUnit(base), func(p *model.Property) { p.Unit = "kw" }, model.ChangeBreaking, "unit", false},
		{"change class", base, func(p *model.Property) { p.ChangeClass = model.ChangeStatic }, model.ChangeBreaking, "change_class", false},
		{"number", base, func(p *model.Property) { p.AttrNo = 100009 }, model.ChangeBreaking, "attr_no", false},
		{"nothing", base, func(p *model.Property) {}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := tt.prev
			next.Aliases = append([]string(nil), tt.prev.Aliases...)
			tt.change(&next)
			got := model.ClassifyOverlay(serverOverlay(tt.prev), serverOverlay(next))
			if tt.nothing {
				if len(got) != 0 {
					t.Fatalf("ClassifyOverlay() = %+v, want no change", got)
				}
				return
			}
			if len(got) != 1 || got[0].Kind != tt.want || got[0].Field != tt.field || got[0].Property != tt.prev.ID {
				t.Fatalf("ClassifyOverlay() = %+v, want one %s change of %s", got, tt.want, tt.field)
			}
			if tt.want == model.ChangeBreaking && !model.HasBreaking(got) {
				t.Errorf("HasBreaking() is false for %+v", got)
			}
		})
	}
}

func withSensitivity(p model.Property, s model.Sensitivity) model.Property {
	p.Sensitivity = s
	return p
}

func withUnit(p model.Property) model.Property {
	p.ValueType, p.Unit = vtID("vt.quantity"), "w"
	return p
}

// [REQ-347]
func TestClassifyAddedAndGoneProperties(t *testing.T) {
	// A new property is an addition; one that is gone, or a changed target, is breaking and says how to retire it instead.
	a, b := orgProp("server.a", "a", "vt.string", 100001), orgProp("server.b", "b", "vt.string", 100002)
	got := model.ClassifyOverlay(serverOverlay(a), serverOverlay(a, b))
	if len(got) != 1 || got[0].Kind != model.ChangeAdded || got[0].Property != b.ID {
		t.Errorf("an added property: %+v", got)
	}
	got = model.ClassifyOverlay(serverOverlay(a, b), serverOverlay(b))
	if len(got) != 1 || got[0].Kind != model.ChangeBreaking || got[0].Property != a.ID || got[0].Field != "property" {
		t.Errorf("a property that is gone: %+v", got)
	}
	got = model.ClassifyOverlay(model.Overlay{}, serverOverlay(b, a))
	if len(got) != 2 || got[0].Property != a.ID || got[1].Property != b.ID || got[0].Kind != model.ChangeAdded {
		t.Errorf("a first overlay is all additions in the order of the ids: %+v", got)
	}
	rack := model.NewOverlay(model.TargetObjectType, "ot.rack", []model.Property{a})
	if got = model.ClassifyOverlay(serverOverlay(a), rack); !model.HasBreaking(got) || got[0].Field != "target" {
		t.Errorf("a changed target: %+v", got)
	}
	if got = model.ClassifyOverlay(serverOverlay(a, b), serverOverlay(b, a)); len(got) != 0 {
		t.Errorf("the order of the properties is a change: %+v", got)
	}
}
