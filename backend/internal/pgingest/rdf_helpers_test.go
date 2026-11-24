package pgingest

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the comparison of the received rows with the raw records (ADR-0102). Every helper, type and field they add starts
// with rdf, so that none meets a name of another test file of this folder.

// rdfT0 is the time of the first run of a test; the runs after it are some hours later.
var rdfT0 = time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)

// rdfSetup is a KG with the source of the test plan put to it.
type rdfSetup struct {
	e    *pgiEnv
	kg   graph.KG
	plan *ingest.Plan
	ref  SourceRef
}

func rdfNew(t *testing.T, plan *ingest.Plan) *rdfSetup {
	t.Helper()
	e := pgiOpen(t)
	kg := e.kg(t, 1)
	return &rdfSetup{e: e, kg: kg, plan: plan, ref: e.attach(t, kg, plan)}
}

func rdfHash(text string) []byte {
	h := kernel.HashBytes([]byte(text))
	return h[:]
}

// rdfRaw is a raw record the source keeps.
type rdfRaw struct {
	dataset string
	key     string
	content string
	fate    int16
}

// rdfStaged is a row the receiving staged. kind is what the receiving found of it against the raw record: the same rows have no
// fields, as the receiving writes them.
type rdfStaged struct {
	dataset  string
	page     int
	row      int
	key      string
	content  string
	kind     int16
	filtered bool
}

// rdfWaitRow is a wait the source keeps.
type rdfWaitRow struct {
	dataset string
	key     string
	kind    int16
	runs    int
	first   time.Time
	expired bool
}

func (s *rdfSetup) rdfNo(dataset string) int64 { return s.ref.Datasets[dataset] }

func (s *rdfSetup) rdfPutRaw(t *testing.T, rows ...rdfRaw) {
	t.Helper()
	for _, r := range rows {
		s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES ($1, $2, $3, $4, $5, 1, 100, $6)`,
			pgiTable(s.ref, pgschema.TableRawRecord)), s.ref.SrcNo, s.rdfNo(r.dataset), r.key, rdfHash(r.content), textArray([]string{r.content}), r.fate)
	}
}

func (s *rdfSetup) rdfPutStage(t *testing.T, rows ...rdfStaged) {
	t.Helper()
	for _, r := range rows {
		var hash, fields any
		if r.kind != pgschema.ReceiveSame {
			hash, fields = rdfHash(r.content), textArray([]string{r.content})
		}
		s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, page, row_no, key, place, filtered, change_kind, raw_hash, fields) VALUES ($1, $2, $3, $4, $5, '', $6, $7, $8, $9)`,
			pgiTable(s.ref, pgschema.TableStageRecord)), s.ref.SrcNo, s.rdfNo(r.dataset), r.page, r.row, r.key, r.filtered, r.kind, hash, fields)
	}
}

