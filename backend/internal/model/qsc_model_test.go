package model_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// The tests of the rules that the query schema needs from the model (ADR-0113). Their names start with Qsc.

// [REQ-626] The name of an object and the value of an identity key are shown to every grade of the query schema (the name is a column of
// the table of the type, the value of a key is an identifier), so the grade cannot cut them: the model must not make them sensitive.
func TestQscAPropertyThatIsNotOpenCannotGiveAnObjectItsNameOrBeAnIdentityKey(t *testing.T) {
	asset := func(d *model.Definitions) *model.ObjectType { return objectType(d, "ot.asset") }
	withName := func(d *model.Definitions, s model.Sensitivity) {
		name := prop("pr.asset.name", model.ObjectNameProperty, "vt.string", model.ChangeSlow)
		name.Sensitivity = s
		asset(d).Properties = append(asset(d).Properties, name)
	}
	runRuleCases(t, []ruleCase{
		{"a restricted property is an identity key", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).Properties[0].Sensitivity = model.SensitivityRestricted
		}},
		{"a confidential property is an identity key", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).Properties[0].Sensitivity = model.SensitivityConfidential
		}},
		{"the name of an object is restricted", model.CodeClosedList, func(d *model.Definitions) { withName(d, model.SensitivityRestricted) }},
		{"the name of an object is confidential", model.CodeClosedList, func(d *model.Definitions) { withName(d, model.SensitivityConfidential) }},
		{"the name of an object is restricted in a trait", model.CodeClosedList, func(d *model.Definitions) {
			name := prop("pr.powered.name", model.ObjectNameProperty, "vt.string", model.ChangeSlow)
			name.Sensitivity = model.SensitivityRestricted
			d.Traits[0].Properties = append(d.Traits[0].Properties, name)
		}},
	})
	// the same properties are fine when they are open (or when the grade is not said), and a property of another name may be restricted
	for _, s := range []model.Sensitivity{"", model.SensitivityOpen} {
		d := baseline()
		withName(&d, s)
		asset(&d).Properties[0].Sensitivity = s
		if errs := model.Check(d); len(errs) != 0 {
			t.Errorf("an open name and key (%q) are refused: %v", s, errs)
		}
	}
	d := baseline()
	asset(&d).Properties[2].Sensitivity = model.SensitivityRestricted
	if errs := model.Check(d); len(errs) != 0 {
		t.Errorf("a property that is neither the name nor a key may be restricted: %v", errs)
	}
	d = baseline()
	asset(&d).Properties[0].Sensitivity = model.SensitivityRestricted
	err := model.Validate(d)
	if err == nil || !strings.Contains(err.Error(), "restricted") || !strings.Contains(err.Error(), "identifier") {
		t.Errorf("the message of a restricted key does not say what and why: %v", err)
	}
}
