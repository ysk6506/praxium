package worldgen

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShapeCardOfAWorld(t *testing.T) {
	w := smallWorld(t, 41, 60000, func(p *Params) { p.Sites = 3; p.CrossSiteRatio = 0.2 })
	m, st := runAll(t, w)
	c := m.Shape
	if c.Format != ShapeFormat || c.Objects != m.Counts.Objects || c.Links != m.Counts.Links {
		t.Fatalf("%+v", c)
	}
	if c.OutDegree.Count != c.Objects || c.OutDegree.Mean < 1 || c.InDegree.Max < 10 {
		t.Fatalf("degrees: %+v %+v", c.OutDegree, c.InDegree)
	}
	if c.ContainmentDepth.Max < 5 || c.ServiceDepth.Max < 1 || c.ServiceDepth.Max >= w.P.ServiceDepth+3 {
		t.Fatalf("depth: %+v %+v", c.ContainmentDepth, c.ServiceDepth)
	}
	if c.CrossSite.CallRatio < 0.1 || c.CrossSite.CallRatio > 0.3 {
		t.Fatalf("call ratio %v for a parameter of 0.2", c.CrossSite.CallRatio)
	}
	if c.CrossSite.Ratio <= 0 || c.CrossSite.Ratio >= c.CrossSite.CallRatio {
		t.Fatalf("link ratio %v", c.CrossSite.Ratio)
	}
	for _, g := range []string{"EcmpGroup", "Lag", "PowerRedundancyGroup", "ComputeCluster", "LbPool", "HaPair", "RaidGroup"} {
		gs, ok := c.Groups[g]
		if !ok || gs.Groups == 0 || gs.Members.Count != gs.Groups || gs.Slack.Max < 0 {
			t.Errorf("group %s: %+v", g, gs)
		}
	}
	var back ShapeCard
	if err := json.Unmarshal([]byte(st.Text(ShapeFile)), &back); err != nil || back.Objects != c.Objects {
		t.Fatalf("the written card: %v", err)
	}
}

// A shape card sets what a parameter can say and lists what it cannot.
func TestApplyShape(t *testing.T) {
	card := &ShapeCard{ServiceCallsPerSvc: 2.5, ServiceDepth: Dist{Count: 5, Max: 6}, CrossSite: CrossSite{CallEdges: 100, CallRatio: 0.12}}
	p := DefaultParams(1, 10000)
	applied, unsupported := ApplyShape(card, &p)
	if len(applied) != 3 || p.CrossSiteRatio != 0.12 || p.ServiceDepth != 7 || p.DependencyDegree != 2.5 {
		t.Fatalf("%v %+v", applied, p)
	}
	if len(unsupported) == 0 || !strings.Contains(unsupported[0], "out_degree") {
		t.Fatalf("%v", unsupported)
	}
	q := DefaultParams(1, 10000)
	if applied, _ := ApplyShape(&ShapeCard{}, &q); len(applied) != 0 || q != DefaultParams(1, 10000) {
		t.Fatal("an empty card changes nothing")
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The generator can be set again from the card of its own world.
func TestShapeRoundTrip(t *testing.T) {
	w := smallWorld(t, 42, 60000, func(p *Params) { p.Sites = 2; p.CrossSiteRatio = 0.25; p.DependencyDegree = 3 })
	m, _ := runAll(t, w)
	p := DefaultParams(42, 60000)
	ApplyShape(m.Shape, &p)
	if p.DependencyDegree < 2 || p.DependencyDegree > 3.5 || p.CrossSiteRatio < 0.12 || p.CrossSiteRatio > 0.4 {
		t.Fatalf("recalibrated to %v and %v", p.DependencyDegree, p.CrossSiteRatio)
	}
}
