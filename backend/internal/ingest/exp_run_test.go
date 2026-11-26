package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The expansion's port (ADR-0114): the state machine that drives it, with a fake that writes down what it is called with (the order of
// the calls and the transitions are what is looked at here; what the rules mean is the work of adapter.pgingest's tests).

type expFake struct {
	stage    RunStage
	begun    bool
	units    int
	total    int
	calls    []string
	failStep int
}

func (f *expFake) BeginExpand(_ context.Context, _ RunLease, _ ExpandStart) error {
	f.calls = append(f.calls, "begin")
	f.begun = true
	return nil
}

func (f *expFake) ExpandPlace(_ context.Context, _ RunLease) (ExpandPlace, error) {
	return ExpandPlace{Stage: f.stage, Begun: f.begun, Finished: f.begun && f.units == f.total, Rule: "r", Unit: f.units}, nil
}

func (f *expFake) ExpandStep(_ context.Context, _ RunLease) error {
	f.calls = append(f.calls, "step")
	if f.failStep > 0 && f.units+1 == f.failStep {
		return errors.New("a unit fails")
	}
	f.units++
	return nil
}

func (f *expFake) ExpandReport(_ context.Context, _ RunLease) (ExpandCounts, error) {
	f.calls = append(f.calls, "report")
	return ExpandCounts{Retiring: f.units}, nil
}

// expOneFeed is the table of the feeds the runs of the fake are given: every expansion is given one (ADR-0121).
var expOneFeed = []ExpandFeed{{SrcNo: 1, Feed: 0, Creates: true}}

// [REQ-890]
func TestExpTheRunOfTheExpansionBeginsOnceDoesTheUnitsInOrderAndReportsAtTheEnd(t *testing.T) {
	f := &expFake{stage: RunComputing, total: 3}
	counts, err := RunExpand(context.Background(), f, RunLease{No: 1}, ExpandStart{Feeds: expOneFeed})
	if err != nil || counts.Retiring != 3 {
		t.Fatalf("%+v %v", counts, err)
	}
	want := []string{"begin", "step", "step", "step", "report"}
	if len(f.calls) != len(want) {
		t.Fatalf("%v", f.calls)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Errorf("call %d is %s, not %s", i, f.calls[i], want[i])
		}
	}
	// a run that has begun goes on from where it is, without beginning again
	g := &expFake{stage: RunComputing, total: 3, begun: true, units: 2}
	if _, err := RunExpand(context.Background(), g, RunLease{No: 1}, ExpandStart{Feeds: expOneFeed}); err != nil || g.calls[0] != "step" || len(g.calls) != 2 {
		t.Errorf("%v %v", g.calls, err)
	}
}

// [REQ-890]
func TestExpTheRunOfTheExpansionStopsAtAUnitThatFailsAtARunThatIsNotComputingAndAtACancelledContext(t *testing.T) {
	f := &expFake{stage: RunComputing, total: 3, failStep: 2}
	if _, err := RunExpand(context.Background(), f, RunLease{No: 1}, ExpandStart{Feeds: expOneFeed}); err == nil || f.units != 1 {
		t.Errorf("a unit that fails: %v after %d units", err, f.units)
	}
	if _, err := RunExpand(context.Background(), &expFake{stage: RunApplying, total: 3}, RunLease{No: 1}, ExpandStart{Feeds: expOneFeed}); kernel.CodeOf(err) != CodeRunStage {
		t.Errorf("a run that is applying: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := &expFake{stage: RunComputing, total: 3, begun: true}
	if _, err := RunExpand(ctx, g, RunLease{No: 1}, ExpandStart{Feeds: expOneFeed}); !errors.Is(err, context.Canceled) || g.units != 0 {
		t.Errorf("a context that is cancelled: %v after %d units", err, g.units)
	}
	if _, err := RunExpand(context.Background(), &expFake{stage: RunComputing}, RunLease{No: 1}, ExpandStart{Rows: -1, Feeds: expOneFeed}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a size below nothing: %v", err)
	}
	if _, err := RunExpand(context.Background(), &expFake{stage: RunComputing}, RunLease{No: 1}, ExpandStart{Cards: []ExpandCard{{LinkTypeNo: 1}, {LinkTypeNo: 1}}, Feeds: expOneFeed}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a link type given twice: %v", err)
	}
}

// [REQ-891] [REQ-892]
func TestExpTheLinkTypesThatHoldAnEndToOneAreTheOnesTheBindingsOfTheModelSayAndTheFeedsSayWhichMayRetire(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	cards := ExpandCards(reg)
	mounted, ok := reg.LinkTypeByName("MOUNTED_IN")
	if !ok {
		t.Fatal("no link type MOUNTED_IN")
	}
	found := false
	for _, c := range cards {
		if !c.FromOne && !c.ToOne {
			t.Errorf("the link type %d holds no end to one and is given", c.LinkTypeNo)
		}
		if c.LinkTypeNo == mounted.LinkTypeNo {
			found, _ = true, c
			if !c.FromOne {
				t.Errorf("a server is mounted in one rack, a link type that holds the start to one: %+v", c)
			}
		}
	}
	if !found {
		t.Errorf("MOUNTED_IN is not given among %d link types", len(cards))
	}
	r := loaded(t)
	if got := r.plan.ExpandCards(); len(got) != len(cards) {
		t.Errorf("the plan gives %d link types and the model %d", len(got), len(cards))
	}
	for _, f := range r.plan.IdentifyFeeds(1) {
		if !f.Retires || !f.Creates {
			t.Errorf("the feed %d of a mapping whose feeds all retire what they make: %+v", f.Feed, f)
		}
	}
	plain := newRig(t, sourceJSON, strings.ReplaceAll(mappingJSON, `"retires_objects":true`, `"retires_objects":false`))
	for _, f := range plain.plan.IdentifyFeeds(1) {
		if f.Retires {
			t.Errorf("the feed %d retires, and its mapping does not say so: %+v", f.Feed, f)
		}
	}
}
