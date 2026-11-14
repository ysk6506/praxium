package model_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// orgProp is a good organisation property of the given value type.
func orgProp(id, name, vt string, no int) model.Property {
	p := prop("pr.org."+id, "org."+name, vt, model.ChangeSlow)
	p.AttrNo = no
	return p
}

func serverOverlay(props ...model.Property) model.Overlay {
	return model.NewOverlay(model.TargetObjectType, "ot.server", props)
}

func composeErrs(t *testing.T, items ...model.Overlay) []*kernel.Error {
	t.Helper()
	_, errs := model.ComposeOverlays(baseline(), items)
	return errs
}

// [REQ-342] [REQ-345]
func TestComposeAddsOrgPropertiesToATypeAndATrait(t *testing.T) {
	// An overlay's properties are added to the target with the namespace org; both a type and a trait take them.
	onType := serverOverlay(orgProp("server.dept", "management_department", "vt.string", 100001))
	onTrait := model.NewOverlay(model.TargetTrait, "tr.powered", []model.Property{orgProp("powered.owner", "owner_team", "vt.string", 100002)})
	r, errs := model.ComposeRegistry(baseline(), []model.Overlay{onType, onTrait})
	if len(errs) != 0 {
		t.Fatalf("ComposeRegistry() = %v", errs)
	}
	ref, ok := r.PropertyByNo(100001)
	if !ok || ref.Property.Name != "org.management_department" || ref.Property.Namespace != model.NamespaceOrg || ref.Owner != "ot.server" {
		t.Errorf("number 100001 is %+v", ref)
	}
	found := map[string]bool{}
	for _, p := range r.EffectiveProperties(otID("ot.server")) {
		found[p.Name] = true
	}
	if !found["org.management_department"] || !found["org.owner_team"] || !found["hostname"] {
		t.Errorf("ot.server has %v: want its own, the standard and the one of the trait", found)
	}
	for _, id := range []string{"ot.acme_server"} { // a subtype has them too
		eff := r.EffectiveProperties(otID(id))
		if !hasProp(eff, "org.management_department") {
			t.Errorf("%s does not inherit the added property", id)
		}
	}
	if r.NextOrgAttrNo() != 100003 || r.Next().AttrNo != 10 {
		t.Errorf("next numbers = %d for an organisation property and %d for a standard one", r.NextOrgAttrNo(), r.Next().AttrNo)
	}
}

func hasProp(props []model.Property, name string) bool {
	for _, p := range props {
		if p.Name == name {
			return true
		}
	}
	return false
}

// [REQ-342] [REQ-345]
func TestComposeDoesNotChangeItsInput(t *testing.T) {
	// The standard definitions and the overlay come out of a composition as they went in.
	base := baseline()
	o := serverOverlay(orgProp("server.dept", "management_department", "vt.string", 100001))
	if _, errs := model.ComposeOverlays(base, []model.Overlay{o}); len(errs) != 0 {
		t.Fatalf("ComposeOverlays() = %v", errs)
	}
	if len(objectType(&base, "ot.server").Properties) != 1 || o.Properties[0].Namespace != model.NamespaceOrg {
		t.Errorf("the input changed")
	}
	if got, errs := model.ComposeOverlays(base, nil); len(errs) != 0 || len(got.ObjectTypes) != len(base.ObjectTypes) {
		t.Errorf("composing nothing gives %d types and %v", len(got.ObjectTypes), errs)
	}
}

