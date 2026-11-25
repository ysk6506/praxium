package pgingest

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// dump is everything the comparison made, without the numbers the database gives to the rows, as sorted lines of text.
func (s *rdfSetup) rdfDump(t *testing.T) string {
	t.Helper()
	var lines []string
	for _, r := range s.rdfRecords(t) {
		lines = append(lines, fmt.Sprintf("record %s/%s why=%d fields=%s fate=%d opened=%d at=%s:%s", r.Dataset, r.Key, r.Why, r.Fields, r.Fate, r.Opened, r.Page, r.Row))
	}
	for _, w := range s.rdfCalcWaits(t) {
		lines = append(lines, fmt.Sprintf("wait %s/%s kind=%d runs=%d first=%s expired=%t", w.Dataset, w.Key, w.Kind, w.Runs, w.First.Format(time.RFC3339), w.Expired))
	}
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT r.key, p.kind, p.reason, p.def_no IS NULL FROM "s_%d".calc_problem p JOIN "s_%d".calc_record r ON r.rid = p.rid`, s.ref.KGNo, s.ref.KGNo))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key, kind, reason string
		var noDef bool
		if err := rows.Scan(&key, &kind, &reason, &noDef); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("problem %s %s %q nodef=%t", key, kind, reason, noDef))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func rdfCover() map[string]ingest.Coverage {
	return map[string]ingest.Coverage{"ds_places": ingest.CoverageComplete, "ds_servers": ingest.CoverageComplete}
}

// rdfReference is what one bucket gives, which the other cuts are held to.
func rdfReference(t *testing.T) string {
	t.Helper()
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	rdfSeedFates(t, s)
	l := s.rdfComputing(t, "server-a", rdfCover())
	if rep := s.rdfRun(t, l, s.rdfStart(rdfNow, 0)); rep.Buckets != 1 {
		t.Fatalf("%+v", rep)
	}
	return s.rdfDump(t)
}

// [REQ-782]
func TestRdfWhatIsMadeDoesNotDependOnHowTheWorkIsCutIntoBuckets(t *testing.T) {
	want := rdfReference(t)
	last := 1
	for _, rows := range []int{5, 2, 1} {
		s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
		rdfSeedFates(t, s)
		l := s.rdfComputing(t, "server-a", rdfCover())
		rep := s.rdfRun(t, l, s.rdfStart(rdfNow, rows))
		if rep.Buckets < last || rep.Buckets < 2 {
			t.Errorf("%d rows to a bucket: %d buckets, and %d before with more rows to a bucket", rows, rep.Buckets, last)
		}
		last = rep.Buckets
		if got := s.rdfDump(t); got != want {
			t.Errorf("%d rows to a bucket:\n%s\nwant\n%s", rows, got, want)
		}
		if c := rep.Counts[0]; c.Starting != 11 || c.Added != 2 || c.Unseen != 3 {
			t.Errorf("%d rows to a bucket: %+v", rows, c)
		}
		// the run record keeps one line to a dataset however many buckets there are, not a line to every bucket
		if n := s.e.count(t, `SELECT jsonb_array_length(counts->'raw_diff'->'totals') FROM sync_run WHERE run_no = $1`, l.No); n != len(rep.Counts) {
			t.Errorf("%d rows to a bucket: the run keeps %d lines of what the buckets made, for %d datasets", rows, n, len(rep.Counts))
		}
		if n := s.e.count(t, `SELECT count(*) FROM sync_run WHERE run_no = $1 AND counts->'raw_diff' ? 'parts'`, l.No); n != 0 {
			t.Errorf("%d rows to a bucket: the run keeps a line to every bucket", rows)
		}
	}
}

// [REQ-782]
func TestRdfTheBucketsAreRangesOfDatasetAndKeyThatFollowOneAnotherAndTheLastHasNoEnd(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	rdfSeedFates(t, s)
	l := s.rdfComputing(t, "server-a", rdfCover())
	if _, err := s.e.st.BeginRawDiff(s.e.ctx, l, s.rdfStart(rdfNow, 1)); err != nil {
		t.Fatal(err)
	}
	var prev *rawDiffKey
	for part := 0; part < 100; part++ {
		b, err := s.e.st.DiffRawBucket(s.e.ctx, l, part)
		if err != nil {
			t.Fatal(err)
		}
		var counts []byte
		if err := s.e.admin.QueryRowContext(s.e.ctx, `SELECT counts FROM sync_run WHERE run_no = $1`, l.No).Scan(&counts); err != nil {
			t.Fatal(err)
		}
		state, err := stateOf(counts)
		if err != nil {
			t.Fatal(err)
		}
		if b.Last != state.Done || b.Last != (state.Lo == nil) {
			t.Fatalf("bucket %d: last %t, done %t, next start %+v", part, b.Last, state.Done, state.Lo)
		}
		if b.Last {
			if part < 3 {
				t.Fatalf("%d buckets for rows of a size of 1", part+1)
			}
			if _, err := s.e.st.DiffRawBucket(s.e.ctx, l, part+1); !pgiCodeIs(err, ingest.CodeRunStage) {
				t.Fatalf("a bucket after the last: %v", err)
			}
			return
		}
		if prev != nil && (state.Lo.DatasetNo < prev.DatasetNo || (state.Lo.DatasetNo == prev.DatasetNo && state.Lo.Key <= prev.Key)) {
			t.Fatalf("bucket %d ends at %+v, which is not past %+v", part, state.Lo, prev)
		}
		prev = state.Lo
	}
	t.Fatal("the buckets do not end")
}

// [REQ-782]
func TestRdfAServerThatTakesTheRunOverGoesOnFromTheBucketTheRunIsAtAndCountsAtTheTimeTheComparisonBeganAt(t *testing.T) {
	want := rdfReference(t)
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	rdfSeedFates(t, s)
	a := s.rdfComputing(t, "server-a", rdfCover())
	if _, err := s.e.st.BeginRawDiff(s.e.ctx, a, s.rdfStart(rdfNow, 1)); err != nil {
		t.Fatal(err)
	}
	for part := 0; part < 3; part++ {
		if b, err := s.e.st.DiffRawBucket(s.e.ctx, a, part); err != nil || b.Last {
			t.Fatalf("%+v %v", b, err)
		}
	}
	s.e.expire(t, a.No)
	b, err := s.e.st.AcquireRun(s.e.ctx, a.No, "server-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	before := s.rdfDump(t)
	if _, err := s.e.st.DiffRawBucket(s.e.ctx, a, 3); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
		t.Fatalf("the server that woke up late: %v", err)
	}
	if after := s.rdfDump(t); after != before {
		t.Fatal("a bucket of the server that woke up late left something")
	}
	if place, _ := s.e.st.RawDiffPlace(s.e.ctx, a.No); place.Next != 3 {
		t.Fatalf("%+v", place)
	}
	late := s.rdfStart(rdfNow.Add(100*time.Hour), 99)
	rep, err := ingest.RunRawDiff(s.e.ctx, s.e.st, b, late)
	if err != nil || rep.Buckets < 4 {
		t.Fatalf("%+v %v", rep, err)
	}
	if got := s.rdfDump(t); got != want {
		t.Errorf("a run that was taken over made something else:\n%s\nwant\n%s", got, want)
	}
}

// [REQ-782]
func TestRdfABucketThatFailsLeavesNothingAndThePlaceStaysAtIt(t *testing.T) {
	want := rdfReference(t)
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	rdfSeedFates(t, s)
	l := s.rdfComputing(t, "server-a", rdfCover())
	s.e.exec(t, `CREATE FUNCTION rdf_boom() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.key = 'twice' THEN RAISE EXCEPTION 'boom'; END IF; RETURN NEW; END $$`)
	s.e.exec(t, fmt.Sprintf(`CREATE TRIGGER rdf_boom BEFORE INSERT ON "s_%d".calc_record FOR EACH ROW EXECUTE FUNCTION rdf_boom()`, s.ref.KGNo))
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfNow, 1)); err == nil {
		t.Fatal("the bucket of the key that the trigger refuses was done")
	}
	place, err := s.e.st.RawDiffPlace(s.e.ctx, l.No)
	if err != nil || !place.Begun || place.Done() {
		t.Fatalf("%+v %v", place, err)
	}
	rep, err := s.e.st.RawDiffReport(s.e.ctx, l.No)
	if err != nil {
		t.Fatal(err)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record`, s.ref.KGNo)); n != rep.Counts[0].Starting {
		t.Errorf("%d records, and the buckets that are done made %d", n, rep.Counts[0].Starting)
	}
	s.e.exec(t, `DROP TRIGGER rdf_boom ON "s_`+rdfItoa(s.ref.KGNo)+`".calc_record`)
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfNow, 1)); err != nil {
		t.Fatal(err)
	}
	if got := s.rdfDump(t); got != want {
		t.Errorf("a bucket that was done again:\n%s\nwant\n%s", got, want)
	}
}

