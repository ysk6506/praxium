package ingest

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// orgRegistry is the standard model with the properties of a file of organization properties added to PhysicalServer.
func orgRegistry(t *testing.T, properties string) *model.Registry {
	t.Helper()
	base, err := model.StandardDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	file := `{"format":"praxium-org-model","version":1,"overlays":[{"type":"PhysicalServer","properties":[` + properties + `]}]}`
	wanted, err := model.ParseOrgModelFile([]byte(file), base)
	if err != nil {
		t.Fatal(err)
	}
	next := model.OrgNumberBase
	filled := model.FillOverlays(base, nil, wanted, func() int { next++; return next - 1 })
	reg, errs := model.ComposeRegistry(base, filled)
	if len(errs) > 0 {
		t.Fatalf("the organization properties of the test: %v", errs)
	}
	return reg
}

// orgPlan compiles the mapping of the ledger with an attribute named as given against a registry.
func orgPlan(t *testing.T, reg *model.Registry, attribute string) error {
	t.Helper()
	src, err := ParseSource(strings.NewReader(sourceJSON))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(mappingJSON, `{"attribute":"name","column":"asset_id"}`, `{"attribute":"name","column":"asset_id"},{"attribute":"`+attribute+`","column":"power_state"}`, 1)
	m, err := ParseMapping(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(reg, src, m)
	return err
}

const (
	deptProp = `{"name":"org.department","value_type":"string"}`
	oldYear  = `{"id":"pr.org.physical_server.year","name":"org.year","value_type":"integer","status":"deprecated"`
)

// [REQ-370]
func TestAnOrgAttributeCompilesLikeAStandardOne(t *testing.T) {
	reg := orgRegistry(t, deptProp)
	if err := orgPlan(t, reg, "org.department"); err != nil {
		t.Fatalf("an organization property is an attribute of the type: %v", err)
	}
	err := orgPlan(t, reg, "department")
	if err == nil || !strings.Contains(err.Error(), "org.department") || !strings.Contains(err.Error(), "write that") {
		t.Fatalf("a name without the prefix says which organization property is meant: %v", err)
	}
	if err := orgPlan(t, reg, "nothing_like_it"); err == nil || strings.Contains(err.Error(), "write that") {
		t.Fatalf("a name that is no organization property gets no such hint: %v", err)
	}
}

// [REQ-370]
func TestAMappingOntoADeprecatedAttributeIsRefusedWithItsSuccessor(t *testing.T) {
	withSuccessor := deprecatedStandardAttribute(t, "form_factor", "boot_mode")
	err := orgPlan(t, withSuccessor, "form_factor")
	if err == nil || !strings.Contains(err.Error(), "form_factor is deprecated") || !strings.Contains(err.Error(), "use boot_mode instead") {
		t.Fatalf("a deprecated attribute names what replaces it: %v", err)
	}
	if err := orgPlan(t, withSuccessor, "boot_mode"); err != nil {
		t.Fatalf("the successor works: %v", err)
	}
	alone := orgRegistry(t, oldYear+`}`)
	if err := orgPlan(t, alone, "org.year"); err == nil || !strings.Contains(err.Error(), "nothing replaces it") {
		t.Fatalf("without a successor the way out is said: %v", err)
	}
}

// [REQ-370]
func TestTheOldNameOfARenamedAttributeStillMaps(t *testing.T) {
	reg := orgRegistry(t, `{"name":"org.owning_department","aliases":["org.department"],"value_type":"string"}`)
	for _, name := range []string{"org.department", "org.owning_department"} {
		if err := orgPlan(t, reg, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// [REQ-370]
func TestAnOrgPropertyCannotBePartOfAnIdentityKey(t *testing.T) {
	reg := orgRegistry(t, deptProp)
	src, _ := ParseSource(strings.NewReader(sourceJSON))
	text := strings.Replace(mappingJSON, `{"property":"manufacturer","column":"manufacturer"}`, `{"property":"org.department","column":"manufacturer"}`, 1)
	m, err := ParseMapping(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(reg, src, m)
	if err == nil || !strings.Contains(err.Error(), "org.department is an organization property") || !strings.Contains(err.Error(), "lose their history") {
		t.Fatalf("the reason is said: %v", err)
	}
}

// deprecatedStandardAttribute is a model in which a property of PhysicalServer is deprecated in favour of another (the
// standard model has none yet; an organization property is retired without a successor).
func deprecatedStandardAttribute(t *testing.T, old, successor string) *model.Registry {
	t.Helper()
	base, err := model.StandardDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	defs := base.Clone()
	var next model.PropertyID
	for i := range defs.ObjectTypes {
		if defs.ObjectTypes[i].Name != "PhysicalServer" {
			continue
		}
		props := defs.ObjectTypes[i].Properties
		for _, p := range props {
			if p.Name == successor {
				next = p.ID
			}
		}
		for j := range props {
			if props[j].Name == old {
				props[j].Status, props[j].SupersededBy = model.StatusDeprecated, next
			}
		}
	}
	reg, errs := model.ComposeRegistry(defs, nil)
	if len(errs) > 0 || next.IsZero() {
		t.Fatalf("the test model: %v", errs)
	}
	return reg
}
