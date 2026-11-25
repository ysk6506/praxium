package pgingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// rdfLoadMany keeps n raw records, receives all of them (one in ten changed) and waits for one in a hundred, in the first dataset.
func rdfLoadMany(t *testing.T, s *rdfSetup, n int) {
	t.Helper()
	no := s.rdfNo("ds_places")
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate)
		SELECT $1, $2, 'k' || i, sha256(convert_to('v1-' || i, 'UTF8')), ARRAY['v1-' || i], 1, 1, 0 FROM generate_series(1, $3::int) i`, pgiTable(s.ref, pgschema.TableRawRecord)), s.ref.SrcNo, no, n)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, page, row_no, key, place, filtered, change_kind, raw_hash, fields)
		SELECT $1, $2, i / 1000, i %% 1000, 'k' || i, '', false, %d, NULL, NULL FROM generate_series(1, $3::int) i`, pgiTable(s.ref, pgschema.TableStageRecord), pgschema.ReceiveSame), s.ref.SrcNo, no, n)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_wait (src_no, dataset_no, key, kind, runs, first_at)
		SELECT $1, $2, 'k' || i, %d, 1, now() FROM generate_series(1, $3::int, 100) i`, s.ref.KGNo, pgschema.WaitUnseen), s.ref.SrcNo, no, n)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_problem (src_no, dataset_no, key, def_no, field, kind, reason)
		SELECT $1, $2, 'k' || i, NULL, '', 'duplicate_key', 'an old one' FROM generate_series(1, $3::int, 50) i`, s.ref.KGNo), s.ref.SrcNo, no, n)
	s.e.exec(t, `VACUUM ANALYZE`)
}

// rdfScanned is what the statement of a bucket in the middle of a source of n records reads, as the plan says after it ran: the rows
// each of the three tables gave (by the name of the table) and the plan in words. The statement is run in a transaction that rolls back.
func rdfScanned(t *testing.T, n, size int) (map[string]float64, string) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	rdfLoadMany(t, s, n)
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_places": ingest.CoverageComplete})
	if _, err := s.e.st.BeginRawDiff(s.e.ctx, l, s.rdfStart(rdfT0, size)); err != nil {
		t.Fatal(err)
	}
	for part := 0; part < 2; part++ {
		if b, err := s.e.st.DiffRawBucket(s.e.ctx, l, part); err != nil || b.Last {
			t.Fatalf("%+v %v", b, err)
		}
	}
	var counts []byte
	if err := s.e.admin.QueryRowContext(s.e.ctx, `SELECT counts FROM sync_run WHERE run_no = $1`, l.No).Scan(&counts); err != nil {
		t.Fatal(err)
	}
	state, err := stateOf(counts)
	if err != nil {
		t.Fatal(err)
	}
	b := bucketRun{kgNo: s.ref.KGNo, srcNo: s.ref.SrcNo, runNo: l.No, state: state}
	tx, err := s.e.admin.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if b.end, err = bucketEnd(s.e.ctx, tx, b.kgNo, b.srcNo, state); err != nil || b.end == nil {
		t.Fatalf("%+v %v", b.end, err)
	}
	args, err := bucketArgs(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(s.e.ctx, bucketSettingSQL); err != nil {
		t.Fatal(err)
	}
	return rdfPlanRows(t, tx, "EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, FORMAT JSON) "+rawDiffSQL(b.kgNo, b.srcNo), args)
}

// rdfJoinWork is the key of the rows that the joins of the plan compared and threw away (a join that compares each row of one side with
// each row of the other makes it the square of the size of a bucket).
const rdfJoinWork = "~joins"

// rdfPlanRows runs an EXPLAIN and gives the rows read by the scan nodes of each table (by the name of the table, with the loops
// counted), and the plan as text of "node on table" lines.
func rdfPlanRows(t *testing.T, tx *sql.Tx, query string, args []any) (map[string]float64, string) {
	t.Helper()
	var doc []byte
	if err := tx.QueryRow(query, args...).Scan(&doc); err != nil {
		t.Fatal(err)
	}
	var top []struct{ Plan map[string]any }
	if err := json.Unmarshal(doc, &top); err != nil {
		t.Fatal(err)
	}
	rows, lines := map[string]float64{}, []string{}
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if removed, ok := node["Rows Removed by Join Filter"].(float64); ok {
			rows[rdfJoinWork] += removed
		}
		if name, ok := node["Relation Name"].(string); ok {
			actual, _ := node["Actual Rows"].(float64)
			loops, _ := node["Actual Loops"].(float64)
			removed, _ := node["Rows Removed by Filter"].(float64)
			rows[name] += (actual + removed) * loops
			lines = append(lines, fmt.Sprintf("%v on %s", node["Node Type"], name))
		}
		for _, key := range []string{"Plans"} {
			if kids, ok := node[key].([]any); ok {
				for _, kid := range kids {
					if m, ok := kid.(map[string]any); ok {
						walk(m)
					}
				}
			}
		}
	}
	for _, p := range top {
		walk(p.Plan)
	}
	return rows, strings.Join(lines, "\n")
}

// [REQ-782]
func TestRdfABucketReadsTheIndexRangeItCoversAndNotTheWholeTableAndWhatItReadsDoesNotGrowWithTheSource(t *testing.T) {
	const size = 500
	small, plan := rdfScanned(t, 20_000, size)
	large, _ := rdfScanned(t, 60_000, size)
	for table, read := range small {
		if table == rdfJoinWork {
			if read > 2*size {
				t.Errorf("the joins of a bucket of %d rows compared %v pairs of rows that did not match:\n%s", size, read, plan)
			}
			continue
		}
		if table == "source_dataset" { // the datasets of the source: a few rows
			continue
		}
		if strings.Contains(plan, "Seq Scan on "+table) {
			t.Errorf("a bucket reads the whole of %s:\n%s", table, plan)
		}
		if read > 4*size {
			t.Errorf("a bucket of %d rows reads %v rows of %s:\n%s", size, read, table, plan)
		}
		if bigger := large[table]; bigger > 1.2*read+10 {
			t.Errorf("a bucket reads %v rows of %s in a source of 20000 records and %v in one of 60000", read, table, bigger)
		}
	}
	for _, kind := range []string{"raw_record", "stage_record", "record_wait"} {
		found := false
		for table := range small {
			found = found || strings.Contains(table, kind)
		}
		if !found {
			t.Errorf("the plan has no scan of %s:\n%s", kind, plan)
		}
	}
}
