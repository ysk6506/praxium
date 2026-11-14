package readcheck

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/query"
)

// (g) Dependency loops and a whole site: the calculation ends and keeps counting everything it reached.
//
// [REQ-304]
func TestImpactEndsOnAWholeSiteAndOnEveryPduAtOnce(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	site := tr.itOfTypeT("ot.site")[0]
	a := w.itImpact(query.ImpactInput{SiteFailure: w.itIDsOf(tr, site), Mode: query.ModeWithAssumptions, MaxItems: 20})
	c := a.Data.Computation
	if !c.Complete || c.StoppedBy != nil || c.Rounds < 3 {
		t.Fatalf("computation: %+v", c)
	}
	// every server of the world is contained in the site, so every one of them is in the counts
	if got := a.Data.WithAssumptions.CountsByResult[query.ResultDown]; got < len(tr.itOfTypeT("ot.physical_server")) {
		t.Fatalf("down: %d of %d servers", got, len(tr.itOfTypeT("ot.physical_server")))
	}
	all := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, tr.itOfTypeT("ot.rack_pdu")...), Mode: query.ModeFactsOnly, MaxItems: 20})
	if !all.Data.Computation.Complete {
		t.Fatalf("every PDU: %+v", all.Data.Computation)
	}
}

// (h) A link whose grade is assumed carries a failure only into the answer with assumptions, and the item says so.
//
// [REQ-302]
func TestImpactFailureAcrossAnAssumedLinkIsMarkedAndOnlyBelievedWithAssumptions(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	checked := 0
	for _, l := range tr.links {
		if l.Grade != "assumed" || l.Type != "lt.runs_on" || tr.objs[l.To].Type != "ot.hypervisor_host" || tr.objs[l.From].Type != "ot.virtual_machine" {
			continue
		}
		// the VM runs on exactly this host through exactly this link, so no other route reaches it
		if len(tr.itOutTo(l.From, "lt.runs_on")) != 1 {
			continue
		}
		a := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, l.To), MaxItems: 100000})
		name := tr.objs[l.From].itName()
		if r := itNamed(a.Data.FactsOnly)[name]; r == query.ResultDown || r == query.ResultInterrupted {
			t.Fatalf("facts only: the VM %s is %s across an assumed link", name, r)
		}
		var item *query.ImpactItem
		for i := range a.Data.WithAssumptions.Items {
			if a.Data.WithAssumptions.Items[i].DisplayName == name {
				item = &a.Data.WithAssumptions.Items[i]
			}
		}
		if item == nil || item.Result != query.ResultDown && item.Result != query.ResultInterrupted || !item.AssumptionBased || item.WeakestGrade != "assumed" {
			t.Fatalf("with assumptions, VM %s: %+v", name, item)
		}
		checked++
		if checked == 3 {
			break
		}
	}
	if checked == 0 {
		t.Fatal("the world has no assumed link from a VM to its host")
	}
}

// (i) The limits of a question are stated when they cut the answer, and the counts still cover everything.
//
// [REQ-304]
func TestImpactSaysWhenALimitCutTheAnswer(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	pdus := w.itIDsOf(tr, tr.itOfTypeT("ot.rack_pdu")...)
	full := w.itImpact(query.ImpactInput{Dead: pdus, Mode: query.ModeWithAssumptions, MaxItems: 100000})
	capped := w.itImpact(query.ImpactInput{Dead: pdus, Mode: query.ModeWithAssumptions, MaxItems: 50})
	if !capped.Data.Computation.ItemsOverCap || len(capped.Data.WithAssumptions.Items) != 50 || capped.Meta.ResultComplete.Total != full.Meta.ResultComplete.Total {
		t.Fatalf("capped: %+v", capped.Meta.ResultComplete)
	}
	if rc := capped.Meta.ResultComplete; !rc.Truncated || *rc.Reason != "size_budget" {
		t.Fatalf("result_complete: %+v", rc)
	}
	if js, jf := capped.Data.WithAssumptions.CountsByResult, full.Data.WithAssumptions.CountsByResult; len(js) != len(jf) {
		t.Fatalf("the counts differ: %v %v", js, jf)
	}
	hops := w.itImpact(query.ImpactInput{Dead: pdus, Mode: query.ModeWithAssumptions, MaxHops: 3, MaxItems: 100000})
	if hops.Data.Computation.Complete || *hops.Data.Computation.StoppedBy != query.StopMaxHops || hops.Data.Computation.Rounds != 3 {
		t.Fatalf("max_hops: %+v", hops.Data.Computation)
	}
	if hops.Meta.ResultComplete.Total >= full.Meta.ResultComplete.Total {
		t.Fatalf("three rounds reached %d of %d", hops.Meta.ResultComplete.Total, full.Meta.ResultComplete.Total)
	}
}

