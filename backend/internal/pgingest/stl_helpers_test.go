package pgingest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the cleaning (ADR-0128). Every helper, type and constant they add starts with stl, so that none meets a name of another test
// file of this folder, and the one method they declare (stlConfirms.Confirm) has a name no other test file of the folder declares.

// stlConfirms stands for the graph's confirmation: it counts the calls.
type stlConfirms struct{ n int }

func (c *stlConfirms) Confirm(context.Context) error {
	c.n++
	return nil
}

// stlSettling moves a run whose computing is done to settling, as the apply and the volatile values would.
func stlSettling(s *rdfSetup, t *testing.T, l ingest.RunLease) {
	t.Helper()
	for _, mv := range [][2]ingest.RunStage{{ingest.RunComputing, ingest.RunApplying}, {ingest.RunApplying, ingest.RunVolatile}, {ingest.RunVolatile, ingest.RunSettling}} {
		if err := s.e.st.AdvanceRun(s.e.ctx, l, mv[0], mv[1]); err != nil {
			t.Fatal(err)
		}
	}
}

// stlGraph does to the graph what the apply does with what the run staged (the registry and the names of the objects, the keys, the
// retirement, the links) and nothing to the tables of the records, which are the cleaning's: the stand-in of the apply until the difference
// (I-5c2) gives it its chunks.
func stlGraph(s *rdfSetup, t *testing.T) {
	t.Helper()
	k := s.ref.KGNo
	for _, q := range []string{
		`INSERT INTO "s_%[1]d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT obj_no, ref, type_no, ref, 0, 1 FROM "s_%[1]d".calc_newobj`,
		`INSERT INTO "s_%[1]d".object_key (type_no, ctx_no, sig_hash, obj_no, bound_seq) SELECT type_no, ctx_no, sig_hash, obj_no, 1 FROM "s_%[1]d".calc_newkey`,
		`UPDATE "s_%[1]d".object SET retired_seq = 1 WHERE obj_no IN (SELECT obj_no FROM "s_%[1]d".calc_object WHERE why = 4)`,
		`UPDATE "s_%[1]d".object SET retired_seq = NULL WHERE obj_no IN (SELECT obj_no FROM "s_%[1]d".calc_object WHERE why = 1)`,
		`UPDATE "s_%[1]d".object o SET name = x.name, min_grade = x.min_grade, first_key = COALESCE(o.first_key, x.first_key) FROM "s_%[1]d".stage_object x WHERE x.obj_no = o.obj_no`,
		// the links the records of the run made before and do not make any more go, and the links of an object that retires; the ones the
		// run staged are there
		`DELETE FROM "s_%[1]d".link k WHERE NOT EXISTS (SELECT 1 FROM "s_%[1]d".stage_link l WHERE l.link_no = k.link_no) AND (EXISTS (SELECT 1 FROM "s_%[1]d".record_ref x
			JOIN "s_%[1]d".calc_record c ON c.src_no = x.src_no AND c.dataset_no = x.dataset_no AND c.key = x.key WHERE x.role = 2 AND x.target_no = k.link_no)
			OR EXISTS (SELECT 1 FROM "s_%[1]d".calc_object o WHERE o.why = 4 AND o.obj_no IN (k.src, k.dst)))`,
		`INSERT INTO "s_%[1]d".link (link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, seq_from)
			SELECT link_no, ref, type_no, src, dst, grade, basis, content_hash, row_hash, 1 FROM "s_%[1]d".stage_link ON CONFLICT (link_no) DO NOTHING`,
	} {
		s.e.exec(t, fmt.Sprintf(q, k))
	}
}

// stlStart is the start of the cleaning of the source of the setup with buckets of the size given.
func stlStart(s *rdfSetup, rows int) ingest.SettleStart {
	return ingest.SettleStart{Rows: rows, Feeds: ingest.SettleFeedsOf(s.plan)}
}

// stlClean does the cleaning of a run that is settling.
func stlClean(s *rdfSetup, t *testing.T, l ingest.RunLease, rows int) ingest.SettleCounts {
	t.Helper()
	c := &stlConfirms{}
	counts, err := ingest.RunSettle(s.e.ctx, s.e.st, l, stlStart(s, rows), c)
	if err != nil {
		t.Fatal(err)
	}
	if c.n != 1 {
		t.Fatalf("the graph was confirmed %d times", c.n)
	}
	return counts
}

