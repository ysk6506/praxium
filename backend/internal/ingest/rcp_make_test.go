package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// What the computer of the objects makes of a page with the lines of two records of one object, and the run that drives the step with a
// fake of the port (ADR-0115). Every helper and type starts with rcp.

const rcpSeenAt = "2026-01-01T00:00:00Z"

func rcpComputer(t *testing.T) (*ObjectComputer, *Plan) {
	t.Helper()
	plan := sxScalePlan(t, sourceJSON, mappingJSON)
	start := RecomputeStart{Seen: rcpSeenAt, Schemes: plan.SchemeNumbers()}
	return NewObjectComputer(map[int64]*Plan{7: plan}, graph.NewContentHasher(sxRegistry(t), graph.NewTestKeyProvider("rcp"), graph.KGID{}), start), plan
}

func rcpServerLine(feed int, key string, rid int64, cells ...string) RecomputeLine {
	return RecomputeLine{Rid: rid, SrcNo: 7, DatasetNo: 2, Dataset: "ds_servers", Key: key, Fields: cells, OpenedSeq: 1, Feed: feed}
}

// [REQ-930]
func TestRcpTwoRecordsOfAnObjectMakeOneRowAndTheOneThatLosesIsAProblemWhateverTheOrderOfTheLines(t *testing.T) {
	c, plan := rcpComputer(t)
	var feed, typeNo int
	for _, f := range plan.IdentifyFeeds(7) {
		if f.NameCol == 0 {
			feed, typeNo = f.Feed, f.TypeNo
		}
	}
	a := rcpServerLine(feed, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on")
	b := rcpServerLine(feed, "SV2", 2, "SV2", "sn1", "Zorvane", "RK1", "off")
	kept := rcpServerLine(feed, "SV3", 0, "SV3", "sn1", "Zorvane", "RK1", "on")
	make1 := func(lines ...RecomputeLine) RecomputedPage {
		page := RecomputePage{Unit: 3, Through: 9, Last: true, Objects: []RecomputeObject{
			{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "old", Lines: lines},
			{ObjNo: 2, Ref: "obj-2", TypeNo: typeNo, Name: "none"},
			{ObjNo: 3, Ref: "obj-3", TypeNo: typeNo, Name: "odd", Lines: []RecomputeLine{{SrcNo: 99, Dataset: "ds_servers", Key: "Q", Fields: []string{"x"}}}},
			{ObjNo: 4, Ref: "obj-4", TypeNo: typeNo, Name: "kept", Lines: []RecomputeLine{rcpServerLine(feed, "SV4", 4, "", "sn4", "Zorvane", "RK1", "on")}},
			{ObjNo: 5, Ref: "obj-5", TypeNo: typeNo, IsNew: true, OriginRid: 99, OriginFeed: feed, Lines: []RecomputeLine{rcpServerLine(feed, "SV5", 5, "SV5", "sn5", "Zorvane", "RK1", "on")}},
		}, TooMany: []int64{8}}
		out, err := c.Make(page)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	one := make1(a, b, kept)
	two := make1(kept, b, a)
	if len(one.Objects) != 2 || len(one.Empty) != 1 || len(one.Unreadable) != 2 || one.Lines != 6 || one.TooMany != 1 || one.Unit != 3 || !one.Last {
		t.Fatalf("%+v", one)
	}
	if len(one.Records) != 1 || one.Records[0].Key != "SV3" {
		t.Errorf("the record that is not in the run is made one: %+v", one.Records)
	}
	if one.Objects[1].Name != "kept" {
		t.Errorf("an object no value names keeps the name it has: %q", one.Objects[1].Name)
	}
	if one.Objects[0].Name != "SV1" || len(one.Problems) == 0 {
		t.Errorf("%q %+v", one.Objects[0].Name, one.Problems)
	}
	for _, p := range one.Problems {
		if p.Kind != ProblemSourceConflict || p.Key == "SV1" {
			t.Errorf("a problem that is not of a record that lost: %+v", p)
		}
	}
	if one.Objects[0].ContentHash != two.Objects[0].ContentHash || one.Objects[0].RowHash != two.Objects[0].RowHash || len(one.Problems) != len(two.Problems) {
		t.Errorf("the order of the lines changes what is made:\n%+v\n%+v", one.Objects[0], two.Objects[0])
	}
}

type rcpFake struct {
	pages  int
	begun  bool
	calls  []string
	failAt string
}

func (f *rcpFake) fail(what string) error {
	f.calls = append(f.calls, what)
	if f.failAt == what {
		return errors.New(what + " fails")
	}
	return nil
}

func (f *rcpFake) BeginRecompute(context.Context, RunLease, RecomputeStart) error {
	f.begun = true
	return f.fail("begin")
}

func (f *rcpFake) RecomputePlace(context.Context, RunLease) (RecomputePlace, error) {
	if err := f.fail("place"); err != nil {
		return RecomputePlace{}, err
	}
	stage := RunComputing
	if f.failAt == "stage" {
		stage = RunStage("done")
	}
	return RecomputePlace{Stage: stage, Begun: f.begun, Finished: f.pages == 2}, nil
}

func (f *rcpFake) RecomputePage(context.Context, RunLease) (RecomputePage, error) {
	return RecomputePage{Unit: f.pages}, f.fail("page")
}

func (f *rcpFake) PutRecomputed(context.Context, RunLease, RecomputedPage) error {
	f.pages++
	return f.fail("put")
}

func (f *rcpFake) RecomputeReport(context.Context, RunLease) (RecomputeCounts, error) {
	return RecomputeCounts{Lines: f.pages}, f.fail("report")
}

type rcpMakerFake struct{ fail bool }

func (m rcpMakerFake) Make(RecomputePage) (RecomputedPage, error) {
	if m.fail {
		return RecomputedPage{}, errors.New("make fails")
	}
	return RecomputedPage{}, nil
}

// [REQ-937]
func TestRcpTheRunOfTheStepBeginsOnceDoesThePagesAndStopsAtTheFirstThingThatFails(t *testing.T) {
	ctx, lease := context.Background(), RunLease{No: 1}
	f := &rcpFake{}
	counts, err := RunRecompute(ctx, f, lease, RecomputeStart{Seen: rcpSeenAt}, rcpMakerFake{})
	if err != nil || counts.Lines != 2 || f.calls[1] != "begin" {
		t.Fatalf("%+v %v %v", counts, err, f.calls)
	}
	if _, err := RunRecompute(ctx, &rcpFake{pages: 2, begun: true}, lease, RecomputeStart{Seen: rcpSeenAt}, rcpMakerFake{}); err != nil {
		t.Errorf("a step that is done is reported: %v", err)
	}
	if _, err := RunRecompute(ctx, &rcpFake{}, lease, RecomputeStart{Seen: "x"}, rcpMakerFake{}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a start that is not valid: %v", err)
	}
	if _, err := RunRecompute(ctx, &rcpFake{failAt: "stage"}, lease, RecomputeStart{Seen: rcpSeenAt}, rcpMakerFake{}); kernel.CodeOf(err) != CodeRunStage {
		t.Errorf("a run that is not computing: %v", err)
	}
	for _, at := range []string{"place", "begin", "page", "put", "report"} {
		if _, err := RunRecompute(ctx, &rcpFake{failAt: at}, lease, RecomputeStart{Seen: rcpSeenAt}, rcpMakerFake{}); err == nil {
			t.Errorf("%s fails and the step does not", at)
		}
	}
	if _, err := RunRecompute(ctx, &rcpFake{}, lease, RecomputeStart{Seen: rcpSeenAt}, rcpMakerFake{fail: true}); err == nil {
		t.Error("the maker fails and the step does not")
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := RunRecompute(stopped, &rcpFake{begun: true}, lease, RecomputeStart{Seen: rcpSeenAt}, rcpMakerFake{}); !errors.Is(err, context.Canceled) {
		t.Errorf("a stopped context: %v", err)
	}
	if !(RecomputePlace{Begun: true, Finished: true}).Done() || (RecomputePlace{Finished: true}).Done() {
		t.Error("Done")
	}
}

// [REQ-931]
func TestRcpAnIdentifierThatARecordClaimsAndTheObjectWonIsTheIdentifierOfTheRowAndOneItDidNotWinIsNot(t *testing.T) {
	c, plan := rcpComputer(t)
	var feed, typeNo int
	for _, f := range plan.IdentifyFeeds(7) {
		if f.Creates && f.NameCol == 1 && f.TypeNo != 0 {
			feed, typeNo = f.Feed, f.TypeNo
		}
	}
	vm := func(key, uuid string, won []RecomputeClaim) RecomputeLine {
		return RecomputeLine{Rid: 1, SrcNo: 7, DatasetNo: 3, Dataset: "ds_vms", Key: key, Fields: []string{uuid, "vm-" + key, "2"}, OpenedSeq: 1, Feed: feed, Won: won}
	}
	// the hash of the claim the line makes, as the line itself gives it
	probe := vm("a", "u-a", nil)
	r, ok := c.readLine(&probe)
	if !ok {
		t.Fatal("the line cannot be read")
	}
	into := map[string][]identLine{}
	c.claimsOf(&r, into)
	formed, _ := r.pr.formAll(r.pf, r.row)
	var sp StagePage
	if err := r.pr.claims(&sp, 0, r.pf, formed, rowView{row: r.row}); err != nil || len(sp.Claims) == 0 {
		t.Fatalf("the line makes no claim: %v %+v", err, sp.Claims)
	}
	claim := RecomputeClaim{Scheme: c.schemes[sp.Claims[0].Scheme], Hash: sp.Claims[0].ValueHash}
	for name, won := range map[string][]RecomputeClaim{"won": {claim}, "not won": {{Scheme: claim.Scheme + 100}}} {
		line := vm("a", "u-a", won)
		page := RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "vm", Lines: []RecomputeLine{line}}}}
		out, err := c.Make(page)
		if err != nil || len(out.Objects) != 1 {
			t.Fatalf("%s: %+v %v", name, out, err)
		}
		if got := len(out.Objects[0].Idents); (name == "won") != (got == 1) {
			t.Errorf("%s: %d identifiers: %+v", name, got, out.Objects[0].Idents)
		}
	}
}

