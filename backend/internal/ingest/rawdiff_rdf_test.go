package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// rdfDiffer is a RawDiffer that keeps the place in memory and notes what it was asked.
type rdfDiffer struct {
	stage   RunStage
	begun   bool
	buckets int
	next    int
	asked   []int
	began   int
	failAt  int
}

func (d *rdfDiffer) BeginRawDiff(_ context.Context, _ RunLease, _ RawDiffStart) (RawDiffPlan, error) {
	d.began++
	d.begun, d.next = true, 0
	return RawDiffPlan{}, nil
}

func (d *rdfDiffer) RawDiffPlace(_ context.Context, _ int64) (RawDiffPlace, error) {
	return RawDiffPlace{Stage: d.stage, Begun: d.begun, Finished: d.begun && d.next >= d.buckets, Next: d.next}, nil
}

func (d *rdfDiffer) DiffRawBucket(_ context.Context, _ RunLease, part int) (RawDiffBucket, error) {
	d.asked = append(d.asked, part)
	if part == d.failAt {
		return RawDiffBucket{}, kernel.Errorf(kernel.CodeInternal, "the bucket failed")
	}
	d.next = part + 1
	return RawDiffBucket{Part: part, Last: part == d.buckets-1}, nil
}

func (d *rdfDiffer) RawDiffReport(_ context.Context, _ int64) (RawDiffReport, error) {
	return RawDiffReport{Buckets: d.buckets}, nil
}

func rdfStartAt() RawDiffStart {
	return RawDiffStart{Now: time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC), Datasets: []RawDiffDataset{{Dataset: "ds_a", Retire: true}}}
}

// [REQ-780]
func TestRdfRunRawDiffBeginsOnceDoesTheBucketsInOrderAndGoesOnFromTheBucketTheRunIsAt(t *testing.T) {
	ctx := context.Background()
	d := &rdfDiffer{stage: RunComputing, buckets: 4, failAt: 2}
	if _, err := RunRawDiff(ctx, d, RunLease{No: 1, Attempt: 1}, rdfStartAt()); err == nil {
		t.Fatal("the failure of a bucket is not told")
	}
	if d.began != 1 || len(d.asked) != 3 || d.asked[0] != 0 || d.asked[2] != 2 {
		t.Fatalf("begun %d times, asked %v", d.began, d.asked)
	}
	d.failAt, d.asked = -1, nil
	rep, err := RunRawDiff(ctx, d, RunLease{No: 1, Attempt: 2}, rdfStartAt())
	if err != nil || rep.Buckets != 4 || d.began != 1 || len(d.asked) != 2 || d.asked[0] != 2 || d.asked[1] != 3 {
		t.Fatalf("a server that goes on does not begin again and starts at the bucket the run is at: %+v %v begun %d, asked %v", rep, err, d.began, d.asked)
	}
	d.stage = RunApplying
	if _, err := RunRawDiff(ctx, d, RunLease{No: 1, Attempt: 2}, rdfStartAt()); kernel.CodeOf(err) != CodeRunStage {
		t.Fatalf("a run that is not computing: %v", err)
	}
}

// [REQ-780]
func TestRdfRunRawDiffStopsAfterTheBucketInHandWhenTheContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &rdfDiffer{stage: RunComputing, buckets: 5, failAt: -1}
	cancelling := &rdfCanceller{rdfDiffer: d, cancel: cancel, after: 1}
	if _, err := RunRawDiff(ctx, cancelling, RunLease{No: 1, Attempt: 1}, rdfStartAt()); err == nil || len(d.asked) != 2 {
		t.Fatalf("asked %v, %v", d.asked, err)
	}
}

type rdfCanceller struct {
	*rdfDiffer
	cancel func()
	after  int
}

func (c *rdfCanceller) DiffRawBucket(ctx context.Context, l RunLease, part int) (RawDiffBucket, error) {
	b, err := c.rdfDiffer.DiffRawBucket(ctx, l, part)
	if part == c.after {
		c.cancel()
	}
	return b, err
}