// stlCompute does a run of the texts up to the end of its computing: the receiving, the comparison, the identification, the expansion, the
// objects, the links and the fifth step.
func stlCompute(s *rdfSetup, t *testing.T, texts pgiTexts, at time.Time, full bool) ingest.RunLease {
	t.Helper()
	l := rclPrepare(s, t, texts, at, full)
	rclDo(s, t, l, rclPlans(s), 0)
	isoDo(s, t, l, rclPlans(s), 0)
	return l
}

// stlWhole does a whole run of the texts with the cleaning and gives what the cleaning counted.
func stlWhole(s *rdfSetup, t *testing.T, texts pgiTexts, at time.Time, rows int) ingest.SettleCounts {
	t.Helper()
	l := stlCompute(s, t, texts, at, false)
	stlSettling(s, t, l)
	stlGraph(s, t)
	return stlClean(s, t, l, rows)
}

// stlAt is the time of the n-th run of a test: 13 hours apart, so that a wait ends after its third run.
func stlAt(n int) time.Time { return rdfT0.Add(time.Duration(n) * 13 * time.Hour) }

// stlDump is the permanent tables of the records of the KG as lines of text, each table sorted: what two ways of cleaning are compared by.
// The raw records are without their fate when fate is false.
func stlDump(s *rdfSetup, t *testing.T, fate bool) map[string][]string {
	t.Helper()
	k := s.ref.KGNo
	f := "''"
	if fate {
		f = "r.fate::text"
	}
	q := map[string]string{
		"raw": `SELECT d.dataset_id || '/' || r.key || '/' || encode(r.raw_hash, 'hex') || '/' || array_to_string(r.fields, ',') || '/' || r.decl_rev || '/' || r.opened_seq || '/' || ` + f +
			` FROM "s_%[1]d".raw_record r JOIN source_dataset d ON d.dataset_no = r.dataset_no ORDER BY 1`,
		"wait": `SELECT d.dataset_id || '/' || w.key || '/' || w.kind || '/' || w.runs || '/' || w.first_at || '/' || w.expired FROM "s_%[1]d".record_wait w
			JOIN source_dataset d ON d.dataset_no = w.dataset_no ORDER BY 1`,
		"dup": `SELECT d.dataset_id || '/' || p.key || '/' || p.kind || '/' || p.reason FROM "s_%[1]d".record_problem p JOIN source_dataset d ON d.dataset_no = p.dataset_no
			WHERE p.kind = 'duplicate_key' ORDER BY 1`,
		"ref": `SELECT d.dataset_id || '/' || x.key || '/' || x.role || '/' || x.def_no || '/' || x.sub || '/' || COALESCE(o.ref, l.ref, '-') || '/' || x.state || '/' || x.retirable
			FROM "s_%[1]d".record_ref x JOIN source_dataset d ON d.dataset_no = x.dataset_no LEFT JOIN "s_%[1]d".object o ON x.role IN (1, 3, 4) AND o.obj_no = x.target_no
			LEFT JOIN "s_%[1]d".link l ON x.role = 2 AND l.link_no = x.target_no ORDER BY 1`,
		"problem": `SELECT d.dataset_id || '/' || p.key || '/' || COALESCE(p.def_no::text, '-') || '/' || p.kind || '/' || p.field FROM "s_%[1]d".record_problem p
			JOIN source_dataset d ON d.dataset_no = p.dataset_no ORDER BY 1`,
	}
	out := map[string][]string{}
	for name, query := range q {
		out[name] = expList(s.e, t, fmt.Sprintf(query, k))
	}
	return out
}

// stlSame fails the test when the tables named differ between two dumps.
func stlSame(t *testing.T, what string, a, b map[string][]string, tables ...string) {
	t.Helper()
	for _, name := range tables {
		if strings.Join(a[name], "\n") != strings.Join(b[name], "\n") {
			t.Errorf("%s: the table %s differs:\n%s\n---\n%s", what, name, strings.Join(a[name], "\n"), strings.Join(b[name], "\n"))
		}
	}
}

// stlFate is the fate of the raw record of a key of a dataset.
func stlFate(s *rdfSetup, t *testing.T, dataset, key string) int {
	t.Helper()
	return s.e.count(t, fmt.Sprintf(`SELECT fate FROM "s_%d".raw_record WHERE dataset_no = $1 AND key = $2`, s.ref.KGNo), s.rdfNo(dataset), key)
}

// stlEmpty says how many rows the staged tables of the KG and the source have.
func stlEmpty(s *rdfSetup, t *testing.T) int {
	t.Helper()
	n := 0
	for _, table := range pgschema.ComputeTables() {
		n += s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d"."%s"`, s.ref.KGNo, table))
	}
	for _, table := range pgschema.ReceiveTables() {
		n += s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM %s`, pgiTable(s.ref, table)))
	}
	return n
}
