package ingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The port of the cleaning (ADR-0128), on a Settler written by the test. Every helper and type starts with stl, and the methods are the
// ones of the port, which no other test file of this folder declares.

// stlFake is a cleaning kept in memory: the place, the buckets the run asks for, and an error to give at a call.
type stlFake struct {
	place   SettlePlace
	jobs    int
	buckets []SettlePlace
	fail    string
	begun   int
}

func (f *stlFake) BeginSettle(context.Context, RunLease, SettleStart) error {
	if f.fail == "begin" {
		return kernel.Errorf(kernel.CodeConflict, "begin")
	}
	f.begun++
	f.place.Begun, f.place.Job, f.place.Part = true, SettleJobConfirm, 0
	return nil
}

func (f *stlFake) SettlePlace(context.Context, RunLease) (SettlePlace, error) {
	if f.fail == "place" || f.fail == "place-after" && f.begun > 0 {
		return SettlePlace{}, kernel.Errorf(kernel.CodeConflict, "place")
	}
	return f.place, nil
}

func (f *stlFake) SettleBucket(_ context.Context, _ RunLease, job SettleJob, part int) (SettleStep, error) {
	if f.fail == "bucket" {
		return SettleStep{}, kernel.Errorf(kernel.CodeConflict, "bucket")
	}
	f.buckets = append(f.buckets, SettlePlace{Job: job, Part: part})
	// every job has two buckets, so that the runner goes from a bucket to the next and from a job to the next
	next := SettlePlace{Stage: RunSettling, Begun: true, Job: job, Part: part + 1}
	if part == 1 {
		next.Job, next.Part = job+1, 0
	}
	f.place = next
	return SettleStep{Next: next, Counts: SettleCounts{Buckets: len(f.buckets)}, Over: job == SettleJobEnd && part == 1}, nil
}

// stlCount counts the confirmations and gives an error when it is told to.
type stlCount struct {
	n   int
	err error
}

func (c *stlCount) Confirm(context.Context) error {
	c.n++
	return c.err
}

// [REQ-986]
func TestStlTheRunnerOfTheCleaningBeginsItOnceAndDoesTheBucketsInOrderUntilTheRunIsOver(t *testing.T) {
	ctx, lease := context.Background(), RunLease{No: 1, Attempt: 1}
	f := &stlFake{place: SettlePlace{Stage: RunSettling}}
	c := &stlCount{}
	counts, err := RunSettle(ctx, f, lease, SettleStart{}, c)
	if err != nil {
		t.Fatal(err)
	}
	if f.begun != 1 || c.n != 2 || len(f.buckets) != 2*int(SettleJobEnd) || counts.Buckets != len(f.buckets) {
		t.Fatalf("begun %d, confirmed %d, %d buckets: %+v", f.begun, c.n, len(f.buckets), f.buckets)
	}
	for i, b := range f.buckets {
		if b.Job != SettleJob(i/2+1) || b.Part != i%2 {
			t.Errorf("the bucket %d is %+v", i, b)
		}
	}
	// a cleaning that was begun goes on from its place and does not confirm again past the confirmation
	g := &stlFake{place: SettlePlace{Stage: RunSettling, Begun: true, Job: SettleJobRaw, Part: 1}}
	c = &stlCount{}
	if _, err := RunSettle(ctx, g, lease, SettleStart{}, c); err != nil || g.begun != 0 || c.n != 0 || g.buckets[0] != (SettlePlace{Job: SettleJobRaw, Part: 1}) {
		t.Errorf("going on: %v, begun %d, confirmed %d, %+v", err, g.begun, c.n, g.buckets)
	}
}

