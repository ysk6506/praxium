package pgingest

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// BenchmarkIdnStageIndex is the cost the index of the rows received puts on the receiving (ADR-0108 decision 4): the time to write the
// keys of a source into a table of the shape of stage_key with no index, with a BRIN index on (dataset, page), and with a B-tree on
// the same columns. The rows come in the order the receiving writes them (page after page). Run it with
// go test ./internal/pgingest -run '^$' -bench IdnStageIndex -benchtime 1x
func BenchmarkIdnStageIndex(b *testing.B) {
	const rows = 5_000_000
	dsn := pgconn.NewTestDatabase(b)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	for _, index := range []string{"none", "brin", "btree"} {
		b.Run(index, func(b *testing.B) {
			table := "idn_bench_" + index
			create := fmt.Sprintf(`CREATE TABLE %s (dataset_no bigint NOT NULL, page integer NOT NULL, row_no integer NOT NULL, feed_no integer NOT NULL,
				key_ord smallint NOT NULL, type_no smallint NOT NULL, sig_hash bytea NOT NULL, cand_ref text COLLATE "C", held boolean NOT NULL DEFAULT false)`, table)
			if _, err := db.Exec(create); err != nil {
				b.Fatal(err)
			}
			switch index {
			case "brin":
				_, err = db.Exec(fmt.Sprintf("CREATE INDEX ON %s USING brin (dataset_no, page)", table))
			case "btree":
				_, err = db.Exec(fmt.Sprintf("CREATE INDEX ON %s (dataset_no, page)", table))
			}
			if err != nil {
				b.Fatal(err)
			}
			start := idnNow.Now()
			// 1000 rows a page, five keys a row: the way a source of a million records is staged
			if _, err := db.Exec(fmt.Sprintf(`INSERT INTO %s (dataset_no, page, row_no, feed_no, key_ord, type_no, sig_hash, cand_ref)
				SELECT 1, (i / 5) / 1000, (i / 5) %% 1000, i %% 5, 0, 1, sha256(convert_to(i::text, 'UTF8')), 'obj_' || i FROM generate_series(0, %d) i`, table, rows-1)); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(idnNow.Now().Sub(start).Seconds(), "s/write")
			var size int64
			if err := db.QueryRow(fmt.Sprintf(`SELECT pg_indexes_size('%s')`, table)).Scan(&size); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(size)/(1<<20), "index-MiB")
		})
	}
}
