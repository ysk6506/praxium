package ingest

import (
	"context"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// idnFake is an Identifier that keeps the place in memory and notes the calls.
type idnFake struct {
	stage   RunStage
	begun   bool
	phase   int
	units   []string
	rebuilt [][]RebuildRow
	answers [][]Answer
	failAt  string
}

var idnScript = []IdentifyNeed{NeedNothing, NeedRebuild, NeedAsk, NeedNothing}

func (d *idnFake) BeginIdentify(_ context.Context, _ RunLease, _ IdentifyStart) error {
	d.begun = true
	return nil
}

func (d *idnFake) IdentifyPlace(_ context.Context, _ int64) (IdentifyPlace, error) {
	p := IdentifyPlace{Stage: d.stage, Begun: d.begun, Finished: d.begun && d.phase >= len(idnScript)}
	if d.phase < len(idnScript) {
		p.Need = idnScript[d.phase]
	}
	return p, nil
}

func (d *idnFake) idnDo(what string) error {
	d.units = append(d.units, what)
	if d.failAt == what {
		return kernel.Errorf(kernel.CodeInternal, "the unit failed")
	}
	d.phase++
	return nil
}

func (d *idnFake) IdentifyStep(_ context.Context, _ RunLease) error { return d.idnDo("step") }

func (d *idnFake) RebuildPage(_ context.Context, _ RunLease) (RebuildPage, error) {
	return RebuildPage{Unit: 3, Rows: []RebuildRow{{Rid: 1, SrcNo: 1, Dataset: "b"}, {Rid: 2, SrcNo: 1, Dataset: "a"}, {Rid: 3, SrcNo: 1, Dataset: "b"}}}, nil
}

func (d *idnFake) PutRebuilt(_ context.Context, _ RunLease, _ int, rows []RebuiltRow) error {
	d.rebuilt = append(d.rebuilt, nil)
	for _, r := range rows {
		d.rebuilt[0] = append(d.rebuilt[0], RebuildRow{Rid: r.Rid})
	}
	return d.idnDo("put")
}

func (d *idnFake) SettleRead(_ context.Context, _ RunLease, _ int64) (SettlePage, error) {
	return SettlePage{Done: true}, nil
}

func (d *idnFake) PutSettled(_ context.Context, _ RunLease, _ []SettledPair, _ SettleEnd) error {
	return d.idnDo("settled")
}

func (d *idnFake) AskPage(_ context.Context, _ RunLease) ([]AskRow, error) {
	return []AskRow{{Round: 1, Label: 5}, {Round: 1, Label: 6}}, nil
}

func (d *idnFake) PutAnswers(_ context.Context, _ RunLease, answers []Answer) error {
	d.answers = append(d.answers, answers)
	return d.idnDo("answers")
}

func (d *idnFake) IdentifyReport(_ context.Context, _ int64) (IdentifyCounts, error) {
	return IdentifyCounts{Rounds: len(d.units)}, nil
}

// idnKeys is a KeyMaker that makes one row of each row it is given and an answer of each ask.
type idnKeys struct {
	groups []string
	fail   bool
}

func (k *idnKeys) Rebuild(_ int64, dataset string, rows []RebuildRow) ([]RebuiltRow, error) {
	k.groups = append(k.groups, dataset)
	if k.fail {
		return nil, kernel.Errorf(kernel.CodeInternal, "the keys failed")
	}
	out := make([]RebuiltRow, len(rows))
	for i, r := range rows {
		out[i].Rid = r.Rid
	}
	return out, nil
}

func (k *idnKeys) Answer(a AskRow) (Answer, error) {
	if k.fail {
		return Answer{}, kernel.Errorf(kernel.CodeInternal, "the answer failed")
	}
	return Answer{Round: a.Round, Label: a.Label, Ref: "x"}, nil
}

// [REQ-883]
func TestIdnRunIdentifyBeginsOnceGivesTheRowsToTheKeyMakerByDatasetAndTheAsksOneByOneAndReportsAtTheEnd(t *testing.T) {
	d := &idnFake{stage: RunComputing}
	keys := &idnKeys{}
	counts, err := RunIdentify(context.Background(), d, RunLease{No: 1, Attempt: 1}, IdentifyStart{}, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !d.begun || counts.Rounds != 4 || len(d.units) != 4 {
		t.Fatalf("%+v %v", counts, d.units)
	}
	// the datasets come in the order the rows first name them, and what was made comes back in the order of the rows
	if len(keys.groups) != 2 || keys.groups[0] != "b" || keys.groups[1] != "a" {
		t.Errorf("%v", keys.groups)
	}
	if len(d.rebuilt[0]) != 3 || d.rebuilt[0][0].Rid != 1 || d.rebuilt[0][1].Rid != 3 || d.rebuilt[0][2].Rid != 2 {
		t.Errorf("%+v", d.rebuilt)
	}
	if len(d.answers) != 1 || len(d.answers[0]) != 2 || d.answers[0][1].Label != 6 {
		t.Errorf("%+v", d.answers)
	}
	// a run that is done reports and does nothing more
	d.units = nil
	if _, err := RunIdentify(context.Background(), d, RunLease{No: 1, Attempt: 1}, IdentifyStart{}, keys); err != nil || len(d.units) != 0 {
		t.Errorf("%v %v", err, d.units)
	}
}

// [REQ-883]
func TestIdnRunIdentifyRefusesARunThatIsNotComputingAndStopsAtTheFirstFailure(t *testing.T) {
	ctx := context.Background()
	lease := RunLease{No: 1, Attempt: 1}
	if _, err := RunIdentify(ctx, &idnFake{stage: RunReceiving}, lease, IdentifyStart{}, &idnKeys{}); kernel.CodeOf(err) != CodeRunStage {
		t.Errorf("a run that is not computing: %v", err)
	}
	if _, err := RunIdentify(ctx, &idnFake{stage: RunComputing}, lease, IdentifyStart{Feeds: []IdentifyFeed{{Feed: 2000}}}, &idnKeys{}); err == nil {
		t.Error("a feed that is out of range")
	}
	for _, at := range []string{"step", "put", "answers"} {
		d := &idnFake{stage: RunComputing, failAt: at}
		if _, err := RunIdentify(ctx, d, lease, IdentifyStart{}, &idnKeys{}); err == nil {
			t.Errorf("the failure of %s is not told", at)
		}
	}
	if _, err := RunIdentify(ctx, &idnFake{stage: RunComputing}, lease, IdentifyStart{}, &idnKeys{fail: true}); err == nil {
		t.Error("the failure of the keys is not told")
	}
	d := &idnFake{stage: RunComputing, phase: 2}
	if _, err := RunIdentify(ctx, d, lease, IdentifyStart{}, &idnKeys{fail: true}); err == nil {
		t.Error("the failure of an answer is not told")
	}
}

// [REQ-883]
func TestIdnTheStartOfTheIdentificationRefusesFeedsAndSizesThatCannotBe(t *testing.T) {
	for name, s := range map[string]IdentifyStart{
		"negative feed": {Feeds: []IdentifyFeed{{Feed: -1}}},
		"twice":         {Feeds: []IdentifyFeed{{Feed: 1}, {Feed: 1}}},
		"size":          {Feeds: []IdentifyFeed{{Feed: 1}}, BucketRows: -1},
	} {
		if s.Validate() == nil {
			t.Errorf("%s", name)
		}
	}
	if err := (IdentifyStart{Feeds: []IdentifyFeed{{Feed: 0}, {Feed: 1}}}).Validate(); err != nil {
		t.Error(err)
	}
	if (IdentifyPlace{Begun: true}).Done() || !(IdentifyPlace{Begun: true, Finished: true}).Done() {
		t.Error("done")
	}
}