// [REQ-343] [REQ-344] [REQ-345]
func TestOverlayErrors(t *testing.T) {
	// Each way an overlay can be wrong has its code.
	good := func() model.Property { return orgProp("server.dept", "management_department", "vt.string", 100001) }
	with := func(damage func(p *model.Property)) model.Overlay {
		p := good()
		damage(&p)
		return serverOverlay(p)
	}
	tests := []struct {
		name string
		item model.Overlay
		code kernel.Code
	}{
		{"name without the namespace", with(func(p *model.Property) { p.Name = "management_department" }), model.CodeOverlayName},
		{"name in capitals", with(func(p *model.Property) { p.Name = "org.Management" }), model.CodeOverlayName},
		{"name with a double underscore", with(func(p *model.Property) { p.Name = "org.a__b" }), model.CodeOverlayName},
		{"name over 64 characters", with(func(p *model.Property) { p.Name = "org." + strings.Repeat("a", 61) }), model.CodeOverlayName},
		{"alias without the namespace", with(func(p *model.Property) { p.Aliases = []string{"dept"} }), model.CodeOverlayName},
		{"id outside pr.org.", with(func(p *model.Property) { p.ID = prID("pr.server.dept") }), model.CodeOverlayName},
		{"no id", with(func(p *model.Property) { p.ID = model.PropertyID{} }), model.CodeRequiredField},
		{"no number", with(func(p *model.Property) { p.AttrNo = 0 }), model.CodeOverlayNumber},
		{"number of the standard", with(func(p *model.Property) { p.AttrNo = 99999 }), model.CodeOverlayNumber},
		{"required", with(func(p *model.Property) { p.Required = true }), model.CodeOverlayField},
		{"scope key", with(func(p *model.Property) { p.ScopeKey = true }), model.CodeOverlayField},
		{"history kind", with(func(p *model.Property) { p.HistoryKind = model.HistoryVolatile }), model.CodeOverlayField},
		{"dynamic", with(func(p *model.Property) { p.ChangeClass = model.ChangeDynamic }), model.CodeOverlayField},
		{"successor", with(func(p *model.Property) { p.SupersededBy = prID("pr.org.server.other") }), model.CodeOverlayField},
		{"reference", with(func(p *model.Property) { p.ValueType = vtID("vt.ref") }), model.CodeOverlayValueType},
		{"list", with(func(p *model.Property) { p.ValueType = vtID("vt.list") }), model.CodeOverlayValueType},
		{"map", with(func(p *model.Property) { p.ValueType = vtID("vt.map") }), model.CodeOverlayValueType},
		{"description too long", with(func(p *model.Property) { p.Description = strings.Repeat("가", 1001) }), model.CodeOverlayLimit},
		{"display name too long", with(func(p *model.Property) { p.DisplayNames = map[string]string{"ko": strings.Repeat("a", 101)} }), model.CodeOverlayLimit},
		{"too many enum values", with(func(p *model.Property) {
			p.ValueType = vtID("vt.enum")
			for i := 0; i < 101; i++ {
				p.EnumValues = append(p.EnumValues, "v"+strings.Repeat("a", i%5)+string(rune('a'+i%26))+strings.Repeat("b", i/26))
			}
		}), model.CodeOverlayLimit},
		{"enum value too long", with(func(p *model.Property) {
			p.ValueType, p.EnumValues = vtID("vt.enum"), []string{strings.Repeat("a", 65)}
		}), model.CodeOverlayLimit},
		{"target does not exist", model.NewOverlay(model.TargetObjectType, "ot.nothing", []model.Property{good()}), model.CodeOverlayTarget},
		{"trait does not exist", model.NewOverlay(model.TargetTrait, "tr.nothing", []model.Property{good()}), model.CodeOverlayTarget},
		{"target kind unknown", model.NewOverlay("link_type", "lt.runs_on", []model.Property{good()}), model.CodeOverlayTarget},
		{"target is not standard", model.NewOverlay(model.TargetObjectType, "ot.acme_server", []model.Property{good()}), model.CodeOverlayTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if errs := composeErrs(t, tt.item); !hasCode(errs, tt.code) {
				t.Errorf("ComposeOverlays() = %v, want the code %s", errs, tt.code)
			}
		})
	}
}

// [REQ-345]
func TestOverlayCannotTargetSys(t *testing.T) {
	// A sys type is the product's own: an overlay on it is refused, with its reason.
	d := baseline()
	sys := objectType(&d, "ot.rack")
	sys.Namespace, sys.Layer = model.NamespaceSys, model.LayerSystem
	o := model.NewOverlay(model.TargetObjectType, "ot.rack", []model.Property{orgProp("rack.x", "x", "vt.string", 100001)})
	_, errs := model.ComposeOverlays(d, []model.Overlay{o})
	if !hasCode(errs, model.CodeOverlayTarget) || !strings.Contains(errs[0].Message, "sys") {
		t.Errorf("ComposeOverlays() = %v", errs)
	}
}

