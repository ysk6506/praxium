package ingestcheck

import (
	"context"
	"time"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

func clean(p *worldgen.Params) { p.Imperfections = worldgen.Imperfections{} }

// [REQ-265]
func TestIngestedKGMatchesTheLoadedReference(t *testing.T) {
	reg, _ := model.LoadStandard()
	files, truth := world(t, reg, 7, 500, clean)
	p := newPipeline(t, files)
	rep := p.run()
	if len(rep.ProblemCounts) != 0 {
		t.Logf("problems %v", rep.ProblemCounts)
	}
	got := readView(t, reg, p.store, p.kg)
	refStore, refKG := developed(t, reg, truth)
	want := readView(t, reg, refStore, refKG)
	if d := want.diff(got); len(d) != 0 {
		t.Fatalf("%d differences, first: %v", len(d), d)
	}
	t.Logf("objects %d links %d problems %v", len(got.objects), len(got.links), rep.ProblemCounts)
	counts := map[string]int{}
	for k := range got.objects {
		counts[typeOf(k)]++
	}
	for _, ty := range []string{"Rack", "PhysicalServer", "HypervisorHost", "VirtualMachine"} {
		if counts[ty] == 0 {
			t.Errorf("no %s objects", ty)
		}
	}
	links := map[string]int{}
	for k := range got.links {
		parts := strings.SplitN(k, "|", 3)
		links[parts[0]+"/"+parts[1]]++
	}
	for _, want := range []string{"MOUNTED_IN/PhysicalServer", "RUNS_ON/VirtualMachine", "RUNS_ON/HypervisorHost"} {
		if links[want] == 0 {
			t.Errorf("no %s links: %v", want, links)
		}
	}
}

// [REQ-266]
func TestReingestingTheSameFilesChangesNothing(t *testing.T) {
	reg, _ := model.LoadStandard()
	files, _ := world(t, reg, 7, 500, clean)
	p := newPipeline(t, files)
	first := p.run()
	headBefore, _ := p.store.Head(context.Background(), p.kg.ID)
	again := p.run()
	headAfter, _ := p.store.Head(context.Background(), p.kg.ID)
	if again.Changed || again.NewVersion || again.GraphHash != first.GraphHash || headAfter.AppliedSeq != headBefore.AppliedSeq {
		t.Fatalf("second run changed the KG: %+v", again)
	}
	for _, d := range again.Datasets {
		if d.Raw.Added+d.Raw.Modified+d.Raw.Deleted != 0 || !d.SameCapture {
			t.Errorf("%s: %+v", d.Dataset, d)
		}
	}
}

// [REQ-266]
func TestChangingOneRowChangesOnlyThatObject(t *testing.T) {
	reg, _ := model.LoadStandard()
	files, _ := world(t, reg, 7, 500, clean)
	p := newPipeline(t, files)
	p.run()
	before := readView(t, reg, p.store, p.kg)
	lines := strings.Split(files["ds_racks"], "\n")
	f := strings.Split(lines[1], ",")
	f[3] = "48"
	lines[1] = strings.Join(f, ",")
	p.files["ds_racks"] = strings.Join(lines, "\n")
	rep := p.run()
	if !rep.NewVersion {
		t.Fatal("no new version")
	}
	d := before.diff(readView(t, reg, p.store, p.kg))
	if len(d) != 1 || !strings.Contains(d[0], "Rack|"+f[1]+".total_u") {
		t.Fatalf("differences %v", d)
	}
}

// [REQ-267]
func TestVanishedRowIsRetiredAfterThreeCompleteCapturesAndADay(t *testing.T) {
	reg, _ := model.LoadStandard()
	files, _ := world(t, reg, 7, 500, clean)
	p := newPipeline(t, files)
	p.run()
	before := readView(t, reg, p.store, p.kg)
	var gone string
	rows := strings.Split(files["ds_vm_power"], "\n")
	gone = rows[1]
	vms := strings.Split(strings.TrimSpace(files["ds_vms"]), "\n")
	p.files["ds_vms"] = strings.Join(vms[1:], "\n") + "\n"
	p.files["ds_vm_power"] = strings.Join(append(rows[:1], rows[2:]...), "\n")
	for i := 0; i < 3; i++ {
		p.clock.Advance(2 * time.Hour)
		if rep := p.run(); rep.RetiredObjects != 0 {
			t.Fatalf("retired at run %d", i+2)
		}
	}
	if len(before.diff(readView(t, reg, p.store, p.kg))) != 0 {
		t.Fatal("the KG moved before 24 hours")
	}
	p.clock.Advance(24 * time.Hour)
	rep := p.run()
	if rep.RetiredObjects != 1 || rep.RetiredLinks < 1 {
		t.Fatalf("retired %d objects %d links (%s)", rep.RetiredObjects, rep.RetiredLinks, gone)
	}
	d := before.diff(readView(t, reg, p.store, p.kg))
	if len(d) < 2 || !strings.HasPrefix(d[0], "link only in the first") && !strings.HasPrefix(d[0], "only in the first") {
		t.Fatalf("differences %v", d)
	}
}

// [REQ-268]
func TestBadRowsAreReportedAndTheRestIsApplied(t *testing.T) {
	reg, _ := model.LoadStandard()
	files, _ := world(t, reg, 7, 500, clean)
	p := newPipeline(t, files)
	servers := strings.Split(strings.TrimSpace(files["ds_servers"]), "\n")
	f := strings.Split(servers[1], ",")
	f[0], f[2] = "SV-NOSERIAL", ""
	servers = append(servers, strings.Join(f, ","))
	p.files["ds_servers"] = strings.Join(servers, "\n") + "\n"
	p.files["ds_hosts"] += "this is not json\n"
	rep := p.run()
	if rep.ProblemCounts[ingest.ProblemMissingIdentity] != 1 || rep.ProblemCounts[ingest.ProblemUnreadable] != 1 {
		t.Fatalf("problems %v", rep.ProblemCounts)
	}
	for _, d := range rep.Datasets {
		if !d.Balanced() {
			t.Errorf("%s does not balance: %+v", d.Dataset, d)
		}
	}
	ref := newPipeline(t, files)
	ref.run()
	if d := readView(t, reg, ref.store, ref.kg).diff(readView(t, reg, p.store, p.kg)); len(d) != 0 {
		t.Fatalf("the good rows changed: %v", d)
	}
}

// [REQ-268]
func TestFlawedFilesStillGiveABalancedRun(t *testing.T) {
	reg, _ := model.LoadStandard()
	files, _ := world(t, reg, 7, 500, nil)
	p := newPipeline(t, files)
	rep := p.run()
	for _, d := range rep.Datasets {
		if !d.Balanced() {
			t.Errorf("%s does not balance: %+v", d.Dataset, d)
		}
	}
	if len(rep.Problems) == 0 {
		t.Error("the flawed files gave no problem")
	}
	if rep.Objects == 0 {
		t.Error("nothing was applied")
	}
}
