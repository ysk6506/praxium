package model_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-342] [REQ-343]
func TestOverlayRefusesWhatItDoesNotKnow(t *testing.T) {
	// A wrong sensitivity, status, change class, schema or kind, a missing target id or name, and a display name without a language are each an error.
	with := func(damage func(o *model.Overlay)) []*kernel.Error {
		o := serverOverlay(orgProp("server.dept", "dept", "vt.string", 100001))
		damage(&o)
		return model.CheckOverlay(o)
	}
	tests := map[string]struct {
		damage func(o *model.Overlay)
		code   kernel.Code
	}{
		"sensitivity":     {func(o *model.Overlay) { o.Properties[0].Sensitivity = "secret" }, model.CodeClosedList},
		"status":          {func(o *model.Overlay) { o.Properties[0].Status = "gone" }, model.CodeClosedList},
		"change class":    {func(o *model.Overlay) { o.Properties[0].ChangeClass = "fast" }, model.CodeOverlayField},
		"schema version":  {func(o *model.Overlay) { o.SchemaVersion = 2 }, model.CodeDataInvalid},
		"kind":            {func(o *model.Overlay) { o.Kind = "type" }, model.CodeDataInvalid},
		"no target id":    {func(o *model.Overlay) { o.Target.ID = "" }, model.CodeOverlayTarget},
		"no name":         {func(o *model.Overlay) { o.Properties[0].Name = "" }, model.CodeOverlayName},
		"empty language":  {func(o *model.Overlay) { o.Properties[0].DisplayNames = map[string]string{"": "x"} }, model.CodeOverlayField},
		"empty text":      {func(o *model.Overlay) { o.Properties[0].DisplayNames = map[string]string{"ko": ""} }, model.CodeOverlayField},
		"too many in one": {func(o *model.Overlay) { o.Properties = append(o.Properties, make([]model.Property, 100)...) }, model.CodeOverlayLimit},
	}
	for name, tt := range tests {
		if errs := with(tt.damage); !hasCode(errs, tt.code) {
			t.Errorf("%s: CheckOverlay() = %v, want the code %s", name, errs, tt.code)
		}
	}
}

// [REQ-343] [REQ-348]
func TestPropertyNamingErrors(t *testing.T) {
	// A property of the standard model with no name, a bad alias or a namespace that is not one is refused; ext names have their own form.
	d := baseline()
	p := &objectType(&d, "ot.server").Properties[0]
	p.Name = ""
	if errs := model.Check(d); !hasCode(errs, model.CodeRequiredField) {
		t.Errorf("a property without a name: %v", errs)
	}
	p.Name, p.Aliases = "hostname", []string{"Host.Name"}
	if errs := model.Check(d); !hasCode(errs, model.CodeNameInvalid) {
		t.Errorf("a bad alias: %v", errs)
	}
	p.Aliases, p.Namespace = nil, model.Namespace("other")
	if errs := model.Check(d); !hasCode(errs, model.CodeClosedList) {
		t.Errorf("an unknown namespace: %v", errs)
	}
	if model.ValidPropertyName(model.Namespace("other"), "other.a") || model.ValidPropertyName(model.NamespaceExt, "org.a") {
		t.Errorf("a name is valid in a namespace it does not belong to")
	}
}

// [REQ-342] [REQ-349]
func TestOverlayBodyKeepsASuccessorSoThatItCanBeRefused(t *testing.T) {
	// A successor is refused by CheckOverlay, and the body still writes it, so that what is stored is what was given.
	o := serverOverlay(orgProp("server.dept", "dept", "vt.string", 100001))
	o.Properties[0].SupersededBy = prID("pr.org.server.other")
	body, err := o.Body()
	if err != nil || !strings.Contains(string(body), `"superseded_by":"pr.org.server.other"`) {
		t.Fatalf("Body() = %s, %v", body, err)
	}
	back, err := model.ParseOverlay(body)
	if err != nil || back.Properties[0].SupersededBy != o.Properties[0].SupersededBy {
		t.Errorf("ParseOverlay() = %+v, %v", back, err)
	}
}

