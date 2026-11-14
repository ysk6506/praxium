package query

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// itCooling builds a server cooled by a group of three fans that need two to run (N+1), and a switch
// pair that forms an MLAG domain the server uplinks to.
func itCooling(r *rig) *itTopo {
	tp := newItTopo(r)
	tp.itAdd("PhysicalServer", "srv", "")
	tp.itAdd("CoolingRedundancyGroup", "fans", "srv", itFact("min_alive", int64(2)))
	for _, f := range []string{"fan1", "fan2", "fan3"} {
		tp.itAdd("Fan", f, "srv")
		tp.itFactLink("MEMBER_OF", f, "fans")
	}
	tp.itFactLink("COOLED_BY", "srv", "fans")
	tp.itAdd("Switch", "swA", "")
	tp.itAdd("Switch", "swB", "")
	tp.itAddSet("MlagDomain", "mlag", []string{"swA", "swB"}, itFact("min_alive", int64(1)))
	tp.itFactLink("MEMBER_OF", "swA", "mlag")
	tp.itFactLink("MEMBER_OF", "swB", "mlag")
	tp.itFactLink("UPLINKED_VIA", "srv", "mlag")
	tp.itLoad()
	return tp
}

// [REQ-301]
func TestImpactCoolingGroupOfThreeFansNeedingTwo(t *testing.T) {
	r := newRig(t, false)
	tp := itCooling(r)
	one := itImpactOf(r, ImpactInput{Dead: tp.itIDs("fan1"), Mode: ModeFactsOnly})
	itWant(t, itResults(one.Data.FactsOnly), map[string]string{"srv": ResultExposedDirect})
	g := one.Data.FactsOnly.GroupsTouched[0]
	if g.Value != "up" || g.Alive != 2 || g.Slack == nil || *g.Slack != 0 || g.N != 3 {
		t.Fatalf("group: %+v", g)
	}
	two := itImpactOf(r, ImpactInput{Dead: tp.itIDs("fan1", "fan2"), Mode: ModeFactsOnly})
	itWant(t, itResults(two.Data.FactsOnly), map[string]string{"fans": ResultDown, "srv": ResultDown})
	if it := two.Data.FactsOnly.Items[0]; len(it.LostChannels) == 0 {
		t.Fatalf("lost channels of %s: %v", it.DisplayName, it.LostChannels)
	}
}

// [REQ-301]
func TestImpactMlagPairLosesOneSwitchThenBoth(t *testing.T) {
	r := newRig(t, false)
	tp := itCooling(r)
	one := itImpactOf(r, ImpactInput{Dead: tp.itIDs("swA"), Mode: ModeFactsOnly})
	got := itResults(one.Data.FactsOnly)
	if got["srv"] != ResultExposedDirect || len(got) != 1 {
		t.Fatalf("one switch: %v", got)
	}
	both := itImpactOf(r, ImpactInput{Dead: tp.itIDs("swA", "swB"), Mode: ModeFactsOnly})
	got = itResults(both.Data.FactsOnly)
	if got["srv"] != ResultDown || got["mlag"] != ResultDown {
		t.Fatalf("both switches: %v", got)
	}
}

// [REQ-301]
func TestImpactAGroupWithoutMinAliveIsOnlyDecidedWhenEveryValueAgrees(t *testing.T) {
	r := newRig(t, false)
	tp := newItTopo(r)
	tp.itAdd("PhysicalServer", "srv", "")
	tp.itAdd("CoolingRedundancyGroup", "fans", "srv")
	for _, f := range []string{"f1", "f2", "f3"} {
		tp.itAdd("Fan", f, "srv")
		tp.itFactLink("MEMBER_OF", f, "fans")
	}
	tp.itFactLink("COOLED_BY", "srv", "fans")
	tp.itLoad()
	// one of three failed: min_alive could be 1, 2 or 3 -> not decided
	one := itImpactOf(r, ImpactInput{Dead: tp.itIDs("f1"), Mode: ModeFactsOnly})
	itWant(t, itResults(one.Data.FactsOnly), map[string]string{"fans": ResultUnknown, "srv": ResultUnknown})
	if g := one.Data.FactsOnly.GroupsTouched[0]; g.MinAlive != nil || g.Slack != nil {
		t.Fatalf("a min_alive that is not known: %+v", g)
	}
	// all of them failed: every min_alive gives down
	all := itImpactOf(r, ImpactInput{Dead: tp.itIDs("f1", "f2", "f3"), Mode: ModeFactsOnly})
	itWant(t, itResults(all.Data.FactsOnly), map[string]string{"fans": ResultDown, "srv": ResultDown})
}