// [REQ-345]
func TestOverlayLimitsAndRepeats(t *testing.T) {
	// 100 properties for one target and 1000 in all; a target or an id twice is an error.
	many := func(n int, prefix string) []model.Property {
		out := make([]model.Property, n)
		for i := range out {
			name := prefix + strings.Repeat("x", i/26) + string(rune('a'+i%26))
			out[i] = orgProp(prefix+"."+name, name, "vt.string", 100001+i)
		}
		return out
	}
	if errs := composeErrs(t, serverOverlay(many(101, "s")...)); !hasCode(errs, model.CodeOverlayLimit) {
		t.Errorf("101 properties: %v", errs)
	}
	if errs := composeErrs(t, serverOverlay(many(100, "s")...)); len(errs) != 0 {
		t.Errorf("100 properties: %v", errs)
	}
	var items []model.Overlay
	for i, target := range []string{"ot.asset", "ot.server", "ot.rack", "ot.cluster", "ot.redundancy_group", "tr.powered", "tr.rack_mountable", "ot.asset", "ot.server", "ot.rack", "ot.cluster"} {
		kind := model.TargetObjectType
		if strings.HasPrefix(target, "tr.") {
			kind = model.TargetTrait
		}
		items = append(items, model.NewOverlay(kind, target, many(100, string(rune('a'+i)))))
	}
	if errs := composeErrs(t, items...); !hasCode(errs, model.CodeOverlayLimit) {
		t.Errorf("1100 properties: %v", errs)
	}
	twice := []model.Overlay{serverOverlay(orgProp("s.a", "a", "vt.string", 100001)), serverOverlay(orgProp("s.b", "b", "vt.string", 100002))}
	if errs := composeErrs(t, twice...); !hasCode(errs, model.CodeOverlayDuplicate) {
		t.Errorf("a target twice: %v", errs)
	}
	sameID := serverOverlay(orgProp("s.a", "a", "vt.string", 100001), orgProp("s.a", "b", "vt.string", 100002))
	if errs := composeErrs(t, sameID); !hasCode(errs, model.CodeOverlayDuplicate) {
		t.Errorf("an id twice: %v", errs)
	}
}

// [REQ-343] [REQ-345]
func TestOverlayNameClashesInOneLineage(t *testing.T) {
	// Two org properties of one type, inherited or from a trait, may not share a name or an alias; the same name on two unrelated types is fine.
	dup := func(items ...model.Overlay) bool {
		_, errs := model.ComposeRegistry(baseline(), items)
		return hasCode(errs, model.CodePropertyDuplicate)
	}
	asset := model.NewOverlay(model.TargetObjectType, "ot.asset", []model.Property{orgProp("asset.dept", "dept", "vt.string", 100001)})
	server := serverOverlay(orgProp("server.dept", "dept", "vt.string", 100002))
	rack := model.NewOverlay(model.TargetObjectType, "ot.rack", []model.Property{orgProp("rack.dept", "dept", "vt.string", 100003)})
	trait := model.NewOverlay(model.TargetTrait, "tr.powered", []model.Property{orgProp("powered.dept", "dept", "vt.string", 100004)})
	if !dup(asset, server) {
		t.Errorf("a type and its ancestor with one name are accepted")
	}
	if !dup(server, trait) {
		t.Errorf("a type and its trait with one name are accepted")
	}
	if dup(server, rack) {
		t.Errorf("one name on two unrelated types is refused")
	}
	aliased := serverOverlay(orgProp("server.owner", "owner", "vt.string", 100005))
	aliased.Properties[0].Aliases = []string{"org.dept"}
	if !dup(asset, aliased) {
		t.Errorf("an alias that is the name of an ancestor's property is accepted")
	}
	if errs := model.Check(baseline()); len(errs) != 0 {
		t.Errorf("baseline: %v", errs)
	}
	_, errs := model.ComposeRegistry(baseline(), []model.Overlay{server, rack})
	if len(errs) != 0 {
		t.Errorf("ComposeRegistry() = %v", errs)
	}
}

