package pgingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// The plan of the read of the questions with the statistics the run gives the planner (ADR-0121). The plan tests of the identification
// ask for the plan that wins at the size of a real run (idnHook turns the hash join and the scan of a whole table off), so this test
// holds that the hint does not paint a plan the server would not choose: the run analyzes the questions once they are written, and at a
// size where a scan of the whole of calc_record costs what it does in a real run, the plan the server chooses with those statistics reads
// a page of questions and their records by the indexes. Every helper starts with idnAsk, so that none meets a name of another file.

// The sizes of the tables a page of questions is read from in the second part: big enough for the plan of a real run.
const (
	idnAskRecords   = 100_000
	idnAskQuestions = 50_000
)

// idnAskRows is the rows each relation of a plan gave (with the loops counted), and the scans of a whole relation in it.
func idnAskRows(t *testing.T, tx *sql.Tx, query string, args []any) (map[string]float64, []string) {
	t.Helper()
	var doc []byte
	if err := tx.QueryRow("EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, FORMAT JSON) "+query, args...).Scan(&doc); err != nil {
		t.Fatal(err)
	}
	var top []struct{ Plan map[string]any }
	if err := json.Unmarshal(doc, &top); err != nil {
		t.Fatal(err)
	}
	rows, whole := map[string]float64{}, []string(nil)
	var walk func(map[string]any)
	walk = func(node map[string]any) {
		if name, ok := node["Relation Name"].(string); ok {
			actual, _ := node["Actual Rows"].(float64)
			loops, _ := node["Actual Loops"].(float64)
			removed, _ := node["Rows Removed by Filter"].(float64)
			rows[name] += (actual + removed) * loops
			if kind, _ := node["Node Type"].(string); kind == "Seq Scan" {
				whole = append(whole, name)
			}
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
	return rows, whole
}

// [REQ-883] [REQ-982]
func TestIdnTheQuestionsOfARoundAreReadByTheirIndexWithTheStatisticsTheRunGivesThePlanner(t *testing.T) {
	const size = 50
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, idnMapping))
	k := s.ref.KGNo
	ask := fmt.Sprintf(`"s_%d".calc_ask`, k)
	// the server does not analyze the questions on its own while the test runs: what the planner knows of them is what the run told it
	s.e.exec(t, `ALTER TABLE `+ask+` SET (autovacuum_enabled = false)`)
	reads, inflated := 0, false
	s.e.st.identSeen = func(tx *sql.Tx, what, query string, args []any) {
		if what != "read the questions" {
			return
		}
		reads++
		// the questions of the round the page is of are all counted in the statistics of the table
		var known float64
		var asked int
		if err := tx.QueryRow(`SELECT reltuples FROM pg_class WHERE oid = $1::regclass`, ask).Scan(&known); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(`SELECT count(*) FROM ` + ask + ` WHERE NOT answer AND round_no = (SELECT max(round_no) FROM ` + ask + ` WHERE NOT answer)`).Scan(&asked); err != nil {
			t.Fatal(err)
		}
		if known < float64(asked) || asked == 0 {
			t.Errorf("the read %d of the questions: the planner knows of %.0f rows of calc_ask, and the round asks %d questions", reads, known, asked)
		}
		if inflated {
			return
		}
		inflated = true
		// at the size of a run that asks many questions, with the statistics the server keeps of them, the page is read by the indexes
		if _, err := tx.Exec("SAVEPOINT idn_ask_plan"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := tx.Exec("ROLLBACK TO SAVEPOINT idn_ask_plan"); err != nil {
				t.Fatal(err)
			}
		}()
		var round int
		if err := tx.QueryRow(`SELECT max(round_no) FROM ` + ask).Scan(&round); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{
			fmt.Sprintf(`INSERT INTO "s_%d".calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why)
				SELECT %d, %d, 'zz' || i, '{x}', '\x00', 0, 1, 0, 1 FROM generate_series(1, %d) i`, k, s.ref.SrcNo, s.rdfNo("ds_places"), idnAskRecords),
			fmt.Sprintf(`INSERT INTO %s (round_no, label, rid, feed_no, key_ord, type_no, parent_ref, answer)
				SELECT %d, 1000000000000 + i, r.rid, 0, 0, 1, 'p', false FROM generate_series(1, %d) i
				JOIN "s_%d".calc_record r ON r.key = 'zz' || (i * 2)`, ask, round, idnAskQuestions, k),
			`ANALYZE ` + ask, fmt.Sprintf(`ANALYZE "s_%d".calc_record`, k),
		} {
			if _, err := tx.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		rows, whole := idnAskRows(t, tx, query, args)
		t.Logf("a page of %d questions of %d, from %d records: %v, whole scans %v", size, idnAskQuestions, idnAskRecords, rows, whole)
		for table, n := range rows {
			if n > 4*size+50 {
				t.Errorf("the read of a page of %d questions reads %.0f rows of %s", size, n, table)
			}
		}
		for _, table := range whole {
			if strings.HasPrefix(table, "calc_") {
				t.Errorf("the read of a page of questions reads the whole of %s", table)
			}
		}
	}
	defer func() { s.e.st.identSeen = nil }()
	_, counts := idnPipeline(s, t, pgiTexts{"ds_places": idnPlaceRows(300, false, func(i int) string { return "T" + strconv.Itoa(i) }),
		"ds_servers": pgiServers[:strings.Index(pgiServers, "\n")+1], "ds_vms": ""}, rdfT0, size)
	if reads == 0 || !inflated || counts.NewObjects == 0 {
		t.Errorf("the questions were read %d times (%+v): the test does not look at what it is meant to", reads, counts)
	}
}
