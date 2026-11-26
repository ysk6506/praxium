package pgstore

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

var sapStart = time.Date(2031, 3, 1, 0, 0, 0, 0, time.UTC)

// sapTruth is the ground truth of a generated world, as the development loader reads it.
func sapTruth(t *testing.T, reg *model.Registry, seed uint64, scale int64) []byte {
	t.Helper()
	p := worldgen.DefaultParams(seed, scale)
	p.Imperfections = worldgen.Imperfections{}
	w, err := worldgen.Build(reg, p)
	if err != nil {
		t.Fatalf("world: %v", err)
	}
	ms := worldgen.NewMemStore()
	if _, err := w.Run(ms, worldgen.RunOptions{Truth: true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	return ms.Files[worldgen.TruthFile].Bytes()
}

// sapKG is a KG with the lease of the one that writes it.
type sapKG struct {
	st    *Store
	kg    graph.KG
	app   *graph.Applier
	lease graph.Lease
	clock kernel.Clock
	reg   *model.Registry
}

func sapOpen(t TB, st *Store, reg *model.Registry, store graph.Store) *sapKG {
	t.Helper()
	ctx := context.Background()
	clock := kernel.NewManualClock(sapStart)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	kg, err := devload.NewKG(ctx, store, reg, ids, clock, "dev")
	if err != nil {
		t.Fatalf("kg: %v", err)
	}
	app, err := graph.NewApplier(store, reg, graph.NewTestKeyProvider("praxium-dev-load"), clock, ids, graph.Options{Semantic: 1, LeaseTTL: 12 * time.Hour})
	if err != nil {
		t.Fatalf("applier: %v", err)
	}
	lease, err := app.Acquire(ctx, kg.ID)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	return &sapKG{st: st, kg: kg, app: app, lease: lease, clock: clock, reg: reg}
}

func (k *sapKG) sapApply(id string) graph.StagedApply {
	return graph.StagedApply{KG: k.kg.ID, Generation: k.kg.Generation, Lease: k.lease, Now: k.clock.Now(), ApplyID: id, Kind: graph.BatchSync, SourceID: "sap"}
}

// sapRun applies the staged difference chunk by chunk and returns the progress at the end.
func sapRun(t *testing.T, k *sapKG, id string) graph.StagedProgress {
	t.Helper()
	ctx := context.Background()
	p, err := k.st.StagedProgress(ctx, k.sapApply(id))
	noErr(t, "progress", err)
	for p.State == graph.StagedPending || p.State == graph.StagedOpen {
		p, err = k.st.ApplyStagedChunk(ctx, k.sapApply(id), p)
		noErr(t, "chunk", err)
	}
	return p
}

// sapVolatile writes the staged volatile values in intervals of limit and returns how many intervals and values.
func sapVolatile(t *testing.T, k *sapKG, id string, limit int) (steps, written int) {
	t.Helper()
	var after graph.StagedVolatileCursor
	for {
		step, err := k.st.ApplyStagedVolatile(context.Background(), graph.StagedVolatile{KG: k.kg.ID, Generation: k.kg.Generation, Lease: k.lease,
			Now: k.clock.Now(), ApplyID: id, Kind: graph.BatchSync, SourceID: "sap", After: after, Limit: limit})
		noErr(t, "volatile", err)
		steps, written, after = steps+1, written+step.Written, step.Next
		if step.Done {
			return steps, written
		}
	}
}

// sapPair is the same world in two stores: the rows path (with the record of the chunks it wrote) and the staged
// apply (with the stager that wrote the difference).
type sapPair struct {
	rows, staged *sapKG
	rec          *sapTape
	stg          *sapStager
	first        []graph.ChunkWrite
	// arm makes the next transaction that moves a head of the staged side lose its backend before its COMMIT.
	arm func()
}

// sapLoadBoth loads the world by the rows path into one store, recording the chunks it writes, and stages the same
// chunks as a stored difference in the other. It does not apply them.
func sapLoadBoth(t *testing.T, reg *model.Registry, truth []byte, items int) *sapPair {
	t.Helper()
	return sapLoadBothInto(t, reg, truth, items, func(tb TB) (*Store, func()) { return NewTestStore(tb), func() {} })
}

// sapSerial are the tests of the staged apply that run alone, after the others of this unit (ADR-0120): the one that counts the
// locks a chunk takes.
var sapSerial = map[string]bool{"TestSapLocksOfAChunkDoNotGrowWithItsRows": true}

// sapParallel lets the test run beside the other tests of this unit, each on stores of its own, unless it is one of sapSerial.
func sapParallel(t *testing.T) {
	if !sapSerial[t.Name()] {
		pgconn.TestInParallel(t)
	}
}

// sapLoadBothInto is sapLoadBoth with the store of the staged side made by open.
func sapLoadBothInto(t *testing.T, reg *model.Registry, truth []byte, items int, open func(TB) (*Store, func())) *sapPair {
	t.Helper()
	sapParallel(t)
	ctx := context.Background()
	a := NewTestStore(t)
	b, arm := open(t)
	p := &sapPair{rec: &sapTape{}, arm: arm}
	a.RecordChunks(p.rec.sapNote)
	p.rows = sapOpen(t, a, reg, a)
	noErr(t, "release", p.rows.app.Release(ctx, p.rows.lease))
	_, err := (devload.Loader{Store: a, Reg: reg, Clock: p.rows.clock, IDs: kernel.NewRandomIDs(kernel.NewSeededRng(5)), BatchItems: items}).Load(ctx, p.rows.kg.ID, bytes.NewReader(truth))
	noErr(t, "rows path", err)
	p.staged = sapOpen(t, b, reg, b)
	p.stg, err = sapNewStager(ctx, b, p.staged.kg.ID)
	noErr(t, "stager", err)
	p.first = p.rec.sapTake()
	p.sapStageChunks(t, p.first)
	return p
}

// sapCache keeps the chunks the rows path writes for the world of the tests, by the size of its batches: the rows path
// is run once for all the tests that need only the rows it wrote.
var sapCache struct {
	sync.Mutex
	chunks map[int][]graph.ChunkWrite
}

// sapFirstChunks gives the chunks of the first load of the world (seed 9, scale 500) by the rows path.
func sapFirstChunks(t *testing.T, reg *model.Registry, items int) []graph.ChunkWrite {
	t.Helper()
	sapCache.Lock()
	defer sapCache.Unlock()
	if c, ok := sapCache.chunks[items]; ok {
		return c
	}
	ctx := context.Background()
	a := NewTestStore(t)
	tape := &sapTape{}
	a.RecordChunks(tape.sapNote)
	rows := sapOpen(t, a, reg, a)
	noErr(t, "release", rows.app.Release(ctx, rows.lease))
	_, err := (devload.Loader{Store: a, Reg: reg, Clock: rows.clock, IDs: kernel.NewRandomIDs(kernel.NewSeededRng(5)), BatchItems: items}).Load(ctx, rows.kg.ID, bytes.NewReader(sapTruth(t, reg, 9, 500)))
	noErr(t, "rows path", err)
	if sapCache.chunks == nil {
		sapCache.chunks = map[int][]graph.ChunkWrite{}
	}
	sapCache.chunks[items] = tape.sapTake()
	return sapCache.chunks[items]
}

// sapStagedInto stages the first load of the world in a store made by open and does not apply it. There is no store
// of the rows path beside it: it is for the tests that look at the staged apply only.
func sapStagedInto(t *testing.T, items int, open func(TB) (*Store, func())) *sapPair {
	t.Helper()
	sapParallel(t)
	reg, err := model.LoadStandard()
	noErr(t, "model", err)
	first := sapFirstChunks(t, reg, items)
	b, arm := open(t)
	p := &sapPair{arm: arm, first: first, staged: sapOpen(t, b, reg, b)}
	p.stg, err = sapNewStager(context.Background(), b, p.staged.kg.ID)
	noErr(t, "stager", err)
	p.sapStageChunks(t, first)
	return p
}

// sapStageChunks writes the chunks as the stored difference of the staged side, from the first chunk.
func (p *sapPair) sapStageChunks(t *testing.T, chunks []graph.ChunkWrite) {
	t.Helper()
	ctx := context.Background()
	noErr(t, "reset", p.stg.sapReset(ctx))
	for _, w := range chunks {
		noErr(t, "stage", p.stg.sapStage(ctx, w))
		noErr(t, "stage volatile", p.stg.sapStageVolatile(ctx, w))
	}
}
