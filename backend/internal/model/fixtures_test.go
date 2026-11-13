package model_test

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The fixtures: a small sound set of definitions that every rule test starts
// from and damages in exactly one way.

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func otID(s string) model.ObjectTypeID { return must(kernel.ParseID[model.ObjectTypeKind](s)) }
func prID(s string) model.PropertyID   { return must(kernel.ParseID[model.PropertyKind](s)) }
func vtID(s string) model.ValueTypeID  { return must(kernel.ParseID[model.ValueTypeKind](s)) }
func schID(s string) model.IdentifierSchemeID {
	return must(kernel.ParseID[model.IdentifierSchemeKind](s))
}
func trID(s string) model.TraitID      { return must(kernel.ParseID[model.TraitKind](s)) }
func ltID(s string) model.LinkTypeID   { return must(kernel.ParseID[model.LinkTypeKind](s)) }
func bdID(s string) model.BindingID    { return must(kernel.ParseID[model.BindingKind](s)) }
func acID(s string) model.ActionTypeID { return must(kernel.ParseID[model.ActionTypeKind](s)) }

func typeEnd(id string) model.Endpoint  { return model.Endpoint{Kind: model.EndpointType, Ref: id} }
func traitEnd(id string) model.Endpoint { return model.Endpoint{Kind: model.EndpointTrait, Ref: id} }

func prop(id, name, valueType string, class model.ChangeClass) model.Property {
	return model.Property{ID: prID(id), Name: name, ValueType: vtID(valueType), ChangeClass: class}
}