// [REQ-986]
func TestStlTheRunnerOfTheCleaningRefusesWhatItCannotCleanAndStopsAtTheFirstError(t *testing.T) {
	ctx, lease := context.Background(), RunLease{No: 1, Attempt: 1}
	settling := SettlePlace{Stage: RunSettling}
	cases := []struct {
		name    string
		f       *stlFake
		start   SettleStart
		confirm Confirmer
		code    kernel.Code
	}{
		{"a bucket of a negative size", &stlFake{place: settling}, SettleStart{Rows: -1}, &stlCount{}, kernel.CodeInvalidValue},
		{"a feed twice", &stlFake{place: settling}, SettleStart{Feeds: []SettleFeed{{Feed: 1, ID: "a"}, {Feed: 1, ID: "b"}}}, &stlCount{}, kernel.CodeInvalidValue},
		{"an id twice", &stlFake{place: settling}, SettleStart{Feeds: []SettleFeed{{Feed: 1, ID: "a"}, {Feed: 2, ID: "a"}}}, &stlCount{}, kernel.CodeInvalidValue},
		{"a feed with no id", &stlFake{place: settling}, SettleStart{Feeds: []SettleFeed{{Feed: 1}}}, &stlCount{}, kernel.CodeInvalidValue},
		{"no confirmer", &stlFake{place: settling}, SettleStart{}, nil, kernel.CodeInvalidValue},
		{"a run that is not settling", &stlFake{place: SettlePlace{Stage: RunComputing}}, SettleStart{}, &stlCount{}, CodeRunStage},
		{"the place cannot be read", &stlFake{place: settling, fail: "place"}, SettleStart{}, &stlCount{}, kernel.CodeConflict},
		{"the place cannot be read after the beginning", &stlFake{place: settling, fail: "place-after"}, SettleStart{}, &stlCount{}, kernel.CodeConflict},
		{"the beginning fails", &stlFake{place: settling, fail: "begin"}, SettleStart{}, &stlCount{}, kernel.CodeConflict},
		{"a bucket fails", &stlFake{place: settling, fail: "bucket"}, SettleStart{}, &stlCount{}, kernel.CodeConflict},
		{"the confirmation fails", &stlFake{place: settling}, SettleStart{}, &stlCount{err: kernel.Errorf(kernel.CodeConflict, "seal")}, kernel.CodeConflict},
	}
	for _, c := range cases {
		if _, err := RunSettle(ctx, c.f, lease, c.start, c.confirm); kernel.CodeOf(err) != c.code {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := RunSettle(done, &stlFake{place: settling}, lease, SettleStart{}, &stlCount{}); err == nil {
		t.Error("a cleaning whose context is done goes on")
	}
}

// [REQ-985] [REQ-988]
func TestStlTheFeedsOfAPlanAreGivenToTheCleaningAndTheConfirmationSealsTheKGOnce(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ParseSource(strings.NewReader(`{"id":"src_stl","name":"stl","adapter":"file","datasets":[
 {"id":"ds_a","parser":"csv","natural_key":["id"],"columns":[{"name":"id"},{"name":"serial"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMapping(strings.NewReader(`{"feeds":[
 {"id":"f_a","dataset":"ds_a","object_type":"PhysicalServer","creates_objects":true,"basis":"observed","attributes":[{"attribute":"name","column":"id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"id"},{"property":"serial_number","column":"serial"}],"priority":1}]},
 {"id":"f_b","dataset":"ds_a","object_type":"PhysicalServer","basis":"observed","attributes":[],
  "identity":[{"components":[{"property":"manufacturer","column":"id"},{"property":"serial_number","column":"serial"}],"priority":1}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Compile(reg, src, m)
	if err != nil {
		t.Fatal(err)
	}
	feeds := SettleFeedsOf(plan)
	if len(feeds) != 2 || feeds[0] != (SettleFeed{Feed: 0, ID: "f_a", Creates: true}) || feeds[1] != (SettleFeed{Feed: 1, ID: "f_b"}) {
		t.Errorf("the feeds: %+v", feeds)
	}
	if err := (SettleStart{Feeds: feeds}).Validate(); err != nil {
		t.Error(err)
	}
	clock := kernel.NewManualClock(time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := kernel.NewSequentialIDs()
	store := graph.NewScratchStore()
	kgid, _ := kernel.NewID[graph.KGKind](ids, "kg")
	kg, err := store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "t", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	a, err := graph.NewApplier(store, reg, graph.NewTestKeyProvider("stl"), clock, ids, graph.Options{Semantic: 1, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := a.Acquire(context.Background(), kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := SealConfirmer{Applier: a, KG: kg.ID, Lease: lease}
	var counts []int
	for range 2 {
		if err := c.Confirm(context.Background()); err != nil {
			t.Fatal(err)
		}
		vs, err := store.Versions(context.Background(), kg.ID)
		if err != nil {
			t.Fatal(err)
		}
		counts = append(counts, len(vs))
	}
	if counts[0] != counts[1] {
		t.Errorf("the versions after one and two confirmations: %v", counts)
	}
	other, _ := kernel.NewID[graph.KGKind](ids, "kg")
	if err := (SealConfirmer{Applier: a, KG: other, Lease: lease}).Confirm(context.Background()); err == nil {
		t.Error("a KG that is not there is confirmed")
	}
}
