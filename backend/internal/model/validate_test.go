package model_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

type ruleCase struct {
	name   string
	code   kernel.Code
	damage func(d *model.Definitions)
}

func runRuleCases(t *testing.T, cases []ruleCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			d := baseline()
			tt.damage(&d)
			errs := model.Check(d)
			if !hasCode(errs, tt.code) {
				t.Fatalf("Check() = %v, want a violation with code %s", errs, tt.code)
			}
			if _, err := model.NewRegistry(d); err == nil {
				t.Fatalf("NewRegistry accepted definitions that break the rule")
			}
		})
	}
}

func TestBaselineIsSound(t *testing.T) {
	d := baseline()
	if errs := model.Check(d); len(errs) != 0 {
		t.Fatalf("the baseline fixture breaks rules: %v", errs)
	}
	if err := model.Validate(d); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestValidateNamesTheRest(t *testing.T) {
	d := baseline()
	objectType(&d, "ot.asset").Name = "asset"
	objectType(&d, "ot.rack").Name = "rack"
	err := model.Validate(d)
	if kernel.CodeOf(err) != model.CodeNameInvalid || !strings.Contains(err.Error(), "and 1 more") {
		t.Fatalf("Validate() = %v, want model_name_invalid and 1 more violation", err)
	}
}

func TestRulesOfIdsAndNames(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"[REQ-100] missing model version", model.CodeRequiredField, func(d *model.Definitions) { d.ModelVersion = 0 }},
		{"[REQ-100] object type without id", model.CodeRequiredField, func(d *model.Definitions) { objectType(d, "ot.rack").ID = model.ObjectTypeID{} }},
		{"[REQ-100] object type without name", model.CodeRequiredField, func(d *model.Definitions) { objectType(d, "ot.rack").Name = "" }},
		{"[REQ-100] property without id", model.CodeRequiredField, func(d *model.Definitions) { objectType(d, "ot.asset").Properties[0].ID = model.PropertyID{} }},
		{"[REQ-100] value type without id", model.CodeRequiredField, func(d *model.Definitions) { d.ValueTypes[0].ID = model.ValueTypeID{} }},
		{"[REQ-100] object type id twice", model.CodeDuplicateID, func(d *model.Definitions) { objectType(d, "ot.rack").ID = otID("ot.asset") }},
		{"[REQ-100] property id twice", model.CodeDuplicateID, func(d *model.Definitions) {
			objectType(d, "ot.server").Properties[0].ID = prID("pr.asset.site")
		}},
		{"[REQ-100] trait id twice", model.CodeDuplicateID, func(d *model.Definitions) { d.Traits[1].ID = d.Traits[0].ID }},
		{"[REQ-100] link type id twice", model.CodeDuplicateID, func(d *model.Definitions) { d.LinkTypes[1].ID = d.LinkTypes[0].ID }},
		{"[REQ-100] action type id twice", model.CodeDuplicateID, func(d *model.Definitions) { d.ActionTypes[1].ID = d.ActionTypes[0].ID }},
		{"[REQ-100] binding id twice", model.CodeDuplicateID, func(d *model.Definitions) {
			linkType(d, "lt.runs_on").Bindings[0].ID = bdID("bd.mounted_in.server.rack")
		}},
		{"[REQ-100] value type id twice", model.CodeDuplicateID, func(d *model.Definitions) { d.ValueTypes[0].ID = vtID("vt.string") }},
		{"[REQ-100] trait listed twice on a type", model.CodeDuplicateID, func(d *model.Definitions) {
			objectType(d, "ot.server").Traits = append(objectType(d, "ot.server").Traits, trID("tr.powered"))
		}},
		{"[REQ-100] object type name not PascalCase", model.CodeNameInvalid, func(d *model.Definitions) { objectType(d, "ot.rack").Name = "rack_unit" }},
		{"[REQ-100] object type name with a hyphen", model.CodeNameInvalid, func(d *model.Definitions) { objectType(d, "ot.rack").Name = "Rack-Unit" }},
		{"[REQ-100] link type name not UPPER_SNAKE", model.CodeNameInvalid, func(d *model.Definitions) { linkType(d, "lt.runs_on").Name = "RunsOn" }},
		{"[REQ-100] property name not snake_case", model.CodeNameInvalid, func(d *model.Definitions) { property(objectType(d, "ot.asset"), "pr.asset.site").Name = "Site" }},
		{"[REQ-100] action type name not snake_case", model.CodeNameInvalid, func(d *model.Definitions) { actionType(d, "ac.power_off").Name = "PowerOff" }},
		{"[REQ-100] trait name not PascalCase", model.CodeNameInvalid, func(d *model.Definitions) { d.Traits[0].Name = "powered" }},
		{"[REQ-100] value type name not snake_case", model.CodeNameInvalid, func(d *model.Definitions) { d.ValueTypes[0].Name = "Fqdn" }},
		{"[REQ-100] enum value not snake_case", model.CodeNameInvalid, func(d *model.Definitions) {
			property(objectType(d, "ot.redundancy_group"), "pr.group.mode").EnumValues = []string{"Count", "capacity"}
		}},
		{"[REQ-233] alias breaks the name form", model.CodeNameInvalid, func(d *model.Definitions) { objectType(d, "ot.rack").Aliases = []string{"old rack"} }},
		{"[REQ-233] alias equals another name", model.CodeNameDuplicate, func(d *model.Definitions) { objectType(d, "ot.rack").Aliases = []string{"Asset"} }},
		{"[REQ-100] two types with one name", model.CodeNameDuplicate, func(d *model.Definitions) { objectType(d, "ot.cluster").Name = "Rack" }},
		{"[REQ-100] two link types with one name", model.CodeNameDuplicate, func(d *model.Definitions) { linkType(d, "lt.runs_on").Name = "PART_OF" }},
		{"[REQ-100] two actions with one name", model.CodeNameDuplicate, func(d *model.Definitions) { actionType(d, "ac.power_on").Name = "power_off" }},
		{"[REQ-100] a value type named like a base kind", model.CodeNameDuplicate, func(d *model.Definitions) { d.ValueTypes[0].Name = "string" }},
		{"[REQ-100] two traits with one name", model.CodeNameDuplicate, func(d *model.Definitions) { d.Traits[1].Name = "Powered" }},
		{"ext type takes a std name", model.CodeNameReserved, func(d *model.Definitions) { objectType(d, "ot.acme_server").Name = "Rack" }},
		{"ext link type takes a std name", model.CodeNameReserved, func(d *model.Definitions) {
			d.LinkTypes = append(d.LinkTypes, model.LinkType{ID: ltID("lt.ext_runs_on"), Name: "RUNS_ON", Namespace: model.NamespaceExt, Meaning: model.MeaningRecordOnly})
		}},
		{"sys type takes an ext name", model.CodeNameReserved, func(d *model.Definitions) {
			d.ObjectTypes = append(d.ObjectTypes, model.ObjectType{ID: otID("ot.sys_acme"), Name: "AcmeServer", Namespace: model.NamespaceSys, Layer: model.LayerSystem})
		}},
	})
}