// [REQ-932]
func TestRcpAnObjectTheRunMakesIsGivenTheKeyOfTheRecordThatMadeItAndOneItsRecordsDoNotMakeIsLeftAsItIs(t *testing.T) {
	c, plan := rcpComputer(t)
	var feed, typeNo int
	for _, f := range plan.IdentifyFeeds(7) {
		if f.NameCol == 0 {
			feed, typeNo = f.Feed, f.TypeNo
		}
	}
	line := rcpServerLine(feed, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on")
	r, ok := c.readLine(&line)
	if !ok {
		t.Fatal("the line cannot be read")
	}
	formed, _ := r.pr.formAll(r.pf, r.row)
	if len(formed) == 0 {
		t.Fatal("the line forms no key")
	}
	id, err := graph.NewObjectID(r.pf.fp.typ.ID, keyParts(r.pf.keys[formed[0].ord].kp, formed[0].own))
	if err != nil {
		t.Fatal(err)
	}
	made := func(ref string, originRid int64) RecomputedPage {
		page := RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: ref, TypeNo: typeNo, IsNew: true, OriginRid: originRid, OriginFeed: feed, Lines: []RecomputeLine{line}}}}
		out, err := c.Make(page)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := made(id.String(), 1); len(out.Objects) != 1 || out.Objects[0].FirstKey == "" || !out.Objects[0].IsNew || out.Objects[0].Name != "SV1" {
		t.Errorf("%+v", out)
	}
	if out := made(id.String(), 2); len(out.Unreadable) != 1 {
		t.Errorf("an object that no record of the run made: %+v", out)
	}
	if out := made("not-this-one", 1); len(out.Unreadable) != 1 {
		t.Errorf("an object whose id is not the key's: %+v", out)
	}
}

