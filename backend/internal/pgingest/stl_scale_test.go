package pgingest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// What a bucket of the cleaning reads (ADR-0128): m records of the run that all stand under one object and made one link (one key many
// records share), beside 2m records of the source that are not in the run. A statement that read, for each record, the other records of its
// key, or the whole of a table for each bucket, would read more at 3m than at m; a bucket reads its records and their lines by the indexes
// of the record and the blocks of the staged tables it is at, so it reads the same at both. The world is made in SQL, and the statements
// are asked how they went in the plan the server chooses (rclOperatingPlan). The test runs alone: it makes its database without the call
// that lets a test run beside the others (pgiSerial is the list of the tests made before this one, and is not changed).

// stlBucketRows is the size of a bucket of the test, and stlRowsPerRecord the most rows a statement may read in a bucket for a record of it
// (the record, its raw record, its references and problems, the ones the run computed, its end, its row received, the object it stands under).
const (
	stlBucketRows    = 100
	stlRowsPerRecord = 24
)

// stlSerialNew is rdfNew for a test that runs alone.
func stlSerialNew(t *testing.T, plan *ingest.Plan) *rdfSetup {
	t.Helper()
	datasets := pgiDatasets(plan)
	then := func(dsn string) error {
		pool, err := pgconn.Open(context.Background(), pgconn.Config{DSN: dsn})
		if err != nil {
			return err
		}
		_, err = New(pool).AttachSource(context.Background(), pgiKG(t, 1).ID, plan.Source.ID, datasets)
		if cerr := pool.Close(); err == nil {
			err = cerr
		}
		return err
	}
	dsn, kg := pgstore.NewTestDatabaseWithKG(t, true, pgiKG(t, 1), "pgingest AttachSource "+plan.Source.ID+" "+strings.Join(datasets, ","), then)
	e := pgiOpenOn(t, dsn)
	return &rdfSetup{e: e, kg: kg, plan: plan, ref: rdfRefOf(t, e, kg, plan.Source.ID)}
}

