package devload

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

var loadedAt = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

func standard(t testing.TB) *model.Registry {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// generated is a world made by the generator, in memory.
type generated struct {
	truth    []byte
	manifest *worldgen.Manifest
}

// generate makes the world of a seed and a scale; tweak may change the parameters.
func generate(t testing.TB, reg *model.Registry, seed uint64, scale int64, tweak func(*worldgen.Params)) generated {
	t.Helper()
	p := worldgen.DefaultParams(seed, scale)
	if tweak != nil {
		tweak(&p)
	}
	w, err := worldgen.Build(reg, p)
	if err != nil {
		t.Fatal(err)
	}
	st := worldgen.NewMemStore()
	m, err := w.Run(st, worldgen.RunOptions{Truth: true})
	if err != nil {
		t.Fatal(err)
	}
	return generated{truth: st.Files[worldgen.TruthFile].Bytes(), manifest: m}
}

// loaded is a KG in a scratch store with a world loaded into it.
type loaded struct {
	store  *graph.ScratchStore
	kg     graph.KG
	report Report
	engine *query.Engine
	reg    *model.Registry
}

// loadWorld loads a truth into a fresh store with fixed clock and ids, so two loads are alike.
func loadWorld(t testing.TB, reg *model.Registry, truth []byte, batch int) loaded {
	t.Helper()
	clock := kernel.NewManualClock(loadedAt)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	store := graph.NewScratchStore()
	ctx := context.Background()
	kg, err := NewKG(ctx, store, reg, ids, clock, "dev")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Loader{Store: store, Reg: reg, Clock: clock, IDs: ids, BatchItems: batch}.Load(ctx, kg.ID, bytes.NewReader(truth))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := query.New(store, reg, clock)
	if err != nil {
		t.Fatal(err)
	}
	return loaded{store: store, kg: kg, report: rep, engine: engine, reg: reg}
}

func (l loaded) common() query.Common {
	return query.Common{KG: graph.MainAlias, Limit: query.MaxLimit, Shape: query.ShapeItems}
}

// get reads one object with its attributes and link counts.
func (l loaded) get(t testing.TB, id graph.ObjectID) query.GetData {
	t.Helper()
	ans, err := l.engine.Get(context.Background(), query.GetInput{Common: l.common(), ObjectID: id.String(),
		Include: []string{"attributes", "identifiers", "link_counts"}}, query.AllowAll())
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return ans.Data
}

// neighbors reads every item around an object, page by page.
func (l loaded) neighbors(t testing.TB, id graph.ObjectID) []query.NeighborItem {
	t.Helper()
	var items []query.NeighborItem
	in := query.NeighborsInput{Common: l.common(), ObjectID: id.String(), Direction: query.DirBoth}
	for {
		ans, err := l.engine.Neighbors(context.Background(), in, query.AllowAll())
		if err != nil {
			t.Fatalf("neighbors %s: %v", id, err)
		}
		items = append(items, ans.Data.Items...)
		if ans.Meta.ResultComplete.NextCursor == nil {
			return items
		}
		in.Cursor = *ans.Meta.ResultComplete.NextCursor
	}
}

// ids resolves the truth the way the loader does and returns the loadable objects by truth id.
func ids(t testing.TB, reg *model.Registry, truth []byte) *world {
	t.Helper()
	w, err := readWorld(reg, bytes.NewReader(truth))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.resolveAll(); err != nil {
		t.Fatal(err)
	}
	return w
}

// shared worlds are generated and loaded once for all the tests of the package; they are read only.
var shared = map[int]struct {
	g generated
	l loaded
}{}

// sharedWorld returns smallWorlds[i] generated and loaded with the default batch size.
func sharedWorld(t testing.TB, i int) (generated, loaded) {
	t.Helper()
	if w, ok := shared[i]; ok {
		return w.g, w.l
	}
	reg := standard(t)
	sw := smallWorlds[i]
	g := generate(t, reg, sw.seed, sw.scale, sw.tweak)
	l := loadWorld(t, reg, g.truth, 0)
	shared[i] = struct {
		g generated
		l loaded
	}{g, l}
	return g, l
}