// [REQ-345]
func TestComposeRegistryReportsWhatTheOrdinaryChecksFind(t *testing.T) {
	// A problem that only the ordinary checks see (a quantity without a unit, a name twice) comes back as errors, and a text the hash cannot hold as one error.
	noUnit := orgProp("server.power", "power", "vt.quantity", 100001)
	if _, errs := model.ComposeRegistry(baseline(), []model.Overlay{serverOverlay(noUnit)}); !hasCode(errs, model.CodeUnit) {
		t.Errorf("a quantity without a unit: %v", errs)
	}
	badUnit := noUnit
	badUnit.Unit = "\xff"
	if _, errs := model.ComposeRegistry(baseline(), []model.Overlay{serverOverlay(badUnit)}); len(errs) != 1 {
		t.Errorf("a unit that is not text: %v", errs)
	}
	if _, err := model.OverlaySetEntries([]model.Overlay{serverOverlay(badUnit)}); err == nil {
		t.Errorf("OverlaySetEntries() accepts a unit that is not text")
	}
}

// [REQ-347]
func TestClassifyNamesTheValuesThatChange(t *testing.T) {
	// The detail of a change to an enum lists every value, in order.
	next := enumProp()
	next.EnumValues = []string{"gold", "bronze", "tin"}
	got := model.ClassifyOverlay(serverOverlay(enumProp()), serverOverlay(next))
	if len(got) != 2 || !strings.Contains(got[0].Detail, "silver") || !strings.Contains(got[1].Detail, "bronze, tin") {
		t.Errorf("ClassifyOverlay() = %+v", got)
	}
}

// [REQ-343] [REQ-345]
func TestWarningsForATraitTarget(t *testing.T) {
	// A property of a trait that looks like one the trait has is warned about too; a target that does not exist has no warning.
	trait := model.NewOverlay(model.TargetTrait, "tr.powered", []model.Property{orgProp("powered.rated_power", "rated_power_w", "vt.number", 100001)})
	ghost := model.NewOverlay(model.TargetObjectType, "ot.nothing", []model.Property{orgProp("x.y", "y", "vt.string", 100002)})
	missing := model.NewOverlay(model.TargetTrait, "tr.nothing", []model.Property{orgProp("x.z", "z", "vt.string", 100003)})
	warnings := model.OverlayWarnings(baseline(), []model.Overlay{trait, ghost, missing})
	if len(warnings) != 1 || warnings[0].ItemID != "ov.tr.powered" || !strings.Contains(warnings[0].Message, "rated_power_w") {
		t.Errorf("OverlayWarnings() = %+v", warnings)
	}
}

// [REQ-342]
func TestOrgModelFileTargetsAndIds(t *testing.T) {
	// A type is found by an alias too; a bad successor id is refused; an id that cannot be made is left empty and refused by the check.
	d := baseline()
	objectType(&d, "ot.server").Aliases = []string{"Machine"}
	text := `{"format":"praxium-org-model","version":1,"overlays":[{"type":"Machine","properties":[{"name":"org.a","value_type":"string"}]}]}`
	items, err := model.ParseOrgModelFile([]byte(text), d)
	if err != nil || items[0].Target.ID != "ot.server" {
		t.Fatalf("ParseOrgModelFile() = %+v, %v", items, err)
	}
	bad := `{"format":"praxium-org-model","version":1,"overlays":[{"type":"Server","properties":[{"name":"org.a","value_type":"string","superseded_by":"!"}]}]}`
	if _, err := model.ParseOrgModelFile([]byte(bad), d); kernel.CodeOf(err) != model.CodeOverlayField {
		t.Errorf("a bad successor id: %v", err)
	}
	long := serverOverlay(model.Property{Name: "org." + strings.Repeat("a", 130), ValueType: vtID("vt.string"), ChangeClass: model.ChangeSlow})
	filled := model.FillOverlays(d, nil, []model.Overlay{long}, counter(100001))
	if !filled[0].Properties[0].ID.IsZero() || !hasCode(model.CheckOverlay(filled[0]), model.CodeRequiredField) {
		t.Errorf("an id too long was made: %+v", filled[0].Properties[0].ID)
	}
	byTrait := model.NewOverlay(model.TargetTrait, "tr.powered", []model.Property{orgProp("powered.x", "x", "vt.string", 100001)})
	if out, err := model.FormatOrgModelFile(d, []model.Overlay{byTrait}); err != nil || !strings.Contains(string(out), `"trait": "Powered"`) {
		t.Errorf("FormatOrgModelFile() = %s, %v", out, err)
	}
	if _, err := model.FormatOrgModelFile(d, []model.Overlay{model.NewOverlay(model.TargetTrait, "tr.nothing", nil)}); err == nil {
		t.Errorf("a trait that does not exist is written")
	}
}