// [REQ-780]
func TestRdfTheStartOfTheComparisonNeedsATimeAndEachDatasetOnceAndTheSizeOfABucketIsNotNegative(t *testing.T) {
	good := rdfStartAt()
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*RawDiffStart){
		"no time":                func(s *RawDiffStart) { s.Now = time.Time{} },
		"a dataset twice":        func(s *RawDiffStart) { s.Datasets = append(s.Datasets, s.Datasets[0]) },
		"a dataset with no name": func(s *RawDiffStart) { s.Datasets = append(s.Datasets, RawDiffDataset{}) },
		"a negative bucket":      func(s *RawDiffStart) { s.BucketRows = -1 },
	} {
		s := rdfStartAt()
		s.Datasets = append([]RawDiffDataset(nil), s.Datasets...)
		mutate(&s)
		if err := s.Validate(); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := RunRawDiff(context.Background(), &rdfDiffer{}, RunLease{}, RawDiffStart{}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a start with no time is refused before the port is asked: %v", err)
	}
}

// [REQ-780]
func TestRdfTheCoverageOfAReadIsOneOfThreeAndAValueThatSaysBothIsReadAsIncompleteAndAHeaderThatChangedLeavesTheDatasetOut(t *testing.T) {
	for _, c := range []struct {
		end  Completeness
		want Coverage
	}{
		{Completeness{Complete: true}, CoverageComplete},
		{Completeness{Complete: true, SeenOnly: true}, CoverageIncomplete},
		{Completeness{SeenOnly: true, Reason: "the source cannot tell"}, CoverageSeenOnly},
		{Completeness{Reason: "a limit"}, CoverageIncomplete},
		{Completeness{}, CoverageIncomplete},
	} {
		if got := c.end.Coverage(); got != c.want || !got.Valid() {
			t.Errorf("%+v: %s, want %s", c.end, got, c.want)
		}
	}
	if Coverage("some").Valid() || Coverage("").Valid() {
		t.Error("a word that is not one of the three is a coverage")
	}
	if err := (Completeness{Complete: true, SeenOnly: true}).Check(); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("an end that says whole and only what was seen: %v", err)
	}
	for _, ok := range []Completeness{{Complete: true}, {SeenOnly: true}, {}} {
		if err := ok.Check(); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	seen := DatasetIntake{Complete: false, SeenOnly: true}
	if seen.Coverage() != CoverageSeenOnly || (DatasetIntake{Complete: true}).Coverage() != CoverageComplete {
		t.Error("the coverage of a dataset is that of its last page")
	}
	if got := (DatasetIntake{Complete: true, NotRead: "the header lacks a column"}).Coverage(); got != CoverageIncomplete {
		t.Errorf("a dataset that gave no rows because its header changed: %s", got)
	}
}

// [REQ-783]
func TestRdfTheReceivingTellsForEachDatasetTheCoverageItsLastPageSaid(t *testing.T) {
	row := Row{Key: "RK1", Line: 2, Fields: map[string]string{"asset_id": "RK1", "rack_name": "a"}}
	for name, c := range map[string]struct {
		end  Completeness
		want Coverage
	}{
		"whole":     {Completeness{Complete: true}, CoverageComplete},
		"what seen": {Completeness{SeenOnly: true, Reason: "the source cannot tell"}, CoverageSeenOnly},
		"cut off":   {Completeness{Reason: "a limit"}, CoverageIncomplete},
	} {
		script := sxScript{pages: []Page{{Dataset: "ds_racks", No: 0, Rows: []Row{row}, Last: true, End: c.end}}}
		rep, err := (Intake{Reader: script, Sink: &sxMemory{}, Plan: sxScalePlan(t, sourceJSON, mappingJSON)}).Run(context.Background())
		if err != nil || len(rep.Datasets) != 1 {
			t.Fatalf("%s: %+v %v", name, rep, err)
		}
		if got := rep.Datasets[0]; got.Coverage() != c.want || got.SeenOnly != c.end.SeenOnly || got.Complete != c.end.Complete || got.Reason != c.end.Reason {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// [REQ-783]
func TestRdfTheReceivingRefusesALastPageThatSaysWholeAndOnlyWhatWasSeen(t *testing.T) {
	row := Row{Key: "RK1", Line: 2, Fields: map[string]string{"asset_id": "RK1", "rack_name": "a"}}
	script := sxScript{pages: []Page{{Dataset: "ds_racks", No: 0, Rows: []Row{row}, Last: true, End: Completeness{Complete: true, SeenOnly: true}}}}
	_, err := (Intake{Reader: script, Sink: &sxMemory{}, Plan: sxScalePlan(t, sourceJSON, mappingJSON)}).Run(context.Background())
	if kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a dataset that is whole and only what was seen: %v", err)
	}
}
