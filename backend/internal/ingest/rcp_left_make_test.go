package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// What the computer of the objects does with an object it does not make, in what order it reads the lines of an object, and how much
// memory the biggest object a page holds takes (ADR-0119, and the places ADR-0115 left to a test). Every helper starts with rcpLeft.

// rcpLeftComputer is the computer of the objects of a plan made of the mapping given (the source of the other tests of the step).
func rcpLeftComputer(t *testing.T, mapping string) (*ObjectComputer, *Plan) {
	t.Helper()
	plan := sxScalePlan(t, sourceJSON, mapping)
	start := RecomputeStart{Seen: rcpSeenAt, Schemes: plan.SchemeNumbers()}
	return NewObjectComputer(map[int64]*Plan{7: plan}, graph.NewContentHasher(sxRegistry(t), graph.NewTestKeyProvider("rcp"), graph.KGID{}), start), plan
}

// rcpLeftServerFeed is the feed that makes the servers and its type, found by what the feed gives (a name from the first cell).
func rcpLeftServerFeed(plan *Plan) (feed, typeNo int) {
	for _, f := range plan.IdentifyFeeds(7) {
		if f.Creates && f.NameCol == 0 {
			return f.Feed, f.TypeNo
		}
	}
	return -1, 0
}

// rcpLeftNo gives the entry of Left of an object, and the zero value when the page did not leave it.
func rcpLeftNo(out RecomputedPage, no int64) RecomputedLeft {
	for _, l := range out.Left {
		if l.ObjNo == no {
			return l
		}
	}
	return RecomputedLeft{}
}

// [REQ-971]
func TestRcpEveryObjectThePageDoesNotMakeIsLeftWithAReasonAndTheNumbersAgreeWithTheLists(t *testing.T) {
	c, plan := rcpLeftComputer(t, mappingJSON)
	feed, typeNo := rcpLeftServerFeed(plan)
	obj := func(no int64, name string, lines ...RecomputeLine) RecomputeObject {
		return RecomputeObject{ObjNo: no, Ref: fmt.Sprintf("obj-%d", no), TypeNo: typeNo, Name: name, Lines: lines}
	}
	page := RecomputePage{Unit: 1, Through: 90, TooMany: []int64{8, 9}, Objects: []RecomputeObject{
		{ObjNo: 1, TypeNo: typeNo, Lines: []RecomputeLine{rcpServerLine(feed, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on")}}, // no id: not in the registry and not made by the run
		obj(2, "empty"),
		obj(3, "no plan", RecomputeLine{Rid: 3, SrcNo: 99, DatasetNo: 2, Dataset: "ds_servers", Key: "SV3", Fields: []string{"x"}, Feed: feed}),
		obj(4, "few cells", rcpServerLine(feed, "SV4", 4, "SV4", "sn4")),
		obj(5, "no feed", rcpServerLine(feed+50, "SV5", 5, "SV5", "sn5", "Zorvane", "RK1", "on")),
		obj(6, "", rcpServerLine(feed, "SV6", 6, "", "sn6", "Zorvane", "RK1", "on")),
		obj(7, "good", rcpServerLine(feed, "SV7", 7, "SV7", "sn7", "Zorvane", "RK1", "on")),
	}}
	out, err := c.Make(page)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]string{1: LeftUnreadable, 2: LeftEmpty, 3: LeftUnreadable, 4: LeftUnreadable, 5: LeftUnreadable, 6: LeftNameless, 8: LeftTooMany, 9: LeftTooMany}
	if len(out.Left) != len(want) || len(out.Objects) != 1 || out.Objects[0].ObjNo != 7 {
		t.Fatalf("%d objects are made and %d are left: %+v", len(out.Objects), len(out.Left), out.Left)
	}
	for no, why := range want {
		if got := rcpLeftNo(out, no).Why; got != why {
			t.Errorf("the object %d is left for %q, want %q", no, got, why)
		}
	}
	// the three lists the report counts by are what Left says
	if !slices.Equal(out.Empty, []int64{2}) || !slices.Equal(out.Unreadable, []int64{1, 3, 4, 5, 6}) || out.TooMany != 2 {
		t.Errorf("empty %v unreadable %v too many %d", out.Empty, out.Unreadable, out.TooMany)
	}
	// the objects the page did not read come last, in the order of the page, and the store gives their reason (it knows the limit)
	if tail := out.Left[len(out.Left)-2:]; tail[0].ObjNo != 8 || tail[1].ObjNo != 9 || tail[0].Reason != "" || tail[0].Lines != 0 {
		t.Errorf("the objects with too many lines: %+v", tail)
	}
	for _, l := range out.Left[:len(out.Left)-2] {
		if l.Reason == "" {
			t.Errorf("the object %d is left without a reason", l.ObjNo)
		}
	}
	if got := rcpLeftNo(out, 4); got.Lines != 1 {
		t.Errorf("the lines of an object that was read are counted: %+v", got)
	}
}

// [REQ-971]
func TestRcpTheReasonALineCannotBeReadNamesTheRecordAndWhatIsWrongWithItAndNeverItsCells(t *testing.T) {
	c, plan := rcpLeftComputer(t, mappingJSON)
	feed, typeNo := rcpLeftServerFeed(plan)
	for name, tc := range map[string]struct {
		line RecomputeLine
		want string
	}{
		"a source with no plan":          {RecomputeLine{Rid: 1, SrcNo: 99, DatasetNo: 2, Dataset: "ds_servers", Key: "SV1", Fields: []string{"secret-cell"}, Feed: feed}, "ds_servers/SV1 is of a source or dataset that has no plan"},
		"cells that are not the shape":   {rcpServerLine(feed, "SV1", 1, "secret-cell", "sn1"), "ds_servers/SV1 keeps cells that are not the columns the dataset declares now"},
		"no cells (another declaration)": {rcpServerLine(feed, "SV1", 1), "ds_servers/SV1 keeps cells that are not the columns the dataset declares now"},
		"a feed the source has not":      {rcpServerLine(feed+50, "SV1", 1, "secret-cell", "sn1", "Zorvane", "RK1", "on"), "ds_servers/SV1 is read by a feed the source does not have"},
	} {
		out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "x", Lines: []RecomputeLine{tc.line}}}})
		if err != nil || len(out.Left) != 1 || out.Left[0].Why != LeftUnreadable {
			t.Errorf("%s: %+v %v", name, out, err)
			continue
		}
		if got := out.Left[0].Reason; got != "the record "+tc.want {
			t.Errorf("%s: the reason is %q, want %q", name, got, "the record "+tc.want)
		}
		if strings.Contains(out.Left[0].Reason, "secret-cell") {
			t.Errorf("%s: the reason says a cell: %s", name, out.Left[0].Reason)
		}
	}
}

