package query

import "testing"

// itPower builds two PDUs, a dual-fed server through a 2N group of power supplies, a server
// that hangs on the A PDU alone, and a hypervisor host and a VM on the dual-fed one.
func itPower(r *rig) *itTopo {
	tp := newItTopo(r)
	tp.itAdd("RackPdu", "pduA", "")
	tp.itAdd("RackPdu", "pduB", "")
	tp.itAdd("PhysicalServer", "dual", "")
	tp.itAdd("PowerSupply", "dual-psu-a", "dual")
	tp.itAdd("PowerSupply", "dual-psu-b", "dual")
	tp.itAdd("PowerRedundancyGroup", "dual-pg", "dual", itFact("min_alive", int64(1)))
	tp.itFactLink("POWERED_BY", "dual-psu-a", "pduA")
	tp.itFactLink("POWERED_BY", "dual-psu-b", "pduB")
	tp.itFactLink("MEMBER_OF", "dual-psu-a", "dual-pg")
	tp.itFactLink("MEMBER_OF", "dual-psu-b", "dual-pg")
	tp.itFactLink("POWERED_BY", "dual", "dual-pg")
	tp.itAdd("PhysicalServer", "single", "")
	tp.itFactLink("POWERED_BY", "single", "pduA")
	tp.itAdd("HypervisorHost", "host", "")
	tp.itFactLink("RUNS_ON", "host", "dual")
	tp.itAdd("VirtualMachine", "vm", "")
	tp.itFactLink("RUNS_ON", "vm", "host")
	tp.itLoad()
	return tp
}

// [REQ-300] [REQ-301]
func TestImpactOnePduLeavesTheDualFedServerExposedAndTakesTheSingleOneDown(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	a := itImpactOf(r, ImpactInput{Dead: []string{tp.itID("pduA")}, Mode: ModeFactsOnly})
	itWant(t, itResults(a.Data.FactsOnly), map[string]string{"dual": ResultExposedDirect, "dual-psu-a": ResultDown, "host": ResultExposedInherited,
		"single": ResultDown, "vm": ResultExposedInherited})
	if a.Data.WithAssumptions != nil || a.Data.Direction != DirectionDownstream || len(a.Data.Seeds) != 1 || a.Data.Seeds[0].DisplayName != "pduA" {
		t.Fatalf("answer: %+v", a.Data)
	}
	sec := a.Data.FactsOnly
	if sec.CountsByResult[ResultDown] != 2 || sec.CountsByResult[ResultExposedDirect] != 1 || sec.CountsByResult[ResultExposedInherited] != 2 {
		t.Fatalf("counts: %v", sec.CountsByResult)
	}
	if len(sec.GroupsTouched) != 1 {
		t.Fatalf("groups: %+v", sec.GroupsTouched)
	}
	g := sec.GroupsTouched[0]
	if g.DisplayName != "dual-pg" || g.Value != "up" || g.Alive != 1 || g.N != 2 || g.MinAlive == nil || *g.MinAlive != 1 || g.Slack == nil || *g.Slack != 0 {
		t.Fatalf("group: %+v", g)
	}
}

// [REQ-300] [REQ-301]
func TestImpactBothPdusTakeTheDualFedServerDownAndTheDownSpreadsToHostAndVM(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	a := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pduA", "pduB"), Mode: ModeFactsOnly})
	itWant(t, itResults(a.Data.FactsOnly), map[string]string{"dual": ResultDown, "dual-pg": ResultDown, "dual-psu-a": ResultDown, "dual-psu-b": ResultDown,
		"host": ResultDown, "single": ResultDown, "vm": ResultDown})
	for _, it := range a.Data.FactsOnly.Items {
		if it.DisplayName == "vm" && (it.Hops != 5 || it.Via == nil || *it.Via != tp.itID("host") || len(it.Path) != 6) {
			t.Fatalf("vm: hops %d via %v path %d", it.Hops, it.Via, len(it.Path))
		}
	}
}

// [REQ-300]
func TestImpactOrderOfTheSeedsDoesNotChangeTheAnswer(t *testing.T) {
	r := newRig(t, false)
	tp := itPower(r)
	one := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pduA", "pduB")})
	two := itImpactOf(r, ImpactInput{Dead: tp.itIDs("pduB", "pduA", "pduA")})
	if js(t, one.Data.FactsOnly) != js(t, two.Data.FactsOnly) || js(t, one.Data.WithAssumptions) != js(t, two.Data.WithAssumptions) {
		t.Fatal("the answer depends on the order of the seeds")
	}
}