func TestRulesOfPlacement(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"namespace outside the four", model.CodeClosedList, func(d *model.Definitions) { objectType(d, "ot.rack").Namespace = "team" }},
		{"layer outside the ten", model.CodeClosedList, func(d *model.Definitions) { objectType(d, "ot.rack").Layer = "wireless" }},
		{"tier outside core and extension", model.CodeClosedList, func(d *model.Definitions) { objectType(d, "ot.rack").Tier = "draft" }},
		{"[REQ-100] status outside the three", model.CodeClosedList, func(d *model.Definitions) { objectType(d, "ot.rack").Status = "retired" }},
		{"sys type outside the system layer", model.CodeLayerNamespace, func(d *model.Definitions) { objectType(d, "ot.rack").Namespace = model.NamespaceSys }},
		{"std type in the system layer", model.CodeLayerNamespace, func(d *model.Definitions) { objectType(d, "ot.rack").Layer = model.LayerSystem }},
		{"ext type in another layer than its parent", model.CodeLayerNotInherited, func(d *model.Definitions) { objectType(d, "ot.acme_server").Layer = model.LayerNetwork }},
		{"org type in another layer than its parent", model.CodeLayerNotInherited, func(d *model.Definitions) {
			objectType(d, "ot.acme_server").Namespace = model.NamespaceOrg
			objectType(d, "ot.acme_server").Layer = model.LayerStorage
		}},
		{"[REQ-100] parent that does not exist", model.CodeParentUnknown, func(d *model.Definitions) { objectType(d, "ot.server").Parent = otID("ot.nothing") }},
		{"[REQ-100] parents that form a cycle", model.CodeParentCycle, func(d *model.Definitions) { objectType(d, "ot.asset").Parent = otID("ot.acme_server") }},
		{"std type on an ext parent", model.CodeParentNamespace, func(d *model.Definitions) { objectType(d, "ot.cluster").Parent = otID("ot.acme_server") }},
		{"ext type on a sys parent", model.CodeParentNamespace, func(d *model.Definitions) {
			d.ObjectTypes = append(d.ObjectTypes, model.ObjectType{ID: otID("ot.sys_part"), Name: "SysPart", Namespace: model.NamespaceSys, Layer: model.LayerSystem})
			objectType(d, "ot.acme_server").Parent = otID("ot.sys_part")
		}},
		{"[REQ-100] trait that does not exist", model.CodeTraitUnknown, func(d *model.Definitions) { objectType(d, "ot.server").Traits[0] = trID("tr.nothing") }},
		{"[REQ-100] successor that does not exist", model.CodeSuperseded, func(d *model.Definitions) {
			objectType(d, "ot.rack").Status = model.StatusDeprecated
			objectType(d, "ot.rack").SupersededBy = otID("ot.nothing")
		}},
		{"[REQ-100] deprecated without a successor", model.CodeSuperseded, func(d *model.Definitions) { objectType(d, "ot.rack").Status = model.StatusDeprecated }},
		{"[REQ-100] successor on an active type", model.CodeSuperseded, func(d *model.Definitions) { objectType(d, "ot.rack").SupersededBy = otID("ot.asset") }},
		{"[REQ-100] type that supersedes itself", model.CodeSuperseded, func(d *model.Definitions) {
			objectType(d, "ot.rack").Status = model.StatusDeprecated
			objectType(d, "ot.rack").SupersededBy = otID("ot.rack")
		}},
	})
}

