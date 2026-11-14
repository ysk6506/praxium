package readcheck

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/query"
)

// The scenarios run on one generated world (seed 7, scale 12000). What each answer must be is
// derived from the structure of the truth, never by asking the engine twice.
const (
	itSeed  = 7
	itScale = 12000
)

// itRack is a rack of the truth with what it holds.
type itRack struct {
	id       string
	pdu      [2]string // feed side a and b
	tor      [2]string
	servers  []string
	mlag     string
	hasArray bool
}

// racks lists the racks of the truth with servers, their two PDUs and two ToR switches.
func (tr *itTruth) itRacks() []itRack {
	var out []itRack
	for _, id := range tr.itOfTypeT("ot.rack") {
		r := itRack{id: id}
		for _, p := range tr.itInFrom(id, "lt.mounted_in") {
			switch tr.objs[p].Type {
			case "ot.rack_pdu":
				r.pdu[tr.itSideOf(p)] = p
			case "ot.switch":
				r.tor[tr.itSideOf(p)] = p
			case "ot.physical_server":
				r.servers = append(r.servers, p)
			case "ot.storage_array":
				r.hasArray = true
			}
		}
		if len(r.servers) > 0 && r.pdu[0] != "" && r.pdu[1] != "" && r.tor[0] != "" {
			for _, m := range tr.itOutTo(r.tor[0], "lt.member_of") {
				r.mlag = m
			}
			out = append(out, r)
		}
	}
	return out
}

// sideOf is 0 for an object on feed side a and 1 for b: a PDU by the side its link up names, a ToR switch by the PDU that powers it.
func (tr *itTruth) itSideOf(id string) int {
	o := tr.objs[id]
	for _, l := range tr.out[id] {
		if l.Type == "lt.powered_by" && tr.objs[l.To].Type == "ot.rack_pdu" {
			return tr.itSideOf(l.To)
		}
		if l.Type == "lt.powered_by" && l.Props["feed_side"] == "b" {
			return 1
		}
	}
	if o.Type == "ot.rack_pdu" {
		for _, l := range tr.out[id] {
			if l.Type == "lt.powered_by" && l.Props["feed_side"] == "b" {
				return 1
			}
		}
	}
	return 0
}

// plain returns the first rack that has no storage array.
func (tr *itTruth) itPlain(skip int) itRack {
	n := 0
	for _, r := range tr.itRacks() {
		if !r.hasArray {
			if n == skip {
				return r
			}
			n++
		}
	}
	return itRack{}
}

// (a) A/B dual power: one PDU leaves the servers without redundancy, a single-power device goes down.
//
// [REQ-300] [REQ-301]
func TestImpactOneRackPduExposesTheDualFedServersAndDownsTheSinglyFedToRSwitch(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	rk := tr.itPlain(0)
	if rk.id == "" || len(rk.servers) == 0 {
		t.Fatal("no rack with servers")
	}
	// from the truth: every server is powered through a group of two power supplies, one on each PDU
	for _, s := range rk.servers {
		groups := tr.itOutTo(s, "lt.powered_by")
		if len(groups) != 1 || tr.objs[groups[0]].Type != "ot.power_redundancy_group" || len(tr.itInFrom(groups[0], "lt.member_of")) != 2 {
			t.Fatalf("server %s is not fed through a group of two", tr.objs[s].itName())
		}
	}
	if got := tr.itOutTo(rk.tor[0], "lt.powered_by"); len(got) != 1 || got[0] != rk.pdu[0] {
		t.Fatalf("the ToR switch of side a is not fed by the PDU of side a alone: %v", got)
	}
	a := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, rk.pdu[0]), MaxItems: 100000})
	for name, sec := range map[string]*query.ImpactSection{"facts_only": a.Data.FactsOnly, "with_assumptions": a.Data.WithAssumptions} {
		got := itNamed(sec)
		if got[tr.objs[rk.tor[0]].itName()] != query.ResultDown {
			t.Fatalf("%s: the ToR switch fed by the PDU alone: %q", name, got[tr.objs[rk.tor[0]].itName()])
		}
		for _, s := range rk.servers {
			res := got[tr.objs[s].itName()]
			if res == query.ResultDown || res == query.ResultInterrupted {
				t.Fatalf("%s: a dual-fed server %s is %s after one PDU", name, tr.objs[s].itName(), res)
			}
			if res != query.ResultExposedDirect {
				t.Fatalf("%s: server %s should be exposed (redundancy lost), is %q", name, tr.objs[s].itName(), res)
			}
		}
		if got[tr.objs[rk.tor[1]].itName()] != "" {
			t.Fatalf("%s: the ToR switch of the other side is affected: %q", name, got[tr.objs[rk.tor[1]].itName()])
		}
	}
}