// [REQ-933]
func TestRcpARecordThatCannotBeReadAsItsFeedReadsItLeavesTheObjectAsItIs(t *testing.T) {
	c, plan := rcpComputer(t)
	var feed, typeNo int
	for _, f := range plan.IdentifyFeeds(7) {
		if f.NameCol == 0 {
			feed, typeNo = f.Feed, f.TypeNo
		}
	}
	for name, line := range map[string]RecomputeLine{
		"too few cells":    rcpServerLine(feed, "SV1", 1, "SV1", "sn1"),
		"a feed it hasn't": rcpServerLine(feed+50, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on"),
	} {
		out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "x", Lines: []RecomputeLine{line}}}})
		if err != nil || len(out.Unreadable) != 1 || len(out.Objects) != 0 {
			t.Errorf("%s: %+v %v", name, out, err)
		}
	}
}

// [REQ-934]
func TestRcpACellAFeedThatMakesNoObjectCannotReadIsAProblemOfARecordReceivedNowAndOfNoOtherRecord(t *testing.T) {
	feedText := ` {"id":"f_state","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":false,"basis":"observed",
  "attributes":[{"attribute":"power_state","column":"power_state"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
`
	mapping := strings.Replace(mappingJSON, ` {"id":"f_vm"`, feedText+` {"id":"f_vm"`, 1)
	plan := sxScalePlan(t, sourceJSON, mapping)
	start := RecomputeStart{Seen: rcpSeenAt, Schemes: plan.SchemeNumbers()}
	c := NewObjectComputer(map[int64]*Plan{7: plan}, graph.NewContentHasher(sxRegistry(t), graph.NewTestKeyProvider("rcp"), graph.KGID{}), start)
	var feed, typeNo int
	for _, f := range plan.IdentifyFeeds(7) {
		if !f.Creates {
			feed, typeNo = f.Feed, f.TypeNo
		}
	}
	for why, want := range map[int16]int{0: 1, 3: 1, 4: 1, 1: 0, 2: 0, -1: 0} {
		line := rcpServerLine(feed, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "maybe")
		line.Why = why
		out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "x", Lines: []RecomputeLine{line}}}})
		if err != nil || len(out.Problems) != want {
			t.Errorf("a record that is there for the reason %d: %+v %v", why, out.Problems, err)
		}
	}
}

// [REQ-932]
func TestRcpAnObjectNoValueNamesIsNamedByTheFirstValueOfTheKeyItWasFirstSeenWith(t *testing.T) {
	c, plan := rcpComputer(t)
	var feed, typeNo int
	for _, f := range plan.IdentifyFeeds(7) {
		if f.NameCol == 0 {
			feed, typeNo = f.Feed, f.TypeNo
		}
	}
	stored, err := graph.EncodeKey([]graph.KeyPart{{Kind: graph.PartContext, Ref: "c"}, {Kind: graph.PartProperty, Ref: "p", Value: "first-value"}})
	if err != nil {
		t.Fatal(err)
	}
	line := rcpServerLine(feed, "SV1", 1, "", "sn1", "Zorvane", "RK1", "on")
	out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "the registry", FirstKey: []byte(stored.(string)), Lines: []RecomputeLine{line}}}})
	if err != nil || len(out.Objects) != 1 || out.Objects[0].Name != "first-value" {
		t.Errorf("%+v %v", out, err)
	}
}
