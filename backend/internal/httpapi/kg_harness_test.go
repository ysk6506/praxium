package httpapi_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/access"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/httpapi"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// kgWorld is a server over a scratch store that holds one production KG with a small world.
type kgWorld struct {
	*harness
	store *graph.ScratchStore
	kg    graph.KG
	batch graph.Batch
}

// sampleRackID is the id of the first rack of the world, which the samples of the KG routes name.
var sampleRackID = func() string {
	fx, err := graph.NewFixture()
	if err != nil {
		panic(err)
	}
	return fx.RackID(fx.SiteID("S00"), "R00").String()
}()

// newKGHarness loads 2 sites with 2 racks each and 3 servers in every rack.
func newKGHarness(t *testing.T, roles ...access.Role) *kgWorld {
	t.Helper()
	return buildKGWorld(t, kgOptions{}, roles...)
}

// kgOptions varies the world of a test: a model whose server model attribute is
// restricted, and a wrapper around the store the KG routes read from.
type kgOptions struct {
	restricted bool
	wrap       func(*graph.ScratchStore) httpapi.KGSource
}

func buildKGWorld(t *testing.T, opts kgOptions, roles ...access.Role) *kgWorld {
	t.Helper()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	if opts.restricted {
		defs := fx.Reg.Definitions().Clone()
		restrict := func(props []model.Property) {
			for j := range props {
				if props[j].AttrNo == graph.AttrServerModel {
					props[j].Sensitivity = model.SensitivityRestricted
				}
			}
		}
		for i := range defs.ObjectTypes {
			restrict(defs.ObjectTypes[i].Properties)
		}
		for i := range defs.Traits {
			restrict(defs.Traits[i].Properties)
		}
		if fx.Reg, err = model.NewRegistry(defs); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	clock := kernel.NewManualClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	store := graph.NewScratchStore()
	kg, err := graph.NewTestKG(ctx, store, fx.Reg, ids, clock, graph.KGSpec{Name: "main-kg", Role: graph.RoleProduction})
	if err != nil {
		t.Fatal(err)
	}
	applier, err := graph.NewApplier(store, fx.Reg, graph.NewTestKeyProvider("h"), clock, ids, graph.Options{Semantic: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := applier.Acquire(ctx, kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	batch := fx.World(2, 2, 3)
	server, _ := fx.Reg.ObjectTypeByName("PhysicalServer")
	for i := range batch.Objects {
		if o := &batch.Objects[i]; o.Type == server.ID {
			o.Attrs = map[int]graph.Attr{graph.AttrServerModel: {Value: "M1", Grade: model.GradeFact, Basis: model.BasisConfigured,
				SourceTime: "2030-01-01T10:00:00Z", SourceRef: "src-inventory"}}
		}
	}
	if _, err := applier.Apply(ctx, kg.ID, lease, batch); err != nil {
		t.Fatal(err)
	}
	engine, err := query.New(store, fx.Reg, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) == 0 {
		roles = []access.Role{access.RoleViewer}
	}
	var kgs httpapi.KGSource = store
	if opts.wrap != nil {
		kgs = opts.wrap(store)
	}
	logs := &bytes.Buffer{}
	srv, err := httpapi.New(httpapi.Config{
		Registry: fx.Reg, Authenticator: fixedAuthenticator{token: testToken, roles: roles}, Clock: clock, IDs: kernel.NewSequentialIDs(),
		Logger: slog.New(slog.NewJSONHandler(logs, nil)), Version: "test", Queries: engine, KGs: kgs,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.MarkStarted()
	return &kgWorld{harness: &harness{t: t, server: srv, handler: srv.Handler(), logs: logs}, store: store, kg: kg, batch: batch}
}