func TestRulesOfProperties(t *testing.T) {
	asset := func(d *model.Definitions) *model.ObjectType { return objectType(d, "ot.asset") }
	runRuleCases(t, []ruleCase{
		{"[REQ-100] value type that does not exist", model.CodeValueTypeUnknown, func(d *model.Definitions) { asset(d).Properties[0].ValueType = vtID("vt.nothing") }},
		{"[REQ-100] named value type without an implementation", model.CodeValueTypeUnknown, func(d *model.Definitions) {
			d.ValueTypes = append(d.ValueTypes, model.ValueType{ID: vtID("vt.invented"), Name: "invented", Base: model.BaseString})
		}},
		{"[REQ-100] value type on a base kind outside the ten", model.CodeClosedList, func(d *model.Definitions) { d.ValueTypes[0].Base = "text" }},
		{"[REQ-100] enum without values", model.CodeEnumValues, func(d *model.Definitions) {
			property(objectType(d, "ot.redundancy_group"), "pr.group.mode").EnumValues = nil
		}},
		{"[REQ-100] values on a property that is not an enum", model.CodeEnumValues, func(d *model.Definitions) { asset(d).Properties[0].EnumValues = []string{"a"} }},
		{"[REQ-100] enum value twice", model.CodeEnumValues, func(d *model.Definitions) {
			property(objectType(d, "ot.redundancy_group"), "pr.group.mode").EnumValues = []string{"count", "count"}
		}},
		{"[REQ-100] quantity without a unit", model.CodeUnit, func(d *model.Definitions) {
			property(objectType(d, "ot.redundancy_group"), "pr.group.min_capacity").Unit = ""
		}},
		{"[REQ-100] unit on a property that is not a quantity", model.CodeUnit, func(d *model.Definitions) { asset(d).Properties[0].Unit = "w" }},
		{"change class outside the three", model.CodeClosedList, func(d *model.Definitions) { asset(d).Properties[0].ChangeClass = "fast" }},
		{"history kind outside the two", model.CodeClosedList, func(d *model.Definitions) { asset(d).Properties[0].HistoryKind = "forever" }},
		{"[REQ-100] sensitivity outside the three", model.CodeClosedList, func(d *model.Definitions) { asset(d).Properties[0].Sensitivity = "secret" }},
		{"scope key that is not a short text or a reference", model.CodeScopeKeyType, func(d *model.Definitions) {
			asset(d).Properties[0].ValueType = vtID("vt.integer")
			asset(d).Properties[0].ScopeKey = true
		}},
		{"scope key that is dynamic", model.CodeScopeKeyVolatile, func(d *model.Definitions) { property(asset(d), "pr.asset.site").ChangeClass = model.ChangeDynamic }},
		{"scope key with volatile history", model.CodeScopeKeyVolatile, func(d *model.Definitions) { property(asset(d), "pr.asset.site").HistoryKind = model.HistoryVolatile }},
		{"more than four scope keys", model.CodeScopeKeyLimit, func(d *model.Definitions) {
			for _, name := range []string{"a", "b", "c", "d"} {
				key := prop("pr.asset.scope_"+name, "scope_"+name, "vt.string", model.ChangeSlow)
				key.ScopeKey = true
				asset(d).Properties = append(asset(d).Properties, key)
			}
		}},
		{"[REQ-100] property name used twice in one type", model.CodePropertyDuplicate, func(d *model.Definitions) {
			asset(d).Properties = append(asset(d).Properties, prop("pr.asset.site_again", "site", "vt.string", model.ChangeSlow))
		}},
		{"[REQ-100] child redefines an inherited property", model.CodePropertyDuplicate, func(d *model.Definitions) {
			objectType(d, "ot.server").Properties[0].Name = "site"
		}},
		{"[REQ-100] type redefines a trait property", model.CodePropertyDuplicate, func(d *model.Definitions) {
			objectType(d, "ot.server").Properties[0].Name = "rated_power_w"
		}},
		{"[REQ-100] successor property that does not exist", model.CodeSuperseded, func(d *model.Definitions) {
			asset(d).Properties[0].Status = model.StatusDeprecated
			asset(d).Properties[0].SupersededBy = prID("pr.nothing")
		}},
	})
}

