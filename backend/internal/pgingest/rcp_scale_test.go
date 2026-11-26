package pgingest

import (
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// What the computation of the objects reads when many records stand under the same objects: m objects of three records each and one
// object that m records stand under, all of them read by one page. A statement that read the references of every object for each object
// (or the records for each reference) would read m squared rows; the ones that find the lines of an object by the index of the targets
// read a number of rows that goes with the lines. The world is made in SQL (no pipeline), and the statements are asked how they went
// (EXPLAIN ANALYZE), as the measurement of the expansion does it (exp_scale_test.go).

// rcpShared makes m small objects and one big object, with the lines under them, in a run whose expansion is done.
func rcpShared(t *testing.T, m int) (*rdfSetup, ingest.RunLease) {
	t.Helper()
	return rcpSharedWith(t, m, idnServerMapping)
}

func rcpSharedWith(t *testing.T, m int, mapping string) (*rdfSetup, ingest.RunLease) {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, idnServerSource, mapping))
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_s": ingest.CoverageComplete})
	k, src, ds := s.ref.KGNo, s.ref.SrcNo, s.rdfNo("ds_s")
	typeNo := s.plan.IdentifyFeeds(src)[0].TypeNo
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, k), l.No)
	s.e.exec(t, `UPDATE sync_run SET calc_job = $2, counts = counts || '{"expand":{"rule":"done"}}'::jsonb WHERE run_no = $1`, l.No, pgschema.CalcJobExpand)
	// the small objects have the numbers 1 to m, the big one m + 1
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%[1]d".object (obj_no, ref, type_no, name, min_grade, seq_from) SELECT i, 'obj-' || i, %[2]d, 'n' || i, 0, 1 FROM generate_series(1, %[3]d) i`, k, typeNo, m+1))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_object (obj_no, why) SELECT i, %d FROM generate_series(1, %d) i`, k, pgschema.ObjectWhyIdentified, m+1))
	// three records for each small object and m for the big one, every record of an object with the same cells, so the object has one value
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate)
		SELECT $1::int, $2::int, 'r' || o || '-' || j, '\x00'::bytea, ARRAY['n' || o, 'sn' || o, 'Zorvane', 'u' || o], d.decl_rev, 1, 0::smallint
		FROM generate_series(1, %d) o CROSS JOIN generate_series(1, 3) j CROSS JOIN source_dataset d WHERE d.dataset_no = $2 AND o <= %d
		UNION ALL SELECT $1::int, $2::int, 'b' || j, '\x00'::bytea, ARRAY['big', 'snb', 'Zorvane', 'ub'], d.decl_rev, 1, 0::smallint
		FROM generate_series(1, %d) j CROSS JOIN source_dataset d WHERE d.dataset_no = $2`, pgiTable(s.ref, pgschema.TableRawRecord), m+1, m, m), src, ds)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, target_no, state, retirable)
		SELECT src_no, dataset_no, key, %d, 0, 0, CASE WHEN key LIKE 'b%%' THEN %d ELSE split_part(substr(key, 2), '-', 1)::bigint END, %d, false FROM %s`,
		k, pgschema.RefRoleIdentity, m+1, pgschema.RefResolved, pgiTable(s.ref, pgschema.TableRawRecord)))
	// the references of other objects, many more than these, as a KG has: they are not the lines of any object of the run
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_ref (src_no, dataset_no, key, role, def_no, sub, target_no, state, retirable)
		SELECT $1::int, $2::int, 'z' || i, %d, 0, 0, 1000000 + i, %d, false FROM generate_series(1, %d) i`, k, pgschema.RefRoleIdentity, pgschema.RefResolved, 30*m), src, ds)
	for _, table := range []string{pgschema.TableRecordRef, pgschema.TableObject, pgschema.TableCalcObject} {
		s.e.exec(t, fmt.Sprintf(`ANALYZE "s_%d"."%s"`, k, table))
	}
	s.e.exec(t, fmt.Sprintf(`ANALYZE %s`, pgiTable(s.ref, pgschema.TableRawRecord)))
	return s, l
}

// the statements of the computation that read the lines, by what they say they do
var rcpSharedWhats = []string{"read the objects", "read the lines of the objects", "read the claims of the lines"}

// rcpRowsRead is the rows each statement of the computation read for m, and the report.
func rcpRowsRead(t *testing.T, m int) (map[string]float64, ingest.RecomputeCounts) {
	t.Helper()
	s, l := rcpShared(t, m)
	p := &idnPlans{most: map[string]*idnRead{}}
	s.e.st.rcpSeen = p.idnHook
	counts := rcpDo(s, t, l, 100*m, 0)
	out := map[string]float64{}
	for what, read := range p.most {
		for _, n := range read.rows {
			out[what] += n
		}
	}
	return out, counts
}

// [REQ-936]
func TestRcpWhatManyRecordsOfAnObjectShareIsReadOnceSoTheRowsReadGoWithTheNumberOfLines(t *testing.T) {
	const m = 150
	small, counts := rcpRowsRead(t, m)
	big, bigCounts := rcpRowsRead(t, 3*m)
	if counts.Objects != m+1 || counts.Lines != 4*m || bigCounts.Objects != 3*m+1 || bigCounts.Lines != 12*m {
		t.Errorf("%+v %+v", counts, bigCounts)
	}
	for _, what := range rcpSharedWhats {
		a, b := small[what], big[what]
		t.Logf("%s: %d objects %.0f rows, %d objects %.0f rows", what, m+1, a, 3*m+1, b)
		if a < float64(counts.Lines) && what == "read the lines of the objects" {
			t.Errorf("%s: read %.0f rows, fewer than the lines", what, a)
		}
		if b > 3.4*a {
			t.Errorf("%s: %.0f rows for %d lines and %.0f for %d: more than three times as many", what, a, counts.Lines, b, bigCounts.Lines)
		}
	}
}