// [REQ-972]
func TestRcpALimitOfAnObjectThatIsMoreThanAPageIsRefusedAndTheDefaultLimitIsThePage(t *testing.T) {
	if DefaultRecomputeObjectMax != DefaultRecomputeLines {
		t.Errorf("the limit of an object is %d lines and a page %d", DefaultRecomputeObjectMax, DefaultRecomputeLines)
	}
	for name, tc := range map[string]struct {
		lines, max int
		ok         bool
	}{
		"the same":                   {10, 10, true},
		"less":                       {10, 3, true},
		"the default is not given":   {0, 0, true},
		"more than a page":           {10, 11, false},
		"more than the default page": {0, DefaultRecomputeLines + 1, false},
		"the default page":           {0, DefaultRecomputeLines, true},
	} {
		err := RecomputeStart{Seen: rcpSeenAt, Lines: tc.lines, ObjectMax: tc.max}.Validate()
		if (err == nil) != tc.ok || (err != nil && kernel.CodeOf(err) != kernel.CodeInvalidValue) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// [REQ-973]
func TestRcpAServerThatIsGivenAnotherTimeOrOtherSchemesThanTheRunBeganWithIsRefusedBeforeItReadsAPage(t *testing.T) {
	ctx, lease := context.Background(), RunLease{No: 1}
	for name, start := range map[string]RecomputeStart{
		"another time":    {Seen: "2026-01-02T00:00:00Z"},
		"other schemes":   {Seen: rcpSeenAt, Schemes: map[string]int{"is.a": 1}},
		"a scheme number": {Seen: rcpSeenAt, Schemes: map[string]int{"is.a": 2}},
	} {
		f := &rcpFake{begun: true}
		if _, err := RunRecompute(ctx, f, lease, start, rcpMakerFake{}); kernel.CodeOf(err) != kernel.CodeConflict {
			t.Errorf("%s: %v", name, err)
		}
		if slices.Contains(f.calls, "page") || slices.Contains(f.calls, "put") || slices.Contains(f.calls, "begin") {
			t.Errorf("%s: a server that was refused went on: %v", name, f.calls)
		}
	}
	if _, err := RunRecompute(ctx, &rcpFake{begun: true, pages: 2}, lease, RecomputeStart{Seen: rcpSeenAt}, rcpMakerFake{}); err != nil {
		t.Errorf("the start the run began with: %v", err)
	}
	// the numbers of the schemes are compared as they are, whatever order they were written in
	a, b := RecomputeStart{Schemes: map[string]int{"x": 1, "y": 2, "z": 3}}, RecomputeStart{Schemes: map[string]int{"z": 3, "y": 2, "x": 1}}
	if a.SchemesText() != b.SchemesText() || a.SchemesText() != "x=1;y=2;z=3;" || a.SchemesText() == (RecomputeStart{Schemes: map[string]int{"x": 1, "y": 2, "z": 4}}).SchemesText() {
		t.Errorf("%q %q", a.SchemesText(), b.SchemesText())
	}
}

// rcpLeftClaim is the claim a line makes, as the line itself gives it: what the references that say it is solved are made of.
func rcpLeftClaim(t *testing.T, c *ObjectComputer, line RecomputeLine) RecomputeClaim {
	t.Helper()
	r, ok := c.readLine(&line)
	if !ok {
		t.Fatal("the line cannot be read")
	}
	formed, _ := r.pr.formAll(r.pf, r.row)
	var sp StagePage
	if err := r.pr.claims(&sp, 0, r.pf, formed, rowView{row: r.row}); err != nil || len(sp.Claims) == 0 {
		t.Fatalf("the line makes no claim: %v %+v", err, sp.Claims)
	}
	return RecomputeClaim{Scheme: c.schemes[sp.Claims[0].Scheme], Hash: sp.Claims[0].ValueHash}
}

// [REQ-931]
func TestRcpTheIdentifierOfAnObjectIsTheOneOfTheRecordOpenedLastAndThenTheOneOfTheSmallerKey(t *testing.T) {
	c, plan := rcpLeftComputer(t, mappingJSON)
	var feed, typeNo int
	for _, f := range plan.IdentifyFeeds(7) {
		if f.Creates && f.NameCol == 1 && f.TypeNo != 0 {
			feed, typeNo = f.Feed, f.TypeNo
		}
	}
	vm := func(key, uuid string, opened int64) RecomputeLine {
		line := RecomputeLine{Rid: 1, SrcNo: 7, DatasetNo: 3, Dataset: "ds_vms", Key: key, Fields: []string{uuid, "vm", "2"}, OpenedSeq: opened, Feed: feed}
		line.Won = []RecomputeClaim{rcpLeftClaim(t, c, line)}
		return line
	}
	idents := func(lines ...RecomputeLine) string {
		out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "vm", Lines: lines}}})
		if err != nil || len(out.Objects) != 1 {
			t.Fatalf("%+v %v", out, err)
		}
		var values []string
		for _, id := range out.Objects[0].Idents {
			values = append(values, id.Value)
		}
		return strings.Join(values, ",")
	}
	// the record opened later wins, though its key comes after: the direction of opened_seq
	if got := idents(vm("a", "u-old", 1), vm("z", "u-new", 5)); got != "u-new" {
		t.Errorf("the identifier of the record that was opened last is not chosen: %q", got)
	}
	if got := idents(vm("z", "u-new", 5), vm("a", "u-old", 1)); got != "u-new" {
		t.Errorf("the order the lines are read in changes the identifier: %q", got)
	}
	// opened together, the smaller key wins: the direction of the key
	if got := idents(vm("z", "u-two", 3), vm("a", "u-one", 3)); got != "u-one" {
		t.Errorf("the identifier of the smaller key is not chosen: %q", got)
	}
}

