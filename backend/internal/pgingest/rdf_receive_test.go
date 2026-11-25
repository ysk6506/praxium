package pgingest

import (
	"encoding/json"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/ingest"
)

// [REQ-783]
func TestRdfTheCountsOfTheRunSayForEachDatasetHowMuchOfItTheNextStepMayCount(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	sink, err := e.st.newSink(e.ctx, lease, plan)
	if err != nil {
		t.Fatal(err)
	}
	rep := ingest.IntakeReport{Datasets: []ingest.DatasetIntake{
		{Dataset: "ds_places", Collected: true, Complete: true},
		{Dataset: "ds_servers", Collected: true, SeenOnly: true, Reason: "the source cannot tell"},
		{Dataset: "ds_vms", Collected: true, Complete: true, NotRead: "the header lacks a column"},
	}}
	if err := e.st.saveCounts(e.ctx, sink, rep); err != nil {
		t.Fatal(err)
	}
	var counts struct {
		Receive struct {
			Complete bool `json:"complete"`
			Datasets []struct {
				Dataset  string `json:"dataset"`
				Coverage string `json:"coverage"`
				Complete bool   `json:"complete"`
			} `json:"datasets"`
		} `json:"receive"`
	}
	if err := json.Unmarshal([]byte(e.text(t, `SELECT counts::text FROM sync_run WHERE run_no = $1`, lease.No)), &counts); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ds_places": "complete", "ds_servers": "seen_only", "ds_vms": "incomplete"}
	if len(counts.Receive.Datasets) != 3 || counts.Receive.Complete {
		t.Fatalf("%+v", counts)
	}
	for _, d := range counts.Receive.Datasets {
		if d.Coverage != want[d.Dataset] {
			t.Errorf("%s: %s, want %s", d.Dataset, d.Coverage, want[d.Dataset])
		}
	}
}

// [REQ-783]
func TestRdfTheComparisonReadsTheCoverageOfTheReceivingFromTheCountsAndTakesTheOldFlagWhereThereIsNone(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, pgiSourceJSON, pgiMappingJSON))
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	s.e.exec(t, `UPDATE sync_run SET counts = '{"receive":{"datasets":[{"dataset":"ds_places","complete":true},{"dataset":"ds_servers","complete":false},{"dataset":"ds_vms","complete":true,"not_read":"the header changed"}]}}' WHERE run_no = $1`, l.No)
	if err := s.e.st.AdvanceRun(s.e.ctx, l, ingest.RunReceiving, ingest.RunComputing); err != nil {
		t.Fatal(err)
	}
	plan, err := s.e.st.BeginRawDiff(s.e.ctx, l, s.rdfStart(rdfNow, 0))
	if err != nil {
		t.Fatal(err)
	}
	got := []ingest.Coverage{plan.Datasets[0].Coverage, plan.Datasets[1].Coverage, plan.Datasets[2].Coverage}
	if got[0] != ingest.CoverageComplete || got[1] != ingest.CoverageIncomplete || got[2] != ingest.CoverageIncomplete {
		t.Errorf("%v", got)
	}
}