// [REQ-780]
func TestRdfWhatTheComparisonMadeOfARunIsWhatTheNextRunStartsFrom(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	const ds = "ds_places"
	s.rdfPutRaw(t, rdfRaw{ds, "a", "v1", pgschema.FateUsed}, rdfRaw{ds, "b", "v1", pgschema.FateUsed}, rdfRaw{ds, "c", "v1", pgschema.FateUsed})
	s.rdfPutStage(t, rdfStaged{ds, 0, 0, "a", "v1", pgschema.ReceiveSame, false}, rdfStaged{ds, 0, 1, "b", "v2", pgschema.ReceiveChanged, false},
		rdfStaged{ds, 0, 2, "d", "v1", pgschema.ReceiveNew, false})
	cover := map[string]ingest.Coverage{ds: ingest.CoverageComplete}
	l := s.rdfComputing(t, "server-a", cover)
	s.rdfRun(t, l, s.rdfStart(rdfT0, 0))
	s.rdfSettle(t, l)
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM %s`, pgiTable(s.ref, pgschema.TableRawRecord))); n != 4 {
		t.Fatalf("%d raw records", n)
	}
	// the second run shows a and d only, and the runs after it show a only
	s.rdfPutStage(t, rdfStaged{ds, 0, 0, "a", "v1", pgschema.ReceiveSame, false}, rdfStaged{ds, 0, 1, "d", "v1", pgschema.ReceiveSame, false})
	l2 := s.rdfComputing(t, "server-a", cover)
	rep := s.rdfRun(t, l2, s.rdfStart(rdfT0.Add(time.Hour), 0))
	if c := rep.Counts[0]; c.Unseen != 2 || c.Deleted != 0 || c.Unchanged != 2 || c.Starting != 0 {
		t.Fatalf("%+v", c)
	}
	s.rdfSettle(t, l2)
	s.rdfPutStage(t, rdfStaged{ds, 0, 0, "a", "v1", pgschema.ReceiveSame, false})
	l3 := s.rdfComputing(t, "server-a", cover)
	rep = s.rdfRun(t, l3, s.rdfStart(rdfT0.Add(2*time.Hour), 0))
	if c := rep.Counts[0]; c.Unseen != 3 || c.Deleted != 0 {
		t.Fatalf("%+v", c)
	}
	s.rdfSettle(t, l3)
	s.rdfPutStage(t, rdfStaged{ds, 0, 0, "a", "v1", pgschema.ReceiveSame, false})
	l4 := s.rdfComputing(t, "server-a", cover)
	rep = s.rdfRun(t, l4, s.rdfStart(rdfT0.Add(30*time.Hour), 0))
	if c := rep.Counts[0]; c.Deleted != 2 || c.Unseen != 1 || c.Starting != 2 {
		t.Fatalf("a record that was not shown three runs in a row and for a day is closed: %+v", c)
	}
	if waits := s.rdfCalcWaits(t); len(waits) != 3 {
		t.Fatalf("%+v", waits)
	}
}

// [REQ-782]
func TestRdfARunThatFindsTheTablesOfTheComputationAnotherRunsHasNotBegunAndDoesTheComparisonAgainFromItsStart(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	rdfSeedFates(t, s)
	a := s.rdfComputing(t, "server-a", rdfCover())
	want, err := ingest.RunRawDiff(s.e.ctx, s.e.st, a, s.rdfStart(rdfNow, 0))
	if err != nil {
		t.Fatal(err)
	}
	dump := s.rdfDump(t)
	if place, err := s.e.st.RawDiffPlace(s.e.ctx, a.No); err != nil || !place.Done() {
		t.Fatalf("%+v %v", place, err)
	}
	// the run that is done is asked again and says what it made, without making it again
	if again, err := ingest.RunRawDiff(s.e.ctx, s.e.st, a, s.rdfStart(rdfNow.Add(time.Hour), 0)); err != nil || !reflect.DeepEqual(again, want) || s.rdfDump(t) != dump {
		t.Fatalf("a run that is done and whose tables are its own: %+v %v", again, err)
	}
	// the run waits for a confirmation and lets go of the gate of the KG; another source of the KG begins its computation: the tables
	// are emptied and are its own
	if err := s.e.st.AdvanceRun(s.e.ctx, a, ingest.RunComputing, ingest.RunAwaitingConfirmation); err != nil {
		t.Fatal(err)
	}
	if _, err := s.e.st.AttachSource(s.e.ctx, s.kg.ID, "src_other", []string{"ds_x"}); err != nil {
		t.Fatal(err)
	}
	b := s.e.open(t, s.kg, "src_other", "server-b")
	s.e.exec(t, `UPDATE sync_run SET counts = '{"receive":{"datasets":[]}}' WHERE run_no = $1`, b.No)
	if err := s.e.st.AdvanceRun(s.e.ctx, b, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Fatal(err)
	}
	if _, err := s.e.st.BeginRawDiff(s.e.ctx, b, ingest.RawDiffStart{Now: rdfNow, Datasets: []ingest.RawDiffDataset{{Dataset: "ds_x"}}}); err != nil {
		t.Fatal(err)
	}
	if place, err := s.e.st.RawDiffPlace(s.e.ctx, a.No); err != nil || place.Begun || place.Done() || place.Stage != ingest.RunAwaitingConfirmation {
		t.Fatalf("a run whose tables were emptied by another has not begun: %+v %v", place, err)
	}
	if place, err := s.e.st.RawDiffPlace(s.e.ctx, b.No); err != nil || !place.Begun {
		t.Fatalf("the run that emptied them has: %+v %v", place, err)
	}
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM "s_%d".calc_record`, s.ref.KGNo)); n != 0 {
		t.Fatalf("%d records of the run that waited are left", n)
	}
	// the run is confirmed, takes the gate again once the other has let go of it, and does the comparison from its start, at the time
	// it was given, and makes what it made
	if err := s.e.st.AdvanceRun(s.e.ctx, b, ingest.RunComputing, ingest.RunAwaitingConfirmation); err != nil {
		t.Fatal(err)
	}
	if err := s.e.st.AdvanceRun(s.e.ctx, a, ingest.RunAwaitingConfirmation, ingest.RunComputing); err != nil {
		t.Fatal(err)
	}
	got, err := ingest.RunRawDiff(s.e.ctx, s.e.st, a, s.rdfStart(rdfNow, 0))
	if err != nil || !reflect.DeepEqual(got, want) || s.rdfDump(t) != dump {
		t.Fatalf("the comparison again: %+v %v", got, err)
	}
	if place, _ := s.e.st.RawDiffPlace(s.e.ctx, b.No); place.Begun {
		t.Fatalf("the tables are the first run's again: %+v", place)
	}
}