// rcpLeftTwoFeeds is the mapping of the other tests with a second feed that reads the servers too and gives the manufacturer on
// another basis, so that the feed a value comes from can be told from the value.
func rcpLeftTwoFeeds(t *testing.T) string {
	t.Helper()
	extra := ` {"id":"f_extra","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":false,"basis":"declared",
  "attributes":[{"attribute":"manufacturer","column":"manufacturer"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1}]},
`
	m := strings.Replace(mappingJSON, ` {"id":"f_vm"`, extra+` {"id":"f_vm"`, 1)
	if m == mappingJSON {
		t.Fatal("the mapping of the test was not made")
	}
	return m
}

// [REQ-930]
func TestRcpTheValueTwoFeedsGiveOfOneRecordIsTheOneOfTheFeedWithTheSmallerNumber(t *testing.T) {
	c, plan := rcpLeftComputer(t, rcpLeftTwoFeeds(t))
	feed, typeNo := rcpLeftServerFeed(plan)
	other := -1
	for _, f := range plan.IdentifyFeeds(7) {
		if !f.Creates && f.TypeNo == typeNo {
			other = f.Feed
		}
	}
	if other <= feed {
		t.Fatalf("the feeds are %d and %d: the case does not tell the directions apart", feed, other)
	}
	basisOf := func(first, second int) string {
		a, b := rcpServerLine(first, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on"), rcpServerLine(second, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on")
		out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "x", Lines: []RecomputeLine{a, b}}}})
		if err != nil || len(out.Objects) != 1 {
			t.Fatalf("%+v %v", out, err)
		}
		var raw map[string]map[string]any
		if err := json.Unmarshal(out.Objects[0].Doc, &raw); err != nil {
			t.Fatal(err)
		}
		for _, e := range raw {
			if e["v"] == "Zorvane" {
				return fmt.Sprint(e["b"])
			}
		}
		t.Fatalf("no attribute has the manufacturer: %s", out.Objects[0].Doc)
		return ""
	}
	if got := basisOf(feed, other); got != "observed" {
		t.Errorf("the value of the feed with the smaller number says %q, not observed", got)
	}
	if got := basisOf(other, feed); got != "observed" {
		t.Errorf("the order of the lines changes the value: %q", got)
	}
}

