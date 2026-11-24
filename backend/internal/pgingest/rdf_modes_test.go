package pgingest

import (
	"reflect"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// rdfSeedModes keeps and receives rows in three datasets that the comparison counts differently: one that was read as what was seen,
// one that was left out whole, and one that was read whole and never retires what is gone.
func rdfSeedModes(t *testing.T, s *rdfSetup) {
	t.Helper()
	const (
		places, servers, vms = "ds_places", "ds_servers", "ds_vms"
	)
	old := rdfT0
	s.rdfPutRaw(t,
		rdfRaw{places, "s1", "a", pgschema.FateUsed}, rdfRaw{places, "s2", "a", pgschema.FateUsed}, rdfRaw{places, "s3", "a", pgschema.FateUsed},
		rdfRaw{places, "s4", "a", pgschema.FateFiltered}, rdfRaw{places, "s5", "a", pgschema.FateUsed},
		rdfRaw{servers, "x1", "a", pgschema.FateUsed},
		rdfRaw{vms, "v1", "a", pgschema.FateUsed}, rdfRaw{vms, "v2", "a", pgschema.FateUsed}, rdfRaw{vms, "v3", "a", pgschema.FateUsed},
		rdfRaw{vms, "v4", "a", pgschema.FateFiltered},
	)
	s.rdfPutWaits(t,
		rdfWaitRow{places, "s2", pgschema.WaitUnseen, 2, old, false},
		rdfWaitRow{places, "s3", pgschema.WaitFiltered, 4, old, false},
		rdfWaitRow{places, "s4", pgschema.WaitFiltered, 2, old, false},
		rdfWaitRow{servers, "x1", pgschema.WaitUnseen, 1, old, false},
		rdfWaitRow{vms, "v2", pgschema.WaitUnseen, 2, old, false},
		rdfWaitRow{vms, "v3", pgschema.WaitFiltered, 2, old, false},
		rdfWaitRow{vms, "v4", pgschema.WaitFiltered, 5, old, false},
	)
	s.rdfPutStage(t,
		rdfStaged{places, 0, 0, "s1", "a", pgschema.ReceiveSame, false}, rdfStaged{places, 0, 1, "s4", "a", pgschema.ReceiveSame, true},
		rdfStaged{places, 0, 2, "s5", "b", pgschema.ReceiveChanged, false}, rdfStaged{places, 0, 3, "s6", "a", pgschema.ReceiveNew, false},
		rdfStaged{servers, 0, 0, "x1", "b", pgschema.ReceiveChanged, false}, rdfStaged{servers, 0, 1, "x2", "a", pgschema.ReceiveNew, false},
		rdfStaged{vms, 0, 0, "v4", "a", pgschema.ReceiveSame, true},
	)
}

// [REQ-781]
func TestRdfADatasetReadAsWhatWasSeenCountsNoAbsenceAndADatasetLeftOutWholeCountsNothingAndADatasetThatNeverRetiresWaitsForNothing(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	rdfSeedModes(t, s)
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_places": ingest.CoverageSeenOnly, "ds_servers": ingest.CoverageIncomplete, "ds_vms": ingest.CoverageComplete})
	rep := s.rdfRun(t, l, s.rdfStart(rdfNow, 0, "ds_vms"))
	recs := s.rdfRecords(t)
	got := map[string]int16{}
	for _, r := range recs {
		got[r.Dataset+"/"+r.Key] = r.Why
	}
	want := map[string]int16{
		"ds_places/s4": pgschema.CalcWhyReleased, "ds_places/s5": pgschema.CalcWhyChanged, "ds_places/s6": pgschema.CalcWhyNew,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the records: %v, want %v", got, want)
	}
	waits := s.rdfCalcWaits(t)
	type w struct {
		dataset, key string
		kind         int16
		runs         int
		expired      bool
	}
	var seen []w
	for _, x := range waits {
		seen = append(seen, w{x.Dataset, x.Key, x.Kind, x.Runs, x.Expired})
	}
	wantWaits := []w{
		{"ds_places", "s2", pgschema.WaitUnseen, 2, false},   // not shown, and a read of what is seen does not count it
		{"ds_places", "s3", pgschema.WaitFiltered, 4, false}, // not shown: left as it was
		{"ds_places", "s4", pgschema.WaitFiltered, 3, true},  // shown, filtered, and its time is up
		{"ds_servers", "x1", pgschema.WaitUnseen, 1, false},  // the dataset was left out whole
		{"ds_vms", "v2", pgschema.WaitUnseen, 2, false},      // the dataset never retires: what was there stays and nothing is added
		{"ds_vms", "v4", pgschema.WaitFiltered, 6, false},    // counted, and never up
	}
	if !reflect.DeepEqual(seen, wantWaits) {
		t.Errorf("the waits:\n got %v\nwant %v", seen, wantWaits)
	}
	by := map[string]ingest.RawDiffCounts{}
	for _, c := range rep.Counts {
		by[c.Dataset] = c
	}
	if c := by["ds_places"]; c.Added != 1 || c.Modified != 1 || c.Unchanged != 2 || c.Deleted != 0 || c.Unseen != 0 || c.Released != 1 {
		t.Errorf("a dataset read as what was seen: %+v", c)
	}
	if c := by["ds_servers"]; c != (ingest.RawDiffCounts{Dataset: "ds_servers"}) {
		t.Errorf("a dataset that is left out whole has no counts: %+v", c)
	}
	if rep.Datasets[0].Coverage != ingest.CoverageSeenOnly || rep.Datasets[1].Coverage != ingest.CoverageIncomplete || !rep.Datasets[0].Retire || rep.Datasets[2].Retire {
		t.Errorf("%+v", rep.Datasets)
	}
}

// [REQ-781]
func TestRdfADatasetTheReceivingDidNotGiveIsLeftOutWhole(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	s.rdfPutRaw(t, rdfRaw{"ds_places", "p1", "a", pgschema.FateUsed})
	s.rdfPutWaits(t, rdfWaitRow{"ds_places", "p1", pgschema.WaitUnseen, 2, rdfT0, false})
	l := s.rdfComputing(t, "server-a", map[string]ingest.Coverage{"ds_servers": ingest.CoverageComplete})
	st := s.rdfStart(rdfNow, 0)
	plan, err := s.e.st.BeginRawDiff(s.e.ctx, l, st)
	if err != nil || len(plan.Datasets) != 3 || plan.Datasets[0].Coverage != ingest.CoverageIncomplete {
		t.Fatalf("%+v %v", plan, err)
	}
	s.rdfRun(t, l, st)
	if ws := s.rdfCalcWaits(t); len(ws) != 1 || ws[0].Runs != 2 || len(s.rdfRecords(t)) != 0 {
		t.Errorf("a dataset that was not received is left as it is: %+v %+v", ws, s.rdfRecords(t))
	}
}

// [REQ-780]
func TestRdfTheComparisonDoesNotBeginForARunThatIsNotComputingOrForAnotherAttempt(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := ingest.RunRawDiff(s.e.ctx, s.e.st, l, s.rdfStart(rdfNow, 0)); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Fatalf("a run that is still receiving: %v", err)
	}
	s.e.exec(t, `UPDATE sync_run SET counts = '{"receive":{"datasets":[]}}' WHERE run_no = $1`, l.No)
	if err := s.e.st.AdvanceRun(s.e.ctx, l, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Fatal(err)
	}
	if _, err := s.e.st.DiffRawBucket(s.e.ctx, l, 0); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Fatalf("a bucket before the comparison began: %v", err)
	}
	late := l
	late.Attempt++
	if _, err := s.e.st.BeginRawDiff(s.e.ctx, late, s.rdfStart(rdfNow, 0)); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
		t.Fatalf("another attempt: %v", err)
	}
	bad := s.rdfStart(rdfNow, 0)
	bad.Datasets = append(bad.Datasets, ingest.RawDiffDataset{Dataset: "ds_nope"})
	if _, err := s.e.st.BeginRawDiff(s.e.ctx, l, bad); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("a dataset the source does not have: %v", err)
	}
	if _, err := s.e.st.BeginRawDiff(s.e.ctx, l, ingest.RawDiffStart{}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("no time: %v", err)
	}
	if _, err := s.e.st.BeginRawDiff(s.e.ctx, l, s.rdfStart(rdfNow, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.e.st.DiffRawBucket(s.e.ctx, l, 1); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Fatalf("a bucket that is not the one the run is at: %v", err)
	}
	if place, err := s.e.st.RawDiffPlace(s.e.ctx, l.No); err != nil || !place.Begun || place.Next != 0 || place.Done() {
		t.Fatalf("%+v %v", place, err)
	}
	if _, err := s.e.st.DiffRawBucket(s.e.ctx, l, 0); err != nil {
		t.Fatal(err)
	}
	if place, _ := s.e.st.RawDiffPlace(s.e.ctx, l.No); !place.Done() {
		t.Fatalf("%+v", place)
	}
}