// (b) Both PDUs of a rack: the servers go down and the failure goes on to hosts, VMs and what runs on them.
//
// [REQ-300] [REQ-301]
func TestImpactBothRackPdusTakeTheServersDownAndItReachesHostsAndVMs(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	rk := tr.itPlain(3)
	a := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, rk.pdu[0], rk.pdu[1]), MaxItems: 100000})
	got := itNamed(a.Data.WithAssumptions)
	var hosts, vms int
	for _, s := range rk.servers {
		if got[tr.objs[s].itName()] != query.ResultDown {
			t.Fatalf("server %s: %q", tr.objs[s].itName(), got[tr.objs[s].itName()])
		}
		for _, h := range tr.itInFrom(s, "lt.runs_on") {
			if tr.objs[h].Type != "ot.hypervisor_host" {
				continue
			}
			hosts++
			if r := got[tr.objs[h].itName()]; r != query.ResultDown && r != query.ResultInterrupted {
				t.Fatalf("host %s on a dead server: %q", tr.objs[h].itName(), r)
			}
			for _, vm := range tr.itInFrom(h, "lt.runs_on") {
				if tr.objs[vm].Type != "ot.virtual_machine" {
					continue
				}
				vms++
				if r := got[tr.objs[vm].itName()]; r != query.ResultDown && r != query.ResultInterrupted {
					t.Fatalf("VM %s on a dead host: %q", tr.objs[vm].itName(), r)
				}
			}
		}
	}
	if hosts == 0 || vms == 0 {
		t.Fatalf("the rack has %d hosts and %d VMs: the scenario needs both", hosts, vms)
	}
	types := map[string]int{}
	for _, c := range a.Data.WithAssumptions.CountsByType {
		types[c.ObjectType] = c.Count
	}
	if types["Container"] == 0 || types["PhysicalServer"] != len(rk.servers) {
		t.Fatalf("counts by type: %v", types)
	}
}

// (c) The MLAG pair of a rack: one ToR switch leaves the servers exposed, both take their network away.
//
// [REQ-300] [REQ-301]
func TestImpactMlagPairOfARackLosesOneSwitchAndThenBoth(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	rk := tr.itPlain(1)
	if rk.mlag == "" || len(tr.itInFrom(rk.mlag, "lt.member_of")) != 2 {
		t.Fatal("the rack has no MLAG pair")
	}
	one := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, rk.tor[0]), Mode: query.ModeWithAssumptions, MaxItems: 100000})
	got := itNamed(one.Data.WithAssumptions)
	for _, s := range rk.servers {
		if got[tr.objs[s].itName()] != query.ResultExposedDirect {
			t.Fatalf("server %s with one of two switches: %q", tr.objs[s].itName(), got[tr.objs[s].itName()])
		}
	}
	var touched bool
	for _, g := range one.Data.WithAssumptions.GroupsTouched {
		if g.DisplayName == tr.objs[rk.mlag].itName() {
			touched = g.Value == "up" && g.Alive == 1 && g.N == 2 && g.Slack != nil && *g.Slack == 0
		}
	}
	if !touched {
		t.Fatalf("the MLAG domain is not reported with one of two alive: %+v", one.Data.WithAssumptions.GroupsTouched)
	}
	both := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, rk.tor[0], rk.tor[1]), Mode: query.ModeWithAssumptions, MaxItems: 100000})
	got = itNamed(both.Data.WithAssumptions)
	if got[tr.objs[rk.mlag].itName()] != query.ResultDown {
		t.Fatalf("the MLAG domain with both switches dead: %q", got[tr.objs[rk.mlag].itName()])
	}
	for _, it := range both.Data.WithAssumptions.Items {
		if it.ObjectType != "PhysicalServer" {
			continue
		}
		if it.Result != query.ResultDown || !itContains(it.LostChannels, "network") {
			t.Fatalf("server %s: %s, lost %v", it.DisplayName, it.Result, it.LostChannels)
		}
	}
}

func itContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// (d) A group of fans that needs all but one: one fan leaves no slack, two take the server down.
//
// [REQ-301]
func TestImpactFanGroupOfAServerNeedsAllButOneFan(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	rk := tr.itPlain(2)
	srv := rk.servers[0]
	groups := tr.itOutTo(srv, "lt.cooled_by")
	if len(groups) != 1 || tr.objs[groups[0]].Type != "ot.cooling_redundancy_group" {
		t.Fatalf("the server is not cooled by a group: %v", groups)
	}
	fans := tr.itInFrom(groups[0], "lt.member_of")
	k := int(tr.objs[groups[0]].Props["min_alive"].(float64))
	if len(fans) != k+1 {
		t.Fatalf("%d fans and min_alive %d: not N+1", len(fans), k)
	}
	one := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, fans[0]), Mode: query.ModeWithAssumptions, MaxItems: 100000})
	got := itNamed(one.Data.WithAssumptions)
	if got[tr.objs[srv].itName()] != query.ResultExposedDirect || got[tr.objs[groups[0]].itName()] != "" {
		t.Fatalf("one fan: server %q group %q", got[tr.objs[srv].itName()], got[tr.objs[groups[0]].itName()])
	}
	two := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, fans[0], fans[1]), Mode: query.ModeWithAssumptions, MaxItems: 100000})
	got = itNamed(two.Data.WithAssumptions)
	if got[tr.objs[srv].itName()] != query.ResultDown || got[tr.objs[groups[0]].itName()] != query.ResultDown {
		t.Fatalf("two fans: server %q group %q", got[tr.objs[srv].itName()], got[tr.objs[groups[0]].itName()])
	}
}

