package readcheck

import (
	"bytes"
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/query"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// A generated world loaded into the reference store and into the latest-only store gives one graph hash, and
// the reference store answers the queries over it at an older change number too.
//
// [REQ-258]
func TestAWorldLoadsToTheSameHashInBothStores(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	w, err := worldgen.Build(reg, worldgen.DefaultParams(9, 500))
	if err != nil {
		t.Fatal(err)
	}
	files := worldgen.NewMemStore()
	if _, err := w.Run(files, worldgen.RunOptions{Truth: true}); err != nil {
		t.Fatal(err)
	}
	truth := files.Files[worldgen.TruthFile].Bytes()
	clock := kernel.NewManualClock(epoch)
	ctx := context.Background()
	load := func(store graph.Store) devload.Report {
		ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
		kg, err := devload.NewKG(ctx, store, reg, ids, clock, "dev")
		if err != nil {
			t.Fatal(err)
		}
		rep, err := devload.Loader{Store: store, Reg: reg, Clock: clock, IDs: ids, BatchItems: 200}.Load(ctx, kg.ID, bytes.NewReader(truth))
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	mem := memstore.New()
	a, b := load(mem), load(graph.NewScratchStore())
	if a != b || a.Batches < 3 {
		t.Fatalf("the stores loaded differently:\n%+v\n%+v", a, b)
	}
	eng, err := query.New(mem, reg, clock)
	if err != nil {
		t.Fatal(err)
	}
	// after the first batch only part of the objects were there; the last state has all of them
	count := func(seq *int64) int {
		ans, err := eng.Find(ctx, query.FindInput{Common: query.Common{KG: graph.MainAlias, ChangeSeq: seq}}, query.AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range ans.Data.CountsByType {
			n += c.Count
		}
		return n
	}
	first := int64(1)
	if all, early := count(nil), count(&first); int64(all) != a.Objects || early == 0 || early >= all {
		t.Fatalf("%d objects at the end, %d after the first change, %d loaded", all, early, a.Objects)
	}
}