// [REQ-933]
func TestRcpTwoRecordsThatSayTheSameThingAreNotAProblemWhateverTheOrderOfThem(t *testing.T) {
	c, plan := rcpLeftComputer(t, mappingJSON)
	feed, typeNo := rcpLeftServerFeed(plan)
	a := rcpServerLine(feed, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on")
	b := rcpServerLine(feed, "SV1b", 2, "SV1", "sn1", "Zorvane", "RK1", "on")
	out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "x", Lines: []RecomputeLine{a, b}}}})
	if err != nil || len(out.Objects) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	if len(out.Problems) != 0 {
		t.Errorf("two records that say the same are a problem: %+v", out.Problems)
	}
	// and one that says another thing is, once
	b.Fields = []string{"SV1", "sn1", "Zorvane", "RK1", "off"}
	out, err = c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "x", Lines: []RecomputeLine{b, a}}}})
	if err != nil || len(out.Problems) != 1 || out.Problems[0].Kind != ProblemSourceConflict || out.Problems[0].Key != "SV1b" {
		t.Errorf("%+v %v", out.Problems, err)
	}
}

// [REQ-932]
func TestRcpTheNameAnObjectKeepsWhenNoValueNamesItIsTheFirstPartOfItsKeyThatIsNotItsContext(t *testing.T) {
	c, plan := rcpLeftComputer(t, mappingJSON)
	feed, typeNo := rcpLeftServerFeed(plan)
	parent, err := kernel.ParseID[graph.ObjectKind]("obj_" + strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	key, err := graph.EncodeKey([]graph.KeyPart{
		{Kind: graph.PartContext, Ref: "LOCATED_IN", Value: "the-context", Objects: []graph.ObjectID{parent}},
		{Kind: graph.PartProperty, Ref: "name", Value: "the-first-part"},
	})
	if err != nil {
		t.Fatal(err)
	}
	line := rcpServerLine(feed, "SV1", 1, "", "sn1", "Zorvane", "RK1", "on")
	out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "registry", FirstKey: []byte(key.(string)), Lines: []RecomputeLine{line}}}})
	if err != nil || len(out.Objects) != 1 || out.Objects[0].Name != "the-first-part" {
		t.Errorf("%+v %v", out, err)
	}
}

// [REQ-934]
func TestRcpACellAFeedThatMakesObjectsCannotReadIsNotAProblemOfTheStepForTheReceivingReportsIt(t *testing.T) {
	c, plan := rcpLeftComputer(t, mappingJSON)
	var feed, typeNo int
	for _, f := range plan.IdentifyFeeds(7) {
		if f.Creates && f.NameCol == 1 && f.TypeNo != 0 {
			feed, typeNo = f.Feed, f.TypeNo
		}
	}
	// the number of cpus is not a number, and the record was received now (why 3 is new)
	line := RecomputeLine{Rid: 1, Why: 3, SrcNo: 7, DatasetNo: 3, Dataset: "ds_vms", Key: "a", Fields: []string{"u-a", "vm", "many"}, OpenedSeq: 1, Feed: feed}
	out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "vm", Lines: []RecomputeLine{line}}}})
	if err != nil || len(out.Objects) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	if len(out.Problems) != 0 {
		t.Errorf("a cell of a feed that makes the objects is a problem of the step too: %+v", out.Problems)
	}
}

