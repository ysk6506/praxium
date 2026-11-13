package readcheck

import (
	"bytes"
	"context"
	"runtime"
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

// The measurements of ADR-0027: what loading the generator's truth into the in-memory store costs, and what the
// three queries cost over it. They are benchmarks, so a plain `go test` does not run them:
//
//	go test ./internal/readcheck -run '^$' -bench . -benchtime 1x -benchmem
//
// A world is named by the scale given to the generator. Scale counts objects and links together, so about a
// third of it is objects.

// measured sizes: small is a few hundred objects, large is about a hundred thousand.
var measured = []struct {
	name  string
	scale int64
}{{"small", 500}, {"large", 280000}}

type measuredWorld struct {
	truth   []byte
	objects int64
	rack    string
	hub     string
	leaf    string
	store   *memstore.Store
	eng     *query.Engine
}

var (
	measuredOnce   sync.Map // name -> *sync.Once
	measuredWorlds sync.Map // name -> *measuredWorld
)

func benchWorld(b *testing.B, name string, scale int64) *measuredWorld {
	b.Helper()
	once, _ := measuredOnce.LoadOrStore(name, new(sync.Once))
	once.(*sync.Once).Do(func() {
		reg, err := model.LoadStandard()
		if err != nil {
			b.Fatal(err)
		}
		w, err := worldgen.Build(reg, worldgen.DefaultParams(7, scale))
		if err != nil {
			b.Fatal(err)
		}
		files := worldgen.NewMemStore()
		m, err := w.Run(files, worldgen.RunOptions{Truth: true})
		if err != nil {
			b.Fatal(err)
		}
		mw := &measuredWorld{truth: files.Files[worldgen.TruthFile].Bytes(), objects: m.Counts.Objects}
		mw.store, mw.eng = loadMeasured(b, reg, mw.truth)
		mw.pick(b)
		measuredWorlds.Store(name, mw)
	})
	v, _ := measuredWorlds.Load(name)
	return v.(*measuredWorld)
}

func loadMeasured(b *testing.B, reg *model.Registry, truth []byte) (*memstore.Store, *query.Engine) {
	b.Helper()
	clock := kernel.NewManualClock(epoch)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	store := memstore.New()
	ctx := context.Background()
	kg, err := devload.NewKG(ctx, store, reg, ids, clock, "dev")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := (devload.Loader{Store: store, Reg: reg, Clock: clock, IDs: ids}).Load(ctx, kg.ID, bytes.NewReader(truth)); err != nil {
		b.Fatal(err)
	}
	eng, err := query.New(store, reg, clock)
	if err != nil {
		b.Fatal(err)
	}
	return store, eng
}

// pick chooses the objects the latency benchmarks read: a rack, the virtualization manager (the hub: every host and VM of the estate hangs on it) and a leaf (a fan).
func (mw *measuredWorld) pick(b *testing.B) {
	b.Helper()
	ctx := context.Background()
	ans, err := mw.eng.Find(ctx, query.FindInput{Common: query.Common{KG: graph.MainAlias, Limit: 1, Shape: query.ShapeItems}, ObjectType: "Rack"}, query.AllowAll())
	if err != nil || len(ans.Data.Items) == 0 {
		b.Fatalf("no rack: %v", err)
	}
	mw.rack = ans.Data.Items[0].ObjectID
	hub, err := mw.eng.Find(ctx, query.FindInput{Common: query.Common{KG: graph.MainAlias, Limit: 1, Shape: query.ShapeItems}, ObjectType: "VirtualizationManager"}, query.AllowAll())
	if err != nil || len(hub.Data.Items) == 0 {
		b.Fatalf("no virtualization manager: %v", err)
	}
	mw.hub = hub.Data.Items[0].ObjectID
	leaf, err := mw.eng.Find(ctx, query.FindInput{Common: query.Common{KG: graph.MainAlias, Limit: 1, Shape: query.ShapeItems}, ObjectType: "Fan"}, query.AllowAll())
	if err != nil || len(leaf.Data.Items) == 0 {
		b.Fatalf("no fan: %v", err)
	}
	mw.leaf = leaf.Data.Items[0].ObjectID
}

// BenchmarkLoad times a load of the whole truth and reports the heap the loaded store keeps.
func BenchmarkLoad(b *testing.B) {
	for _, sz := range measured {
		b.Run(sz.name, func(b *testing.B) {
			mw := benchWorld(b, sz.name, sz.scale)
			reg, _ := model.LoadStandard()
			var kept *memstore.Store
			for i := 0; i < b.N; i++ {
				kept, _ = loadMeasured(b, reg, mw.truth)
			}
			runtime.GC()
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			b.ReportMetric(float64(mw.objects), "objects")
			b.ReportMetric(float64(ms.HeapAlloc)/(1<<20), "heap_MiB")
			runtime.KeepAlive(kept)
		})
	}
}

// BenchmarkQueries times the three queries over the loaded store.
func BenchmarkQueries(b *testing.B) {
	for _, sz := range measured {
		mw := benchWorld(b, sz.name, sz.scale)
		ctx := context.Background()
		common := query.Common{KG: graph.MainAlias, Limit: 50, Shape: query.ShapeItems}
		b.Run(sz.name+"/find_by_type", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := mw.eng.Find(ctx, query.FindInput{Common: common, ObjectType: "PhysicalServer"}, query.AllowAll()); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(sz.name+"/find_by_text", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := mw.eng.Find(ctx, query.FindInput{Common: common, Text: "rack-0-0-0"}, query.AllowAll()); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(sz.name+"/get", func(b *testing.B) {
			in := query.GetInput{Common: query.Common{KG: graph.MainAlias}, ObjectID: mw.rack, Include: []string{"attributes", "identifiers", "link_counts"}}
			for i := 0; i < b.N; i++ {
				if _, err := mw.eng.Get(ctx, in, query.AllowAll()); err != nil {
					b.Fatal(err)
				}
			}
		})
		for _, one := range []struct{ name, id string }{{"rack", mw.rack}, {"hub", mw.hub}, {"leaf", mw.leaf}} {
			b.Run(sz.name+"/neighbors_"+one.name, func(b *testing.B) {
				in := query.NeighborsInput{Common: common, ObjectID: one.id}
				at, err := mw.eng.Get(ctx, query.GetInput{Common: query.Common{KG: graph.MainAlias}, ObjectID: one.id, Include: []string{"link_counts"}}, query.AllowAll())
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(at.Data.LinkCounts.In+at.Data.LinkCounts.Out), "links_at_object")
				for i := 0; i < b.N; i++ {
					if _, err := mw.eng.Neighbors(ctx, in, query.AllowAll()); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
