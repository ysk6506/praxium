package model_test

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func loadStd(t *testing.T) *model.Registry {
	t.Helper()
	r, err := model.LoadStandard()
	if err != nil {
		t.Fatalf("LoadStandard() = %v", err)
	}
	return r
}

func TestStandardCounts(t *testing.T) {
	// [REQ-100] The data files are the source of truth, and these are the counts they hold: the documents repeat them.
	r := loadStd(t)
	core := map[model.Layer]int{}
	ext := map[model.Layer]int{}
	for _, ot := range r.ObjectTypes() {
		if ot.EffectiveTier() == model.TierExtension {
			ext[ot.Layer]++
		} else {
			core[ot.Layer]++
		}
	}
	wantCore := map[model.Layer]int{"facility": 28, "hardware": 12, "network": 22, "storage": 9, "virtualization": 11, "container": 12, "platform": 11, "service": 8, "cross_cutting": 9}
	wantExt := map[model.Layer]int{"facility": 13, "hardware": 4, "network": 6, "storage": 5, "virtualization": 4, "container": 4, "platform": 3, "service": 6, "cross_cutting": 1}
	if !reflect.DeepEqual(core, wantCore) {
		t.Errorf("core types per layer = %v, want %v", core, wantCore)
	}
	if !reflect.DeepEqual(ext, wantExt) {
		t.Errorf("extension types per layer = %v, want %v", ext, wantExt)
	}
	bindings, props := 0, 0
	for _, lt := range r.LinkTypes() {
		bindings += len(lt.Bindings)
	}
	for _, tr := range r.Traits() {
		props += len(tr.Properties)
	}
	for _, ot := range r.ObjectTypes() {
		props += len(ot.Properties)
	}
	got := map[string]int{
		"object types": len(r.ObjectTypes()), "link types": len(r.LinkTypes()), "bindings": bindings,
		"action types": len(r.ActionTypes()), "traits": len(r.Traits()), "properties": props, "named value types": len(r.ValueTypes()) - len(model.BaseKinds()),
	}
	want := map[string]int{"object types": 168, "link types": 55, "bindings": 551, "action types": 112, "traits": 19, "properties": 671, "named value types": 4}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("counts = %v, want %v", got, want)
	}
}

func TestStandardSpotChecks(t *testing.T) {
	// [REQ-100, REQ-101, REQ-102] A handful of facts from the documents, read back through the registry.
	r := loadStd(t)
	server, ok := r.ObjectTypeByName("PhysicalServer")
	if !ok || server.Namespace != model.NamespaceStd || server.Layer != model.LayerHardware {
		t.Fatalf("PhysicalServer = %+v", server)
	}
	if again, _ := r.ObjectTypeByNo(server.TypeNo); again.ID != server.ID {
		t.Errorf("type_no %d does not lead back to PhysicalServer", server.TypeNo)
	}
	for _, trait := range []string{"Powered", "RackMountable", "HardwareAssembly"} {
		tr, ok := r.TraitByName(trait)
		if !ok || !r.HasTrait(server.ID, tr.ID) {
			t.Errorf("PhysicalServer does not carry %s", trait)
		}
	}
	lag, _ := r.ObjectTypeByName("Lag")
	group, _ := r.ObjectTypeByName("RedundancyGroup")
	if !r.IsSubtypeOf(lag.ID, group.ID) {
		t.Errorf("Lag is not a RedundancyGroup")
	}
	team, _ := r.ObjectTypeByName("Team")
	logical, _ := r.ObjectTypeByName("LogicalGroup")
	if !r.IsSubtypeOf(team.ID, logical.ID) || r.IsSubtypeOf(logical.ID, team.ID) {
		t.Errorf("Team and LogicalGroup are not in the documented order")
	}
	vm, _ := r.ObjectTypeByName("VirtualMachine")
	host, _ := r.ObjectTypeByName("HypervisorHost")
	runsOn, _ := r.LinkTypeByName("RUNS_ON")
	found := false
	for _, b := range r.BindingsFor(vm.ID, host.ID) {
		if b.LinkType == runsOn.ID {
			found = true
			if b.Binding.Cardinality != model.CardManyToOne || !reflect.DeepEqual(b.Binding.ParamConstraints["failover_to"], []string{"ot.compute_cluster"}) {
				t.Errorf("RUNS_ON from VM to host = %+v", b.Binding)
			}
		}
	}
	if !found {
		t.Errorf("a VirtualMachine cannot run on a HypervisorHost")
	}
	cpu, _ := r.ObjectTypeByName("Cpu")
	partOf, _ := r.LinkTypeByName("PART_OF")
	hard := false
	for _, b := range r.BindingsFor(cpu.ID, server.ID) {
		hard = hard || b.LinkType == partOf.ID
	}
	if !hard {
		t.Errorf("a Cpu is not part of a PhysicalServer")
	}
	if _, ok := r.ActionTypeByName("replace_component"); !ok {
		t.Errorf("the action replace_component is missing")
	}
	if got := r.Next(); got.TypeNo <= 168 || got.LinkTypeNo <= 55 {
		t.Errorf("the next numbers %+v reuse a number in use", got)
	}
}

