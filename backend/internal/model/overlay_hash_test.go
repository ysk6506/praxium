package model_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The hash of the standard model; the organisation model never changes it when there is none.
const standardModelHash = "712b5ae602cd347af9b4eecaa975b3637b5e96ccaa9d8682064d31749a07f98a"

// [REQ-346]
func TestStandardHashIsTheSameWithNoOverlay(t *testing.T) {
	// Composing nothing, and the field a property now has for a namespace, leave the hash of the standard model as it was.
	std, err := model.StandardDefinitions()
	if err != nil {
		t.Fatalf("StandardDefinitions() = %v", err)
	}
	r, errs := model.ComposeRegistry(std, nil)
	if len(errs) != 0 {
		t.Fatalf("ComposeRegistry() = %v", errs)
	}
	if got := r.Hash().String(); got != standardModelHash {
		t.Errorf("model hash = %s, want %s", got, standardModelHash)
	}
}

// [REQ-346]
func TestOrgPropertyChangesTheModelHashButNotItsName(t *testing.T) {
	// A new property changes the model hash; renaming it, its display names and its description do not; its meaning does. The numbering hash follows the number.
	build := func(damage ...func(p *model.Property)) *model.Registry {
		p := orgProp("server.dept", "dept", "vt.string", 100001)
		for _, d := range damage {
			d(&p)
		}
		r, errs := model.ComposeRegistry(baseline(), []model.Overlay{serverOverlay(p)})
		if len(errs) != 0 {
			t.Fatalf("ComposeRegistry() = %v", errs)
		}
		return r
	}
	plain, none := build(), newBaseline(t)
	if plain.Hash() == none.Hash() {
		t.Errorf("adding a property does not change the model hash")
	}
	renamed := build(func(p *model.Property) {
		p.Name, p.Aliases = "org.department", []string{"org.dept"}
		p.DisplayNames, p.Description = map[string]string{"ko": "부서"}, "text"
	})
	if renamed.Hash() != plain.Hash() {
		t.Errorf("a rename and labels change the model hash")
	}
	if ref, ok := renamed.Property(prID("pr.org.server.dept")); !ok || ref.Property.Name != "org.department" || ref.Property.AttrNo != 100001 {
		t.Errorf("renamed property = %+v", ref)
	}
	for name, damage := range map[string]func(p *model.Property){
		"value type":  func(p *model.Property) { p.ValueType = vtID("vt.integer") },
		"sensitivity": func(p *model.Property) { p.Sensitivity = model.SensitivityConfidential },
		"status":      func(p *model.Property) { p.Status = model.StatusDeprecated },
		"change":      func(p *model.Property) { p.ChangeClass = model.ChangeStatic },
	} {
		if build(damage).Hash() == plain.Hash() {
			t.Errorf("a change of %s does not change the model hash", name)
		}
	}
	renumbered := build(func(p *model.Property) { p.AttrNo = 100777 })
	if renumbered.Hash() != plain.Hash() {
		t.Errorf("a number is in the model hash")
	}
	if mustHash(t, renumbered.NumberingHash) == mustHash(t, plain.NumberingHash) {
		t.Errorf("a changed number does not change the numbering hash")
	}
}

// [REQ-346]
func TestRowHashDoesNotSeeNames(t *testing.T) {
	// A row names a property by its id and number: the same values hash alike under any name of the property.
	hashRow := func(name string) string {
		p := orgProp("server.dept", "dept", "vt.string", 100001)
		p.Name = "org." + name
		r, errs := model.ComposeRegistry(baseline(), []model.Overlay{serverOverlay(p)})
		if len(errs) != 0 {
			t.Fatalf("ComposeRegistry() = %v", errs)
		}
		ref, _ := r.PropertyByNo(100001)
		h, err := kernel.ContentHash(map[string]any{"id": ref.Property.ID.String(), "no": ref.Property.AttrNo, "value": "A"})
		if err != nil {
			t.Fatalf("ContentHash() = %v", err)
		}
		return h.String()
	}
	if hashRow("dept") != hashRow("department") {
		t.Errorf("the name of the property is in the hash of a row")
	}
}

// [REQ-349]
func TestOverlayBodyIsCanonical(t *testing.T) {
	// One overlay gives one body, whatever the order it was written in; the body is the normal form of the definition store (sorted keys, no white space, no HTML escapes) and follows every field.
	a, b := orgProp("server.a", "a", "vt.string", 100001), enumProp()
	a.Aliases = []string{"org.y", "org.x"}
	a.Description = "a < b & c"
	b.EnumValues = []string{"silver", "gold"}
	body, err := serverOverlay(a, b).Body()
	if err != nil {
		t.Fatalf("Body() = %v", err)
	}
	const want = `{"kind":"type_overlay","properties":[{"aliases":["org.x","org.y"],"attr_no":100001,"change_class":"slow","description":"a < b & c","id":"pr.org.server.a","name":"org.a","value_type":"vt.string"},` +
		`{"attr_no":100001,"change_class":"slow","enum_values":["gold","silver"],"id":"pr.org.server.grade","name":"org.grade","value_type":"vt.enum"}],"schema_version":1,"target":{"id":"ot.server","kind":"object_type"}}`
	if string(body) != want {
		t.Errorf("Body() =\n%s\nwant\n%s", body, want)
	}
	swapped, _ := serverOverlay(b, a).Body()
	if string(swapped) != want {
		t.Errorf("the order of the properties changes the body")
	}
	changes := map[string]func(p *model.Property){
		"name":         func(p *model.Property) { p.Name = "org.other" },
		"display name": func(p *model.Property) { p.DisplayNames = map[string]string{"ko": "가"} },
		"description":  func(p *model.Property) { p.Description = "d" },
		"number":       func(p *model.Property) { p.AttrNo = 100123 },
		"alias":        func(p *model.Property) { p.Aliases = nil },
		"sensitivity":  func(p *model.Property) { p.Sensitivity = model.SensitivityRestricted },
	}
	for name, change := range changes {
		c := a
		change(&c)
		if other, _ := serverOverlay(c, b).Body(); string(other) == want {
			t.Errorf("a change of %s does not change the body", name)
		}
	}
	if other, _ := model.NewOverlay(model.TargetObjectType, "ot.rack", []model.Property{a, b}).Body(); string(other) == want {
		t.Errorf("the target is not in the body")
	}
}
