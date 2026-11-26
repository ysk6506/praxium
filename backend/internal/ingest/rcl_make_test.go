package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// What the computer of the links makes of the ends of a page (ADR-0122). Every helper and type starts with rcl.

const rclSrc = 7

func rclComputer(t *testing.T, links string) (*LinkComputer, *Plan) {
	t.Helper()
	plan := sxScalePlan(t, laSource, laMapping(links))
	return NewLinkComputer(map[int64]*Plan{rclSrc: plan}, graph.NewContentHasher(sxRegistry(t), graph.NewTestKeyProvider("rcl"), graph.KGID{})), plan
}

// rclEnd is the end of a mount record: the cells are the ones of ds_servers, in the order of its columns.
func rclEnd(t *testing.T, targets int, cells ...string) RecomputeEnd {
	t.Helper()
	ids := kernel.NewSequentialIDs()
	from, _ := kernel.NewID[graph.ObjectKind](ids, "obj")
	to, _ := kernel.NewID[graph.ObjectKind](ids, "obj")
	e := RecomputeEnd{Rid: 5, SrcNo: rclSrc, DatasetNo: 3, Dataset: "ds_servers", Key: cells[0], Fields: cells, Def: 0, FromObj: 11, FromRef: from.String(), Targets: targets}
	if targets == 1 {
		e.ToObj, e.ToRef = 12, to.String()
	}
	return e
}

func rclPage(es ...RecomputeEnd) RecomputeLinksPage {
	return RecomputeLinksPage{Unit: 3, Phase: LinksPhaseFrom, Last: true, Through: LinkCursor{Def: 0, Rid: 5}, Ends: es}
}

var rclCells = []string{"SV1", "sn100", "Zorvane", "RK1", "on", "5", "", "", "", ""}

