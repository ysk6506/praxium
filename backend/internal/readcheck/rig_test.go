package readcheck

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
)

var epoch = time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

const seenAt = "2030-01-01T10:00:00Z"

// rig is a world in the in-memory reference store with an engine over it.
type rig struct {
	t     *testing.T
	ctx   context.Context
	store *memstore.Store
	fx    *graph.Fixture
	a     *graph.Applier
	kg    graph.KG
	lease graph.Lease
	eng   *query.Engine
}

func newRig(t *testing.T, _ bool) *rig {
	t.Helper()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(epoch)
	r := &rig{t: t, ctx: context.Background(), store: memstore.New(), fx: fx}
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	if r.kg, err = graph.NewTestKG(r.ctx, r.store, fx.Reg, ids, clock, graph.KGSpec{Name: "main-kg", Role: graph.RoleProduction}); err != nil {
		t.Fatal(err)
	}
	if r.a, err = graph.NewApplier(r.store, fx.Reg, graph.NewTestKeyProvider("q"), clock, ids, graph.Options{Semantic: 1}); err != nil {
		t.Fatal(err)
	}
	if r.lease, err = r.a.Acquire(r.ctx, r.kg.ID); err != nil {
		t.Fatal(err)
	}
	if r.eng, err = query.New(r.store, fx.Reg, clock); err != nil {
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
	server, _ := r.fx.Reg.ObjectTypeByName("PhysicalServer")
	for i := range b.Objects {
		if o := &b.Objects[i]; o.Type == server.ID {
			o.Attrs = map[int]graph.Attr{graph.AttrServerModel: {Value: "M1", Grade: model.GradeFact, Basis: model.BasisConfigured, SourceTime: seenAt, SourceRef: "src-inventory"}}
		}
	}
	r.apply(b)
	return b
}

func (r *rig) find(in query.FindInput, f query.ReadFilter) (query.Answer[query.FindData], error) {
	return r.eng.Find(r.ctx, in, f)
}

func (r *rig) mustFind(in query.FindInput) query.Answer[query.FindData] {
	r.t.Helper()
	if in.KG == "" {
		in.Common = r.common()
	}
	a, err := r.find(in, query.AllowAll())
	if err != nil {
		r.t.Fatalf("find: %v", err)
	}
	return a
}

func (r *rig) common() query.Common { return query.Common{KG: "main", Shape: query.ShapeItems} }

func (r *rig) get(in query.GetInput, f query.ReadFilter) (query.Answer[query.GetData], error) {
	if in.KG == "" {
		in.Common = r.common()
	}
	return r.eng.Get(r.ctx, in, f)
}

func (r *rig) neighbors(in query.NeighborsInput, f query.ReadFilter) (query.Answer[query.NeighborsData], error) {
	if in.KG == "" {
		in.Common = r.common()
	}
	return r.eng.Neighbors(r.ctx, in, f)
}

func (r *rig) must(in query.NeighborsInput, f query.ReadFilter) query.Answer[query.NeighborsData] {
	r.t.Helper()
	a, err := r.neighbors(in, f)
	if err != nil {
		r.t.Fatalf("neighbors: %v", err)
	}
	return a
}

func (r *rig) powered(serial string) {
	r.t.Helper()
	r.apply(graph.Batch{Volatile: []graph.VolatileWrite{r.fx.Power(r.fx.ServerID(serial), "off", seenAt)}})
}

func (r *rig) headSeq() int64 {
	r.t.Helper()
	h, err := r.store.Head(r.ctx, r.kg.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	return h.ReadableThrough
}

func codeOf(err error) kernel.Code { return kernel.CodeOf(err) }

func attrByName(attrs []query.AttributeValue, name string) (query.AttributeValue, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a, true
		}
	}
	return query.AttributeValue{}, false
}
