package ingest

import (
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-700]
func TestRunStagesMoveOnlyAlongTheWorkAndAnEndIsOver(t *testing.T) {
	work := 0
	for _, s := range RunStages() {
		if !s.Valid() {
			t.Errorf("%s is not valid", s)
		}
		if !s.Over() {
			work++
		}
		for _, to := range RunStages() {
			if CanAdvance(s, to) && (s.Over() || to.Over()) {
				t.Errorf("%s to %s: an end is not an advance", s, to)
			}
		}
	}
	if work != 6 || RunStage("nope").Valid() {
		t.Error("the stages")
	}
	for from, tos := range map[RunStage][]RunStage{
		RunReceiving: {RunComputing}, RunComputing: {RunAwaitingConfirmation, RunApplying},
		RunAwaitingConfirmation: {RunComputing}, RunApplying: {RunVolatile}, RunVolatile: {RunSettling},
	} {
		for _, to := range tos {
			if !CanAdvance(from, to) {
				t.Errorf("%s to %s", from, to)
			}
		}
	}
	if CanAdvance(RunReceiving, RunApplying) || CanAdvance(RunSettling, RunReceiving) || CanAdvance(RunSucceeded, RunComputing) {
		t.Error("a stage that is skipped or gone back to")
	}
}

// [REQ-700]
func TestARunIsOpenedAndEndedOnlyWithWhatItNeeds(t *testing.T) {
	kg, _ := kernel.ParseID[graph.KGKind]("kg-00000000000000a1")
	ok := RunOpen{KG: kg, Source: "s", Owner: "o", Term: time.Second}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]RunOpen{
		"no kg": {Source: "s", Owner: "o", Term: time.Second}, "no source": {KG: kg, Owner: "o", Term: time.Second},
		"no owner": {KG: kg, Source: "s", Term: time.Second}, "no term": {KG: kg, Source: "s", Owner: "o"},
	} {
		if kernel.CodeOf(bad.Validate()) != kernel.CodeInvalidValue {
			t.Errorf("%s was accepted", name)
		}
	}
	for name, tc := range map[string]struct {
		end RunEnd
		ok  bool
	}{
		"succeeded":               {RunEnd{Stage: RunSucceeded}, true},
		"failed with a class":     {RunEnd{Stage: RunFailed, Class: EndTransient}, true},
		"failed with none":        {RunEnd{Stage: RunFailed}, false},
		"failed with a wrong one": {RunEnd{Stage: RunFailed, Class: "x"}, false},
		"canceled with a class":   {RunEnd{Stage: RunCanceled, Class: EndConfig}, false},
		"not an end":              {RunEnd{Stage: RunComputing}, false},
	} {
		if err := tc.end.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	codes := RunCodes()
	if len(codes) != 5 {
		t.Fatalf("%v", codes)
	}
	for _, c := range codes {
		if m, ok := RunCodeMeaning(c); !ok || m == "" {
			t.Errorf("%s has no meaning", c)
		}
	}
	if _, ok := RunCodeMeaning("x"); ok {
		t.Error("a code that is not one")
	}
}

// [REQ-701]
func TestSchemeNumbersAreTheOrderOfTheModelFromOne(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	p := &Plan{reg: reg}
	got := p.SchemeNumbers()
	schemes := reg.IdentifierSchemes()
	if len(schemes) == 0 || len(got) != len(schemes) {
		t.Fatalf("%d numbers for %d schemes", len(got), len(schemes))
	}
	for i, s := range schemes {
		if got[s.ID.String()] != i+1 {
			t.Errorf("%s has %d, wanted %d", s.ID, got[s.ID.String()], i+1)
		}
	}
}