// (j) The HA pair of a storage array goes down with one rack PDU. The truth says why: the array is
// wired to both PDUs directly (two POWERED_BY links to the array, none through a group of power
// supplies), its controllers and its HA pair are parts of the array without a power path of their
// own, so in the model the two PDUs are in series for the array and the pair follows its whole.
// The generator makes the structure, the engine reads it right. A server of the same rack, fed
// through a group of two power supplies, stays up on the same failure.
//
// [REQ-300] [REQ-301]
func TestImpactHAPairOfAnArrayGoesDownWithOnePduBecauseTheArrayIsWiredToBothInSeries(t *testing.T) {
	w := loadImpactWorld(t, itSeed, itScale)
	tr := w.itTruthOf()
	var rk itRack
	for _, r := range tr.itRacks() {
		if r.hasArray {
			rk = r
			break
		}
	}
	if rk.id == "" {
		t.Fatal("no storage rack")
	}
	var array string
	for _, p := range tr.itInFrom(rk.id, "lt.mounted_in") {
		if tr.objs[p].Type == "ot.storage_array" {
			array = p
		}
	}
	// the power paths, from the truth: the array is fed by the two PDUs and by nothing else
	feeds := tr.itOutTo(array, "lt.powered_by")
	if len(feeds) != 2 || feeds[0] == feeds[1] {
		t.Fatalf("the array's feeds: %v", feeds)
	}
	for _, f := range feeds {
		if tr.objs[f].Type != "ot.rack_pdu" || f != rk.pdu[0] && f != rk.pdu[1] {
			t.Fatalf("a feed of the array that is not a PDU of its rack: %s", tr.objs[f].Type)
		}
	}
	ctls := tr.itInFrom(array, "lt.part_of")
	var controllers, pairs []string
	for _, c := range ctls {
		switch tr.objs[c].Type {
		case "ot.array_controller":
			controllers = append(controllers, c)
		case "ot.ha_pair":
			pairs = append(pairs, c)
		}
	}
	if len(controllers) != 2 || len(pairs) != 1 || len(tr.itInFrom(pairs[0], "lt.member_of")) != 2 {
		t.Fatalf("%d controllers, %d pairs", len(controllers), len(pairs))
	}
	for _, c := range controllers {
		if len(tr.itOutTo(c, "lt.powered_by")) != 0 || len(tr.itOutTo(c, "lt.part_of")) != 1 || tr.itOutTo(c, "lt.part_of")[0] != array {
			t.Fatalf("controller %s has a power path of its own or is part of something else", tr.objs[c].itName())
		}
	}
	// the engine, on each PDU alone: the array, both controllers and the pair go down; the dual-fed server does not
	for side := 0; side < 2; side++ {
		a := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, rk.pdu[side]), MaxItems: 100000})
		for name, sec := range map[string]*query.ImpactSection{"facts_only": a.Data.FactsOnly, "with_assumptions": a.Data.WithAssumptions} {
			got := itNamed(sec)
			for _, id := range append([]string{array, pairs[0]}, controllers...) {
				if got[tr.objs[id].itName()] != query.ResultDown {
					t.Fatalf("side %d, %s: %s is %q", side, name, tr.objs[id].itName(), got[tr.objs[id].itName()])
				}
			}
			if r := got[tr.objs[rk.servers[0]].itName()]; r == query.ResultDown {
				t.Fatalf("side %d, %s: the dual-fed server is down", side, name)
			}
		}
	}
	// the path of a controller shows the two steps: supply from the PDU to the array, then part follows whole
	a := w.itImpact(query.ImpactInput{Dead: w.itIDsOf(tr, rk.pdu[0]), Mode: query.ModeFactsOnly, MaxItems: 100000})
	for _, it := range a.Data.FactsOnly.Items {
		if it.ObjectType != "ArrayController" {
			continue
		}
		if len(it.Path) != 3 || it.Path[1].Link == nil || it.Path[1].Link.Role != "supply" || it.Path[2].Link == nil || it.Path[2].Link.Role != "part_follows_whole" || it.Path[1].ObjectType != "StorageArray" {
			t.Fatalf("path of %s: %+v", it.DisplayName, it.Path)
		}
	}
}