func TestEveryBindingUsesAnAllowedMeaningAndChannel(t *testing.T) {
	// [REQ-101] A link spreads along a channel only if its meaning says which; supplied_by names one of the four resources.
	r := loadStd(t)
	used := map[model.Meaning]bool{}
	for _, lt := range r.LinkTypes() {
		spec, ok := model.SpecOf(lt.Meaning)
		if !ok {
			t.Errorf("%s has the meaning %q outside the closed list", lt.Name, lt.Meaning)
			continue
		}
		used[lt.Meaning] = true
		if len(lt.Bindings) == 0 {
			t.Errorf("%s has no binding", lt.Name)
		}
		channel := spec.To
		if spec.FromParam != "" {
			channel = model.Channel(lt.MeaningParams[spec.FromParam])
		}
		switch {
		case spec.Spreads() && !channel.Valid():
			t.Errorf("%s (%s) spreads along %q, which is not a channel", lt.Name, lt.Meaning, channel)
		case !spec.Spreads() && channel != "":
			t.Errorf("%s (%s) does not spread but names the channel %q", lt.Name, lt.Meaning, channel)
		case lt.Meaning == model.MeaningSuppliedBy && channel != model.ChannelPower && channel != model.ChannelCooling && channel != model.ChannelNetwork && channel != model.ChannelStorage:
			t.Errorf("%s is supplied_by along %s, which no resource gives", lt.Name, channel)
		}
	}
	if len(used) != 11 {
		t.Errorf("the standard model uses %d meanings, want all eleven: %v", len(used), used)
	}
}

func TestStandardIsDeterministic(t *testing.T) {
	// [REQ-233] Two loads give the same numbers and the same hash; the hash is a fixed 64-digit value.
	a, b := loadStd(t), loadStd(t)
	if a.Hash() != b.Hash() {
		t.Fatalf("two loads give different hashes")
	}
	if !reflect.DeepEqual(a.ObjectTypes(), b.ObjectTypes()) || !reflect.DeepEqual(a.LinkTypes(), b.LinkTypes()) {
		t.Fatalf("two loads give different numbers")
	}
	na, nb := mustHash(t, a.NumberingHash), mustHash(t, b.NumberingHash)
	if na != nb {
		t.Fatalf("two loads give different numbering hashes")
	}
	const wantModel = "712b5ae602cd347af9b4eecaa975b3637b5e96ccaa9d8682064d31749a07f98a"
	const wantNumbering = "3b9b3505c4efd21ef3d90bce9ec8be956c87776868e622c9149f046342eb6f1e"
	if got := a.Hash().String(); got != wantModel {
		t.Errorf("model hash = %s, want %s: change it only with a decision record", got, wantModel)
	}
	if got := na.String(); got != wantNumbering {
		t.Errorf("numbering hash = %s, want %s: a number once given is never changed", got, wantNumbering)
	}
}

func TestStandardNumbersAreUniqueAndStable(t *testing.T) {
	// [REQ-233] No two types share a number, and the numbers run without a gap from 1.
	r := loadStd(t)
	seen := map[int]bool{}
	for _, ot := range r.ObjectTypes() {
		if ot.TypeNo < 1 || seen[ot.TypeNo] {
			t.Fatalf("%s has type_no %d, which is not unique", ot.Name, ot.TypeNo)
		}
		seen[ot.TypeNo] = true
	}
	for no := 1; no <= len(seen); no++ {
		if !seen[no] {
			t.Fatalf("type_no %d is missing", no)
		}
	}
}

