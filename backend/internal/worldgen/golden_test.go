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
	goldenObjects      = 1394
	goldenLinks        = 2509
	goldenHash         = "2b482ee1c866524057d8c55aacb72b9843eb55fb08da84386e632a3f873db1b9"
	goldenManifestHash = "e2af04bd4ff8c74743608ae9bab126c680353d809a8bb94b9bf5aaca7bf7eea0"
)

// [REQ-001]
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