func TestRulesOfIdentityAndNeeds(t *testing.T) {
	asset := func(d *model.Definitions) *model.ObjectType { return objectType(d, "ot.asset") }
	runRuleCases(t, []ruleCase{
		{"identity key on an unknown property", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).IdentityKeys[0].Components[0].Property = prID("pr.nothing")
		}},
		{"identity key on a property of another type", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).IdentityKeys[0].Components[0].Property = prID("pr.server.hostname")
		}},
		{"two identity keys that are the same", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).IdentityKeys = append(asset(d).IdentityKeys, asset(d).IdentityKeys[0])
		}},
		{"identity key that uses a component twice", model.CodeIdentityKey, func(d *model.Definitions) {
			key := &asset(d).IdentityKeys[0]
			key.Components = append(key.Components, key.Components[0])
		}},
		{"identity key with no component and no scheme", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).IdentityKeys[2].Scheme = model.IdentifierSchemeID{}
		}},
		{"identity key with a context and nothing else", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).IdentityKeys[1].Components = asset(d).IdentityKeys[1].Components[:1]
		}},
		{"component that is a property and a context", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).IdentityKeys[0].Components[0].Context = ltID("lt.part_of")
		}},
		{"component that is neither", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).IdentityKeys[0].Components = append(asset(d).IdentityKeys[0].Components, model.KeyComponent{})
		}},
		{"identity key on a confidential property", model.CodeIdentityKey, func(d *model.Definitions) {
			asset(d).Properties[0].Sensitivity = model.SensitivityConfidential
		}},
		{"identity key on a volatile property", model.CodeIdentityKeyVolatile, func(d *model.Definitions) {
			asset(d).IdentityKeys[0].Components[0].Property = prID("pr.asset.power_state")
		}},
		{"identity key in the context of a link type that does not exist", model.CodeIdentityContext, func(d *model.Definitions) {
			asset(d).IdentityKeys[1].Components[0].Context = ltID("lt.nothing")
		}},
		{"identity key in the context of a link with no binding from the type", model.CodeIdentityContext, func(d *model.Definitions) {
			asset(d).IdentityKeys[1].Components[0].Context = ltID("lt.runs_on")
		}},
		{"identity key in the context of a binding that is many to many", model.CodeIdentityContext, func(d *model.Definitions) {
			linkType(d, "lt.part_of").Bindings[0].Cardinality = model.CardManyToMany
		}},
		{"identity key in the context of a binding that points the wrong way", model.CodeIdentityContext, func(d *model.Definitions) {
			b := &linkType(d, "lt.part_of").Bindings[0]
			b.From, b.To = b.To, b.From
		}},
		{"identity key with an identifier scheme that does not exist", model.CodeSchemeUnknown, func(d *model.Definitions) {
			asset(d).IdentityKeys[2].Scheme = schID("is.nothing")
		}},
		{"identity strength outside strong and weak", model.CodeClosedList, func(d *model.Definitions) { asset(d).IdentityKeys[0].Strength = "firm" }},
		{"ext type changes the identity keys of its parent", model.CodeIdentityKeyChanged, func(d *model.Definitions) {
			objectType(d, "ot.acme_server").IdentityKeys = []model.IdentityKey{{Components: []model.KeyComponent{{Property: prID("pr.acme_server.owner")}}, Strength: model.IdentityWeak}}
		}},
		{"ext type reorders the identity keys of its parent", model.CodeIdentityKeyChanged, func(d *model.Definitions) {
			keys := objectType(d, "ot.asset").IdentityKeys
			objectType(d, "ot.acme_server").IdentityKeys = []model.IdentityKey{keys[1], keys[0], keys[2]}
		}},
		{"identifier scheme with a name that is not snake_case", model.CodeNameInvalid, func(d *model.Definitions) { d.Schemes[0].Name = "Vendor-Id" }},
		{"identifier scheme with no name", model.CodeRequiredField, func(d *model.Definitions) { d.Schemes[0].Name = "" }},
		{"identifier scheme with a normaliser that is not a value type", model.CodeSchemeUnknown, func(d *model.Definitions) {
			d.Schemes[0].Normalizer = vtID("vt.nothing")
		}},
		{"identifier scheme with a namespace outside the four", model.CodeClosedList, func(d *model.Definitions) { d.Schemes[0].Namespace = "team" }},
		{"identifier scheme id used twice", model.CodeDuplicateID, func(d *model.Definitions) { d.Schemes = append(d.Schemes, d.Schemes[0]) }},
		{"identifier scheme with no id", model.CodeRequiredField, func(d *model.Definitions) { d.Schemes[0].ID = model.IdentifierSchemeID{} }},
		{"identifier scheme name used by an ext scheme and a std one", model.CodeNameReserved, func(d *model.Definitions) {
			d.Schemes = append(d.Schemes, model.IdentifierScheme{ID: schID("is.other"), Name: "vendor_id", Namespace: model.NamespaceExt, Normalizer: vtID("vt.string")})
		}},
		{"[REQ-100] title property that is not a property", model.CodeTitleProperty, func(d *model.Definitions) { asset(d).TitleProperty = prID("pr.server.hostname") }},
		{"[REQ-101] need for a channel outside the six", model.CodeClosedList, func(d *model.Definitions) { d.Traits[0].Needs[0].Channel = "light" }},
		{"[REQ-101] need with a strength outside hard and soft", model.CodeClosedList, func(d *model.Definitions) { d.Traits[0].Needs[0].Strength = "firm" }},
		{"[REQ-101] need declared twice", model.CodeNeed, func(d *model.Definitions) {
			d.Traits[0].Needs = append(d.Traits[0].Needs, model.Need{Channel: model.ChannelPower, Strength: model.StrengthSoft})
		}},
		{"[REQ-101] need for power without a strength", model.CodeNeed, func(d *model.Definitions) { objectType(d, "ot.server").Needs[0].Strength = "" }},
	})
}