// [REQ-867]
func TestRclAnEndThatFindsOneObjectMakesTheLinkWithTheValuesOfTheOldFlow(t *testing.T) {
	c, plan := rclComputer(t, laLink(laRackLink, laPos+`,{"attribute":"face","value":"front"}`))
	got, err := c.MakeLinks(rclPage(rclEnd(t, 1, rclCells...)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Unit != 3 || got.Phase != LinksPhaseFrom || !got.Last || got.Ends != 1 || got.Skipped != 0 || len(got.Results) != 1 || len(got.Candidates) != 1 || len(got.Problems) != 0 {
		t.Fatalf("a page: %+v", got)
	}
	cand := got.Candidates[0]
	if !strings.HasPrefix(cand.Row.Ref, "lnk_") || got.Results[0].Ref != cand.Row.Ref || cand.Row.Src != 11 || cand.Row.Dst != 12 {
		t.Errorf("the link: %+v", cand.Row)
	}
	if !strings.Contains(string(cand.Row.Doc), `"v":5`) && !strings.Contains(string(cand.Row.Doc), `"v": 5`) {
		t.Errorf("the attributes: %s", cand.Row.Doc)
	}
	if !strings.Contains(string(cand.Row.SourceRefs), plan.Source.ID+"/ds_servers/SV1") {
		t.Errorf("the reference of the source: %s", cand.Row.SourceRefs)
	}
	if !strings.Contains(cand.Who.Shown, "position_u=5") || !strings.Contains(cand.Who.Shown, "face=front") || cand.Who.Sig == "" {
		t.Errorf("the words of the attributes: %q (%q)", cand.Who.Shown, cand.Who.Sig)
	}
	if cand.Who.LinkName != "MOUNTED_IN" || cand.Who.DefID != "l_mounted" || cand.Who.Dataset != "ds_servers" || len(cand.Who.Left) != 0 {
		t.Errorf("the names: %+v", cand.Who)
	}
	if cand.Row.ContentHash == (kernel.Hash{}) || cand.Row.RowHash == (kernel.Hash{}) || cand.Row.Grade == "" || cand.Row.Basis != "declared" {
		t.Errorf("the hashes and the grade: %+v", cand.Row)
	}
	// the same end again makes the same link and the same hashes, and another cell for the attribute makes another content
	again, _ := c.MakeLinks(rclPage(rclEnd(t, 1, rclCells...)))
	other := append([]string{}, rclCells...)
	other[5] = "6"
	moved, _ := c.MakeLinks(rclPage(rclEnd(t, 1, other...)))
	if again.Candidates[0].Row.ContentHash != cand.Row.ContentHash || again.Candidates[0].Row.Ref != cand.Row.Ref {
		t.Error("the same end makes another link")
	}
	if moved.Candidates[0].Row.ContentHash == cand.Row.ContentHash || moved.Candidates[0].Row.Ref != cand.Row.Ref || moved.Candidates[0].Who.Sig == cand.Who.Sig {
		t.Error("a link of another position is the same content")
	}
}

// [REQ-867]
func TestRclALinkWithNoAttributesIsSaidSoAnEmptyCellGivesNoValueAndAWrongOneIsSaid(t *testing.T) {
	c, _ := rclComputer(t, laLink(laRackLink, ""))
	got, err := c.MakeLinks(rclPage(rclEnd(t, 1, rclCells...)))
	if err != nil || got.Candidates[0].Who.Shown != "{no attributes}" || got.Candidates[0].Who.Sig != "" {
		t.Errorf("a link with no attributes: %+v %v", got.Candidates, err)
	}
	c, _ = rclComputer(t, laLink(laRackLink, laPos))
	empty := append([]string{}, rclCells...)
	empty[5] = ""
	got, err = c.MakeLinks(rclPage(rclEnd(t, 1, empty...)))
	if err != nil || len(got.Candidates) != 1 || len(got.Candidates[0].Who.Left) != 0 || string(got.Candidates[0].Row.Doc) != "{}" {
		t.Errorf("an empty cell gives a value: %+v %v", got.Candidates, err)
	}
	wrong := append([]string{}, rclCells...)
	wrong[5] = "front"
	got, err = c.MakeLinks(rclPage(rclEnd(t, 1, wrong...)))
	if err != nil || len(got.Candidates) != 1 || len(got.Candidates[0].Who.Left) != 1 || string(got.Candidates[0].Row.Doc) != "{}" {
		t.Errorf("a wrong cell is not said: %+v %v", got.Candidates, err)
	}
}

// [REQ-866]
func TestRclAnEndThatFindsNoObjectOrMoreThanOneIsAProblemOfItsRecordAndMakesNoLink(t *testing.T) {
	c, _ := rclComputer(t, laLink(laRackLink, laPos))
	got, err := c.MakeLinks(rclPage(rclEnd(t, 0, rclCells...), rclEnd(t, 2, rclCells...)))
	if err != nil || len(got.Candidates) != 0 || len(got.Results) != 2 || got.Results[0].Ref != "" || got.Results[1].Ref != "" {
		t.Fatalf("ends of no link: %+v %v", got, err)
	}
	if len(got.Problems) != 2 || got.Problems[0].Kind != ProblemLinkTarget || got.Problems[1].Kind != ProblemLinkAmbiguous || got.Problems[0].Key != "SV1" || got.Problems[0].DatasetNo != 3 {
		t.Errorf("the problems: %+v", got.Problems)
	}
}

// [REQ-872]
func TestRclAnEndWhoseCellsAreNotTheOnesTheDatasetDeclaresOrWhoseKeyIsEmptyOrWhoseDefinitionIsNotKnownMakesNothing(t *testing.T) {
	c, _ := rclComputer(t, laLink(laRackLink, laPos))
	noKey := append([]string{}, rclCells...)
	noKey[3] = ""
	short := rclEnd(t, 1, rclCells...)
	short.Fields = short.Fields[:4]
	unknownSource := rclEnd(t, 1, rclCells...)
	unknownSource.SrcNo = 99
	unknownDef := rclEnd(t, 1, rclCells...)
	unknownDef.Def = 4
	negative := rclEnd(t, 1, rclCells...)
	negative.Def = -1
	unknownDataset := rclEnd(t, 1, rclCells...)
	unknownDataset.Dataset = "ds_nothing"
	got, err := c.MakeLinks(rclPage(rclEnd(t, 1, noKey...), short, unknownSource, unknownDef, negative, unknownDataset))
	if err != nil {
		t.Fatal(err)
	}
	if got.Skipped != 6 || got.Ends != 6 || len(got.Results) != 0 || len(got.Candidates) != 0 || len(got.Problems) != 0 {
		t.Errorf("ends that make nothing: %+v", got)
	}
}

// [REQ-867]
func TestRclAnObjectThatIsNotAnIdOfAnObjectIsAnErrorAndNotALink(t *testing.T) {
	c, _ := rclComputer(t, laLink(laRackLink, laPos))
	bad := rclEnd(t, 1, rclCells...)
	bad.FromRef = "not an id"
	if _, err := c.MakeLinks(rclPage(bad)); err == nil {
		t.Error("an end of a bad object makes a link")
	}
	bad = rclEnd(t, 1, rclCells...)
	bad.ToRef = "not an id"
	if _, err := c.MakeLinks(rclPage(bad)); err == nil {
		t.Error("an end to a bad object makes a link")
	}
}

// [REQ-874]
func TestRclTheStartOfTheStepRefusesTwoDefinitionsOfOneAndANegativePageAndTheDefinitionsAreTheSameTextInAnyOrder(t *testing.T) {
	d := []RecomputeLinkDef{{SrcNo: 2, Def: 1, FromFeed: 3, ToFeed: 4}, {SrcNo: 1, Def: 0, FromFeed: 1, ToFeed: 2}}
	a := RecomputeLinksStart{Defs: d}
	b := RecomputeLinksStart{Defs: []RecomputeLinkDef{d[1], d[0]}}
	if a.DefsText() != b.DefsText() || a.DefsText() != "1/0=1>2;2/1=3>4;" {
		t.Errorf("the text of the definitions: %q %q", a.DefsText(), b.DefsText())
	}
	if err := a.Validate(); err != nil {
		t.Error(err)
	}
	if (RecomputeLinksStart{Lines: -1}).Validate() == nil || (RecomputeLinksStart{Defs: []RecomputeLinkDef{d[0], d[0]}}).Validate() == nil {
		t.Error("a start that cannot work is accepted")
	}
	plan := sxScalePlan(t, laSource, laMapping(laLink(laRackLink, laPos)+","+laLink(laCoolLink, "")))
	defs := RecomputeLinkDefsOf(map[int64]*Plan{9: plan, 4: plan})
	if len(defs) != 4 || defs[0].SrcNo != 4 || defs[3].SrcNo != 9 || defs[1].Def != 1 || defs[0].FromFeed == defs[0].ToFeed {
		t.Errorf("the definitions of the plans: %+v", defs)
	}
}

// rclFake is a port of the step that says what it is told.
type rclFake struct {
	place RecomputeLinksPlace
	err   error
	pages int
	begun int
}

func (f *rclFake) BeginRecomputeLinks(_ context.Context, _ RunLease, start RecomputeLinksStart) error {
	f.begun++
	f.place.Begun, f.place.Defs = true, start.DefsText()
	return nil
}
func (f *rclFake) RecomputeLinksPlace(context.Context, RunLease) (RecomputeLinksPlace, error) {
	return f.place, f.err
}
func (f *rclFake) RecomputeLinksPage(context.Context, RunLease) (RecomputeLinksPage, error) {
	return RecomputeLinksPage{}, nil
}
func (f *rclFake) PutRecomputedLinks(context.Context, RunLease, RecomputedLinksPage) error {
	f.pages++
	f.place.Finished = true
	return nil
}
func (f *rclFake) RecomputeLinksReport(context.Context, RunLease) (RecomputeLinksCounts, error) {
	return RecomputeLinksCounts{Links: 4}, nil
}

type rclMakeNothing struct{}

func (rclMakeNothing) MakeLinks(RecomputeLinksPage) (RecomputedLinksPage, error) {
	return RecomputedLinksPage{}, nil
}

// [REQ-874]
func TestRclTheRunDrivesTheStepFromWhereItIsAndRefusesARunThatIsNotComputingOrAnotherDefinitionOrAStoppedContext(t *testing.T) {
	ctx := context.Background()
	lease := RunLease{No: 3}
	start := RecomputeLinksStart{Defs: []RecomputeLinkDef{{SrcNo: 1, Def: 0, FromFeed: 1, ToFeed: 2}}}
	f := &rclFake{place: RecomputeLinksPlace{Stage: RunComputing}}
	counts, err := RunRecomputeLinks(ctx, f, lease, start, rclMakeNothing{})
	if err != nil || counts.Links != 4 || f.begun != 1 || f.pages != 1 {
		t.Errorf("the step: %+v %v (begun %d, pages %d)", counts, err, f.begun, f.pages)
	}
	// it is done: nothing is begun or done again
	if _, err := RunRecomputeLinks(ctx, f, lease, start, rclMakeNothing{}); err != nil || f.begun != 1 || f.pages != 1 {
		t.Errorf("a step that is done is done again: %v", err)
	}
	if _, err := RunRecomputeLinks(ctx, &rclFake{place: RecomputeLinksPlace{Stage: RunReceiving}}, lease, start, rclMakeNothing{}); kernel.CodeOf(err) != CodeRunStage {
		t.Errorf("a run that is not computing: %v", err)
	}
	other := &rclFake{place: RecomputeLinksPlace{Stage: RunComputing, Begun: true, Defs: "9/9=1>1;"}}
	if _, err := RunRecomputeLinks(ctx, other, lease, start, rclMakeNothing{}); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Errorf("a step that began with other definitions: %v", err)
	}
	if _, err := RunRecomputeLinks(ctx, f, lease, RecomputeLinksStart{Lines: -2}, rclMakeNothing{}); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Errorf("a start that cannot work: %v", err)
	}
	if _, err := RunRecomputeLinks(ctx, &rclFake{err: kernel.Errorf(CodeRunLeaseLost, "lost")}, lease, start, rclMakeNothing{}); kernel.CodeOf(err) != CodeRunLeaseLost {
		t.Errorf("a lease that is lost: %v", err)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	mid := &rclFake{place: RecomputeLinksPlace{Stage: RunComputing, Begun: true, Defs: start.DefsText()}}
	if _, err := RunRecomputeLinks(stopped, mid, lease, start, rclMakeNothing{}); err == nil {
		t.Error("a stopped context goes on")
	}
}