// [REQ-301] [REQ-302]
func TestImpactAnAssumedMinAliveIsBelievedOnlyWithAssumptions(t *testing.T) {
	r := newRig(t, false)
	tp := newItTopo(r)
	tp.itAdd("PhysicalServer", "srv", "")
	tp.itAdd("CoolingRedundancyGroup", "fans", "srv", itAssumed("min_alive", int64(2)))
	for _, f := range []string{"f1", "f2", "f3"} {
		tp.itAdd("Fan", f, "srv")
		tp.itFactLink("MEMBER_OF", f, "fans")
	}
	tp.itFactLink("COOLED_BY", "srv", "fans")
	tp.itLoad()
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("f1", "f2")})
	itWant(t, itResults(a.Data.FactsOnly), map[string]string{"fans": ResultUnknown, "srv": ResultUnknown})
	itWant(t, itResults(a.Data.WithAssumptions), map[string]string{"fans": ResultDown, "srv": ResultDown})
	for _, it := range a.Data.WithAssumptions.Items {
		if !it.AssumptionBased || it.WeakestGrade != string(model.GradeAssumed) {
			t.Fatalf("%s should stand on an assumption: %+v", it.DisplayName, it)
		}
	}
	only := itImpactOf(r, ImpactInput{Dead: tp.itIDs("f1", "f2"), Mode: ModeWithAssumptions})
	if only.Data.FactsOnly != nil || only.Data.WithAssumptions == nil {
		t.Fatal("with_assumptions gives only that reading")
	}
}

// itCluster builds three hosts of a pool with two VMs on the first one; the VMs may fail over to the pool.
func itCluster(r *rig, extra ...itAttr) *itTopo {
	tp := newItTopo(r)
	tp.itAdd("VirtualizationManager", "vmgr", "")
	attrs := append([]itAttr{itFact("min_alive", int64(1))}, extra...)
	tp.itAdd("ComputeCluster", "pool", "vmgr", attrs...)
	for _, h := range []string{"h1", "h2", "h3"} {
		tp.itAdd("HypervisorHost", h, "")
		tp.itFactLink("MEMBER_OF", h, "pool")
	}
	for _, v := range []string{"vm1", "vm2"} {
		tp.itAdd("VirtualMachine", v, "")
		tp.itFactLink("RUNS_ON", v, "h1")
	}
	tp.itLoad()
	return tp
}

// [REQ-301]
func TestImpactAHostOfAPoolThatCanFailOverOnlyInterruptsItsVMs(t *testing.T) {
	r := newRig(t, false)
	tp := itCluster(r)
	one := itImpactOf(r, ImpactInput{Dead: tp.itIDs("h1"), Mode: ModeFactsOnly})
	itWant(t, itResults(one.Data.FactsOnly), map[string]string{"vm1": ResultInterrupted, "vm2": ResultInterrupted})
	for _, it := range one.Data.FactsOnly.Items {
		var viaGroup bool
		for _, st := range it.Path {
			viaGroup = viaGroup || st.ViaGroup != nil && st.ViaGroup.DisplayName == "pool"
		}
		if !viaGroup {
			t.Fatalf("%s: the path does not name the failover group: %+v", it.DisplayName, it.Path)
		}
	}
	all := itImpactOf(r, ImpactInput{Dead: tp.itIDs("h1", "h2", "h3"), Mode: ModeFactsOnly})
	itWant(t, itResults(all.Data.FactsOnly), map[string]string{"pool": ResultDown, "vm1": ResultDown, "vm2": ResultDown})
}

// [REQ-301]
func TestImpactACapacityGroupIsUnknownWhenAMemberFailsAndSaysSo(t *testing.T) {
	r := newRig(t, false)
	tp := itCluster(r, itFact("threshold_kind", "capacity_sum"))
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("h2"), Mode: ModeFactsOnly})
	itWant(t, itResults(a.Data.FactsOnly), map[string]string{"pool": ResultUnknown})
	if len(a.Meta.Warnings) != 1 || a.Meta.Warnings[0] != "capacity_group_unknown" {
		t.Fatalf("warnings: %v", a.Meta.Warnings)
	}
	if g := a.Data.FactsOnly.GroupsTouched[0]; g.Mode != "capacity" || g.Slack != nil {
		t.Fatalf("group: %+v", g)
	}
	none := itImpactOf(r, ImpactInput{Dead: tp.itIDs("vm2"), Mode: ModeFactsOnly})
	if len(none.Data.FactsOnly.Items) != 0 || len(none.Meta.Warnings) != 0 {
		t.Fatalf("a VM that fails alone: %+v %v", none.Data.FactsOnly, none.Meta.Warnings)
	}
}
