package ingest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The port of the difference (ADR-0127): the state machine that drives it, with a fake that writes down what it is called with (the order of
// the calls and the transitions are what is looked at here; what the difference means is the work of adapter.pgingest's tests).

type dfiFake struct {
	stage      RunStage
	begun      bool
	units      int
	total      int
	calls      []string
	failStep   int
	failPlace  bool
	failBegin  bool
	failReport bool
}

func (f *dfiFake) BeginDiff(_ context.Context, _ RunLease, _ DiffStart) error {
	f.calls = append(f.calls, "begin")
	if f.failBegin {
		return errors.New("the begin fails")
	}
	f.begun = true
	return nil
}

func (f *dfiFake) DiffPlace(_ context.Context, _ RunLease) (DiffPlace, error) {
	if f.failPlace && f.begun {
		return DiffPlace{}, errors.New("the place cannot be read")
	}
	return DiffPlace{Stage: f.stage, Begun: f.begun, Finished: f.begun && f.units == f.total, Phase: DiffPhaseRows, Unit: f.units}, nil
}

func (f *dfiFake) DiffStep(_ context.Context, _ RunLease) error {
	f.calls = append(f.calls, "step")
	if f.failStep > 0 && f.units+1 == f.failStep {
		return errors.New("a unit fails")
	}
	f.units++
	return nil
}

func (f *dfiFake) DiffReport(_ context.Context, _ RunLease) (DiffCounts, error) {
	f.calls = append(f.calls, "report")
	if f.failReport {
		return DiffCounts{}, errors.New("the report fails")
	}
	return DiffCounts{Rows: f.units}, nil
}

// [REQ-968]
func TestDfiTheRunOfTheDifferenceBeginsOnceDoesTheUnitsInOrderAndReportsAtTheEnd(t *testing.T) {
	f := &dfiFake{stage: RunComputing, total: 3}
	counts, err := RunDiff(context.Background(), f, RunLease{No: 1}, DiffStart{})
	if err != nil || counts.Rows != 3 {
		t.Fatalf("%+v %v", counts, err)
	}
	if want := []string{"begin", "step", "step", "step", "report"}; !slices.Equal(f.calls, want) {
		t.Errorf("the calls are %v, want %v", f.calls, want)
	}
	// a run that has begun goes on from where it is, without beginning again
	g := &dfiFake{stage: RunComputing, total: 3, begun: true, units: 2}
	if _, err := RunDiff(context.Background(), g, RunLease{No: 1}, DiffStart{}); err != nil || !slices.Equal(g.calls, []string{"step", "report"}) {
		t.Errorf("%v %v", g.calls, err)
	}
}

// [REQ-968]
func TestDfiTheRunOfTheDifferenceStopsAtWhatFailsAtARunThatIsNotComputingAndAtACancelledContext(t *testing.T) {
	ctx := context.Background()
	if _, err := RunDiff(ctx, &dfiFake{stage: RunComputing, total: 3, failStep: 2}, RunLease{No: 1}, DiffStart{}); err == nil {
		t.Error("a unit that fails")
	}
	if _, err := RunDiff(ctx, &dfiFake{stage: RunApplying, total: 3}, RunLease{No: 1}, DiffStart{}); kernel.CodeOf(err) != CodeRunStage {
		t.Errorf("a run that is applying: %v", err)
	}
	for name, f := range map[string]*dfiFake{
		"begin": {stage: RunComputing, total: 1, failBegin: true}, "place": {stage: RunComputing, total: 1, failPlace: true},
		"report": {stage: RunComputing, total: 1, failReport: true},
	} {
		if _, err := RunDiff(ctx, f, RunLease{No: 1}, DiffStart{}); err == nil {
			t.Errorf("a %s that fails", name)
		}
	}
	if _, err := RunDiff(ctx, &dfiFake{stage: RunComputing, total: 1, failPlace: true, begun: true}, RunLease{No: 1}, DiffStart{}); err == nil {
		t.Error("a place that cannot be read")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	f := &dfiFake{stage: RunComputing, total: 3}
	if _, err := RunDiff(cancelled, f, RunLease{No: 1}, DiffStart{}); !errors.Is(err, context.Canceled) || f.units != 0 {
		t.Errorf("a cancelled context: %v after %d units", err, f.units)
	}
	for _, bad := range []DiffStart{{Rows: -1}, {ChunkRows: -1}} {
		if _, err := RunDiff(ctx, &dfiFake{stage: RunComputing}, RunLease{No: 1}, bad); kernel.CodeOf(err) != kernel.CodeInvalidValue {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

// [REQ-966]
func TestDfiTheTallyIsInTheOrderOfTheApplyAndCountsAKindAndAChangeInEveryType(t *testing.T) {
	tally := []DiffTally{
		{Kind: DiffIdentifier, TypeNo: 1, Change: DiffAdded, Rows: 1}, {Kind: DiffObject, TypeNo: 9, Change: DiffRetired, Rows: 2},
		{Kind: DiffLink, TypeNo: 2, Change: DiffChanged, Rows: 3}, {Kind: DiffObject, TypeNo: 9, Change: DiffAdded, Rows: 4},
		{Kind: DiffKey, TypeNo: 9, Change: DiffAdded, Rows: 5}, {Kind: DiffObject, TypeNo: 3, Change: DiffRetired, Rows: 6},
		{Kind: "other", TypeNo: 0, Change: "other", Rows: 7}, {Kind: DiffObject, TypeNo: 9, Change: DiffChanged, Rows: 8},
	}
	SortTally(tally)
	var got []string
	for _, x := range tally {
		got = append(got, x.Kind+"/"+x.Change)
	}
	want := []string{"object/retired", "object/added", "object/changed", "object/retired", "key/added", "link/changed", "identifier/added", "other/other"}
	if !slices.Equal(got, want) || tally[0].TypeNo != 3 {
		t.Errorf("the order is %v", got)
	}
	c := DiffCounts{Tally: tally}
	if c.Of(DiffObject, DiffRetired) != 8 || c.Of(DiffObject, DiffAdded) != 4 || c.Of(DiffLink, DiffAdded) != 0 {
		t.Errorf("the counts of %+v", c)
	}
	if (DiffPlace{Begun: true}).Done() || !(DiffPlace{Begun: true, Finished: true}).Done() {
		t.Error("a place is done when it has begun and is finished")
	}
}