// [REQ-344]
func TestNumbersOfTwoRanges(t *testing.T) {
	// A number is of the standard's range or of an organisation's, and the missing ones are given in their own.
	d := baseline()
	org := orgProp("server.dept", "dept", "vt.string", 100005)
	org.Namespace = model.NamespaceOrg
	t1 := objectType(&d, "ot.server")
	t1.Properties = append(t1.Properties, org)
	more := org
	more.ID, more.Name, more.AttrNo = prID("pr.org.server.team"), "org.team", 0
	t1.Properties = append(t1.Properties, more)
	std := prop("pr.server.vendor", "vendor", "vt.string", model.ChangeSlow)
	t1.Properties = append(t1.Properties, std)
	r, err := model.NewRegistry(d)
	if err != nil {
		t.Fatalf("NewRegistry() = %v", err)
	}
	if ref, _ := r.Property(prID("pr.org.server.team")); ref.Property.AttrNo != 100006 {
		t.Errorf("an organisation property without a number gets %d, want 100006", ref.Property.AttrNo)
	}
	if ref, _ := r.Property(prID("pr.server.vendor")); ref.Property.AttrNo != 10 {
		t.Errorf("a standard property without a number gets %d, want 10", ref.Property.AttrNo)
	}
	for name, damage := range map[string]func(d *model.Definitions){
		"organisation number in the low range": func(d *model.Definitions) { property(objectType(d, "ot.server"), "pr.org.server.dept").AttrNo = 99999 },
		"standard number in the high range":    func(d *model.Definitions) { property(objectType(d, "ot.server"), "pr.server.vendor").AttrNo = 100000 },
	} {
		broken := d.Clone()
		damage(&broken)
		if errs := model.Check(broken); !hasCode(errs, model.CodeNumber) {
			t.Errorf("%s: Check() = %v", name, errs)
		}
	}
}

// [REQ-343] [REQ-348]
func TestPropertyNamespaceRules(t *testing.T) {
	// A property of org says org. in its name and a standard one has no dot; a namespace other than org and ext is refused.
	d := baseline()
	p := &objectType(&d, "ot.server").Properties[0]
	p.Name = "org.hostname"
	if errs := model.Check(d); !hasCode(errs, model.CodeNameInvalid) {
		t.Errorf("a dotted name without a namespace: %v", errs)
	}
	p.Name, p.Namespace = "hostname", model.NamespaceOrg
	if errs := model.Check(d); !hasCode(errs, model.CodeNameInvalid) {
		t.Errorf("an org property without org.: %v", errs)
	}
	p.Namespace = model.NamespaceStd
	if errs := model.Check(d); !hasCode(errs, model.CodeClosedList) {
		t.Errorf("an explicit std namespace: %v", errs)
	}
	for _, ok := range []string{"org.a", "org.a_b2", "ext.a"} {
		if ns := model.Namespace(ok[:3]); !model.ValidPropertyName(ns, ok) {
			t.Errorf("%s is refused", ok)
		}
	}
	for _, bad := range []string{"org.", "org._a", "org.a_", "org.A", "org.1a", "org.a.b", "orgx.a"} {
		if model.ValidPropertyName(model.NamespaceOrg, bad) {
			t.Errorf("%s is accepted", bad)
		}
	}
}

// [REQ-348]
func TestOrgPropertyIsRetiredWithoutASuccessor(t *testing.T) {
	// An org property may be deprecated with nobody to follow it; a standard one still must name its successor.
	deprecated := orgProp("server.dept", "dept", "vt.string", 100001)
	deprecated.Status = model.StatusDeprecated
	if _, errs := model.ComposeRegistry(baseline(), []model.Overlay{serverOverlay(deprecated)}); len(errs) != 0 {
		t.Errorf("a deprecated org property: %v", errs)
	}
	d := baseline()
	property(objectType(&d, "ot.server"), "pr.server.hostname").Status = model.StatusDeprecated
	if errs := model.Check(d); !hasCode(errs, model.CodeSuperseded) {
		t.Errorf("a deprecated standard property without a successor: %v", errs)
	}
}

// [REQ-343] [REQ-346] [REQ-348]
func TestOverlayWarnings(t *testing.T) {
	// A deprecated target and a name like a standard property are warnings, not errors.
	d := baseline()
	objectType(&d, "ot.rack").Status = model.StatusDeprecated
	objectType(&d, "ot.rack").SupersededBy = otID("ot.server")
	rack := model.NewOverlay(model.TargetObjectType, "ot.rack", []model.Property{orgProp("rack.x", "x", "vt.string", 100001)})
	server := serverOverlay(orgProp("server.host_name", "host_name", "vt.string", 100002), orgProp("server.vendor", "vendor", "vt.string", 100003))
	if _, errs := model.ComposeRegistry(d, []model.Overlay{rack, server}); len(errs) != 0 {
		t.Fatalf("ComposeRegistry() = %v", errs)
	}
	warnings := model.OverlayWarnings(d, []model.Overlay{rack, server})
	if len(warnings) != 2 || warnings[0].ItemID != "ov.ot.rack" || !strings.Contains(warnings[0].Message, "deprecated") ||
		warnings[1].ItemID != "ov.ot.server" || !strings.Contains(warnings[1].Message, "hostname") {
		t.Errorf("OverlayWarnings() = %+v", warnings)
	}
}
