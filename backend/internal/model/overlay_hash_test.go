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
	set, err := model.DefinitionSetHash(r.Hash(), nil)
	if err != nil || set != r.Hash() {
		t.Errorf("the set hash of no items = %v (%v), want the registry hash", set, err)
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
func TestOverlayHashCoversEveryFieldButNotTheOrder(t *testing.T) {
	// The item hash follows every field, labels included, and ignores the order of properties, aliases and enum values.
	a, b := orgProp("server.a", "a", "vt.string", 100001), enumProp()
	a.Aliases = []string{"org.x", "org.y"}
	base := mustOverlayHash(t, serverOverlay(a, b))
	reordered := a
	reordered.Aliases = []string{"org.y", "org.x"}
	swapped := enumProp()
	swapped.EnumValues = []string{"silver", "gold"}
	if got := mustOverlayHash(t, serverOverlay(swapped, reordered)); got != base {
		t.Errorf("the order changes the hash")
	}
	changes := map[string]func(p *model.Property){
		"name":         func(p *model.Property) { p.Name = "org.other" },
		"display name": func(p *model.Property) { p.DisplayNames = map[string]string{"ko": "가"} },
		"description":  func(p *model.Property) { p.Description = "d" },
		"number":       func(p *model.Property) { p.AttrNo = 100123 },
		"alias":        func(p *model.Property) { p.Aliases = nil },
		"unit":         func(p *model.Property) { p.Unit = "w" },
		"sensitivity":  func(p *model.Property) { p.Sensitivity = model.SensitivityRestricted },
	}
	for name, change := range changes {
		c := a
		change(&c)
		if mustOverlayHash(t, serverOverlay(c, b)) == base {
			t.Errorf("a change of %s does not change the item hash", name)
		}
	}
	if mustOverlayHash(t, model.NewOverlay(model.TargetObjectType, "ot.rack", []model.Property{a, b})) == base {
		t.Errorf("the target is not in the item hash")
	}
}

func mustOverlayHash(t *testing.T, o model.Overlay) kernel.Hash {
	t.Helper()
	h, err := model.OverlayHash(o)
	if err != nil {
		t.Fatalf("OverlayHash() = %v", err)
	}
	return h
}

// [REQ-349]
func TestDefinitionSetHash(t *testing.T) {
	// The set hash follows the standard and the items and not their order or where the set came from.
	std := newBaseline(t).Hash()
	entries, err := model.OverlaySetEntries([]model.Overlay{
		serverOverlay(orgProp("server.a", "a", "vt.string", 100001)),
		model.NewOverlay(model.TargetObjectType, "ot.rack", []model.Property{orgProp("rack.b", "b", "vt.string", 100002)}),
	})
	if err != nil {
		t.Fatalf("OverlaySetEntries() = %v", err)
	}
	hash := func(std kernel.Hash, e []model.SetEntry) kernel.Hash {
		h, err := model.DefinitionSetHash(std, e)
		if err != nil {
			t.Fatalf("DefinitionSetHash() = %v", err)
		}
		return h
	}
	base := hash(std, entries)
	if base == std {
		t.Errorf("a set with items has the hash of the standard")
	}
	if hash(std, []model.SetEntry{entries[1], entries[0]}) != base {
		t.Errorf("the order of the items changes the set hash")
	}
	if hash(std, entries[:1]) == base {
		t.Errorf("an item fewer does not change the set hash")
	}
	if hash(mustHash(t, newBaseline(t).NumberingHash), entries) == base {
		t.Errorf("another standard gives the same set hash")
	}
	changed := []model.SetEntry{entries[0], {ItemID: entries[1].ItemID, ContentHash: entries[0].ContentHash}}
	if hash(std, changed) == base {
		t.Errorf("another content of an item does not change the set hash")
	}
	if entries[0].ItemID != "ov.ot.server" || entries[1].ItemID != "ov.ot.rack" {
		t.Errorf("item ids = %s, %s", entries[0].ItemID, entries[1].ItemID)
	}
}
