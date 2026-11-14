package readcheck

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// impactWorld is a generated world in the reference store with an engine over
// it, and the truth of the generator to derive expectations from.
type impactWorld struct {
	t     *testing.T
	ctx   context.Context
	eng   *query.Engine
	truth []byte
	reg   *model.Registry
	cache map[string]map[string]string
}

var impactWorlds sync.Map // "seed/scale" -> *impactWorld; the worlds are only read

// loadImpactWorld builds (once per test binary) the world of a seed and a scale.
func loadImpactWorld(t *testing.T, seed uint64, scale int64) *impactWorld {
	t.Helper()
	key := [2]uint64{seed, uint64(scale)}
	if v, ok := impactWorlds.Load(key); ok {
		w := v.(*impactWorld)
		return &impactWorld{t: t, ctx: w.ctx, eng: w.eng, truth: w.truth, reg: w.reg}
	}
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	w, err := worldgen.Build(reg, worldgen.DefaultParams(seed, scale))
	if err != nil {
		t.Fatal(err)
	}
	files := worldgen.NewMemStore()
	if _, err := w.Run(files, worldgen.RunOptions{Truth: true}); err != nil {
		t.Fatal(err)
	}
	truth := files.Files[worldgen.TruthFile].Bytes()
	clock := kernel.NewManualClock(epoch)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	store := memstore.New()
	ctx := context.Background()
	kg, err := devload.NewKG(ctx, store, reg, ids, clock, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (devload.Loader{Store: store, Reg: reg, Clock: clock, IDs: ids}).Load(ctx, kg.ID, bytes.NewReader(truth)); err != nil {
		t.Fatal(err)
	}
	eng, err := query.New(store, reg, clock)
	if err != nil {
		t.Fatal(err)
	}
	iw := &impactWorld{t: t, ctx: ctx, eng: eng, truth: truth, reg: reg}
	impactWorlds.Store(key, iw)
	return iw
}

func (w *impactWorld) itCommon() query.Common {
	return query.Common{KG: graph.MainAlias, Limit: 1000, Shape: query.ShapeItems}
}

// ofType lists the objects of a type (with its subtypes), ordered by id.
func (w *impactWorld) itOfType(typ string) []query.FindItem {
	w.t.Helper()
	a, err := w.eng.Find(w.ctx, query.FindInput{Common: query.Common{KG: graph.MainAlias, Limit: 1000, Shape: query.ShapeItems}, ObjectType: typ}, query.AllowAll())
	if err != nil {
		w.t.Fatal(err)
	}
	return a.Data.Items
}

func (w *impactWorld) itImpact(in query.ImpactInput) query.Answer[query.ImpactData] {
	w.t.Helper()
	in.Common = w.itCommon()
	a, err := w.eng.Impact(w.ctx, in, query.AllowAll())
	if err != nil {
		w.t.Fatalf("impact: %v", err)
	}
	return a
}