// [REQ-934]
func TestRcpTheReferencesOfAnObjectAreOfEveryRecordUnderItEvenOneThatGivesNoValue(t *testing.T) {
	c, plan := rcpLeftComputer(t, mappingJSON)
	feed, typeNo := rcpLeftServerFeed(plan)
	full := rcpServerLine(feed, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on")
	bare := rcpServerLine(feed, "SV1b", 2, "", "", "", "", "")
	out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "x", Lines: []RecomputeLine{full, bare}}}})
	if err != nil || len(out.Objects) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	var refs []string
	if err := json.Unmarshal(out.Objects[0].SourceRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if want := []string{"src_ledger/ds_servers/SV1", "src_ledger/ds_servers/SV1b"}; !slices.Equal(refs, want) {
		t.Errorf("the references are %v, want %v", refs, want)
	}
}

// [REQ-934]
func TestRcpTheGradeOfAnObjectIsTheLowestOfTheFeedsThatGiveItsValuesAndAFeedThatIsApproximateGivesAssumed(t *testing.T) {
	approx := strings.Replace(mappingJSON, `"id":"f_server","dataset":"ds_servers","object_type":"PhysicalServer","creates_objects":true,`, `"id":"f_server","dataset":"ds_servers","object_type":"PhysicalServer","approximate":true,"creates_objects":true,`, 1)
	if approx == mappingJSON {
		t.Fatal("the mapping of the test was not made")
	}
	for name, tc := range map[string]struct {
		mapping string
		grade   model.Grade
	}{"a feed that is exact": {mappingJSON, model.GradeFact}, "a feed that is approximate": {approx, model.GradeAssumed}} {
		c, plan := rcpLeftComputer(t, tc.mapping)
		feed, typeNo := rcpLeftServerFeed(plan)
		out, err := c.Make(RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "x", Lines: []RecomputeLine{rcpServerLine(feed, "SV1", 1, "SV1", "sn1", "Zorvane", "RK1", "on")}}}})
		if err != nil || len(out.Objects) != 1 {
			t.Fatalf("%s: %+v %v", name, out, err)
		}
		if got := out.Objects[0].MinGrade; got != tc.grade {
			t.Errorf("%s: the grade is %q, want %q", name, got, tc.grade)
		}
	}
}

// rcpLeftPeak runs f and gives the largest heap it saw while it ran (sampled every millisecond).
func rcpLeftPeak(f func()) uint64 {
	var stop atomic.Bool
	var peak atomic.Uint64
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for !stop.Load() {
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
			time.Sleep(time.Millisecond)
		}
	}()
	f()
	stop.Store(true)
	<-done
	return peak.Load()
}

// the most heap, above what the lines themselves hold, that the making of the biggest object a page holds may take; the ADR gives the
// measured values, and the limit has room for the machine the test runs on
const rcpLeftHeapAboveLines = 100 << 20

// [REQ-972]
func TestRcpTheMemoryOfTheBiggestObjectAPageHoldsIsBoundedByTheNumberOfLinesOfAPage(t *testing.T) {
	c, plan := rcpLeftComputer(t, mappingJSON)
	feed, typeNo := rcpLeftServerFeed(plan)
	for name, tc := range map[string]struct {
		same bool
		pad  int
	}{"values that are all the same": {true, 0}, "values that are all different": {false, 0}, "cells of 200 bytes": {false, 200}} {
		pad := strings.Repeat("x", tc.pad)
		lines := make([]RecomputeLine, DefaultRecomputeObjectMax)
		for i := range lines {
			asset := "SV"
			if !tc.same {
				asset = fmt.Sprintf("SV%07d", i)
			}
			lines[i] = rcpServerLine(feed, fmt.Sprintf("K%07d", i), int64(i+1), asset+pad, "sn1", "Zorvane", "RK1", "on")
			lines[i].OpenedSeq = int64(i)
		}
		runtime.GC()
		var base runtime.MemStats
		runtime.ReadMemStats(&base)
		page := RecomputePage{Objects: []RecomputeObject{{ObjNo: 1, Ref: "obj-1", TypeNo: typeNo, Name: "old", Lines: lines}}}
		var out RecomputedPage
		peak := rcpLeftPeak(func() {
			var err error
			if out, err = c.Make(page); err != nil || len(out.Objects) != 1 {
				t.Errorf("%s: %v %+v", name, err, out.Left)
			}
		})
		above := int64(peak) - int64(base.HeapAlloc)
		t.Logf("%s: %d lines, the heap peaks at %.1f MB, %.1f MB above the lines", name, len(lines), float64(peak)/1e6, float64(above)/1e6)
		if above > rcpLeftHeapAboveLines {
			t.Errorf("%s: making an object of %d lines took %.1f MB above the lines", name, len(lines), float64(above)/1e6)
		}
		runtime.KeepAlive(page)
		runtime.KeepAlive(out)
	}
}
