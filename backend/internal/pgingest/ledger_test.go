package pgingest

import (
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// [REQ-700]
func TestPgiASourceHasOneRunThatIsNotOver(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	e.attach(t, kg, plan)
	first := e.open(t, kg, plan.Source.ID, "server-a")
	if first.Attempt != 1 || first.No == 0 {
		t.Fatalf("the first lease: %+v", first)
	}
	_, err := e.st.OpenRun(e.ctx, ingest.RunOpen{KG: kg.ID, Source: plan.Source.ID, Owner: "server-b", Term: time.Minute})
	if !pgiCodeIs(err, ingest.CodeRunOpen) {
		t.Fatalf("a second run of the source while the first is not over: %v", err)
	}
	if err := e.st.EndRun(e.ctx, first, ingest.RunEnd{Stage: ingest.RunCanceled}); err != nil {
		t.Fatal(err)
	}
	second, err := e.st.OpenRun(e.ctx, ingest.RunOpen{KG: kg.ID, Source: plan.Source.ID, Owner: "server-b", Term: time.Minute})
	if err != nil || second.No <= first.No {
		t.Fatalf("a run can be opened when the one before is over: %+v %v", second, err)
	}
	if _, err := e.st.OpenRun(e.ctx, ingest.RunOpen{KG: kg.ID, Source: "src_unknown", Owner: "x", Term: time.Minute}); !pgiCodeIs(err, kernel.CodeNotFound) {
		t.Fatalf("a source that is not put to the KG: %v", err)
	}
	if _, err := e.st.OpenRun(e.ctx, ingest.RunOpen{KG: kg.ID, Source: plan.Source.ID, Term: time.Minute}); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Fatalf("a run with no owner: %v", err)
	}
	got := e.text(t, `SELECT kind || ' ' || trigger_kind || ' ' || apply_mode || ' ' || stage FROM sync_run WHERE run_no = $1`, second.No)
	if got != "full manual automatic receiving" {
		t.Fatalf("a run opened with nothing named is %q", got)
	}
}

// [REQ-700]
func TestPgiAWriteOfAnAttemptThatIsNotTheRunsIsRefusedAndChangesNothing(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	e.attach(t, kg, plan)
	old := e.open(t, kg, plan.Source.ID, "server-a")
	if _, err := e.st.AcquireRun(e.ctx, old.No, "server-b", time.Minute); !pgiCodeIs(err, ingest.CodeRunHeld) {
		t.Fatalf("a lease that is alive is another server's: %v", err)
	}
	if _, err := e.st.AcquireRun(e.ctx, old.No+100, "server-b", time.Minute); !pgiCodeIs(err, kernel.CodeNotFound) {
		t.Fatalf("a run that does not exist: %v", err)
	}
	if _, err := e.st.AcquireRun(e.ctx, old.No, "", time.Minute); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Fatalf("a server that names no one: %v", err)
	}
	e.expire(t, old.No)
	taken, err := e.st.AcquireRun(e.ctx, old.No, "server-b", time.Minute)
	if err != nil || taken.Attempt != 2 || taken.No != old.No {
		t.Fatalf("a run whose lease has run out is taken over with the next attempt: %+v %v", taken, err)
	}
	stage := e.text(t, `SELECT stage FROM sync_run WHERE run_no = $1`, old.No)
	writes := map[string]error{
		"renew":   e.st.RenewRun(e.ctx, old),
		"advance": e.st.AdvanceRun(e.ctx, old, ingest.RunReceiving, ingest.RunComputing),
		"end":     e.st.EndRun(e.ctx, old, ingest.RunEnd{Stage: ingest.RunCanceled}),
	}
	for name, err := range writes {
		if !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
			t.Errorf("%s with the attempt that was taken over: %v", name, err)
		}
	}
	if got := e.text(t, `SELECT stage || ' ' || attempt || ' ' || owner FROM sync_run WHERE run_no = $1`, old.No); got != stage+" 2 server-b" {
		t.Fatalf("a refused write changed the run: %q", got)
	}
	if err := e.st.RenewRun(e.ctx, taken); err != nil {
		t.Fatalf("the attempt that holds the run renews: %v", err)
	}
	// the same owner takes its own run again, with the next attempt, whatever the lease says.
	again, err := e.st.AcquireRun(e.ctx, old.No, "server-b", time.Minute)
	if err != nil || again.Attempt != 3 {
		t.Fatalf("%+v %v", again, err)
	}
	if err := e.st.EndRun(e.ctx, again, ingest.RunEnd{Stage: ingest.RunFailed, Class: ingest.EndTransient, Code: "x", Reason: "the source went away"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AcquireRun(e.ctx, old.No, "server-c", time.Minute); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Fatalf("a run that is over is not taken: %v", err)
	}
	if err := e.st.RenewRun(e.ctx, again); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Fatalf("a run that is over holds no lease: %v", err)
	}
	if err := e.st.RenewRun(e.ctx, ingest.RunLease{No: old.No + 100, Attempt: 1, Term: time.Minute}); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
		t.Fatalf("a run that is not there: %v", err)
	}
	got := e.text(t, `SELECT stage || ' ' || end_class || ' ' || end_code || ' ' || end_reason || ' ' || (ended_at IS NOT NULL) || ' ' || (lease_until IS NULL) FROM sync_run WHERE run_no = $1`, old.No)
	if got != "failed transient x the source went away true true" {
		t.Fatalf("the end of a run that failed: %q", got)
	}
}

