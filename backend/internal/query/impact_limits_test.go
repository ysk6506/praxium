package query

import (
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-304]
func TestImpactStopsAtMaxHopsAndSaysTheAnswerIsNotComplete(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pduA", "pduB"), Mode: ModeFactsOnly, MaxHops: 2})
	c := a.Data.Computation
	if c.Complete || c.StoppedBy == nil || *c.StoppedBy != StopMaxHops || c.MaxHops != 2 || c.Rounds != 2 {
		t.Fatalf("computation: %+v", c)
	}
	rc := a.Meta.ResultComplete
	if !rc.Truncated || rc.Reason == nil || *rc.Reason != StopMaxHops {
		t.Fatalf("result_complete: %+v", rc)
	}
	if _, has := itResults(a.Data.FactsOnly)["vm"]; has {
		t.Fatal("two rounds cannot reach the VM")
	}
	full := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pduA", "pduB"), Mode: ModeFactsOnly})
	if !full.Data.Computation.Complete || full.Meta.ResultComplete.Truncated {
		t.Fatalf("complete answer: %+v", full.Data.Computation)
	}
}

// [REQ-304]
func TestImpactStopsWhenTheTimeBudgetIsUsedUp(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	eng, err := New(r.store, r.reg, kernel.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	in := ImpactInput{Common: r.common(), Dead: tp.itIDs("pduA", "pduB"), Mode: ModeFactsOnly, TimeBudget: time.Nanosecond}
	a, err := eng.Impact(r.ctx, in, AllowAll())
	if err != nil {
		t.Fatal(err)
	}
	if a.Data.Computation.Complete || *a.Data.Computation.StoppedBy != StopTimeBudget || *a.Meta.ResultComplete.Reason != StopTimeBudget {
		t.Fatalf("computation: %+v %+v", a.Data.Computation, a.Meta.ResultComplete)
	}
	up := ImpactInput{Common: r.common(), Direction: DirectionUpstream, TargetObjectID: tp.itID("vm"), TimeBudget: time.Nanosecond}
	u, err := eng.Impact(r.ctx, up, AllowAll())
	if err != nil || u.Data.Computation.Complete || *u.Meta.ResultComplete.Reason != StopTimeBudget {
		t.Fatalf("upstream: %v %+v", err, u.Meta.ResultComplete)
	}
}

// [REQ-304]
func TestImpactKeepsAtMostMaxItemsAndSaysSo(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pduA", "pduB"), Mode: ModeFactsOnly, MaxItems: 3})
	sec := a.Data.FactsOnly
	if len(sec.Items) != 3 || !a.Data.Computation.ItemsOverCap || a.Data.Computation.MaxItems != 3 {
		t.Fatalf("items %d: %+v", len(sec.Items), a.Data.Computation)
	}
	rc := a.Meta.ResultComplete
	if !rc.Truncated || *rc.Reason != "size_budget" || rc.Total != 7 || rc.Returned != 3 || rc.NextCursor != nil {
		t.Fatalf("result_complete: %+v", rc)
	}
	var sum int
	for _, n := range sec.CountsByResult {
		sum += n
	}
	if sum != 7 {
		t.Fatalf("the counts still cover everything: %v", sec.CountsByResult)
	}
}

// [REQ-304]
func TestImpactPagesInTheOrderOfSeverityAndOtherQueriesCursorsAreRefused(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	in := ImpactInput{Dead: tp.itIDs("pduA"), Mode: ModeBoth}
	var got []string
	for pages := 0; ; pages++ {
		in.Common = r.common(func(c *Common) { c.Limit = 2; c.Cursor = in.Cursor })
		a, err := r.eng.Impact(r.ctx, in, AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range a.Data.FactsOnly.Items {
			got = append(got, it.DisplayName)
		}
		if a.Meta.ResultComplete.NextCursor == nil {
			break
		}
		if pages > 10 {
			t.Fatal("too many pages")
		}
		in.Cursor = *a.Meta.ResultComplete.NextCursor
	}
	whole := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pduA"), Mode: ModeBoth})
	var want []string
	for _, it := range whole.Data.FactsOnly.Items {
		want = append(want, it.DisplayName)
	}
	if js(t, got) != js(t, want) || len(got) != 5 || want[0] != "dual-psu-a" && want[0] != "single" {
		t.Fatalf("pages %v, whole %v", got, want)
	}
	cur := in.Cursor
	in = ImpactInput{Common: r.common(func(c *Common) { c.Limit = 2; c.Cursor = cur }), Dead: tp.itIDs("pduB")}
	if _, err := r.eng.Impact(r.ctx, in, AllowAll()); codeOf(err) != CodeCursorInvalid {
		t.Fatalf("a cursor of another question: %v", err)
	}
}

// [REQ-304]
func TestImpactRefusesWhatItCannotAnswer(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	ok := tp.itID("pduA")
	many := make([]string, MaxSeeds+1)
	for i := range many {
		many[i] = ok
	}
	cases := []struct {
		name  string
		in    ImpactInput
		code  kernel.Code
		field string
	}{
		{"no seed", ImpactInput{}, CodeInput, "dead"},
		{"direction", ImpactInput{Direction: "sideways", Dead: []string{ok}}, CodeInput, "direction"},
		{"mode", ImpactInput{Dead: []string{ok}, Mode: "maybe"}, CodeInput, "mode"},
		{"too many seeds", ImpactInput{Dead: many}, CodeInput, "dead"},
		{"hops", ImpactInput{Dead: []string{ok}, MaxHops: -1}, CodeInput, "max_hops"},
		{"items low", ImpactInput{Dead: []string{ok}, MaxItems: -1}, CodeInput, "max_items"},
		{"items high", ImpactInput{Dead: []string{ok}, MaxItems: MaxItemsCap + 1}, CodeInput, "max_items"},
		{"budget", ImpactInput{Dead: []string{ok}, TimeBudget: -time.Second}, CodeInput, "time_budget"},
		{"scope", ImpactInput{DeadScopeID: "scope_1", Dead: []string{ok}}, CodeScopeUnsupported, ""},
		{"not an id", ImpactInput{Dead: []string{"nope"}}, CodeObjectNotFound, ""},
		{"no such object", ImpactInput{Dead: []string{tp.itID("pduA")[:len(ok)-1] + "0"}}, CodeObjectNotFound, ""},
		{"link id", ImpactInput{DeadLinks: []string{"nope"}}, CodeInput, "dead_links"},
		{"no such link", ImpactInput{DeadLinks: []string{tp.itLinkID("POWERED_BY", "single", "pduB")}}, CodeInput, "dead_links"},
		{"target on downstream", ImpactInput{Dead: []string{ok}, TargetObjectID: ok}, CodeInput, "target_object_id"},
		{"upstream without target", ImpactInput{Direction: DirectionUpstream}, CodeInput, "target_object_id"},
		{"upstream with dead", ImpactInput{Direction: DirectionUpstream, TargetObjectID: ok, Dead: []string{ok}}, CodeInput, "dead"},
		{"upstream target", ImpactInput{Direction: DirectionUpstream, TargetObjectID: "nope"}, CodeObjectNotFound, ""},
		{"shape", ImpactInput{Common: Common{KG: "main", Shape: "tree"}, Dead: []string{ok}}, CodeInput, "shape"},
	}
	for _, tc := range cases {
		in := tc.in
		if in.KG == "" {
			in.Common = r.common()
		}
		_, err := r.eng.Impact(r.ctx, in, AllowAll())
		if codeOf(err) != tc.code || FieldOf(err) != tc.field {
			t.Errorf("%s: code %q field %q (%v), want %q %q", tc.name, codeOf(err), FieldOf(err), err, tc.code, tc.field)
		}
	}
}

// [REQ-305]
func TestImpactUpstreamListsWhatTheTargetDependsOnAndWhichOnesAreSinglePoints(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	a := itImpactOf(r, ImpactInput{Direction: DirectionUpstream, TargetObjectID: tp.itID("vm"), Mode: ModeFactsOnly})
	up := a.Data.Upstream
	if a.Data.Direction != DirectionUpstream || a.Data.Target.DisplayName != "vm" || up == nil || up.Total != 7 || up.SinglePoints != 3 {
		t.Fatalf("upstream: %+v", a.Data)
	}
	effects := map[string]string{}
	spof := map[string]bool{}
	hops := map[string]int{}
	for _, it := range up.Items {
		effects[it.DisplayName] = it.Effects[ModeFactsOnly]
		spof[it.DisplayName] = it.SinglePointOfFailure
		hops[it.DisplayName] = it.Hops
		if len(it.Path) == 0 || it.Path[len(it.Path)-1].DisplayName != "vm" || it.Path[0].DisplayName != it.DisplayName {
			t.Fatalf("%s: the path runs from the dependency to the target: %+v", it.DisplayName, it.Path)
		}
	}
	wantEffects := map[string]string{"host": ResultDown, "dual": ResultDown, "dual-pg": ResultDown, "dual-psu-a": ResultExposedInherited,
		"dual-psu-b": ResultExposedInherited, "pduA": ResultExposedInherited, "pduB": ResultExposedInherited}
	if js(t, effects) != js(t, wantEffects) {
		t.Fatalf("effects: %v", effects)
	}
	if !spof["host"] || !spof["dual"] || !spof["dual-pg"] || spof["pduA"] || hops["host"] != 1 || hops["pduA"] != 5 {
		t.Fatalf("single points %v hops %v", spof, hops)
	}
	if up.CountsByEffect[ResultDown] != 3 || up.Items[0].DisplayName != "host" && up.Items[0].DisplayName != "dual" && up.Items[0].DisplayName != "dual-pg" {
		t.Fatalf("counts %v first %s", up.CountsByEffect, up.Items[0].DisplayName)
	}
}

// [REQ-305]
func TestImpactUpstreamWithAnAssumedLinkNamesTheSinglePointOnlyWithAssumptions(t *testing.T) {
	r := newRig(t, false)
	tp := itWeakPower(r, model.GradeAssumed)
	a := itImpactOf(r, ImpactInput{Direction: DirectionUpstream, TargetObjectID: tp.itID("host")})
	for _, it := range a.Data.Upstream.Items {
		if it.DisplayName == "pdu" && (it.Effects[ModeFactsOnly] != ResultUnknown || it.Effects[ModeWithAssumptions] != ResultDown || !it.AssumptionBased) {
			t.Fatalf("pdu: %+v", it)
		}
	}
}

// [REQ-305]
func TestImpactUpstreamPagesAndCapsTheDependenciesItTries(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	in := ImpactInput{Direction: DirectionUpstream, TargetObjectID: tp.itID("vm"), Mode: ModeFactsOnly}
	in.Common = r.common(func(c *Common) { c.Limit = 3 })
	first, err := r.eng.Impact(r.ctx, in, AllowAll())
	if err != nil || first.Meta.ResultComplete.NextCursor == nil || len(first.Data.Upstream.Items) != 3 || *first.Meta.ResultComplete.Reason != "limit" {
		t.Fatalf("first page: %v %+v", err, first.Meta.ResultComplete)
	}
	in.Common = r.common(func(c *Common) { c.Limit = 3; c.Cursor = *first.Meta.ResultComplete.NextCursor })
	next, err := r.eng.Impact(r.ctx, in, AllowAll())
	if err != nil || len(next.Data.Upstream.Items) != 3 || next.Data.Upstream.Items[0].DisplayName == first.Data.Upstream.Items[0].DisplayName {
		t.Fatalf("second page: %v", err)
	}
	in.Common = r.common(func(c *Common) { c.Shape = ShapeSummary })
	sum, err := r.eng.Impact(r.ctx, in, AllowAll())
	if err != nil || sum.Data.Upstream.Items != nil || sum.Data.Upstream.Total != 7 {
		t.Fatalf("summary: %v %+v", err, sum.Data.Upstream)
	}
}
