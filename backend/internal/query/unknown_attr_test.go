package query

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const orgAttrNo = model.OrgNumberBase + 7

// withOrgProperty is the standard model with an organization property on PhysicalServer.
func withOrgProperty(t *testing.T, fx *graph.Fixture) *model.Registry {
	t.Helper()
	id, err := kernel.ParseID[model.PropertyKind]("pr.org.physical_server.department")
	if err != nil {
		t.Fatal(err)
	}
	vt, err := kernel.ParseID[model.ValueTypeKind]("vt.string")
	if err != nil {
		t.Fatal(err)
	}
	prop := model.Property{ID: id, AttrNo: orgAttrNo, Name: "org.department", ValueType: vt, ChangeClass: model.ChangeSlow}
	reg, errs := model.ComposeRegistry(fx.Reg.Definitions(), []model.Overlay{model.NewOverlay(model.TargetObjectType, "ot.physical_server", []model.Property{prop})})
	if len(errs) > 0 {
		t.Fatalf("the model of the test: %v", errs)
	}
	return reg
}

// [REQ-372]
func TestAnAttributeNumberTheModelDoesNotKnowIsPassedOverNotShownWithoutAName(t *testing.T) {
	r := newRigWith(t, withOrgProperty)
	b := r.fx.World(2, 2, 3)
	server, _ := r.reg.ObjectTypeByName("PhysicalServer")
	servers := 0
	for i := range b.Objects {
		if b.Objects[i].Type == server.ID {
			servers++
			b.Objects[i].Attrs = map[int]graph.Attr{
				graph.AttrServerModel: {Value: "M1", Grade: model.GradeFact, Basis: model.BasisConfigured, SourceTime: seenAt, SourceRef: "src"},
				orgAttrNo:             {Value: "Infra Team 1", Grade: model.GradeFact, Basis: model.BasisConfigured, SourceTime: seenAt, SourceRef: "src"},
			}
		}
	}
	r.apply(b)
	id := r.fx.ServerID("SN-000102")
	know, err := r.get(GetInput{ObjectID: id.String(), Include: []string{IncludeAttributes}}, AllowAll())
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := attrByName(know.Data.Attributes, "org.department"); !ok || a.Value != "Infra Team 1" {
		t.Fatalf("a model that knows the property shows it: %+v", know.Data.Attributes)
	}
	// An engine whose model is older than the rows: the standard model alone.
	standard, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	old, err := New(r.store, standard, r.clock)
	if err != nil {
		t.Fatal(err)
	}
	got, err := old.Get(r.ctx, GetInput{Common: r.common(), ObjectID: id.String(), Include: []string{IncludeAttributes}}, AllowAll())
	if err != nil {
		t.Fatalf("an unknown attribute number must not fail the read: %v", err)
	}
	for _, a := range got.Data.Attributes {
		if a.Name == "" || a.Value == "Infra Team 1" {
			t.Fatalf("a value of an unknown attribute is passed over, never shown without a name: %+v", a)
		}
	}
	if a, ok := attrByName(got.Data.Attributes, "model"); !ok || a.Value != "M1" {
		t.Fatalf("what the old model knows is still there: %+v", got.Data.Attributes)
	}
	// A text search looks into the values it knows the names of; the value of the unknown number is not searched.
	found := func(e *Engine) int {
		a, err := e.Find(r.ctx, FindInput{Common: r.common(), ObjectType: "PhysicalServer", Text: "infra team"}, AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		return len(a.Data.Items)
	}
	if found(r.eng) != servers || found(old) != 0 {
		t.Fatalf("the model that knows the property finds every server by it, the older one none: %d %d of %d", found(r.eng), found(old), servers)
	}
	_, err = old.Find(r.ctx, FindInput{Common: r.common(), ObjectType: "PhysicalServer", Attributes: []AttrFilter{{Name: "org.department", Op: OpEquals, Values: []string{"Infra Team 1"}}}}, AllowAll())
	if kernel.CodeOf(err) != CodeFilterField {
		t.Fatalf("a filter on a name the model lacks is told so: %v", err)
	}
}
