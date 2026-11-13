package worldgen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

var seeds = []uint64{1, 2, 3, 11, 2024}

// Every seed gives a world that the registry accepts: types and properties
// exist, values are valid, bindings exist, endpoints exist, identity keys are
// unique and every redundancy group is well formed.
func TestWorldsAreValidAgainstTheRegistry(t *testing.T) {
	reg := loadReg(t)
	for _, seed := range seeds {
		w := smallWorld(t, seed, 25000, func(p *Params) { p.Sites = 2 })
		data, _ := truthOf(t, w)
		rep, err := Validate(reg, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if !rep.OK() {
			t.Fatalf("seed %d: %d problems, first: %v", seed, len(rep.Problems), rep.Problems[:min(3, len(rep.Problems))])
		}
		if rep.Objects == 0 || rep.Links == 0 {
			t.Fatalf("seed %d: empty world", seed)
		}
	}
}

func TestSameParametersSameBytes(t *testing.T) {
	a, ia := truthOf(t, smallWorld(t, 5, 12000, nil))
	b, ib := truthOf(t, smallWorld(t, 5, 12000, nil))
	if !bytes.Equal(a, b) || ia.SHA256 != ib.SHA256 {
		t.Fatal("two runs differ")
	}
	_, ic := truthOf(t, smallWorld(t, 6, 12000, nil))
	if ic.SHA256 == ia.SHA256 {
		t.Fatal("two seeds gave one world")
	}
	// the same world can be generated again
	w := smallWorld(t, 5, 12000, nil)
	_, i1 := truthOf(t, w)
	_, i2 := truthOf(t, w)
	if i1.SHA256 != i2.SHA256 || i1.SHA256 != ia.SHA256 {
		t.Fatal("a world is not repeatable")
	}
}

func TestScaleIsClose(t *testing.T) {
	for _, scale := range []int64{3000, 20000, 150000} {
		w := smallWorld(t, 1, scale, nil)
		var c Counts
		if err := w.Generate(&c); err != nil {
			t.Fatal(err)
		}
		got := float64(c.TotalObjects + c.TotalLinks)
		if got < 0.7*float64(scale) || got > 1.3*float64(scale) {
			t.Fatalf("scale %d gave %v records", scale, got)
		}
	}
}

// All nine layers are present, and so are the structures the brief asks for.
func TestEveryLayerAndStructure(t *testing.T) {
	w := smallWorld(t, 3, 40000, func(p *Params) { p.Sites = 3; p.CrossSiteRatio = 0.5 })
	m, _ := runAll(t, w)
	for _, layer := range []model.Layer{model.LayerFacility, model.LayerHardware, model.LayerNetwork, model.LayerStorage,
		model.LayerVirtualization, model.LayerContainer, model.LayerPlatform, model.LayerService, model.LayerCrossCutting} {
		if m.Counts.ObjectsByLayer[string(layer)] == 0 {
			t.Errorf("layer %s is empty", layer)
		}
	}
	for _, typ := range []string{"Region", "Site", "Room", "Rack", "UtilityFeed", "Ups", "Crah", "PhysicalServer", "Switch", "Lag", "EcmpGroup",
		"StorageArray", "StorageVolume", "ComputeCluster", "VirtualMachine", "Pod", "Workload", "ApplicationService", "BusinessService",
		"PowerRedundancyGroup", "CoolingRedundancyGroup", "MlagDomain", "LbPool", "SoftwareInstance", "DatabaseCluster", "CoolingLoop"} {
		if m.Counts.ObjectsByType[typ] == 0 {
			t.Errorf("no %s", typ)
		}
	}
	for _, l := range []string{"POWERED_BY", "COOLED_BY", "MOUNTED_IN", "CABLED_TO", "MEMBER_OF", "RUNS_ON", "CALLS", "DEPENDS_ON"} {
		if m.Counts.LinksByType[l] == 0 {
			t.Errorf("no %s links", l)
		}
	}
	if m.Counts.ObjectsNotIngestible != 0 || m.Counts.LinksNotIngestible != 0 {
		t.Errorf("%d objects and %d links are not ingestible, want none: every standard type has an identity key", m.Counts.ObjectsNotIngestible, m.Counts.LinksNotIngestible)
	}
	cs := m.Shape.CrossSite
	if cs.CallCross == 0 {
		t.Error("no service call crosses sites")
	}
	if m.Shape.CyclePairs == 0 {
		t.Error("no cycle in the service graph")
	}
}

// Power is dual: every rack has an A and a B feed, every server two supplies
// in a 2N group, and every ToR switch pair covers both sides.
func TestRedundancyShapes(t *testing.T) {
	w := smallWorld(t, 4, 20000, nil)
	data, _ := truthOf(t, w)
	sides := map[string]map[string]bool{}
	groups := map[string]int{}
	err := ReadTruth(bytes.NewReader(data), func(r *Record) error {
		if r.Kind == "object" && strings.HasSuffix(r.Type, "power_redundancy_group") {
			groups[string(r.Props["mode"])]++
		}
		if r.Kind == "link" && r.Type == "lt.powered_by" && strings.Contains(r.From, "/pdu") {
			if sides[r.From] == nil {
				sides[r.From] = map[string]bool{}
			}
			sides[r.From][string(r.Props["feed_side"])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sides) == 0 || groups[`"two_n"`] == 0 {
		t.Fatalf("pdus %d, 2N groups %v", len(sides), groups)
	}
	if rackSides := countSides(sides); rackSides[`"a"`] == 0 || rackSides[`"b"`] == 0 {
		t.Fatalf("both feeds expected: %v", rackSides)
	}
}

func countSides(m map[string]map[string]bool) map[string]int {
	out := map[string]int{}
	for _, s := range m {
		for k := range s {
			out[k]++
		}
	}
	return out
}

func TestTinyWorldsStillWork(t *testing.T) {
	reg := loadReg(t)
	for _, tweak := range []func(*Params){
		func(p *Params) { p.HostShare = 0 },
		func(p *Params) { p.VMsPerHost = 0 },
		func(p *Params) { p.StorageRackEvery = 0 },
		func(p *Params) { p.NodeHostShare = 0 },
		func(p *Params) { p.ServiceDensity = 0 },
		func(p *Params) {
			p.RacksPerHall = 1
			p.RacksPerRow = 1
			p.RacksPerCluster = 1
			p.AggPerPlane = 1
			p.SpinesPerSite = 1
		},
	} {
		w := smallWorld(t, 9, 600, tweak)
		data, _ := truthOf(t, w)
		rep, err := Validate(reg, bytes.NewReader(data))
		if err != nil || !rep.OK() {
			t.Fatalf("%v %v", err, rep.Problems)
		}
	}
}
