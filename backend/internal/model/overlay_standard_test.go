package model_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-343] [REQ-344]
func TestStandardPropertiesStayOutOfTheOrganisationSpace(t *testing.T) {
	// No standard property name has a dot and no standard property number reaches 100000, so an organisation can never meet one: names with org. and numbers from 100000 are its own.
	r := loadStd(t)
	count := 0
	check := func(owner string, props []model.Property) {
		for _, p := range props {
			count++
			if strings.Contains(p.Name, ".") || p.Namespace != "" {
				t.Errorf("%s: property %s has the name %q and namespace %q", owner, p.ID, p.Name, p.Namespace)
			}
			if p.AttrNo < 1 || p.AttrNo >= model.OrgNumberBase {
				t.Errorf("%s: property %s has the number %d, outside 1 to %d", owner, p.ID, p.AttrNo, model.OrgNumberBase-1)
			}
			for _, alias := range p.Aliases {
				if strings.Contains(alias, ".") {
					t.Errorf("%s: property %s has the alias %q", owner, p.ID, alias)
				}
			}
		}
	}
	defs := r.Definitions()
	for _, t := range defs.Traits {
		check(t.ID.String(), t.Properties)
	}
	for _, o := range defs.ObjectTypes {
		check(o.ID.String(), o.Properties)
	}
	for _, l := range defs.LinkTypes {
		check(l.ID.String(), l.Properties)
	}
	if count < 500 {
		t.Errorf("only %d standard properties were checked", count)
	}
	if next := r.Next().AttrNo; next >= model.OrgNumberBase || r.NextOrgAttrNo() != model.OrgNumberBase {
		t.Errorf("the next numbers are %d for the standard and %d for an organisation", next, r.NextOrgAttrNo())
	}
}

// [REQ-343] [REQ-345]
func TestStandardFilesAreRefusedAnOrganisationProperty(t *testing.T) {
	// An org property in the standard files is not a standard property: the load of the standard refuses it.
	d, err := model.StandardDefinitions()
	if err != nil {
		t.Fatalf("StandardDefinitions() = %v", err)
	}
	if err := model.RequireStandardForTest(d); err != nil {
		t.Fatalf("the standard files: %v", err)
	}
	d.ObjectTypes[0].Properties = append(d.ObjectTypes[0].Properties, model.Property{ID: prID("pr.org.x"), Namespace: model.NamespaceOrg})
	if err := model.RequireStandardForTest(d); err == nil {
		t.Errorf("an org property in the standard files is accepted")
	}
}

// [REQ-345] [REQ-346]
func TestOverlayOnTheStandardModel(t *testing.T) {
	// An organisation property on a standard type and on the Common trait reaches every type that has them; the standard registry is not changed, and numbers stay.
	std, err := model.StandardDefinitions()
	if err != nil {
		t.Fatalf("StandardDefinitions() = %v", err)
	}
	items := []model.Overlay{
		model.NewOverlay(model.TargetObjectType, "ot.physical_server", []model.Property{orgProp("physical_server.dept", "management_department", "vt.string", 100001)}),
		model.NewOverlay(model.TargetTrait, "tr.common", []model.Property{orgProp("common.owner", "owner_team", "vt.string", 100002)}),
	}
	r, errs := model.ComposeRegistry(std, items)
	if len(errs) != 0 {
		t.Fatalf("ComposeRegistry() = %v", errs)
	}
	server, _ := r.ObjectTypeByName("PhysicalServer")
	if eff := r.EffectiveProperties(server.ID); !hasProp(eff, "org.management_department") || !hasProp(eff, "org.owner_team") {
		t.Errorf("PhysicalServer lacks the added properties")
	}
	plain := loadStd(t)
	with := 0
	for _, ot := range plain.ObjectTypes() {
		if len(r.EffectiveProperties(ot.ID)) > len(plain.EffectiveProperties(ot.ID)) {
			with++
		}
	}
	if with < 100 {
		t.Errorf("only %d types got a property of the Common trait", with)
	}
	if plain.Hash().String() != standardModelHash || r.Hash() == plain.Hash() {
		t.Errorf("the hashes: standard %s, with overlays %s", plain.Hash(), r.Hash())
	}
	if a, b := mustHash(t, plain.NumberingHash), mustHash(t, r.NumberingHash); a == b {
		t.Errorf("the numbering hash does not see the organisation numbers")
	}
	for _, p := range plain.EffectiveProperties(server.ID) {
		if after, ok := r.PropertyByNo(p.AttrNo); !ok || after.Property.ID != p.ID {
			t.Errorf("the number %d is not %s any more", p.AttrNo, p.ID)
		}
	}
}