// baseline is sound: Check finds nothing in it.
func baseline() model.Definitions {
	scope := prop("pr.asset.site", "site", "vt.string", model.ChangeSlow)
	scope.ScopeKey = true
	mode := prop("pr.group.mode", "mode", "vt.enum", model.ChangeSlow)
	mode.EnumValues = []string{"count", "capacity"}
	capacity := prop("pr.group.min_capacity", "min_capacity", "vt.quantity", model.ChangeSlow)
	capacity.Unit = "w"
	return model.Definitions{
		ModelVersion: 1,
		Schemes:      []model.IdentifierScheme{{ID: schID("is.vendor_id"), Name: "vendor_id", Namespace: model.NamespaceStd, Normalizer: vtID("vt.string")}},
		ValueTypes:   []model.ValueType{{ID: vtID("vt.fqdn"), Name: "fqdn", Base: model.BaseString}},
		Traits: []model.Trait{
			{ID: trID("tr.powered"), Name: "Powered",
				Needs:      []model.Need{{Channel: model.ChannelPower, Strength: model.StrengthHard}, {Channel: model.ChannelManagement}},
				Properties: []model.Property{prop("pr.powered.rated_power_w", "rated_power_w", "vt.number", model.ChangeSlow)}},
			{ID: trID("tr.rack_mountable"), Name: "RackMountable"},
		},
		ObjectTypes: []model.ObjectType{
			{ID: otID("ot.asset"), TypeNo: 1, Name: "Asset", Namespace: model.NamespaceStd, Layer: model.LayerHardware, Status: model.StatusActive,
				Properties: []model.Property{
					prop("pr.asset.serial_number", "serial_number", "vt.string", model.ChangeStatic), scope,
					prop("pr.asset.power_state", "power_state", "vt.string", model.ChangeDynamic)},
				IdentityKeys: []model.IdentityKey{
					{Components: []model.KeyComponent{{Property: prID("pr.asset.serial_number")}}, Strength: model.IdentityStrong},
					{Components: []model.KeyComponent{{Context: ltID("lt.part_of")}, {Property: prID("pr.asset.site")}}, Strength: model.IdentityWeak},
					{Strength: model.IdentityWeak, Scheme: schID("is.vendor_id")}},
				TitleProperty: prID("pr.asset.serial_number")},
			{ID: otID("ot.server"), Name: "Server", Namespace: model.NamespaceStd, Layer: model.LayerHardware, Status: model.StatusActive,
				Parent: otID("ot.asset"), Traits: []model.TraitID{trID("tr.powered"), trID("tr.rack_mountable")},
				Needs:      []model.Need{{Channel: model.ChannelPower, Strength: model.StrengthSoft}},
				Properties: []model.Property{prop("pr.server.hostname", "hostname", "vt.fqdn", model.ChangeSlow)}},
			{ID: otID("ot.rack"), Name: "Rack", Namespace: model.NamespaceStd, Layer: model.LayerFacility, Status: model.StatusActive},
			{ID: otID("ot.redundancy_group"), Name: "RedundancyGroup", Namespace: model.NamespaceStd, Layer: model.LayerCrossCutting, Status: model.StatusActive,
				Properties: []model.Property{mode, capacity}},
			{ID: otID("ot.cluster"), Name: "Cluster", Namespace: model.NamespaceStd, Layer: model.LayerVirtualization, Status: model.StatusActive,
				Parent: otID("ot.redundancy_group")},
			{ID: otID("ot.acme_server"), Name: "AcmeServer", Namespace: model.NamespaceExt, Layer: model.LayerHardware, Status: model.StatusExperimental,
				Parent:     otID("ot.server"),
				Properties: []model.Property{prop("pr.acme_server.owner", "owner", "vt.string", model.ChangeSlow)}},
		},
		LinkTypes: []model.LinkType{
			{ID: ltID("lt.mounted_in"), Name: "MOUNTED_IN", Namespace: model.NamespaceStd, Meaning: model.MeaningContainedIn, Status: model.StatusActive,
				Bindings: []model.Binding{
					{ID: bdID("bd.mounted_in.server.rack"), From: typeEnd("ot.server"), To: typeEnd("ot.rack"), Cardinality: model.CardManyToOne},
					{ID: bdID("bd.mounted_in.mountable.rack"), From: traitEnd("tr.rack_mountable"), To: typeEnd("ot.rack"), Cardinality: model.CardManyToMany}}},
			{ID: ltID("lt.powered_by"), Name: "POWERED_BY", Namespace: model.NamespaceStd, Meaning: model.MeaningSuppliedBy, Status: model.StatusActive,
				MeaningParams: map[string]string{"resource": "power"},
				Properties:    []model.Property{prop("pr.powered_by.share", "share", "vt.number", model.ChangeSlow)},
				Bindings: []model.Binding{
					{ID: bdID("bd.powered_by.powered.any"), From: traitEnd("tr.powered"), To: model.Endpoint{Kind: model.EndpointAny}, Cardinality: model.CardManyToMany}}},
			{ID: ltID("lt.runs_on"), Name: "RUNS_ON", Namespace: model.NamespaceStd, Meaning: model.MeaningRunsOn, Status: model.StatusActive,
				Bindings: []model.Binding{
					{ID: bdID("bd.runs_on.server.server"), From: typeEnd("ot.server"), To: typeEnd("ot.server"), Cardinality: model.CardManyToOne,
						ParamConstraints: map[string][]string{"failover_to": {"ot.cluster"}}}}},
			{ID: ltID("lt.part_of"), Name: "PART_OF", Namespace: model.NamespaceStd, Meaning: model.MeaningPartOf, Status: model.StatusActive,
				Bindings: []model.Binding{
					{ID: bdID("bd.part_of.asset.server"), From: typeEnd("ot.asset"), To: typeEnd("ot.server"), Cardinality: model.CardManyToOne,
						ParamConstraints: map[string][]string{"whole_needs_part": {"hard"}}}}},
		},
		ActionTypes: []model.ActionType{
			{ID: acID("ac.power_off"), Name: "power_off", Namespace: model.NamespaceStd, AppliesTo: []model.Endpoint{typeEnd("ot.server")},
				Mutates: true, Reversibility: model.Reversible, Disruption: model.DisruptionServiceAffecting, Inverse: acID("ac.power_on"), Status: model.StatusActive},
			{ID: acID("ac.power_on"), Name: "power_on", Namespace: model.NamespaceStd, AppliesTo: []model.Endpoint{typeEnd("ot.server")},
				Mutates: true, Reversibility: model.Reversible, Disruption: model.DisruptionNone, Status: model.StatusActive},
			{ID: acID("ac.get_status"), Name: "get_status", Namespace: model.NamespaceStd, AppliesTo: []model.Endpoint{traitEnd("tr.powered")},
				Disruption: model.DisruptionNone, Status: model.StatusActive},
		},
	}
}

func objectType(d *model.Definitions, id string) *model.ObjectType {
	for i := range d.ObjectTypes {
		if d.ObjectTypes[i].ID.String() == id {
			return &d.ObjectTypes[i]
		}
	}
	panic("no object type " + id)
}

func linkType(d *model.Definitions, id string) *model.LinkType {
	for i := range d.LinkTypes {
		if d.LinkTypes[i].ID.String() == id {
			return &d.LinkTypes[i]
		}
	}
	panic("no link type " + id)
}

func actionType(d *model.Definitions, id string) *model.ActionType {
	for i := range d.ActionTypes {
		if d.ActionTypes[i].ID.String() == id {
			return &d.ActionTypes[i]
		}
	}
	panic("no action type " + id)
}

func property(t *model.ObjectType, id string) *model.Property {
	for i := range t.Properties {
		if t.Properties[i].ID.String() == id {
			return &t.Properties[i]
		}
	}
	panic("no property " + id)
}

func hasCode(errs []*kernel.Error, code kernel.Code) bool {
	for _, err := range errs {
		if err.Code == code {
			return true
		}
	}
	return false
}