func TestReadDefinitionsErrors(t *testing.T) {
	// [REQ-100] A data file that cannot be read stops the load with a stable code.
	good, err := model.StandardDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if len(good.ObjectTypes) != 168 {
		t.Fatalf("StandardDefinitions has %d object types", len(good.ObjectTypes))
	}
	meta := `{"model_version":1,"reserved":{}}`
	empty := `{"value_types":[]}`
	tests := []struct {
		name string
		fs   fstest.MapFS
		code kernel.Code
	}{
		{"[REQ-100] no meta file", fstest.MapFS{}, model.CodeDataInvalid},
		{"[REQ-100] unknown field", fstest.MapFS{"meta.json": {Data: []byte(`{"model_version":1,"colour":"red"}`)}}, model.CodeDataInvalid},
		{"[REQ-100] text after the value", fstest.MapFS{"meta.json": {Data: []byte(meta + ` {}`)}}, model.CodeDataInvalid},
		{"[REQ-100] not JSON", fstest.MapFS{"meta.json": {Data: []byte(`model_version: 1`)}}, model.CodeDataInvalid},
		{"[REQ-100] no value type file", fstest.MapFS{"meta.json": {Data: []byte(meta)}}, model.CodeDataInvalid},
		{"[REQ-100] layer file for another layer", fstest.MapFS{
			"meta.json": {Data: []byte(meta)}, "value_types.json": {Data: []byte(empty)}, "identifier_schemes.json": {Data: []byte(`[]`)}, "traits.json": {Data: []byte(`{"traits":[]}`)},
			"links.json": {Data: []byte(`{"link_types":[]}`)}, "actions.json": {Data: []byte(`{"action_types":[]}`)},
			"objects/facility.json": {Data: []byte(`{"layer":"network","types":[]}`)},
		}, model.CodeDataInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := model.ReadDefinitions(tt.fs)
			if err == nil || kernel.CodeOf(err) != tt.code {
				t.Fatalf("ReadDefinitions() = %v, want code %s", err, tt.code)
			}
		})
	}
}

func TestStandardIdentityAndRules(t *testing.T) {
	// [REQ-104, REQ-101] Identity keys, the folded and left-out links, and the stated required flags.
	r := loadStd(t)
	propName := func(id model.PropertyID) string {
		ref, ok := r.Property(id)
		if !ok {
			t.Fatalf("property %s is unknown", id)
		}
		return ref.Property.Name
	}
	server, _ := r.ObjectTypeByName("PhysicalServer")
	keys := r.IdentityKeys(server.ID)
	if len(keys) < 3 || keys[0].Strength != model.IdentityStrong || len(keys[0].Components) != 2 ||
		propName(keys[0].Components[0].Property) != "manufacturer" || propName(keys[0].Components[1].Property) != "serial_number" {
		t.Fatalf("PhysicalServer identity keys = %+v, want manufacturer and serial number first", keys)
	}
	if keys[len(keys)-1].Scheme.IsZero() || keys[len(keys)-1].Strength != model.IdentityWeak {
		t.Errorf("the last PhysicalServer key = %+v, want a weak key on an identifier scheme", keys[len(keys)-1])
	}
	if got, err := model.NormalizeValue(vtID("vt.serial_number"), " ab-12 "); err != nil || got != "AB-12" {
		t.Errorf("the serial number is matched as %q (%v), want AB-12", got, err)
	}
	port, _ := r.ObjectTypeByName("Interface")
	named := r.IdentityKeys(port.ID)
	if len(named) == 0 || len(named[0].Components) != 2 || named[0].Components[0].Context.IsZero() ||
		propName(named[0].Components[1].Property) != "name" {
		t.Errorf("Interface is identified by its name inside its parent: %+v", named)
	}
	if lone, ok := r.ObjectTypeByName("Certificate"); ok {
		for _, k := range r.IdentityKeys(lone.ID) {
			if len(k.Components) == 1 && !k.Components[0].Property.IsZero() &&
				propName(k.Components[0].Property) == "serial_number" && k.Strength != model.IdentityWeak {
				t.Errorf("a serial number alone is a weak key: %+v", k)
			}
		}
	}
	for _, gone := range []string{"INSTALLED_IN", "REQUIRES_PART", "SUPPLIED_BY"} {
		if _, ok := r.LinkTypeByName(gone); ok {
			t.Errorf("the link %s is in the standard model: it is folded into PART_OF or outside the closed lists", gone)
		}
	}
	partOf, _ := r.LinkTypeByName("PART_OF")
	if partOf.Meaning != model.MeaningPartOf {
		t.Errorf("PART_OF has the meaning %s", partOf.Meaning)
	}
	for _, name := range []string{"site_code", "total_u"} {
		found := false
		for _, ot := range r.ObjectTypes() {
			for _, p := range ot.Properties {
				if p.Name == name {
					found = true
					if !p.Required {
						t.Errorf("%s of %s is required in the draft", name, ot.Name)
					}
				}
			}
		}
		if !found {
			t.Errorf("the property %s is missing", name)
		}
	}
	for _, ot := range r.ObjectTypes() {
		for _, p := range ot.Properties {
			if p.ScopeKey {
				t.Errorf("%s.%s is a scope key, but no design document names one", ot.Name, p.Name)
			}
		}
	}
}