// stlWorld makes the world of m records of the run in a run that is settling.
func stlWorld(t *testing.T, m int) *stlFix {
	t.Helper()
	s := stlSerialNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_sv": ingest.CoverageComplete})
	f := &stlFix{s: s, l: l}
	k, src, ds := s.ref.KGNo, s.ref.SrcNo, s.rdfNo("ds_sv")
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".calc_owner (run_no) VALUES ($1)`, k), l.No)
	s.e.exec(t, `UPDATE sync_run SET calc_job = $2, counts = counts || $3::jsonb WHERE run_no = $1`, l.No, pgschema.CalcJobIsolate,
		fmt.Sprintf(`{"isolate":{"finished":true},"raw_diff":{"at":"2031-01-01T00:00:00Z","datasets":[{"dataset":"ds_sv","dataset_no":%d,"coverage":"complete"}]}}`, ds))
	stlSettling(s, t, l)
	link := pgschema.ProblemDefOfLink(0)
	for _, q := range []string{
		// 3m raw records, waits, references and problems of the source; the first m are records of the run, half of them received now
		`INSERT INTO {k}.raw_record (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) SELECT $1, $2, 'k' || lpad(i::text, 7, '0'), '\x00', ARRAY['x'], 0, 1, 0 FROM generate_series(1, 3 * $3::int) i`,
		`INSERT INTO {k}.record_wait (src_no, dataset_no, key, kind, runs, first_at) SELECT $1, $2, 'k' || lpad(i::text, 7, '0'), 0, 1, '2031-01-01' FROM generate_series(1, 3 * $3::int) i`,
		`INSERT INTO {k}.record_ref (src_no, dataset_no, key, role, def_no, sub, lookup_hash, target_no, state) SELECT $1, $2, 'k' || lpad(i::text, 7, '0'), r.role, 0, 0, '\x01', r.role, 0
			FROM generate_series(1, 3 * $3::int) i CROSS JOIN (VALUES (1), (2)) AS r(role)`,
		fmt.Sprintf(`INSERT INTO {k}.record_problem (src_no, dataset_no, key, def_no, field, kind, reason) SELECT $1, $2, 'k' || lpad(i::text, 7, '0'), d.def_no, '', 'source_conflict', 'old'
			FROM generate_series(1, 3 * $3::int) i CROSS JOIN (VALUES (0), (%d)) AS d(def_no)`, link),
		`INSERT INTO {k}.calc_record (src_no, dataset_no, key, fields, raw_hash, decl_rev, opened_seq, fate, why, page, row_no)
			SELECT $1, $2, 'k' || lpad(i::text, 7, '0'), ARRAY['y'], '\x01', 0, 2, 0, CASE WHEN i % 2 = 0 THEN 0 ELSE 2 END, CASE WHEN i % 2 = 0 THEN i END, CASE WHEN i % 2 = 0 THEN 0 END FROM generate_series(1, $3::int) i`,
		`INSERT INTO {k}.calc_wait (src_no, dataset_no, key, kind, runs, first_at, expired) SELECT $1, $2, 'k' || lpad(i::text, 7, '0'), 0, 2, '2031-01-01', false FROM generate_series(1, 3 * $3::int) i`,
	} {
		s.e.exec(t, f.stlQ(q), src, ds, m)
	}
	for _, q := range []string{
		`INSERT INTO {k}.calc_ref (rid, role, def_no, sub, lookup_hash, target_no, state) SELECT c.rid, r.role, 0, 0, '\x01', r.role, 0 FROM {k}.calc_record c CROSS JOIN (VALUES (1), (2)) AS r(role)`,
		`INSERT INTO {k}.calc_end (rid, def_no, side, lookup_hash) SELECT rid, 0, 2, '\x01' FROM {k}.calc_record`,
		`INSERT INTO {k}.calc_problem (rid, def_no, field, kind, reason) SELECT rid, 0, '', 'source_conflict', 'new' FROM {k}.calc_record`,
		`INSERT INTO {k}.stage_object (obj_no, ref, type_no, name, min_grade, doc, content_hash, row_hash, is_new) VALUES (1, 'o-1', 1, 'o', 0, '{}', '\x00', '\x00', false)`,
	} {
		s.e.exec(t, f.stlQ(q))
	}
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, page, row_no, key, place, filtered, change_kind, raw_hash, fields, problems)
		SELECT src_no, dataset_no, page, row_no, key, '', false, 1, '\x01', fields, '[{"kind":"invalid_value","feed":"f_sv","reason":"r"}]' FROM "s_%d".calc_record WHERE page IS NOT NULL`,
		pgiTable(s.ref, pgschema.TableStageRecord), k))
	for _, table := range []string{pgschema.TableRawRecord, pgschema.TableRecordWait, pgschema.TableRecordRef, pgschema.TableRecordProblem, pgschema.TableCalcRecord,
		pgschema.TableCalcRef, pgschema.TableCalcEnd, pgschema.TableCalcProblem, pgschema.TableCalcWait, pgschema.TableStageObject, pgschema.TableStageRecord} {
		s.e.exec(t, fmt.Sprintf(`ANALYZE "s_%d"."%s"`, k, table))
	}
	return f
}

// stlRowsRead cleans the world and gives the most rows each statement read in a bucket.
func stlRowsRead(t *testing.T, f *stlFix) map[string]float64 {
	t.Helper()
	p := &idnPlans{most: map[string]*idnRead{}}
	f.s.e.st.stlSeen = rclOperatingPlan(p)
	if _, err := ingest.RunSettle(f.s.e.ctx, f.s.e.st, f.l, stlStart(f.s, stlBucketRows), &stlConfirms{}); err != nil {
		t.Fatal(err)
	}
	f.s.e.st.stlSeen = nil
	out := map[string]float64{}
	for what, read := range p.most {
		for _, n := range read.rows {
			out[what] += n
		}
	}
	return out
}

// [REQ-986]
func TestStlABucketOfTheCleaningReadsTheLinesOfItsRecordsAndNotTheRestOfTheTablesSoWhatItReadsDoesNotGrowWithTheSource(t *testing.T) {
	const m = 600 // big enough that the planner reads the tables of the records by their indexes at both sizes
	a, b := stlWorld(t, m), stlWorld(t, 3*m)
	small, big := stlRowsRead(t, a), stlRowsRead(t, b)
	for _, what := range []string{"take away the problems of what the run computed", "write the problems the receiving found", "take away the references the run wrote again",
		"write the references the run computed", "write the problems the run computed", "delete the records that are closed", "write the records received now",
		"write the fate of the other records", "delete the waits the source had", "write the waits of the run"} {
		x, y := small[what], big[what]
		t.Logf("%s: %.0f rows at %d, %.0f at %d", what, x, m, y, 3*m)
		if x == 0 {
			t.Errorf("%s read no rows", what)
		}
		if limit := float64(stlRowsPerRecord * stlBucketRows); x > limit || y > limit {
			t.Errorf("%s: %.0f and %.0f rows in a bucket of %d records, more than %.0f", what, x, y, stlBucketRows, limit)
		}
		if y > 1.25*x+float64(stlBucketRows) {
			t.Errorf("%s: %.0f rows at %d and %.0f at %d: a bucket reads more as the source grows", what, x, m, y, 3*m)
		}
	}
	for _, f := range []*stlFix{a, b} {
		n := f.s.e.count(t, f.stlQ(`SELECT count(*) FROM {k}.record_problem WHERE reason = 'new'`))
		old := f.s.e.count(t, f.stlQ(`SELECT count(*) FROM {k}.record_problem WHERE reason = 'old'`))
		runs := f.s.e.count(t, f.stlQ(`SELECT count(*) FROM {k}.record_wait WHERE runs = 2`))
		if records := f.s.e.count(t, f.stlQ(`SELECT count(*) FROM {k}.raw_record`)); n == 0 || old == 0 || runs != records {
			t.Errorf("the world was not cleaned: %d new problems, %d old, %d waits of the run for %d records", n, old, runs, records)
		}
	}
}