func TestRulesOfNumbers(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"[REQ-233] negative type number", model.CodeNumber, func(d *model.Definitions) { objectType(d, "ot.asset").TypeNo = -1 }},
		{"[REQ-233] type number used twice", model.CodeNumber, func(d *model.Definitions) {
			objectType(d, "ot.asset").TypeNo = 5
			objectType(d, "ot.rack").TypeNo = 5
		}},
		{"[REQ-233] type number that is reserved", model.CodeNumber, func(d *model.Definitions) {
			d.Reserved.TypeNos = []int{1}
		}},
		{"[REQ-233] link type number used twice", model.CodeNumber, func(d *model.Definitions) {
			d.LinkTypes[0].LinkTypeNo, d.LinkTypes[1].LinkTypeNo = 3, 3
		}},
		{"[REQ-233] link type number reserved", model.CodeNumber, func(d *model.Definitions) {
			d.LinkTypes[0].LinkTypeNo = 4
			d.Reserved.LinkTypeNos = []int{4}
		}},
		{"[REQ-233] attribute number used twice", model.CodeNumber, func(d *model.Definitions) {
			objectType(d, "ot.asset").Properties[0].AttrNo = 9
			objectType(d, "ot.server").Properties[0].AttrNo = 9
		}},
		{"[REQ-233] attribute number reserved", model.CodeNumber, func(d *model.Definitions) {
			objectType(d, "ot.asset").Properties[0].AttrNo = 2
			d.Reserved.AttrNos = []int{2}
		}},
		{"[REQ-233] reserved number that is not positive", model.CodeNumber, func(d *model.Definitions) { d.Reserved.AttrNos = []int{0} }},
	})
}

