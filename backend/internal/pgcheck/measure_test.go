package pgcheck

import (
	"bytes"
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// The measurements of the storage (ADR-0089, ADR-0094): a development load of a generated world, an unchanged load
// again, and the read of one object and of the links at one object. They are benchmarks:
//
//	go test ./internal/pgcheck -run '^$' -bench . -benchtime 1x
var pgxSizes = []struct {
	name  string
	scale int64
}{{"small", 500}, {"medium", 30000}, {"large", 280000}}

func pgxLoad(b *testing.B, reg *model.Registry, store graph.Store, kg graph.KGID, truth []byte) {
	b.Helper()
	clock := kernel.NewManualClock(pgxStart)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	if _, err := (devload.Loader{Store: store, Reg: reg, Clock: clock, IDs: ids}).Load(context.Background(), kg, bytes.NewReader(truth)); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkPgxLoad(b *testing.B) {
	reg, _ := model.LoadStandard()
	for _, sz := range pgxSizes {
		w, err := worldgen.Build(reg, worldgen.DefaultParams(7, sz.scale))
		if err != nil {
			b.Fatal(err)
		}
		files := worldgen.NewMemStore()
		m, err := w.Run(files, worldgen.RunOptions{Truth: true})
		if err != nil {
			b.Fatal(err)
		}
		truth := files.Files[worldgen.TruthFile].Bytes()
		b.Run(sz.name, func(b *testing.B) {
			store := pgstore.NewTestStore(b)
			clock := kernel.NewManualClock(pgxStart)
			ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
			kg, err := devload.NewKG(context.Background(), store, reg, ids, clock, "dev")
			if err != nil {
				b.Fatal(err)
			}
			b.Run("first", func(b *testing.B) {
				pgxLoad(b, reg, store, kg.ID, truth)
				b.ReportMetric(float64(m.Counts.Objects), "objects")
			})
			if sz.scale < 100000 {
				b.Run("again", func(b *testing.B) { pgxLoad(b, reg, store, kg.ID, truth) })
			}
			pgxReads(b, store, kg.ID)
		})
	}
}

// pgxReads times the read of one object and of the links at one object of the loaded KG.
func pgxReads(b *testing.B, store graph.Store, kg graph.KGID) {
	ctx := context.Background()
	snap, err := store.Snapshot(ctx, graph.ReadPoint{KG: kg, Kind: graph.ReadLatest})
	if err != nil {
		b.Fatal(err)
	}
	rows, err := snap.ScanObjects(ctx, graph.ObjectID{}, 50)
	if err != nil || len(rows) == 0 {
		b.Fatalf("scan: %v", err)
	}
	id := rows[len(rows)/2].ID
	b.Run("one-object", func(b *testing.B) {
		for i := 0; i < 200; i++ {
			if _, ok, err := snap.Object(ctx, id); err != nil || !ok {
				b.Fatalf("read: %v %v", ok, err)
			}
		}
		b.ReportMetric(float64(b.Elapsed().Microseconds())/200, "us/read")
	})
	b.Run("neighbors", func(b *testing.B) {
		for i := 0; i < 200; i++ {
			if _, err := snap.LinksOf(ctx, id, graph.Both); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(b.Elapsed().Microseconds())/200, "us/read")
	})
}