// [REQ-300]
func TestImpactADeadObjectFailsEverythingThatIsPartOfItAndAHardPartFailsTheWhole(t *testing.T) {
	r := newRig(t, false)
	tp := newItTopo(r)
	tp.itAdd("PhysicalServer", "srv", "")
	tp.itAdd("Cpu", "cpu0", "srv")
	tp.itAdd("MemoryModule", "dimm0", "srv")
	tp.itAdd("Bmc", "bmc", "srv")
	tp.itFactLink("PART_OF", "cpu0", "srv")
	tp.itFactLink("PART_OF", "dimm0", "srv")
	tp.itFactLink("PART_OF", "bmc", "srv")
	tp.itFactLink("MANAGED_BY", "srv", "bmc")
	tp.itLoad()
	cpu := itImpactOf(r, ImpactInput{Dead: []string{tp.itID("cpu0")}, Mode: ModeFactsOnly})
	itWant(t, itResults(cpu.Data.FactsOnly), map[string]string{"srv": ResultDown, "dimm0": ResultDown, "bmc": ResultDown})
	mem := itImpactOf(r, ImpactInput{Dead: []string{tp.itID("dimm0")}, Mode: ModeFactsOnly})
	itWant(t, itResults(mem.Data.FactsOnly), map[string]string{"srv": ResultDegraded})
	srv := itImpactOf(r, ImpactInput{Dead: []string{tp.itID("srv")}, Mode: ModeFactsOnly})
	itWant(t, itResults(srv.Data.FactsOnly), map[string]string{"cpu0": ResultDown, "dimm0": ResultDown, "bmc": ResultDown})
	// the management channel follows the manager, and the service of the server stays up
	bmc := itImpactOf(r, ImpactInput{Dead: []string{tp.itID("bmc")}, Mode: ModeFactsOnly})
	got := itResults(bmc.Data.FactsOnly)
	if got["srv"] != ResultUnmanageable {
		t.Fatalf("a server without its BMC: %v", got)
	}
}

// [REQ-300] [REQ-303]
func TestImpactAContainerThatFailsTakesWhatIsContainedInItAndLinkCutsAreAccepted(t *testing.T) {
	r := newRig(t, false)
	tp := newItTopo(r)
	tp.itAdd("Site", "site", "")
	tp.itAdd("Rack", "rack", "site")
	tp.itAdd("PhysicalServer", "srv", "")
	tp.itFactLink("LOCATED_IN", "rack", "site")
	mount := tp.itFactLink("MOUNTED_IN", "srv", "rack")
	tp.itAdd("RackPdu", "pdu", "")
	tp.itFactLink("MOUNTED_IN", "pdu", "rack")
	tp.itFactLink("POWERED_BY", "srv", "pdu")
	tp.itLoad()
	room := itImpactOf(r, ImpactInput{SiteFailure: []string{tp.itID("site")}, Mode: ModeFactsOnly})
	got := itResults(room.Data.FactsOnly)
	if got["rack"] != ResultDown || got["srv"] != ResultDown || got["pdu"] != ResultDown || len(room.Data.Seeds) != 1 {
		t.Fatalf("site failure: %v", got)
	}
	var viaContainment bool
	for _, it := range room.Data.FactsOnly.Items {
		for _, st := range it.Path {
			if st.Link != nil && st.Link.Role == "containment" {
				viaContainment = true
			}
		}
	}
	if !viaContainment {
		t.Fatal("no path step says containment")
	}
	// a cut link alone fails nothing that does not depend on it
	cut := itImpactOf(r, ImpactInput{DeadLinks: []string{mount.String()}, Mode: ModeFactsOnly})
	if len(cut.Data.Seeds) != 0 || len(cut.Data.FactsOnly.Items) != 0 {
		t.Fatalf("a cut container link: %+v", cut.Data.FactsOnly)
	}
	supply := itImpactOf(r, ImpactInput{DeadLinks: []string{tp.itLinkID("POWERED_BY", "srv", "pdu")}, Mode: ModeFactsOnly})
	itWant(t, itResults(supply.Data.FactsOnly), map[string]string{"srv": ResultDown})
}
