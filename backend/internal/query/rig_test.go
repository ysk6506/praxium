package query

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
)

var epoch = time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

const seenAt = "2030-01-01T10:00:00Z"

// rig is a world in the in-memory store with an engine over it.
type rig struct {
	t     *testing.T
	ctx   context.Context
	store *memstore.Store
	fx    *graph.Fixture
	reg   *model.Registry
	a     *graph.Applier
	kg    graph.KG
	lease graph.Lease
	clock *kernel.ManualClock
	eng   *Engine
}

// sensitiveRegistry is the standard model with the server model attribute
// restricted and the site address confidential.
func sensitiveRegistry(t *testing.T, fx *graph.Fixture) *model.Registry {
	return variantRegistry(t, fx, func(p *model.Property) {
		switch p.AttrNo {
		case graph.AttrServerModel:
			p.Sensitivity = model.SensitivityRestricted
		case graph.AttrSiteAddress:
			p.Sensitivity = model.SensitivityConfidential
		}
	})
}

// variantRegistry is the standard model with every property passed through change.
func variantRegistry(t *testing.T, fx *graph.Fixture, change func(*model.Property)) *model.Registry {
	t.Helper()
	defs := fx.Reg.Definitions().Clone()
	mark := func(props []model.Property) {
		for i := range props {
			change(&props[i])
		}
	}
	for i := range defs.ObjectTypes {
		mark(defs.ObjectTypes[i].Properties)
	}
	for i := range defs.Traits {
		mark(defs.Traits[i].Properties)
	}
	reg, err := model.NewRegistry(defs)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func newRig(t *testing.T, sensitive bool) *rig {
	t.Helper()
	if sensitive {
		return newRigWith(t, sensitiveRegistry)
	}
	return newRigWith(t, nil)
}

func newRigWith(t *testing.T, variant func(*testing.T, *graph.Fixture) *model.Registry) *rig {
	t.Helper()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	if variant != nil {
		fx.Reg = variant(t, fx)
	}
	r := &rig{t: t, ctx: context.Background(), store: memstore.New(), fx: fx, reg: fx.Reg, clock: kernel.NewManualClock(epoch)}
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	if r.kg, err = graph.NewTestKG(r.ctx, r.store, fx.Reg, ids, r.clock, graph.KGSpec{Name: "main-kg", Role: graph.RoleProduction}); err != nil {
		t.Fatal(err)
	}
	if r.a, err = graph.NewApplier(r.store, fx.Reg, graph.NewTestKeyProvider("q"), r.clock, ids, graph.Options{Semantic: 1}); err != nil {
		t.Fatal(err)
	}
	if r.lease, err = r.a.Acquire(r.ctx, r.kg.ID); err != nil {
		t.Fatal(err)
	}
	if r.eng, err = New(r.store, fx.Reg, r.clock); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) apply(b graph.Batch) graph.Result {
	r.t.Helper()
	res, err := r.a.Apply(r.ctx, r.kg.ID, r.lease, b)
	if err != nil {
		r.t.Fatalf("apply: %v", err)
	}
	return res
}

// world loads 2 sites with 2 racks each and 3 servers in every rack; the
// servers carry a model seen at a known time from a known source.
func (r *rig) world() graph.Batch {
	b := r.fx.World(2, 2, 3)
	server, _ := r.reg.ObjectTypeByName("PhysicalServer")
	rack, _ := r.reg.ObjectTypeByName("Rack")
	for i := range b.Objects {
		o := &b.Objects[i]
		switch o.Type {
		case server.ID:
			o.Attrs = map[int]graph.Attr{graph.AttrServerModel: {Value: "M1", Grade: model.GradeFact, Basis: model.BasisConfigured, SourceTime: seenAt, SourceRef: "src-inventory"}}
		case rack.ID:
			o.Attrs = map[int]graph.Attr{59: {Value: int64(42), Grade: model.GradeFact, Basis: model.BasisConfigured}}
		}
	}
	r.apply(b)
	return b
}

func (r *rig) common(opts ...func(*Common)) Common {
	c := Common{KG: "main", Shape: ShapeItems}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func (r *rig) find(in FindInput, f ReadFilter) (Answer[FindData], error) {
	return r.eng.Find(r.ctx, in, f)
}

func (r *rig) mustFind(in FindInput) Answer[FindData] {
	r.t.Helper()
	if in.KG == "" {
		in.Common = r.common()
	}
	a, err := r.find(in, AllowAll())
	if err != nil {
		r.t.Fatalf("find: %v", err)
	}
	return a
}

func names(items []FindItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.DisplayName)
	}
	return out
}

func codeOf(err error) kernel.Code { return kernel.CodeOf(err) }

func js(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (r *rig) serverID(serial string) string { return r.fx.ServerID(serial).String() }
