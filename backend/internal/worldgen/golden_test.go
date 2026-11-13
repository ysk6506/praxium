package worldgen

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The golden world: a change to the generator that changes this world must
// be deliberate. If it is, the numbers below are updated with the change and
// docs/design-docs/worldgen.md says why.
const (
	goldenObjects      = 1391
	goldenLinks        = 2505
	goldenHash         = "8d648b7bfd6e370de5163d17a9c1a9a8d196cac2fe8c19a2df518621c60b4085"
	goldenManifestHash = "cd5f56fb86006bf1bd59b5ec13c44f1806b42347000d1ef79e5e20623994017a"
)

func TestGoldenTinyWorld(t *testing.T) {
	w := smallWorld(t, 1, 3000, func(p *Params) { p.Changes.Events = 20 })
	m, st := runAll(t, w)
	sum := sha256.Sum256(st.Files[ManifestFile].Bytes())
	got := hex.EncodeToString(sum[:])
	t.Log(m.Counts.Objects, m.Counts.Links, m.ContentHash, got)
	if m.Counts.Objects != goldenObjects || m.Counts.Links != goldenLinks || m.ContentHash != goldenHash {
		t.Fatalf("golden world changed: %d objects, %d links, %s", m.Counts.Objects, m.Counts.Links, m.ContentHash)
	}
	if got != goldenManifestHash {
		t.Fatalf("golden manifest changed: %s", got)
	}
	want := map[string]int64{"Rack": 2, "PhysicalServer": 23, "HypervisorHost": 19, "VirtualMachine": 148, "EcmpGroup": 4, "ApplicationService": 6, "CoolingLoop": 1}
	for name, n := range want {
		if m.Counts.ObjectsByType[name] != n {
			t.Errorf("%s: %d, want %d", name, m.Counts.ObjectsByType[name], n)
		}
	}
	if m.Layout != (LayoutInfo{Sites: 1, Halls: 1, Racks: 2, Servers: 23, Hosts: 19, VMs: 148, Services: 6}) {
		t.Errorf("layout %+v", m.Layout)
	}
}

// The truth does not depend on what else is written, and the manifest says
// what the truth hash is without the file being written.
func TestContentHashIsIndependentOfOutputs(t *testing.T) {
	w := smallWorld(t, 1, 3000, nil)
	m, _ := runAll(t, w)
	counted, err := w.Run(NewMemStore(), RunOptions{})
	if err != nil || counted.ContentHash != m.ContentHash || counted.Counts.Objects != m.Counts.Objects {
		t.Fatalf("%v %s %s", err, counted.ContentHash, m.ContentHash)
	}
}
