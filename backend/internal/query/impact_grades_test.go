package query

import (
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// [REQ-300]
func TestImpactASoftRequirementOnlyDegradesWhileAHardOneBringsTheObjectDown(t *testing.T) {
	r := newRig(t, false)
	tp := newItTopo(r)
	tp.itAdd("Switch", "swA", "")
	tp.itAdd("Lag", "peer", "swA")
	tp.itAdd("Switch", "swB", "")
	tp.itAddSet("MlagDomain", "mlag", []string{"swA", "swB"}, itFact("min_alive", int64(1)))
	tp.itFactLink("MEMBER_OF", "swA", "mlag")
	tp.itFactLink("MEMBER_OF", "swB", "mlag")
	tp.itFactLink("USES_PEER_LINK", "mlag", "peer")
	tp.itLoad()
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("peer"), Mode: ModeFactsOnly})
	itWant(t, itResults(a.Data.FactsOnly), map[string]string{"mlag": ResultDegraded})
}

// [REQ-304]
func TestImpactAnDependencyCycleEndsAndKeepsEveryObjectOnIt(t *testing.T) {
	r := newRig(t, false)
	tp := newItTopo(r)
	for _, s := range []string{"svc1", "svc2", "svc3"} {
		tp.itAdd("ApplicationService", s, "")
	}
	tp.itFactLink("DEPENDS_ON", "svc1", "svc2")
	tp.itFactLink("DEPENDS_ON", "svc2", "svc1")
	tp.itFactLink("DEPENDS_ON", "svc3", "svc1")
	tp.itLoad()
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("svc2"), Mode: ModeFactsOnly})
	itWant(t, itResults(a.Data.FactsOnly), map[string]string{"svc1": ResultDown, "svc3": ResultDown})
	if !a.Data.Computation.Complete || a.Data.Computation.StoppedBy != nil {
		t.Fatalf("computation: %+v", a.Data.Computation)
	}
	up := itImpactOf(r, ImpactInput{Direction: DirectionUpstream, TargetObjectID: tp.itID("svc3"), Mode: ModeFactsOnly})
	if up.Data.Upstream == nil || up.Data.Upstream.Total != 2 {
		t.Fatalf("upstream of a cycle: %+v", up.Data.Upstream)
	}
}

// itWeakPower is a server on a PDU through a link of the given grade.
func itWeakPower(r *rig, grade model.Grade) *itTopo {
	tp := newItTopo(r)
	tp.itAdd("RackPdu", "pdu", "")
	tp.itAdd("PhysicalServer", "srv", "")
	tp.itLink("POWERED_BY", "srv", "pdu", grade)
	tp.itAdd("HypervisorHost", "host", "")
	tp.itFactLink("RUNS_ON", "host", "srv")
	tp.itLoad()
	return tp
}

// [REQ-302]
func TestImpactAnAssumedLinkCarriesAFailureOnlyIntoTheAnswerWithAssumptions(t *testing.T) {
	r := newRig(t, false)
	tp := itWeakPower(r, model.GradeAssumed)
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pdu")})
	itWant(t, itResults(a.Data.FactsOnly), map[string]string{"srv": ResultUnknown, "host": ResultUnknown})
	itWant(t, itResults(a.Data.WithAssumptions), map[string]string{"srv": ResultDown, "host": ResultDown})
	if a.Data.Computation.WeakLinks != 1 {
		t.Fatalf("weak links: %d", a.Data.Computation.WeakLinks)
	}
	for _, it := range a.Data.WithAssumptions.Items {
		if !it.AssumptionBased || it.WeakestGrade != string(model.GradeAssumed) {
			t.Fatalf("%s: %+v", it.DisplayName, it)
		}
	}
	if a.Data.FactsOnly.CountsByResult[ResultDown] != 0 {
		t.Fatalf("an assumed link must not take anything down in the facts: %v", a.Data.FactsOnly.CountsByResult)
	}
}

// [REQ-302]
func TestImpactALinkOfUnknownGradeGivesUnknownInBothAnswers(t *testing.T) {
	r := newRig(t, false)
	tp := itWeakPower(r, model.GradeUnknown)
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pdu")})
	want := map[string]string{"srv": ResultUnknown, "host": ResultUnknown}
	itWant(t, itResults(a.Data.FactsOnly), want)
	itWant(t, itResults(a.Data.WithAssumptions), want)
}