func TestStandardIdentityKeysResolve(t *testing.T) {
	// [REQ-104] Every key of the standard model names things that exist, and every scheme loads.
	r := loadStd(t)
	if len(r.IdentifierSchemes()) != 37 {
		t.Errorf("%d identifier schemes, want 37", len(r.IdentifierSchemes()))
	}
	for _, sch := range r.IdentifierSchemes() {
		if got, ok := r.IdentifierSchemeByName(sch.Name); !ok || got.ID != sch.ID {
			t.Fatalf("scheme %s is not found by name", sch.Name)
		}
		if _, ok := r.IdentifierScheme(sch.ID); !ok {
			t.Fatalf("scheme %s is not found by id", sch.Name)
		}
	}
	keys, contexts, sets := 0, 0, 0
	for _, ot := range r.ObjectTypes() {
		for _, k := range r.IdentityKeys(ot.ID) {
			keys++
			if !k.Scheme.IsZero() {
				if _, ok := r.IdentifierScheme(k.Scheme); !ok {
					t.Errorf("%s names an unknown scheme", ot.Name)
				}
			}
			for _, c := range k.Components {
				if c.Context.IsZero() {
					if _, ok := r.Property(c.Property); !ok {
						t.Errorf("%s has a key on an unknown property", ot.Name)
					}
					continue
				}
				contexts++
				if c.Set {
					sets++
				}
				if _, ok := r.LinkType(c.Context); !ok {
					t.Errorf("%s has a key in an unknown context", ot.Name)
				}
			}
		}
	}
	if keys != 235 || contexts != 116 || sets != 5 {
		t.Errorf("%d keys, %d context components of which %d sets, want 235, 116 and 5", keys, contexts, sets)
	}
}

func TestStandardEveryTypeHasAKey(t *testing.T) {
	// [REQ-104] Every standard type has an identity key of its own (TD-0009 is closed): a type without one is a regression.
	r := loadStd(t)
	var got []string
	for _, ot := range r.Definitions().ObjectTypes {
		if len(ot.IdentityKeys) == 0 {
			got = append(got, ot.Name)
		}
	}
	if len(got) != 0 {
		t.Errorf("types without an identity key = %v, want none", got)
	}
}

func TestStandardGroupsCanHaveMembersAndConsumers(t *testing.T) {
	// [REQ-104] Redundancy groups are real structure: each group type of a domain has a member type that can join it
	// (MEMBER_OF) and a consumer type that can depend on the group (supplied_by, requires or realized_by), so that
	// no consumer has to point at one member.
	r := loadStd(t)
	types := map[string]model.ObjectType{}
	for _, ot := range r.Definitions().ObjectTypes {
		types[ot.ID.String()] = ot
	}
	covers := func(e model.Endpoint, id string) bool {
		for cur, ok := types[id], true; ok; cur, ok = types[cur.Parent.String()] {
			if e.Kind == model.EndpointType && e.Ref == cur.ID.String() {
				return true
			}
			for _, tr := range cur.Traits {
				if e.Kind == model.EndpointTrait && e.Ref == tr.String() {
					return true
				}
			}
		}
		return false
	}
	consumer := map[model.Meaning]bool{model.MeaningSuppliedBy: true, model.MeaningRequires: true, model.MeaningRealizedBy: true, model.MeaningRunsOn: true}
	for _, id := range []string{"ot.power_redundancy_group", "ot.cooling_redundancy_group", "ot.mlag_domain", "ot.lag", "ot.ecmp_group",
		"ot.ha_pair", "ot.compute_cluster", "ot.lb_pool", "ot.database_cluster", "ot.control_plane"} {
		member, user := false, false
		for _, lt := range r.LinkTypes() {
			for _, b := range lt.Bindings {
				member = member || (lt.Meaning == model.MeaningMemberOf && covers(b.To, id))
				user = user || (consumer[lt.Meaning] && covers(b.To, id))
			}
		}
		if !member || !user {
			t.Errorf("%s: a member can join = %v, a consumer can depend on it = %v", id, member, user)
		}
	}
}

func TestStandardMountingAndServiceDependencies(t *testing.T) {
	// [REQ-101] Mounting in a rack is containment (MOUNTED_IN has the meaning contained_in, with no second location link
	// for the same fact), and an application service can depend on another one (DEPENDS_ON).
	r := loadStd(t)
	mounted, ok := r.LinkTypeByName("MOUNTED_IN")
	if !ok || mounted.Meaning != model.MeaningContainedIn {
		t.Fatalf("MOUNTED_IN = %+v, want the meaning contained_in", mounted)
	}
	located, _ := r.LinkTypeByName("LOCATED_IN")
	for _, b := range located.Bindings {
		if b.To.Ref == "ot.rack" && b.From.Kind == model.EndpointTrait {
			t.Errorf("%s says where rack-mounted equipment is, but MOUNTED_IN does", b.ID)
		}
	}
	depends, _ := r.LinkTypeByName("DEPENDS_ON")
	found := false
	for _, b := range depends.Bindings {
		found = found || (b.From.Ref == "ot.application_service" && b.To.Ref == "ot.application_service")
	}
	if !found {
		t.Error("DEPENDS_ON has no binding between application services")
	}
}
