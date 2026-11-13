package model_test

import (
	"reflect"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

type kernelHash = kernel.Hash

func newBaseline(t *testing.T, damage ...func(d *model.Definitions)) *model.Registry {
	t.Helper()
	d := baseline()
	for _, fn := range damage {
		fn(&d)
	}
	r, err := model.NewRegistry(d)
	if err != nil {
		t.Fatalf("NewRegistry() = %v", err)
	}
	return r
}

func TestNumbersAreAssignedDeterministically(t *testing.T) {
	// [REQ-233] The numbers written in the data are kept; the others follow in the order of the ids, whatever the order of the data.
	r := newBaseline(t)
	wantTypes := map[string]int{"ot.asset": 1, "ot.acme_server": 2, "ot.cluster": 3, "ot.rack": 4, "ot.redundancy_group": 5, "ot.server": 6}
	for id, want := range wantTypes {
		got, ok := r.ObjectType(otID(id))
		if !ok || got.TypeNo != want {
			t.Errorf("%s has type_no %d, want %d", id, got.TypeNo, want)
		}
	}
	shuffled := newBaseline(t, func(d *model.Definitions) {
		d.ObjectTypes[1], d.ObjectTypes[5] = d.ObjectTypes[5], d.ObjectTypes[1]
		d.LinkTypes[0], d.LinkTypes[3] = d.LinkTypes[3], d.LinkTypes[0]
	})
	for _, id := range []string{"ot.acme_server", "ot.cluster", "ot.server"} {
		a, _ := r.ObjectType(otID(id))
		b, _ := shuffled.ObjectType(otID(id))
		if a.TypeNo != b.TypeNo {
			t.Errorf("%s has %d, and %d when the data lists it elsewhere", id, a.TypeNo, b.TypeNo)
		}
	}
	for no, id := range map[int]string{1: "lt.mounted_in", 2: "lt.part_of", 3: "lt.powered_by", 4: "lt.runs_on"} {
		got, ok := r.LinkTypeByNo(no)
		if !ok || got.ID.String() != id {
			t.Errorf("link type number %d is %s, want %s", no, got.ID, id)
		}
	}
	if a, b := mustHash(t, r.NumberingHash), mustHash(t, shuffled.NumberingHash); a != b {
		t.Errorf("numbering hash differs with the order of the data")
	}
}

func mustHash(t *testing.T, fn func() (kernelHash, error)) kernelHash {
	t.Helper()
	h, err := fn()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestNumbersSkipReservedAndNeverReuse(t *testing.T) {
	// [REQ-233] A reserved number is never given again, and a new definition goes above everything in use.
	r := newBaseline(t, func(d *model.Definitions) {
		d.Reserved.TypeNos = []int{2, 3}
		objectType(d, "ot.asset").TypeNo = 7
	})
	want := map[string]int{"ot.asset": 7, "ot.acme_server": 8, "ot.cluster": 9, "ot.rack": 10}
	for id, no := range want {
		if got, _ := r.ObjectType(otID(id)); got.TypeNo != no {
			t.Errorf("%s has type_no %d, want %d", id, got.TypeNo, no)
		}
	}
	next := r.Next()
	if next != (model.NextNumbers{TypeNo: 13, LinkTypeNo: 5, AttrNo: 10}) {
		t.Errorf("Next() = %+v, want type 13, link type 5 and attribute 10", next)
	}
	if byNo, ok := r.ObjectTypeByNo(7); !ok || byNo.ID != otID("ot.asset") {
		t.Errorf("ObjectTypeByNo(7) = %v, %v", byNo.ID, ok)
	}
	if _, ok := r.ObjectTypeByNo(2); ok {
		t.Errorf("a reserved number points at a type")
	}
}

func TestLookups(t *testing.T) {
	// [REQ-100] A type is found by id, by number and by name; a link type and an action type by id, number and name.
	r := newBaseline(t, func(d *model.Definitions) { objectType(d, "ot.rack").Aliases = []string{"Cabinet"} })
	byName, ok := r.ObjectTypeByName("Rack")
	if !ok || byName.ID != otID("ot.rack") {
		t.Fatalf("ObjectTypeByName(Rack) = %v, %v", byName.ID, ok)
	}
	if alias, _ := r.ObjectTypeByName("Cabinet"); alias.ID != otID("ot.rack") {
		t.Errorf("the old name Cabinet finds %v", alias.ID)
	}
	if byNo, _ := r.ObjectTypeByNo(byName.TypeNo); byNo.ID != byName.ID {
		t.Errorf("the number finds %v", byNo.ID)
	}
	for _, name := range []string{"Nothing", ""} {
		if _, ok := r.ObjectTypeByName(name); ok {
			t.Errorf("a type is found by the name %q", name)
		}
	}
	link, ok := r.LinkTypeByName("RUNS_ON")
	if !ok || len(link.Bindings) != 1 {
		t.Fatalf("LinkTypeByName(RUNS_ON) = %+v, %v", link, ok)
	}
	if byID, _ := r.LinkType(link.ID); byID.LinkTypeNo != link.LinkTypeNo {
		t.Errorf("LinkType(id) = %+v", byID)
	}
	action, ok := r.ActionTypeByName("power_off")
	if !ok || action.ID != acID("ac.power_off") {
		t.Fatalf("ActionTypeByName(power_off) = %+v, %v", action, ok)
	}
	if byID, _ := r.ActionType(action.ID); byID.Name != "power_off" {
		t.Errorf("ActionType(id) = %+v", byID)
	}
	trait, ok := r.TraitByName("Powered")
	if !ok || trait.ID != trID("tr.powered") {
		t.Fatalf("TraitByName(Powered) = %+v, %v", trait, ok)
	}
	for name, miss := range map[string]bool{
		"link": !has(r.LinkTypeByName("NOTHING")), "action": !has(r.ActionTypeByName("nothing")),
		"trait": !has(r.TraitByName("Nothing")), "link number": !has(r.LinkTypeByNo(99)),
	} {
		if !miss {
			t.Errorf("a %s was found by a name or number that is not there", name)
		}
	}
	if _, ok := r.ObjectType(otID("ot.nothing")); ok {
		t.Errorf("an unknown id finds a type")
	}
	if _, ok := r.LinkType(ltID("lt.nothing")); ok {
		t.Errorf("an unknown id finds a link type")
	}
	if _, ok := r.ActionType(acID("ac.nothing")); ok {
		t.Errorf("an unknown id finds an action type")
	}
	if _, ok := r.Trait(trID("tr.nothing")); ok {
		t.Errorf("an unknown id finds a trait")
	}
}

func has[T any](_ T, ok bool) bool { return ok }

func TestRenameKeepsIdNumberAndHash(t *testing.T) {
	// [REQ-233] A name can be changed: the id, the number and the hash stay, and the old name is an alias.
	before := newBaseline(t)
	after := newBaseline(t, func(d *model.Definitions) {
		rack := objectType(d, "ot.rack")
		rack.Name, rack.Aliases = "Cabinet", []string{"Rack"}
		property(objectType(d, "ot.asset"), "pr.asset.site").Name = "place"
		linkType(d, "lt.runs_on").Name, linkType(d, "lt.runs_on").Aliases = "EXECUTES_ON", []string{"RUNS_ON"}
		rack.Description, rack.DisplayNames, rack.Contacts = "labels only", map[string]string{"ko": "랙"}, []string{"team"}
	})
	renamed, _ := after.ObjectTypeByName("Cabinet")
	old, _ := before.ObjectTypeByName("Rack")
	if renamed.ID != old.ID || renamed.TypeNo != old.TypeNo {
		t.Errorf("renamed type has %v/%d, was %v/%d", renamed.ID, renamed.TypeNo, old.ID, old.TypeNo)
	}
	if viaOld, _ := after.ObjectTypeByName("Rack"); viaOld.ID != old.ID {
		t.Errorf("the old name finds %v", viaOld.ID)
	}
	if link, _ := after.LinkTypeByName("RUNS_ON"); link.Name != "EXECUTES_ON" {
		t.Errorf("the old link name finds %q", link.Name)
	}
	if before.Hash() != after.Hash() {
		t.Errorf("renaming changed the model hash")
	}
	if a, b := mustHash(t, before.NumberingHash), mustHash(t, after.NumberingHash); a != b {
		t.Errorf("renaming changed the numbering hash")
	}
}

func TestHashFollowsMeaning(t *testing.T) {
	// [REQ-233] What gives a definition its meaning is in the hash; the number of a definition is in the numbering hash.
	base := newBaseline(t)
	changes := map[string]func(d *model.Definitions){
		"cardinality": func(d *model.Definitions) { linkType(d, "lt.runs_on").Bindings[0].Cardinality = model.CardOneToOne },
		"status":      func(d *model.Definitions) { objectType(d, "ot.rack").Status = model.StatusExperimental },
		"layer":       func(d *model.Definitions) { objectType(d, "ot.rack").Layer = model.LayerNetwork },
		"history kind": func(d *model.Definitions) {
			property(objectType(d, "ot.asset"), "pr.asset.power_state").HistoryKind = model.HistoryVersioned
		},
		"identity strength": func(d *model.Definitions) { objectType(d, "ot.asset").IdentityKeys[0].Strength = model.IdentityWeak },
		"identity key order": func(d *model.Definitions) {
			keys := objectType(d, "ot.asset").IdentityKeys
			keys[0], keys[1] = keys[1], keys[0]
		},
		"identity component order": func(d *model.Definitions) {
			c := objectType(d, "ot.asset").IdentityKeys[1].Components
			c[0], c[1] = c[1], c[0]
		},
		"identity scheme": func(d *model.Definitions) {
			d.Schemes = append(d.Schemes, d.Schemes[0])
			d.Schemes[1].ID, d.Schemes[1].Name = schID("is.other"), "other_id"
		},
		"scheme normaliser": func(d *model.Definitions) { d.Schemes[0].Normalizer = vtID("vt.fqdn") },
		"parent":            func(d *model.Definitions) { objectType(d, "ot.cluster").Parent = otID("ot.asset") },
		"meaning parameter": func(d *model.Definitions) { linkType(d, "lt.powered_by").MeaningParams["resource"] = "cooling" },
		"action risk":       func(d *model.Definitions) { actionType(d, "ac.power_on").Disruption = model.DisruptionDataLoss },
		"model version":     func(d *model.Definitions) { d.ModelVersion = 2 },
		"reserved number":   func(d *model.Definitions) { d.Reserved.AttrNos = []int{500} },
		"a new trait": func(d *model.Definitions) {
			d.Traits = append(d.Traits, model.Trait{ID: trID("tr.new"), Name: "Fresh"})
		},
		"constraint": func(d *model.Definitions) {
			linkType(d, "lt.part_of").Bindings[0].ParamConstraints["whole_needs_part"] = []string{"soft"}
		},
		"standard reference": func(d *model.Definitions) { objectType(d, "ot.rack").StandardRef = "Some standard" },
	}
	for name, change := range changes {
		if newBaseline(t, change).Hash() == base.Hash() {
			t.Errorf("a change of %s does not change the model hash", name)
		}
	}
	renumbered := newBaseline(t, func(d *model.Definitions) { objectType(d, "ot.asset").TypeNo = 40 })
	if renumbered.Hash() != base.Hash() {
		t.Errorf("a number is in the model hash")
	}
	if mustHash(t, renumbered.NumberingHash) == mustHash(t, base.NumberingHash) {
		t.Errorf("a changed number does not change the numbering hash")
	}
}

func TestHashIgnoresTheOrderOfSets(t *testing.T) {
	// [REQ-001] The same definitions in another order have the same hash.
	base := newBaseline(t)
	reordered := newBaseline(t, func(d *model.Definitions) {
		d.ObjectTypes[0], d.ObjectTypes[2] = d.ObjectTypes[2], d.ObjectTypes[0]
		d.Traits[0], d.Traits[1] = d.Traits[1], d.Traits[0]
		d.ActionTypes[0], d.ActionTypes[2] = d.ActionTypes[2], d.ActionTypes[0]
		l := linkType(d, "lt.mounted_in")
		l.Bindings[0], l.Bindings[1] = l.Bindings[1], l.Bindings[0]
		mode := property(objectType(d, "ot.redundancy_group"), "pr.group.mode")
		mode.EnumValues = []string{"capacity", "count"}
	})
	if base.Hash() != reordered.Hash() {
		t.Errorf("the order of sets changes the hash")
	}
}

func TestSubtypeQueries(t *testing.T) {
	// [REQ-100] Subtype queries: ancestors, is-a, all subtypes, a layer, a trait.
	r := newBaseline(t)
	if got := r.Ancestors(otID("ot.acme_server")); !reflect.DeepEqual(got, []model.ObjectTypeID{otID("ot.server"), otID("ot.asset")}) {
		t.Errorf("Ancestors() = %v", got)
	}
	if r.Ancestors(otID("ot.nothing")) != nil {
		t.Errorf("an unknown type has ancestors")
	}
	if !r.IsSubtypeOf(otID("ot.acme_server"), otID("ot.asset")) || !r.IsSubtypeOf(otID("ot.rack"), otID("ot.rack")) {
		t.Errorf("IsSubtypeOf misses a subtype or the type itself")
	}
	if r.IsSubtypeOf(otID("ot.asset"), otID("ot.server")) || r.IsSubtypeOf(otID("ot.nothing"), otID("ot.nothing")) {
		t.Errorf("IsSubtypeOf reads a parent as a subtype or knows an unknown type")
	}
	subs := r.Subtypes(otID("ot.asset"))
	if len(subs) != 2 || subs[0].ID != otID("ot.acme_server") || subs[1].ID != otID("ot.server") {
		t.Errorf("Subtypes(asset) = %v", subs)
	}
	if len(r.Subtypes(otID("ot.rack"))) != 0 {
		t.Errorf("a type without children has subtypes")
	}
	if got := r.TypesInLayer(model.LayerHardware); len(got) != 3 {
		t.Errorf("TypesInLayer(hardware) has %d types, want 3", len(got))
	}
	if got := r.TypesInLayer(model.LayerNetwork); len(got) != 0 {
		t.Errorf("TypesInLayer(network) = %v", got)
	}
	withTrait := r.TypesWithTrait(trID("tr.powered"))
	if len(withTrait) != 2 || !r.HasTrait(otID("ot.acme_server"), trID("tr.powered")) || r.HasTrait(otID("ot.rack"), trID("tr.powered")) {
		t.Errorf("TypesWithTrait(powered) = %v", withTrait)
	}
	if r.HasTrait(otID("ot.nothing"), trID("tr.powered")) {
		t.Errorf("an unknown type has a trait")
	}
}

func TestInheritance(t *testing.T) {
	// [REQ-107, REQ-165] A child inherits properties, scope keys, identity keys and needs; a type overrides a trait's need.
	r := newBaseline(t)
	var names []string
	for _, p := range r.EffectiveProperties(otID("ot.acme_server")) {
		names = append(names, p.Name)
	}
	want := []string{"serial_number", "site", "power_state", "rated_power_w", "hostname", "owner"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("EffectiveProperties() = %v, want %v", names, want)
	}
	if got := r.ScopeKeys(otID("ot.acme_server")); !reflect.DeepEqual(got, []model.PropertyID{prID("pr.asset.site")}) {
		t.Errorf("ScopeKeys() = %v", got)
	}
	if got := r.IdentityKeys(otID("ot.acme_server")); len(got) != 3 || got[0].Components[0].Property != prID("pr.asset.serial_number") || got[1].Components[0].Context != ltID("lt.part_of") || got[2].Scheme != schID("is.vendor_id") {
		t.Errorf("IdentityKeys() = %v", got)
	}
	needs := r.EffectiveNeeds(otID("ot.server"))
	if len(needs) != 2 || needs[0].Channel != model.ChannelPower || needs[0].Strength != model.StrengthSoft || needs[1].Channel != model.ChannelManagement {
		t.Errorf("EffectiveNeeds() = %v, want power soft (the type overrides the trait) and management", needs)
	}
	if got := r.EffectiveNeeds(otID("ot.acme_server")); len(got) != 2 || got[0].Strength != model.StrengthSoft {
		t.Errorf("a subtype does not inherit the needs: %v", got)
	}
	for _, id := range []string{"ot.nothing"} {
		if r.EffectiveProperties(otID(id)) != nil || r.ScopeKeys(otID(id)) != nil || r.IdentityKeys(otID(id)) != nil || r.EffectiveNeeds(otID(id)) != nil {
			t.Errorf("an unknown type has inherited parts")
		}
	}
}

func TestBindingsForAPair(t *testing.T) {
	// [REQ-100] The bindings for a pair of types: by type, by subtype, by trait and for any object.
	r := newBaseline(t)
	ids := func(pairs []model.BoundLink) []string {
		var out []string
		for _, p := range pairs {
			out = append(out, p.Binding.ID.String())
		}
		return out
	}
	got := ids(r.BindingsFor(otID("ot.server"), otID("ot.rack")))
	want := []string{"bd.mounted_in.mountable.rack", "bd.mounted_in.server.rack", "bd.powered_by.powered.any"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("server to rack (by type, by trait, to any object): %v, want %v", got, want)
	}
	if got := ids(r.BindingsFor(otID("ot.acme_server"), otID("ot.rack"))); !reflect.DeepEqual(got, want) {
		t.Errorf("a subtype uses the bindings of its parent: %v", got)
	}
	if got := ids(r.BindingsFor(otID("ot.rack"), otID("ot.server"))); len(got) != 0 {
		t.Errorf("rack to server: %v, want none", got)
	}
	powered := ids(r.BindingsFor(otID("ot.acme_server"), otID("ot.cluster")))
	if !reflect.DeepEqual(powered, []string{"bd.powered_by.powered.any"}) {
		t.Errorf("a Powered type to any object: %v", powered)
	}
	if got := ids(r.BindingsFor(otID("ot.nothing"), otID("ot.rack"))); len(got) != 0 {
		t.Errorf("an unknown type has bindings: %v", got)
	}
}

func TestRegistryIsImmutable(t *testing.T) {
	// [REQ-233] What the registry returns is a copy; changing it does not change the registry.
	d := baseline()
	r, err := model.NewRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	objectType(&d, "ot.rack").Name = "Changed"
	if _, ok := r.ObjectTypeByName("Rack"); !ok {
		t.Fatalf("changing the input changed the registry")
	}
	got, _ := r.ObjectType(otID("ot.server"))
	got.Name, got.Traits[0], got.Properties[0].Name = "Changed", trID("tr.nothing"), "changed"
	link, _ := r.LinkTypeByName("RUNS_ON")
	link.Bindings[0].ParamConstraints["failover_to"][0] = "ot.nothing"
	again, _ := r.ObjectType(otID("ot.server"))
	if again.Name != "Server" || again.Traits[0] != trID("tr.powered") || again.Properties[0].Name != "hostname" {
		t.Errorf("changing a returned type changed the registry: %+v", again)
	}
	if fresh, _ := r.LinkTypeByName("RUNS_ON"); fresh.Bindings[0].ParamConstraints["failover_to"][0] != "ot.cluster" {
		t.Errorf("changing a returned binding changed the registry")
	}
	out := r.Definitions()
	out.ObjectTypes[0].Name = "Changed"
	if first, _ := r.ObjectTypeByNo(1); first.Name != "Asset" {
		t.Errorf("changing the exported definitions changed the registry")
	}
	if r.ModelVersion() != 1 {
		t.Errorf("ModelVersion() = %d", r.ModelVersion())
	}
}

func TestListsAreOrdered(t *testing.T) {
	// [REQ-233] Lists come in a fixed order: types and link types by number, the rest by name.
	r := newBaseline(t)
	types := r.ObjectTypes()
	for i := 1; i < len(types); i++ {
		if types[i-1].TypeNo >= types[i].TypeNo {
			t.Errorf("ObjectTypes() is not in the order of the numbers")
		}
	}
	links := r.LinkTypes()
	for i := 1; i < len(links); i++ {
		if links[i-1].LinkTypeNo >= links[i].LinkTypeNo {
			t.Errorf("LinkTypes() is not in the order of the numbers")
		}
	}
	if actions := r.ActionTypes(); actions[0].Name != "get_status" || actions[2].Name != "power_on" {
		t.Errorf("ActionTypes() = %v", actions)
	}
	if traits := r.Traits(); traits[0].Name != "Powered" || traits[1].Name != "RackMountable" {
		t.Errorf("Traits() = %v", traits)
	}
	values := r.ValueTypes()
	if len(values) != 11 || values[0].Name != "string" || values[10].Name != "fqdn" {
		t.Errorf("ValueTypes() = %v", values)
	}
	if _, ok := r.ValueType(vtID("vt.fqdn")); !ok {
		t.Errorf("a named value type is not found")
	}
	if _, ok := r.ValueType(vtID("vt.integer")); !ok {
		t.Errorf("a built-in value type is not found")
	}
}

func TestPropertyLookups(t *testing.T) {
	// [REQ-233] A property is found by id and by its attribute number, with its owner.
	r := newBaseline(t)
	ref, ok := r.Property(prID("pr.asset.site"))
	if !ok || ref.OwnerKind != "object_type" || ref.Owner != "ot.asset" || ref.Property.AttrNo == 0 {
		t.Fatalf("Property() = %+v, %v", ref, ok)
	}
	if byNo, _ := r.PropertyByNo(ref.Property.AttrNo); byNo.Property.ID != ref.Property.ID {
		t.Errorf("PropertyByNo() = %+v", byNo)
	}
	if trait, _ := r.Property(prID("pr.powered.rated_power_w")); trait.OwnerKind != "trait" {
		t.Errorf("a trait property is owned by %q", trait.OwnerKind)
	}
	if link, _ := r.Property(prID("pr.powered_by.share")); link.OwnerKind != "link_type" {
		t.Errorf("a link property is owned by %q", link.OwnerKind)
	}
	if _, ok := r.Property(prID("pr.nothing")); ok {
		t.Errorf("an unknown id finds a property")
	}
	if _, ok := r.PropertyByNo(9999); ok {
		t.Errorf("an unknown number finds a property")
	}
}
