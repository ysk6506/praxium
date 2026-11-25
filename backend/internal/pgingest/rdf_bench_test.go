package pgingest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// The measurement of the comparison of the received rows with the raw records (ADR-0102): a source of n records that the source
// keeps, a run that receives all but 1% of them (so 1% is not shown) and in which 10% changed, the comparison done with buckets of
// the default size. It reports the time of the whole step and the number of the buckets and the time of the longest one (a bucket
// must stay under a third of a lease, 20 seconds):
//
//	go test ./internal/pgingest -run '^$' -bench RdfRawDiff -benchtime 1x
//
// The helpers of the tests are for *testing.T, so the benchmark has the few it needs of its own.
func BenchmarkRdfRawDiff(b *testing.B) {
	for _, n := range []int{100_000, 1_000_000} {
		b.Run(fmt.Sprintf("records_%d", n), func(b *testing.B) { rdfMeasure(b, n) })
	}
}

func rdfMeasure(b *testing.B, n int) {
	ctx := context.Background()
	dsn := pgconn.NewTestDatabase(b)
	pst, err := pgstore.Open(ctx, pgstore.Config{DSN: dsn})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = pst.Close() })
	pool, err := pgconn.Open(ctx, pgconn.Config{DSN: dsn})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = pool.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = admin.Close() })
	st := New(pool)
	exec := func(query string, args ...any) {
		if _, err := admin.ExecContext(ctx, query, args...); err != nil {
			b.Fatalf("%s: %v", query, err)
		}
	}
	reg, err := model.LoadStandard()
	if err != nil {
		b.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(pgiSourceJSON))
	if err != nil {
		b.Fatal(err)
	}
	mapping, err := ingest.ParseMapping(strings.NewReader(pgiMappingJSON))
	if err != nil {
		b.Fatal(err)
	}
	plan, err := ingest.CompileScale(reg, src, mapping)
	if err != nil {
		b.Fatal(err)
	}
	id, err := kernel.ParseID[graph.KGKind]("kg-00000000000000a1")
	if err != nil {
		b.Fatal(err)
	}
	kg, err := pst.CreateKG(ctx, graph.KG{ID: id, Name: "rdfbench", Role: graph.RoleTest, DefinitionSetHash: reg.Hash(), CreatedAt: time.Unix(1000, 0)})
	if err != nil {
		b.Fatal(err)
	}
	ref, err := st.AttachSource(ctx, kg.ID, plan.Source.ID, pgiDatasets(plan))
	if err != nil {
		b.Fatal(err)
	}
	no := ref.Datasets["ds_places"]
	exec(fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate)
		SELECT $1, $2, 'k' || i, sha256(convert_to('v1-' || i, 'UTF8')), ARRAY['v1-' || i], 1, 1, 0 FROM generate_series(1, $3::int) i`,
		pgiTable(ref, pgschema.TableRawRecord)), ref.SrcNo, no, n)
	exec(fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, page, row_no, key, place, filtered, change_kind, raw_hash, fields)
		SELECT $1, $2, i / 10000, i %% 10000, 'k' || i, '', false, CASE WHEN i %% 10 = 1 THEN %d ELSE %d END,
		       CASE WHEN i %% 10 = 1 THEN sha256(convert_to('v2-' || i, 'UTF8')) END, CASE WHEN i %% 10 = 1 THEN ARRAY['v2-' || i] END
		FROM generate_series(1, $3::int) i WHERE i %% 100 <> 0`, pgiTable(ref, pgschema.TableStageRecord), pgschema.ReceiveChanged, pgschema.ReceiveSame),
		ref.SrcNo, no, n)
	exec(`VACUUM ANALYZE`)
	lease, err := st.OpenRun(ctx, ingest.RunOpen{KG: kg.ID, Source: plan.Source.ID, Owner: "bench", Term: time.Hour})
	if err != nil {
		b.Fatal(err)
	}
	exec(`UPDATE sync_run SET counts = counts || $2::jsonb WHERE run_no = $1`, lease.No,
		fmt.Sprintf(`{"receive":{"datasets":[{"dataset":"ds_places","dataset_no":%d,"complete":true,"coverage":"complete"}]}}`, no))
	if err := st.AdvanceRun(ctx, lease, ingest.RunReceiving, ingest.RunComputing); err != nil {
		b.Fatal(err)
	}
	start := ingest.RawDiffStart{Now: rdfT0}
	for _, d := range plan.Source.Datasets {
		start.Datasets = append(start.Datasets, ingest.RawDiffDataset{Dataset: d.ID, Retire: true})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.BeginRawDiff(ctx, lease, start); err != nil {
			b.Fatal(err)
		}
		var longest time.Duration
		for part := 0; ; part++ {
			bucket, err := st.DiffRawBucket(ctx, lease, part)
			if err != nil {
				b.Fatal(err)
			}
			longest = max(longest, bucket.Took)
			if bucket.Last {
				b.ReportMetric(float64(part+1), "buckets")
				break
			}
		}
		b.ReportMetric(longest.Seconds(), "s/longest-bucket")
	}
}