// [REQ-700]
func TestPgiARunMovesOnlyAlongTheStagesAndTheGateOfTheKGHoldsOneRunAtWork(t *testing.T) {
	e := pgiOpen(t)
	planA := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	planB := pgiPlan(t, `{"id":"src_other","adapter":"file","datasets":[{"id":"ds_one","parser":"csv","natural_key":["k"],"columns":[{"name":"k"}]}]}`, `{"feeds":[{"id":"f_one","dataset":"ds_one","object_type":"Rack","creates_objects":true,"basis":"declared","attributes":[{"attribute":"name","column":"k"}],"identity":[{"components":[{"property":"asset_tag","column":"k"}]}]}]}`)
	kg := e.kg(t, 1)
	e.attach(t, kg, planA)
	e.attach(t, kg, planB)
	a := e.open(t, kg, planA.Source.ID, "server-a")
	b := e.open(t, kg, planB.Source.ID, "server-a")
	if err := e.st.AdvanceRun(e.ctx, a, ingest.RunReceiving, ingest.RunApplying); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Fatalf("a move the stages do not make: %v", err)
	}
	if err := e.st.AdvanceRun(e.ctx, a, ingest.RunComputing, ingest.RunApplying); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Fatalf("a run that is not in the stage it is moved from: %v", err)
	}
	if err := e.st.AdvanceRun(e.ctx, a, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AdvanceRun(e.ctx, b, ingest.RunReceiving, ingest.RunComputing); !pgiCodeIs(err, ingest.CodeKGGateTaken) {
		t.Fatalf("a second run of the KG computing: %v", err)
	}
	if got := e.text(t, `SELECT stage FROM sync_run WHERE run_no = $1`, b.No); got != "receiving" {
		t.Fatalf("a run that does not get the gate stays where it was: %s", got)
	}
	// a run that waits for a confirmation lets the gate of the KG go.
	if err := e.st.AdvanceRun(e.ctx, a, ingest.RunComputing, ingest.RunAwaitingConfirmation); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AdvanceRun(e.ctx, b, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Fatalf("the gate is free while the other run waits for a confirmation: %v", err)
	}
	if err := e.st.EndRun(e.ctx, b, ingest.RunEnd{Stage: ingest.RunSucceeded, Class: ingest.EndConfig}); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Fatalf("a run that did not fail has no class: %v", err)
	}
	if err := e.st.EndRun(e.ctx, b, ingest.RunEnd{Stage: ingest.RunComputing}); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Fatalf("a stage of work is no end: %v", err)
	}
	if err := e.st.EndRun(e.ctx, b, ingest.RunEnd{Stage: ingest.RunDiscarded}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AdvanceRun(e.ctx, b, ingest.RunComputing, ingest.RunApplying); !pgiCodeIs(err, ingest.CodeRunStage) {
		t.Fatalf("a run that is over cannot be moved: %v", err)
	}
	if _, err := e.st.ReceivePlace(e.ctx, b.No+100); !pgiCodeIs(err, kernel.CodeNotFound) {
		t.Fatalf("the place of a run that is not there: %v", err)
	}
}

func TestPgiTheTokenOfAPageIsWrittenAsTextAndReadBack(t *testing.T) {
	for _, c := range []struct {
		token []byte
		last  bool
		text  any
	}{{nil, false, nil}, {[]byte{0, 1, 255, '\n'}, false, "t:AAH/Cg=="}, {[]byte("x"), true, "last"}} {
		if got := encodeToken(c.token, c.last); got != c.text {
			t.Errorf("%q %v is written as %v, not %v", c.token, c.last, got, c.text)
		}
	}
	if _, _, err := decodeToken(nullText("t:!!")); err == nil {
		t.Error("a token that this unit did not write was read")
	}
	if tok, last, err := decodeToken(nullText("t:AAH/Cg==")); err != nil || last || string(tok) != "\x00\x01\xff\n" {
		t.Errorf("%q %v %v", tok, last, err)
	}
}