// (e) One host of a cluster: the VMs on it are interrupted (the cluster can take them over), the others are not touched.
//
// [REQ-301]
func TestImpactOneHostOfAClusterOnlyInterruptsItsVMs(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	var host, cluster string
	for _, id := range tr.itOfTypeT("ot.hypervisor_host") {
		for _, c := range tr.itOutTo(id, "lt.member_of") {
			if tr.objs[c].Type == "ot.compute_cluster" && len(tr.itInFrom(c, "lt.member_of")) > 2 {
				var vms int
				for _, v := range tr.itInFrom(id, "lt.runs_on") {
					if tr.objs[v].Type == "ot.virtual_machine" {
						vms++
					}
				}
				if vms > 0 && host == "" {
					host, cluster = id, c
				}
			}
		}
	}
	if host == "" {
		t.Fatal("no host with VMs in a cluster of more than two hosts")
	}
	a := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, host), Mode: query.ModeWithAssumptions, MaxItems: 100000})
	got := itNamed(a.Data.WithAssumptions)
	wantOthers := query.ResultDegraded
	if members, k := len(tr.itInFrom(cluster, "lt.member_of")), int(tr.objs[cluster].Props["min_alive"].(float64)); members-1 == k {
		wantOthers = query.ResultExposedDirect
	}
	var vmsOnIt int
	for _, v := range tr.itInFrom(host, "lt.runs_on") {
		if tr.objs[v].Type == "ot.virtual_machine" {
			vmsOnIt++
			if got[tr.objs[v].itName()] != query.ResultInterrupted {
				t.Fatalf("VM %s: %q", tr.objs[v].itName(), got[tr.objs[v].itName()])
			}
		}
	}
	for _, other := range tr.itInFrom(cluster, "lt.member_of") {
		if other == host {
			continue
		}
		for _, v := range tr.itInFrom(other, "lt.runs_on") {
			// they depend on the cluster to fail over to, which lost one member: no slack left means exposed, some left means degraded
			if tr.objs[v].Type == "ot.virtual_machine" && got[tr.objs[v].itName()] != wantOthers {
				t.Fatalf("VM %s on another host of the cluster: %q, want %q", tr.objs[v].itName(), got[tr.objs[v].itName()], wantOthers)
			}
		}
	}
	if a.Data.WithAssumptions.CountsByResult[query.ResultInterrupted] < vmsOnIt {
		t.Fatalf("counts: %v", a.Data.WithAssumptions.CountsByResult)
	}
}

// (f) A soft part only degrades its whole; a hard part takes it down.
//
// [REQ-300]
func TestImpactADeadMemoryModuleDegradesTheServerAndADeadCPUTakesItDown(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	srv := tr.itPlain(0).servers[0]
	var mem, cpu bool
	for _, p := range tr.itInFrom(srv, "lt.part_of") {
		mem = mem || tr.objs[p].Type == "ot.memory_module"
		cpu = cpu || tr.objs[p].Type == "ot.cpu"
	}
	if !mem || !cpu {
		t.Fatal("the server has no memory module or CPU")
	}
	// parts have no names of their own: they are found through the server they are part of
	parts := map[string]string{}
	n, err := w.eng.Neighbors(w.ctx, query.NeighborsInput{Common: query.Common{KG: "main", Limit: query.MaxLimit, Shape: query.ShapeItems}, ObjectID: w.itIDOf(tr, srv), LinkTypes: []string{"PART_OF"}}, query.AllowAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, nb := range n.Data.Items {
		if _, has := parts[nb.PeerType]; !has {
			parts[nb.PeerType] = nb.PeerObjectID
		}
	}
	memID, cpuID := parts["MemoryModule"], parts["Cpu"]
	if memID == "" || cpuID == "" {
		t.Fatalf("parts not found: %v", parts)
	}
	soft := w.itImpact(query.ImpactInput{Dead: []string{memID}, MaxItems: 100000})
	hard := w.itImpact(query.ImpactInput{Dead: []string{cpuID}, MaxItems: 100000})
	for _, sec := range []*query.ImpactSection{soft.Data.FactsOnly, soft.Data.WithAssumptions} {
		if got := itNamed(sec)[tr.objs[srv].itName()]; got != query.ResultDegraded {
			t.Fatalf("a dead memory module: server %q", got)
		}
	}
	for _, sec := range []*query.ImpactSection{hard.Data.FactsOnly, hard.Data.WithAssumptions} {
		if got := itNamed(sec)[tr.objs[srv].itName()]; got != query.ResultDown {
			t.Fatalf("a dead CPU: server %q", got)
		}
	}
}
