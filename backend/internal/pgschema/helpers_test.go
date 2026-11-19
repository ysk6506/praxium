package pgschema_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

func mustID[K any](t *testing.T, raw string) kernel.ID[K] {
	t.Helper()
	id, err := kernel.ParseID[K](raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func prop(t *testing.T, id, name string, no int, vt string, class model.ChangeClass) model.Property {
	t.Helper()
	return model.Property{
		ID: mustID[model.PropertyKind](t, id), AttrNo: no, Name: name,
		ValueType: mustID[model.ValueTypeKind](t, vt), ChangeClass: class,
	}
}

func objType(t *testing.T, id string, no int, name string, traits []string, props ...model.Property) model.ObjectType {
	t.Helper()
	ot := model.ObjectType{
		ID: mustID[model.ObjectTypeKind](t, id), TypeNo: no, Name: name, Namespace: model.NamespaceStd,
		Layer: model.LayerHardware, Status: model.StatusActive, Properties: props,
	}
	for _, tr := range traits {
		ot.Traits = append(ot.Traits, mustID[model.TraitKind](t, tr))
	}
	return ot
}

// smallDefs is two types: a server with a key, a time, a list and a volatile value of its own, and a rack
// with one number. Both have the trait that gives a name and two volatile states.
func smallDefs(t *testing.T) model.Definitions {
	t.Helper()
	common := model.Trait{
		ID: mustID[model.TraitKind](t, "tr.common"), Name: "Common",
		Properties: []model.Property{
			prop(t, "pr.common.name", "name", 1, "vt.string", model.ChangeSlow),
			prop(t, "pr.common.life", "life", 4, "vt.string", model.ChangeDynamic),
			prop(t, "pr.common.oper", "oper", 7, "vt.string", model.ChangeDynamic),
		},
	}
	server := objType(t, "ot.server", 1, "Server", []string{"tr.common"},
		prop(t, "pr.server.serial", "serial", 10, "vt.string", model.ChangeStatic),
		prop(t, "pr.server.cpus", "cpus", 11, "vt.integer", model.ChangeSlow),
		prop(t, "pr.server.booted", "booted", 12, "vt.time", model.ChangeSlow),
		prop(t, "pr.server.temp", "temp", 13, "vt.number", model.ChangeDynamic),
		prop(t, "pr.server.tags", "tags", 14, "vt.list", model.ChangeSlow),
	)
	server.IdentityKeys = []model.IdentityKey{{
		Components: []model.KeyComponent{{Property: mustID[model.PropertyKind](t, "pr.server.serial")}},
		Strength:   model.IdentityStrong,
	}}
	units := prop(t, "pr.rack.units", "units", 20, "vt.quantity", model.ChangeSlow)
	units.Unit = "u"
	rack := objType(t, "ot.rack", 2, "Rack", []string{"tr.common"}, units)
	return model.Definitions{ModelVersion: 1, Traits: []model.Trait{common}, ObjectTypes: []model.ObjectType{server, rack}}
}

func registry(t *testing.T, d model.Definitions) *model.Registry {
	t.Helper()
	reg, err := model.NewRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

type modelVT = model.ValueTypeKind

func kinds() []model.BaseKind { return model.BaseKinds() }