func TestRulesOfLinks(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"[REQ-101] meaning outside the eleven", model.CodeClosedList, func(d *model.Definitions) { linkType(d, "lt.runs_on").Meaning = "depends" }},
		{"[REQ-100] namespace of a link type outside the four", model.CodeClosedList, func(d *model.Definitions) { linkType(d, "lt.runs_on").Namespace = "team" }},
		{"[REQ-100] delay class outside the two", model.CodeClosedList, func(d *model.Definitions) { linkType(d, "lt.runs_on").DelayClass = "later" }},
		{"[REQ-101] status of a link type outside the three", model.CodeClosedList, func(d *model.Definitions) { linkType(d, "lt.runs_on").Status = "gone" }},
		{"[REQ-101] parameter the meaning does not have", model.CodeLinkMeaning, func(d *model.Definitions) {
			linkType(d, "lt.mounted_in").MeaningParams = map[string]string{"strength": "hard"}
		}},
		{"[REQ-101] parameter that is not an enum", model.CodeLinkMeaning, func(d *model.Definitions) {
			linkType(d, "lt.runs_on").MeaningParams = map[string]string{"failover_to": "ot.cluster"}
		}},
		{"[REQ-101] value outside the parameter", model.CodeLinkMeaning, func(d *model.Definitions) {
			linkType(d, "lt.powered_by").MeaningParams["resource"] = "light"
		}},
		{"[REQ-101] supplied_by without a resource", model.CodeLinkMeaning, func(d *model.Definitions) { linkType(d, "lt.powered_by").MeaningParams = nil }},
		{"[REQ-101] resource that is the service channel", model.CodeLinkMeaning, func(d *model.Definitions) {
			linkType(d, "lt.powered_by").MeaningParams["resource"] = "service"
		}},
		{"link property that is volatile by its class", model.CodeLinkVolatile, func(d *model.Definitions) {
			linkType(d, "lt.powered_by").Properties[0].ChangeClass = model.ChangeDynamic
		}},
		{"link property that is volatile by its history", model.CodeLinkVolatile, func(d *model.Definitions) {
			linkType(d, "lt.powered_by").Properties[0].HistoryKind = model.HistoryVolatile
		}},
		{"[REQ-100] binding end that is an unknown type", model.CodeBindingEndpoint, func(d *model.Definitions) {
			linkType(d, "lt.runs_on").Bindings[0].To = typeEnd("ot.nothing")
		}},
		{"[REQ-100] binding end that is an unknown trait", model.CodeBindingEndpoint, func(d *model.Definitions) {
			linkType(d, "lt.runs_on").Bindings[0].From = traitEnd("tr.nothing")
		}},
		{"[REQ-100] binding end for any object that names a type", model.CodeBindingEndpoint, func(d *model.Definitions) {
			linkType(d, "lt.powered_by").Bindings[0].To = model.Endpoint{Kind: model.EndpointAny, Ref: "ot.rack"}
		}},
		{"[REQ-100] binding end of an unknown kind", model.CodeClosedList, func(d *model.Definitions) {
			linkType(d, "lt.runs_on").Bindings[0].From = model.Endpoint{Kind: "layer", Ref: "network"}
		}},
		{"[REQ-100] cardinality outside the four", model.CodeClosedList, func(d *model.Definitions) { linkType(d, "lt.runs_on").Bindings[0].Cardinality = "2:3" }},
		{"[REQ-100] binding without an id", model.CodeRequiredField, func(d *model.Definitions) { linkType(d, "lt.runs_on").Bindings[0].ID = model.BindingID{} }},
		{"[REQ-100] the same binding twice", model.CodeBindingDuplicate, func(d *model.Definitions) {
			l := linkType(d, "lt.runs_on")
			again := l.Bindings[0].Clone()
			again.ID = bdID("bd.runs_on.again")
			l.Bindings = append(l.Bindings, again)
		}},
		{"[REQ-101] constraint on a parameter a binding cannot narrow", model.CodeBindingConstraint, func(d *model.Definitions) {
			linkType(d, "lt.part_of").Bindings[0].ParamConstraints = map[string][]string{"demand": {"1"}}
		}},
		{"[REQ-101] constraint with a value outside the parameter", model.CodeBindingConstraint, func(d *model.Definitions) {
			linkType(d, "lt.part_of").Bindings[0].ParamConstraints = map[string][]string{"whole_needs_part": {"maybe"}}
		}},
		{"[REQ-101] constraint that widens what the link type fixes", model.CodeBindingConstraint, func(d *model.Definitions) {
			linkType(d, "lt.powered_by").Bindings[0].ParamConstraints = map[string][]string{"resource": {"power", "cooling"}}
		}},
		{"[REQ-101] constraint that lists no value", model.CodeBindingConstraint, func(d *model.Definitions) {
			linkType(d, "lt.part_of").Bindings[0].ParamConstraints = map[string][]string{"whole_needs_part": {}}
		}},
		{"[REQ-101] failover target that is not a type", model.CodeBindingConstraint, func(d *model.Definitions) {
			linkType(d, "lt.runs_on").Bindings[0].ParamConstraints = map[string][]string{"failover_to": {"ot.nothing"}}
		}},
	})
}