func (s *rdfSetup) rdfPutWaits(t *testing.T, rows ...rdfWaitRow) {
	t.Helper()
	for _, r := range rows {
		s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_wait (src_no, dataset_no, key, kind, runs, first_at, expired) VALUES ($1, $2, $3, $4, $5, $6, $7)`, s.ref.KGNo),
			s.ref.SrcNo, s.rdfNo(r.dataset), r.key, r.kind, r.runs, r.first, r.expired)
	}
}

// computing opens a run of the source, says what the receiving made of each dataset and moves the run to computing.
func (s *rdfSetup) rdfComputing(t *testing.T, owner string, cover map[string]ingest.Coverage) ingest.RunLease {
	t.Helper()
	l := s.e.open(t, s.kg, s.plan.Source.ID, owner)
	names := make([]string, 0, len(cover))
	for name := range cover {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines []string
	for _, name := range names {
		lines = append(lines, fmt.Sprintf(`{"dataset":%q,"dataset_no":%d,"complete":%t,"coverage":%q}`, name, s.rdfNo(name), cover[name] == ingest.CoverageComplete, cover[name]))
	}
	s.e.exec(t, `UPDATE sync_run SET counts = counts || $2::jsonb WHERE run_no = $1`, l.No, `{"receive":{"datasets":[`+strings.Join(lines, ",")+`]}}`)
	if err := s.e.st.AdvanceRun(s.e.ctx, l, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Fatal(err)
	}
	return l
}

// rdfStart is the start of the comparison at a time, with every dataset of the plan retiring unless it is named in keep.
func (s *rdfSetup) rdfStart(at time.Time, bucketRows int, keep ...string) ingest.RawDiffStart {
	st := ingest.RawDiffStart{Now: at, BucketRows: bucketRows}
	for _, d := range s.plan.Source.Datasets {
		st.Datasets = append(st.Datasets, ingest.RawDiffDataset{Dataset: d.ID, Retire: !strings.Contains(strings.Join(keep, ","), d.ID)})
	}
	return st
}

// rdfRecord is a row of calc_record as the test reads it.
type rdfRecord struct {
	Dataset string
	Key     string
	Why     int16
	Fields  string
	Fate    int16
	Opened  int64
	Page    string
	Row     string
}

func (s *rdfSetup) rdfRecords(t *testing.T) []rdfRecord {
	t.Helper()
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT d.dataset_id, c.key, c.why, c.fields::text, c.fate, c.opened_seq, coalesce(c.page::text, '-'), coalesce(c.row_no::text, '-')
		FROM "s_%d".calc_record c JOIN source_dataset d ON d.dataset_no = c.dataset_no ORDER BY d.dataset_id, c.key`, s.ref.KGNo))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []rdfRecord
	for rows.Next() {
		var r rdfRecord
		if err := rows.Scan(&r.Dataset, &r.Key, &r.Why, &r.Fields, &r.Fate, &r.Opened, &r.Page, &r.Row); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// rdfWaitOut is a row of calc_wait as the test reads it.
type rdfWaitOut struct {
	Dataset string
	Key     string
	Kind    int16
	Runs    int
	First   time.Time
	Expired bool
}

func (s *rdfSetup) rdfCalcWaits(t *testing.T) []rdfWaitOut {
	t.Helper()
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT d.dataset_id, c.key, c.kind, c.runs, c.first_at, c.expired
		FROM "s_%d".calc_wait c JOIN source_dataset d ON d.dataset_no = c.dataset_no ORDER BY d.dataset_id, c.key, c.kind`, s.ref.KGNo))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []rdfWaitOut
	for rows.Next() {
		var w rdfWaitOut
		if err := rows.Scan(&w.Dataset, &w.Key, &w.Kind, &w.Runs, &w.First, &w.Expired); err != nil {
			t.Fatal(err)
		}
		w.First = w.First.UTC()
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// rdfRecordsOf is the records by key of a dataset.
func rdfByKey(rs []rdfRecord, dataset string) map[string]rdfRecord {
	out := map[string]rdfRecord{}
	for _, r := range rs {
		if r.Dataset == dataset {
			out[r.Key] = r
		}
	}
	return out
}

func rdfWaitsByKey(ws []rdfWaitOut, dataset string) map[string][]rdfWaitOut {
	out := map[string][]rdfWaitOut{}
	for _, w := range ws {
		if w.Dataset == dataset {
			out[w.Key] = append(out[w.Key], w)
		}
	}
	return out
}

// rdfRun runs the whole comparison and fails the test on an error.
func (s *rdfSetup) rdfRun(t *testing.T, l ingest.RunLease, st ingest.RawDiffStart) ingest.RawDiffReport {
	t.Helper()
	rep, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, st)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// settle does to the raw records and the waits what the cleaning will do with what the comparison made (ADR-0102 decision 4): a
// record the run opened is written over its raw record, a record that was closed is deleted, and the waits of the source are written
// over by those of the run except the absences that closed a record. Then the run ends and the staged rows are emptied.
func (s *rdfSetup) rdfSettle(t *testing.T, l ingest.RunLease) {
	t.Helper()
	k := s.ref.KGNo
	raw := pgiTable(s.ref, pgschema.TableRawRecord)
	s.e.exec(t, fmt.Sprintf(`DELETE FROM %s r USING "s_%d".calc_record c WHERE c.why = %d AND c.dataset_no = r.dataset_no AND c.key = r.key`, raw, k, pgschema.CalcWhyClosed))
	s.e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate)
		SELECT src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate FROM "s_%d".calc_record WHERE why IN (%d, %d)
		ON CONFLICT (src_no, dataset_no, key) DO UPDATE SET raw_hash = excluded.raw_hash, fields = excluded.fields, decl_rev = excluded.decl_rev,
		opened_seq = excluded.opened_seq, fate = excluded.fate`, raw, k, pgschema.CalcWhyNew, pgschema.CalcWhyChanged))
	s.e.exec(t, fmt.Sprintf(`DELETE FROM "s_%d".record_wait WHERE src_no = $1`, k), s.ref.SrcNo)
	s.e.exec(t, fmt.Sprintf(`INSERT INTO "s_%d".record_wait (src_no, dataset_no, key, kind, runs, first_at, expired)
		SELECT src_no, dataset_no, key, kind, runs, first_at, expired FROM "s_%d".calc_wait WHERE NOT (kind = %d AND expired)`, k, k, pgschema.WaitUnseen))
	if err := s.e.st.EndRun(s.e.ctx, l, ingest.RunEnd{Stage: ingest.RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	trunc, err := pgschema.TruncateReceiveSQL(k, s.ref.SrcNo)
	if err != nil {
		t.Fatal(err)
	}
	s.e.exec(t, trunc)
}

func rdfTimes(h int) time.Time { return rdfT0.Add(time.Duration(h) * time.Hour) }
