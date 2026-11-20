package pgingest

import (
	"context"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-701]
func TestPgiAPageWhoseRowOneTableRefusesLeavesNothingWhicheverTableItIsAndTheSinkGoesOn(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	sink, err := e.st.newSink(e.ctx, lease, plan)
	if err != nil {
		t.Fatal(err)
	}
	var scheme string
	for id := range plan.SchemeNumbers() {
		scheme = id
	}
	// a number that does not fit the column of its table is what the server refuses, one table at a time.
	for name, mutate := range map[string]func(p *ingest.StagePage){
		"keys": func(p *ingest.StagePage) { p.Keys = []ingest.StageKey{{No: 0, Feed: 1 << 33, TypeNo: 1}} },
		"parents": func(p *ingest.StagePage) {
			p.Keys = []ingest.StageKey{{No: 0, TypeNo: 1}}
			p.Parents = []ingest.StageParent{{No: 0, TypeNo: 70000}}
		},
		"ends": func(p *ingest.StagePage) { p.Ends = []ingest.StageEnd{{No: 0, Def: 1 << 33, Side: "to"}} },
		"claims": func(p *ingest.StagePage) {
			p.Claims = []ingest.StageClaim{{No: 0, Feed: 1 << 33, Scheme: scheme, Value: "v"}}
		},
		"records": func(p *ingest.StagePage) { p.Records[0].No = 1 << 40 },
		"unknown scheme": func(p *ingest.StagePage) {
			p.Claims = []ingest.StageClaim{{No: 0, Scheme: "no_such_scheme", Value: "v"}}
		},
		"unknown problem": func(p *ingest.StagePage) { p.Problems = []ingest.StageProblem{{No: 0, Kind: "no_such_kind"}} },
	} {
		page := pgiPage("ds_places", 0, "a")
		mutate(&page)
		if err := sink.Stage(e.ctx, page); err == nil {
			t.Errorf("%s: a page the tables cannot hold was accepted", name)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM `+pgiTable(ref, pgschema.TableStageRecord)); n != 0 {
		t.Errorf("%d rows of the pages that failed stayed", n)
	}
	if err := sink.Stage(e.ctx, pgiPage("ds_places", 0, "a")); err != nil {
		t.Errorf("the sink did not go on after the pages that failed: %v", err)
	}
}

// [REQ-701]
func TestPgiThePlaceOfARecordIsItsRangeOnTheSheetElseItsLineAndTheLinesPassedOverAreCounted(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	sink, err := e.st.newSink(e.ctx, lease, plan)
	if err != nil {
		t.Fatal(err)
	}
	page := pgiPage("ds_places", 0, "a", "b", "c")
	page.Records[0].Where = "B2:D2"
	page.Records[1].Line = 7
	page.Skipped = map[string]int{"no_key": 2}
	if err := sink.Stage(e.ctx, page); err != nil {
		t.Fatal(err)
	}
	page = pgiPage("ds_places", 1, "d")
	page.Skipped = map[string]int{"no_key": 3}
	if err := sink.Stage(e.ctx, page); err != nil {
		t.Fatal(err)
	}
	got := e.text(t, `SELECT string_agg(key || '=' || place, ',' ORDER BY key) FROM `+pgiTable(ref, pgschema.TableStageRecord)+` WHERE page = 0`)
	if got != "a=B2:D2,b=7,c=" {
		t.Errorf("places: %s", got)
	}
	if n := sink.skipped["ds_places"]["no_key"]; n != 5 {
		t.Errorf("%d lines passed over were counted", n)
	}
}

// [REQ-701]
func TestPgiReceivingIsRefusedForAWrongPlanARunThatIsNotThereAndADatasetTheSourceWasNotPutWith(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	if _, err := e.st.AttachSource(e.ctx, kg.ID, plan.Source.ID, pgiDatasets(plan)[:1]); err != nil {
		t.Fatal(err)
	}
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	if _, err := e.st.Receive(e.ctx, lease, ingest.Intake{}); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Errorf("no plan and no reader: %v", err)
	}
	if _, err := e.st.Receive(e.ctx, ingest.RunLease{No: 987654, Attempt: 1}, ingest.Intake{Reader: pgiAll(), Plan: plan}); !pgiCodeIs(err, ingest.CodeRunLeaseLost) {
		t.Errorf("a run that is not there: %v", err)
	}
	if _, err := e.st.Receive(e.ctx, lease, ingest.Intake{Reader: pgiAll(), Plan: plan}); !pgiCodeIs(err, kernel.CodeInvalidValue) {
		t.Errorf("a dataset the source was not put to the KG with: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM sync_run WHERE stage = 'receiving'`); n != 1 {
		t.Errorf("the run is not receiving any more: %d", n)
	}
}

// [REQ-700]
func TestPgiEveryCallWithAContextThatHasEndedFailsAndChangesNothing(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	done, cancel := context.WithCancel(context.Background())
	cancel()
	calls := map[string]func() error{
		"OpenRun": func() error {
			_, err := e.st.OpenRun(done, ingest.RunOpen{KG: kg.ID, Source: plan.Source.ID, Owner: "o", Term: time.Minute})
			return err
		},
		"AcquireRun":   func() error { _, err := e.st.AcquireRun(done, lease.No, "o", time.Minute); return err },
		"RenewRun":     func() error { return e.st.RenewRun(done, lease) },
		"ReceivePlace": func() error { _, err := e.st.ReceivePlace(done, lease.No); return err },
		"AdvanceRun":   func() error { return e.st.AdvanceRun(done, lease, ingest.RunReceiving, ingest.RunComputing) },
		"EndRun":       func() error { return e.st.EndRun(done, lease, ingest.RunEnd{Stage: ingest.RunCanceled}) },
		"AttachSource": func() error { _, err := e.st.AttachSource(done, kg.ID, "src_x", []string{"d"}); return err },
		"DetachSource": func() error { return e.st.DetachSource(done, kg.ID, plan.Source.ID) },
		"Receive": func() error {
			_, err := e.st.Receive(done, lease, ingest.Intake{Reader: pgiAll(), Plan: plan})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s: a call with a context that had ended went through", name)
		}
	}
	if got := e.text(t, `SELECT stage || ' ' || attempt FROM sync_run WHERE run_no = $1`, lease.No); got != "receiving 1" {
		t.Errorf("the run was changed: %s", got)
	}
	if n := e.count(t, `SELECT count(*) FROM source_state`); n != 1 {
		t.Errorf("%d sources", n)
	}
}
