package pgingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the statements of the expansion read in the plan the server chooses (ADR-0121). The scale test of the expansion asks for the plan
// of the indexes (its tables are a few pages); this one leaves the planner as it is and makes the permanent tables big with rows the
// run has nothing to do with, as the tables of a real KG are, so that a statement whose plan turns to a scan of a whole table, or reads
// what many records share once for each of them, reads more than a few rows a record. Every helper starts with expOp, so that none meets
// a name of another test file of this folder.

// The sizes of the test: the records that share what is read (m and three times m) and the rows of each permanent table the run has
// nothing to do with.
const (
	expOpShared    = 200
	expOpUnrelated = 30_000
)

// expOpReadsInto adds up, for each statement of the expansion (by what it says it does), the rows it read in every unit, in the plan the
// server chooses. The statement is run in a savepoint that is rolled back, and then runs for good.
func expOpReadsInto(total map[string]float64) func(tx *sql.Tx, what, query string, args []any) {
	return func(tx *sql.Tx, what, query string, args []any) {
		if _, err := tx.Exec("SAVEPOINT exp_op_plan"); err != nil {
			panic(err)
		}
		defer func() {
			if _, err := tx.Exec("ROLLBACK TO SAVEPOINT exp_op_plan"); err != nil {
				panic(err)
			}
		}()
		var doc []byte
		if err := tx.QueryRow("EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, FORMAT JSON) "+query, args...).Scan(&doc); err != nil {
			panic(fmt.Sprintf("%s: %v", what, err))
		}
		var top []struct{ Plan map[string]any }
		if err := json.Unmarshal(doc, &top); err != nil {
			panic(err)
		}
		var walk func(map[string]any)
		walk = func(node map[string]any) {
			if _, ok := node["Relation Name"].(string); ok {
				actual, _ := node["Actual Rows"].(float64)
				loops, _ := node["Actual Loops"].(float64)
				removed, _ := node["Rows Removed by Filter"].(float64)
				total[what] += (actual + removed) * loops
			}
			if kids, ok := node["Plans"].([]any); ok {
				for _, k := range kids {
					if m, ok := k.(map[string]any); ok {
						walk(m)
					}
				}
			}
		}
		for _, p := range top {
			walk(p.Plan)
		}
	}
}

// expOpNoise puts n rows the run has nothing to do with in each permanent table the expansion reads: references of every role (to objects
// and links that are not there), links of the two link types that hold an end to one, and raw records.
func expOpNoise(t *testing.T, s *rdfSetup, n int) {
	t.Helper()
	k, src, ds := s.ref.KGNo, s.ref.SrcNo, s.rdfNo("ds_s")
	for _, role := range []int16{pgschema.RefRoleIdentity, pgschema.RefRoleLinkFrom, pgschema.RefRoleLinkTo, pgschema.RefRoleClaim} {
		hash := "decode(md5('n' || i), 'hex')"
		if role == pgschema.RefRoleIdentity {
			hash = "NULL"
		}
		s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state, retirable)
			SELECT $1, $2, 'N' || i, %d, 0, 0, %s, 1000000 + i, 0, true FROM generate_series(1, %d) i`, k, role, hash, n), src, ds)
	}
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from)
		SELECT 2000000 + i, 'noise-' || i, CASE WHEN i %% 2 = 0 THEN %d ELSE %d END, 3000000 + i, 4000000 + i, 'declared', 'declared', '\x00', '\x00', 1
		FROM generate_series(1, %d) i`, k, expTypeFromOne, expTypeToOne, n))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) SELECT $1, $2, 'N' || i, '\x00', '{x}', 0, 1, 0
		FROM generate_series(1, %d) i`, pgiTable(s.ref, pgschema.TableRawRecord), n), src, ds)
	for _, table := range []string{fmt.Sprintf(`"s_%d"."%s"`, k, pgschema.TableRecordRef), fmt.Sprintf(`"s_%d"."%s"`, k, pgschema.TableLink), pgiTable(s.ref, pgschema.TableRawRecord)} {
		s.e.exec(t, "ANALYZE "+table)
	}
}

// expOpRead is the rows each statement read, in a unit that holds every record, and the records of the run when the expansion is done.
func expOpRead(t *testing.T, m int) (map[string]float64, int) {
	t.Helper()
	s, l := expShared(t, m)
	expOpNoise(t, s, expOpUnrelated)
	total := map[string]float64{}
	s.e.st.expTest = &expHook{Seen: expOpReadsInto(total)}
	if _, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Rows: 100 * m, Cards: expCards(), Feeds: expFeeds(s)}); err != nil {
		t.Fatal(err)
	}
	return total, s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record`, s.ref.KGNo))
}

// [REQ-899] [REQ-983]
func TestExpWhatTheStatementsReadInThePlanTheServerChoosesGoesWithTheRecordsAndNotWithTheRestOfTheKG(t *testing.T) {
	small, records := expOpRead(t, expOpShared)
	big, bigRecords := expOpRead(t, 3*expOpShared)
	for _, what := range expSharedWhats {
		a, b := small[what], big[what]
		t.Logf("%s: %d records %.0f rows, %d records %.0f rows", what, records, a, bigRecords, b)
		if a == 0 || b == 0 {
			t.Errorf("%s: the hook saw no rows read (%.0f, %.0f)", what, a, b)
			continue
		}
		// a few rows a record, whatever the KG holds besides: not the rows of a whole table, nor what many records share once for each
		if a > 3*float64(records) || b > 3*float64(bigRecords) {
			t.Errorf("%s: read %.0f rows for %d records and %.0f for %d: more than three a record", what, a, records, b, bigRecords)
		}
		if b > 3.4*a {
			t.Errorf("%s: %.0f rows for %d records that share what is read and %.0f for three times as many", what, a, expOpShared, b)
		}
	}
}