func TestRulesOfActions(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"[REQ-100] action that mutates without reversibility", model.CodeActionRisk, func(d *model.Definitions) { actionType(d, "ac.power_on").Reversibility = "" }},
		{"[REQ-100] action that mutates with a reversibility outside the two", model.CodeActionRisk, func(d *model.Definitions) {
			actionType(d, "ac.power_on").Reversibility = "maybe"
		}},
		{"[REQ-100] action that does not mutate with a reversibility", model.CodeActionRisk, func(d *model.Definitions) {
			actionType(d, "ac.get_status").Reversibility = model.Reversible
		}},
		{"[REQ-100] action with an inverse that is irreversible", model.CodeActionRisk, func(d *model.Definitions) {
			actionType(d, "ac.power_off").Reversibility = model.Irreversible
		}},
		{"[REQ-100] disruption outside the three", model.CodeClosedList, func(d *model.Definitions) { actionType(d, "ac.power_on").Disruption = "outage" }},
		{"[REQ-100] namespace of an action outside the four", model.CodeClosedList, func(d *model.Definitions) { actionType(d, "ac.power_on").Namespace = "team" }},
		{"[REQ-100] action without a target", model.CodeRequiredField, func(d *model.Definitions) { actionType(d, "ac.power_on").AppliesTo = nil }},
		{"[REQ-100] action on a type that does not exist", model.CodeActionRef, func(d *model.Definitions) {
			actionType(d, "ac.power_on").AppliesTo = []model.Endpoint{typeEnd("ot.nothing")}
		}},
		{"[REQ-100] action on a trait that does not exist", model.CodeActionRef, func(d *model.Definitions) {
			actionType(d, "ac.get_status").AppliesTo = []model.Endpoint{traitEnd("tr.nothing")}
		}},
		{"[REQ-100] inverse that does not exist", model.CodeActionRef, func(d *model.Definitions) { actionType(d, "ac.power_off").Inverse = acID("ac.nothing") }},
		{"[REQ-100] action that is its own inverse", model.CodeActionRef, func(d *model.Definitions) { actionType(d, "ac.power_off").Inverse = acID("ac.power_off") }},
		{"[REQ-100] action that supersedes itself", model.CodeSuperseded, func(d *model.Definitions) {
			actionType(d, "ac.power_on").Status = model.StatusDeprecated
			actionType(d, "ac.power_on").SupersededBy = acID("ac.power_on")
		}},
	})
}

