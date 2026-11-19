package pgstore

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

// sapSizes are the worlds of the measurements of the storage (ADR-0089, ADR-0094, ADR-0099).
var sapSizes = []struct {
	name  string
	scale int64
}{{"small", 500}, {"medium", 30000}, {"large", 280000}}

// sapWAL is the position in the write-ahead log of the server, in bytes.
func sapWAL(b *testing.B, k *sapKG) int64 {
	b.Helper()
	ctx := context.Background()
	a, err := k.st.access(ctx, k.kg.ID)
	if err != nil {
		b.Fatal(err)
	}
	var n int64
	if err := a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0')::bigint`).Scan(&n)
	}); err != nil {
		b.Fatal(err)
	}
	return n
}

// The first load of a world by the rows path (the development loader) and by the answer loader with the staged apply,
// with the time and the write-ahead log of each part. The rows path is the baseline on the same machine; the staged
// apply is the part the flow does after it has made the difference:
//
//	go test ./internal/pgstore -run '^$' -bench SapFirstLoad -benchtime 1x
func BenchmarkSapFirstLoad(b *testing.B) {
	reg, _ := model.LoadStandard()
	for _, sz := range sapSizes {
		w, err := worldgen.Build(reg, worldgen.DefaultParams(7, sz.scale))
		if err != nil {
			b.Fatal(err)
		}
		files := worldgen.NewMemStore()
		if _, err := w.Run(files, worldgen.RunOptions{Truth: true}); err != nil {
			b.Fatal(err)
		}
		truth := files.Files[worldgen.TruthFile].Bytes()
		b.Run(sz.name, func(b *testing.B) { sapMeasure(b, reg, truth) })
	}
}

func sapMeasure(b *testing.B, reg *model.Registry, truth []byte) {
	ctx := context.Background()
	rowsStore, stagedStore := NewTestStore(b), NewTestStore(b)
	tape := &sapTape{}
	rowsStore.RecordChunks(tape.sapNote)
	rows := sapOpen(b, rowsStore, reg, rowsStore)
	if err := rows.app.Release(ctx, rows.lease); err != nil {
		b.Fatal(err)
	}
	w0 := sapWAL(b, rows)
	b.ResetTimer()
	if _, err := (devload.Loader{Store: rowsStore, Reg: reg, Clock: rows.clock, IDs: kernel.NewRandomIDs(kernel.NewSeededRng(5))}).Load(ctx, rows.kg.ID, bytes.NewReader(truth)); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(b.Elapsed().Seconds(), "rows-path-s")
	b.ReportMetric(float64(sapWAL(b, rows)-w0)/1e6, "rows-path-wal-MB")
	staged := sapOpen(b, stagedStore, reg, stagedStore)
	stg, err := sapNewStager(ctx, stagedStore, staged.kg.ID)
	if err != nil {
		b.Fatal(err)
	}
	chunks := tape.sapTake()
	w1 := sapWAL(b, staged)
	b.ResetTimer()
	for _, c := range chunks {
		if err := stg.sapStage(ctx, c); err != nil {
			b.Fatal(err)
		}
		if err := stg.sapStageVolatile(ctx, c); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(b.Elapsed().Seconds(), "stage-s")
	b.ReportMetric(float64(sapWAL(b, staged)-w1)/1e6, "stage-wal-MB")
	w2 := sapWAL(b, staged)
	b.ResetTimer()
	p, err := staged.st.StagedProgress(ctx, staged.sapApply("sap-run"))
	for err == nil && (p.State == graph.StagedPending || p.State == graph.StagedOpen) {
		p, err = staged.st.ApplyStagedChunk(ctx, staged.sapApply("sap-run"), p)
	}
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(b.Elapsed().Seconds(), "apply-s")
	b.ReportMetric(float64(sapWAL(b, staged)-w2)/1e6, "apply-wal-MB")
	b.ReportMetric(float64(p.ChunkCount), "chunks")
	w3 := sapWAL(b, staged)
	b.ResetTimer()
	var after graph.StagedVolatileCursor
	for {
		step, err := staged.st.ApplyStagedVolatile(ctx, graph.StagedVolatile{KG: staged.kg.ID, Generation: staged.kg.Generation, Lease: staged.lease,
			Now: staged.clock.Now(), ApplyID: "sap-run", Kind: graph.BatchSync, SourceID: "sap", After: after, Limit: 5000})
		if err != nil {
			b.Fatal(err)
		}
		if step.Done {
			break
		}
		after = step.Next
	}
	b.ReportMetric(b.Elapsed().Seconds(), "volatile-s")
	b.ReportMetric(float64(sapWAL(b, staged)-w3)/1e6, "volatile-wal-MB")
}