// [REQ-302]
func TestImpactAnObservedLinkOlderThanMaxStalenessIsNotUsed(t *testing.T) {
	r := newRig(t, false)
	tp := newItTopo(r)
	tp.itAdd("RackPdu", "pdu", "")
	tp.itAdd("PhysicalServer", "srv", "")
	lt, _ := r.reg.LinkTypeByName("POWERED_BY")
	var feed int
	for _, p := range lt.Properties {
		if p.Name == "feed_side" {
			feed = p.AttrNo
		}
	}
	tp.batch.Links = append(tp.batch.Links, graph.LinkUpsert{Type: lt.ID, Src: tp.ids["srv"], Dst: tp.ids["pdu"], Grade: model.GradeFact, Basis: model.BasisObserved,
		Attrs: map[int]graph.Attr{feed: {Value: "a", Grade: model.GradeFact, Basis: model.BasisObserved, SourceTime: "2030-01-01T10:00:00Z"}}})
	tp.itLoad()
	fresh := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pdu")})
	itWant(t, itResults(fresh.Data.FactsOnly), map[string]string{"srv": ResultDown})
	in := ImpactInput{Dead: tp.itIDs("pdu")}
	in.Common = r.common(func(c *Common) { c.MaxStaleness = time.Minute; c.Limit = 100 })
	stale, err := r.eng.Impact(r.ctx, in, AllowAll())
	if err != nil {
		t.Fatal(err)
	}
	itWant(t, itResults(stale.Data.FactsOnly), map[string]string{"srv": ResultUnknown})
	if stale.Data.Computation.StaleInputs != 1 || len(stale.Meta.Warnings) != 1 || stale.Meta.Warnings[0] != "stale_observation_not_used" {
		t.Fatalf("stale: %+v %v", stale.Data.Computation, stale.Meta.Warnings)
	}
	var note bool
	for _, it := range stale.Data.FactsOnly.Items {
		for _, st := range it.Path {
			note = note || st.Link != nil && st.Link.Note != ""
		}
	}
	if !note {
		t.Fatal("no path step says that the observation is old")
	}
}

// [REQ-303]
func TestImpactItemsSayWhichChannelNoLinkSupplies(t *testing.T) {
	r := newRig(t, false)
	tp := newItTopo(r)
	tp.itAdd("PhysicalServer", "srv", "")
	tp.itAdd("Cpu", "cpu", "srv")
	tp.itFactLink("PART_OF", "cpu", "srv")
	tp.itLoad()
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("cpu"), Mode: ModeFactsOnly})
	sec := a.Data.FactsOnly
	if len(sec.Items) != 1 || sec.Items[0].DisplayName != "srv" || sec.UnfilledChannelObjects != 1 {
		t.Fatalf("answer: %+v", sec)
	}
	if got := sec.Items[0].UnfilledChannels; len(got) == 0 {
		t.Fatalf("unfilled channels: %v", got)
	}
}

// [REQ-303]
func TestImpactHidesWhatTheCallerMayNotReadButKeepsTheRoute(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	in := ImpactInput{Dead: tp.itIDs("pduA", "pduB"), Mode: ModeFactsOnly}
	in.Common = r.common(func(c *Common) { c.Limit = 100 })
	a, err := r.eng.Impact(r.ctx, in, itAllBut(r, "HypervisorHost"))
	if err != nil {
		t.Fatal(err)
	}
	got := itResults(a.Data.FactsOnly)
	if _, has := got["host"]; has || got["vm"] != ResultDown {
		t.Fatalf("results: %v", got)
	}
	var hiddenStep bool
	for _, it := range a.Data.FactsOnly.Items {
		if it.DisplayName != "vm" {
			continue
		}
		if !it.ViaHidden || it.Via != nil {
			t.Fatalf("vm via: %+v", it)
		}
		for _, st := range it.Path {
			if st.Hidden {
				hiddenStep = st.ObjectID == nil && st.DisplayName == ""
			}
		}
	}
	if !hiddenStep || !a.Meta.Hidden.Reached {
		t.Fatalf("hidden: %+v", a.Meta.Hidden)
	}
	if _, err := r.eng.Impact(r.ctx, ImpactInput{Common: in.Common, Dead: []string{tp.itID("host")}}, itAllBut(r, "HypervisorHost")); codeOf(err) != CodeObjectNotFound {
		t.Fatalf("a seed the caller may not read: %v", err)
	}
}

// itAllBut is a read filter that lets a caller read every type of the model except the ones named.
func itAllBut(r *rig, hidden ...string) ReadFilter {
	skip := map[string]bool{}
	for _, h := range hidden {
		skip[h] = true
	}
	var types []Selector
	for _, ty := range r.reg.ObjectTypes() {
		if !skip[ty.Name] {
			types = append(types, Selector{Kind: "type", Ref: ty.Name})
		}
	}
	return NewGrants(r.reg, true, Grant{Types: types, Max: model.SensitivityConfidential})
}