func TestWhatIsAllowed(t *testing.T) {
	tests := []struct {
		name   string
		repair func(d *model.Definitions)
	}{
		{"an ext type that keeps the identity keys of its parent", func(d *model.Definitions) {
			objectType(d, "ot.acme_server").IdentityKeys = objectType(d, "ot.asset").IdentityKeys
		}},
		{"an ext type that adds a property and keeps nothing else", func(d *model.Definitions) {
			objectType(d, "ot.acme_server").Properties = append(objectType(d, "ot.acme_server").Properties,
				prop("pr.acme_server.rack_row", "rack_row", "vt.string", model.ChangeSlow))
		}},
		{"[REQ-101] the management need without a strength", func(d *model.Definitions) {
			objectType(d, "ot.server").Needs = append(objectType(d, "ot.server").Needs, model.Need{Channel: model.ChannelManagement})
		}},
		{"a dynamic property that says it is versioned", func(d *model.Definitions) {
			property(objectType(d, "ot.asset"), "pr.asset.power_state").HistoryKind = model.HistoryVersioned
		}},
		{"a deprecated type that points at its successor", func(d *model.Definitions) {
			objectType(d, "ot.rack").Status = model.StatusDeprecated
			objectType(d, "ot.rack").SupersededBy = otID("ot.asset")
		}},
		{"exactly four scope keys", func(d *model.Definitions) {
			for _, name := range []string{"a", "b", "c"} {
				key := prop("pr.asset.scope_"+name, "scope_"+name, "vt.string", model.ChangeSlow)
				key.ScopeKey = true
				objectType(d, "ot.asset").Properties = append(objectType(d, "ot.asset").Properties, key)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := baseline()
			tt.repair(&d)
			if errs := model.Check(d); len(errs) != 0 {
				t.Fatalf("Check() = %v, want none", errs)
			}
		})
	}
}
