package pgingest

import (
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The state of a run as the record of runs says it (ADR-0128): what the runner of the steps (I-7b) reads to know what to do next.

// stlStateOf reads the state of a run and fails the test on an error.
func stlStateOf(s *rdfSetup, t *testing.T, no int64) ingest.RunState {
	t.Helper()
	st, err := s.e.st.RunState(s.e.ctx, no)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// [REQ-988]
func TestStlTheStateOfARunSaysItsStageItsJobsAndWhichStepsAreDoneAsTheRunGoes(t *testing.T) {
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	if _, err := s.e.st.RunState(s.e.ctx, 999); !pgiCodeIs(err, kernel.CodeNotFound) {
		t.Errorf("the state of a run that is not there: %v", err)
	}
	texts, at := stlSteps()[0], stlAt(0)
	plans := rclPlans(s)
	l := s.e.open(t, s.kg, s.plan.Source.ID, "server-a")
	if st := stlStateOf(s, t, l.No); st.Stage != ingest.RunReceiving || st.Done != (ingest.RunStepsDone{}) || st.OwnsCalc {
		t.Errorf("a run that was opened: %+v", st)
	}
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan, Options: ingest.PageOptions{MaxRows: 2}}); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name string
		do   func()
		want ingest.RunStepsDone
	}{
		{"the comparison", func() { s.rdfRun(t, l, s.rdfStart(at, 0)) }, ingest.RunStepsDone{Receive: true, RawDiff: true}},
		{"the identification", func() {
			if _, err := ingest.RunIdentify(s.e.ctx, s.e.st, l, idnStart(s, 0, 1), ingest.NewPlanKeys(plans)); err != nil {
				t.Fatal(err)
			}
		}, ingest.RunStepsDone{Receive: true, RawDiff: true, Identify: true}},
		{"the expansion", func() { rcpExpand(s, t, l, false) }, ingest.RunStepsDone{Receive: true, RawDiff: true, Identify: true, Expand: true}},
		{"the objects", func() { rclObjects(s, t, l, plans, at.UTC().Format(time.RFC3339), 0) },
			ingest.RunStepsDone{Receive: true, RawDiff: true, Identify: true, Expand: true, Recompute: true}},
		{"the links", func() { rclDo(s, t, l, plans, 0) }, ingest.RunStepsDone{Receive: true, RawDiff: true, Identify: true, Expand: true, Recompute: true, Links: true}},
		{"the fifth step", func() { isoDo(s, t, l, plans, 0) },
			ingest.RunStepsDone{Receive: true, RawDiff: true, Identify: true, Expand: true, Recompute: true, Links: true, Isolate: true}},
	}
	for _, step := range steps {
		step.do()
		st := stlStateOf(s, t, l.No)
		if st.Stage != ingest.RunComputing || !st.OwnsCalc || st.Done != step.want || st.CalcJob == 0 {
			t.Errorf("after %s: %+v", step.name, st)
		}
	}
	stlSettling(s, t, l)
	stlGraph(s, t)
	if err := s.e.st.BeginSettle(s.e.ctx, l, stlStart(s, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.e.st.SettleBucket(s.e.ctx, l, ingest.SettleJobConfirm, 0); err != nil {
		t.Fatal(err)
	}
	if st := stlStateOf(s, t, l.No); st.Stage != ingest.RunSettling || st.SettleJob != ingest.SettleJobRefs || st.SettlePart != 0 {
		t.Errorf("a run that is cleaning: %+v", st)
	}
	if _, err := ingest.RunSettle(s.e.ctx, s.e.st, l, stlStart(s, 2), &stlConfirms{}); err != nil {
		t.Fatal(err)
	}
	st := stlStateOf(s, t, l.No)
	if st.Stage != ingest.RunSucceeded || st.OwnsCalc || st.Done != (ingest.RunStepsDone{Receive: true}) || st.SettleJob != ingest.SettleJobEnd+1 {
		t.Errorf("a run that was cleaned: %+v", st)
	}
	// the cleaning measured the tables it wrote
	if n := s.e.count(t, fmt.Sprintf(`SELECT count(*) FROM pg_stat_user_tables WHERE schemaname = 's_%d' AND relname IN ('record_ref', 'record_problem', 'record_wait') AND last_analyze IS NOT NULL`, s.ref.KGNo)); n != 3 {
		t.Errorf("%d of the tables the cleaning wrote were measured", n)
	}
}
