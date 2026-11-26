package pgingest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The links the new flow stages and the ones the old pipeline makes from the same captures are held to each other (ADR-0122): the same
// captures, a step after another, to the code that was there and to the new flow, two sources to one KG.

// rclOldPipeline is the old pipeline with the plans of the two sources of one KG.
type rclOldPipeline struct {
	runner ingest.Runner
	kg     graph.KG
	plans  []*ingest.Plan
	fetch  *idnDsFetcher
	clock  *kernel.ManualClock
}

func rclOldNew(t *testing.T) *rclOldPipeline {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	o := &rclOldPipeline{clock: kernel.NewManualClock(rdfT0), fetch: &idnDsFetcher{text: map[string]string{}}}
	for _, p := range [][2]string{{rclSourceA, rclMappingA}, {rclSourceB, rclMappingBOwn}} {
		src, err := ingest.ParseSource(strings.NewReader(p[0]))
		if err != nil {
			t.Fatal(err)
		}
		mp, err := ingest.ParseMapping(strings.NewReader(p[1]))
		if err != nil {
			t.Fatal(err)
		}
		plan, err := ingest.Compile(reg, src, mp)
		if err != nil {
			t.Fatal(err)
		}
		o.plans = append(o.plans, plan)
	}
	ids := kernel.NewSequentialIDs()
	store := graph.NewScratchStore()
	kgid, _ := kernel.NewID[graph.KGKind](ids, "kg")
	o.kg, err = store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "rcl", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: o.clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	o.runner = ingest.Runner{Store: store, Reg: reg, Clock: o.clock, IDs: ids, Fetcher: o.fetch, States: ingest.NewMemStateStore()}
	return o
}

// rclOldRows is the links the old KG has now, by id.
func (o *rclOldPipeline) rclLinks(t *testing.T) map[string]graph.LinkRow {
	t.Helper()
	snap, err := o.runner.Store.Snapshot(context.Background(), graph.ReadPoint{KG: o.kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	all, err := snap.ScanLinks(context.Background(), graph.LinkID{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]graph.LinkRow{}
	for _, l := range all {
		if l.SeqTo == 0 {
			out[l.ID.String()] = l
		}
	}
	return out
}

// rclProblemsOf is the problems of the link kinds of a report, as kind/dataset/key, but the ones of the records of the keys held (records a
// rule filtered out), which are given apart. The old pipeline gives a record a rule filtered out the problems of the ends that find no
// object or more than one; ADR-0052 (decision 3) and REQ-435 say such a record reports nothing, and the new flow keeps to them (ADR-0122,
// the differences from the old flow).
func rclProblemsOf(r ingest.Report, held map[string]bool) (out, ofHeld []string) {
	for _, p := range r.Problems {
		for _, k := range rclKinds {
			switch {
			case p.Kind != k:
			case held[p.Key]:
				ofHeld = append(ofHeld, p.Kind+"/"+p.Dataset+"/"+p.Key)
			default:
				out = append(out, p.Kind+"/"+p.Dataset+"/"+p.Key)
			}
		}
	}
	sort.Strings(out)
	return out, ofHeld
}

// rclHeldKeys is the keys of the rows of the servers that the rule of the first source filters out (the flag is not on).
func rclHeldKeys(servers []string) map[string]bool {
	out := map[string]bool{}
	for _, row := range servers {
		cells := strings.Split(row, ",")
		if cells[len(cells)-1] != "on" {
			out[cells[0]] = true
		}
	}
	return out
}

// rclOwnRacks is the racks of the second source: the same tags under ids of its own.
func rclOwnRacks(racks []string) []string {
	out := make([]string, len(racks))
	for i, r := range racks {
		out[i] = "B" + r
	}
	return out
}

// rclDiff says how the staged row of a link differs from the row the old pipeline made of it: the names of the parts.
func rclDiff(t *testing.T, old graph.LinkRow, got rclLink) []string {
	t.Helper()
	var out []string
	if old.Src.String() != got.Src || old.Dst.String() != got.Dst {
		out = append(out, "ends")
	}
	if string(old.Grade) != got.Grade {
		out = append(out, "grade")
	}
	if string(old.Basis) != got.Basis {
		out = append(out, "basis")
	}
	oldDoc, err := graph.EncodeAttrs(old.Attrs)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rcpWithoutTime(rcpJSON(t, oldDoc))) != fmt.Sprint(rcpWithoutTime(rcpJSON(t, []byte(got.Doc)))) {
		out = append(out, "doc")
	}
	oldRefs, err := graph.EncodeRefs(old.SourceRefs)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rcpJSON(t, oldRefs)) != fmt.Sprint(rcpJSON(t, []byte(got.SourceRefs))) {
		out = append(out, "source_refs")
	}
	if old.ContentHash.String() != got.Content {
		out = append(out, "content_hash")
	}
	return out
}

// rclStep is the rows of the datasets at a step.
type rclStep struct{ racks, servers, vms, mounts []string }

// rclSteps are seven steps: a server that finds no rack, two racks of one tag, two records of one server (the later wins and the attributes
// differ), a server a rule holds, an empty cell, a rack that goes and comes back, and what the second source says about servers of its own
// on racks of its own of the same tags (two records of one server with other attributes, a missing rack, an empty cell and a wrong one).
func rclSteps() []rclStep {
	racks := []string{"RA,RK1", "RB,RK2", "RC,RK3"}
	more := []string{"SV7,Zorvane,sn7,,,on", "SV8,Zorvane,sn8,,,on"} // servers the first source does not mount: it says no rack
	with := func(rows ...string) []string { return append(append([]string{}, rows...), more...) }
	sv := with("SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on")
	vms := []string{"u1", "u2"}
	dup := with("SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on", "SV3,Zorvane,sn3,RK3,3,on", "SV1b,Zorvane,sn1,RK1,5,on")
	twice := append(append([]string{}, racks...), "RD,RK1")
	held := with("SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on", "SV3,Zorvane,sn3,RK3,3,on", "SV2h,Zorvane,sn2,RK1,9,off", "SV4,Zorvane,sn4,RK9,4,on")
	gone := with("SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on", "SV2h,Zorvane,sn2,RK1,9,off", "SV4,Zorvane,sn4,RK9,4,on")
	empty := with("SV1,Zorvane,sn1,RK1,1,on", "SV2,Zorvane,sn2,RK2,2,on", "SV3,Zorvane,sn3,RK3,,on", "SV2h,Zorvane,sn2,RK1,9,off", "SV4,Zorvane,sn4,RK3,4,on")
	// the mounts of the second source are servers of its own: their serials are not the first source's
	m := func(rows ...string) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = strings.Replace(r, ",sn", ",bn", 1)
		}
		return out
	}
	return []rclStep{
		{racks, sv, vms, m("M7,Zorvane,sn7,RK1,7", "M8,Zorvane,sn8,RK2,8")},
		{racks, dup, vms, m("M7,Zorvane,sn7,RK1,7", "M8,Zorvane,sn8,RK2,8", "M9,Zorvane,sn7,RK1,9")},
		{twice, dup, vms, m("M7,Zorvane,sn7,RK1,7", "M8,Zorvane,sn8,RK2,8")},
		{racks, held, vms, m("M7,Zorvane,sn7,RK9,7", "M8,Zorvane,sn8,RK2,8")},
		{racks[:1], gone, vms, m("M7,Zorvane,sn7,RK1,6", "M8,Zorvane,sn8,RK2,8")},
		{racks, empty, vms[:1], m("M7,Zorvane,sn7,RK1,6")},
		{racks, empty, append(append([]string{}, vms...), "u3"), m("M7,Zorvane,sn7,RK3,", "M8,Zorvane,sn8,RK2,x")},
	}
}

// [REQ-864]
func TestRclTheStagedLinksAreTheOnesTheOldPipelineMakesFromTheSameCaptures(t *testing.T) {
	o := rclOldNew(t)
	k := rclTwo(t, rclMappingBOwn)
	compared, problems, held, ofHeld, ofB := 0, 0, 0, 0, 0
	for n, st := range rclSteps() {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		ta, tb := rclTextsA(st.racks, st.servers, st.vms), rclTextsB(rclOwnRacks(st.racks), st.mounts)
		heldKeys := rclHeldKeys(st.servers)
		for _, m := range []pgiTexts{ta, tb} {
			for name, text := range m {
				o.fetch.text[name] = text
			}
		}
		o.clock.Set(at)
		union := map[string]rclLink{}
		for i, s := range []*rdfSetup{k.a, k.b} {
			report, err := o.runner.Run(context.Background(), o.plans[i], o.kg.ID)
			if err != nil {
				t.Fatalf("step %d: the old run: %v", n, err)
			}
			texts := ta
			if i == 1 {
				texts = tb
			}
			l := rclPrepare(s, t, texts, at, true)
			counts := rclDo(s, t, l, rclPlans(s), 0)
			staged := rclStaged(s, t)
			if counts.Links != len(staged) {
				t.Errorf("step %d source %d: the report says %d links and %d are staged", n, i, counts.Links, len(staged))
			}
			for ref, row := range staged {
				union[ref] = row
			}
			if i == 1 {
				ofB += len(staged)
			}
			old, oldHeld := rclProblemsOf(report, heldKeys)
			got := rclProblems(s, t)
			problems += len(got)
			ofHeld += len(oldHeld)
			// the records a rule filtered out are not told in the new flow: the list of the new flow has none of them, and is the old
			// one without them
			if strings.Join(old, " ") != strings.Join(got, " ") {
				t.Errorf("step %d source %d: the old pipeline reports %v (and %v of the held records), the new flow %v", n, i, old, oldHeld, got)
			}
			rcpApply(s, t)
			s.rdfSettle(t, l)
		}
		oldLinks := o.rclLinks(t)
		for ref, row := range union {
			old, ok := oldLinks[ref]
			if !ok {
				t.Errorf("step %d: the link %s is staged and the old pipeline has no such link", n, ref)
				continue
			}
			compared++
			if d := rclDiff(t, old, row); len(d) > 0 {
				t.Errorf("step %d %s differs from the old pipeline in %v\n old %+v\n new %s", n, ref, d, old, row.rclText())
			}
		}
		for ref := range oldLinks {
			if _, ok := union[ref]; !ok {
				t.Errorf("step %d: the old pipeline has the link %s and the new flow does not stage it", n, ref)
			}
		}
		if n == 3 {
			held = len(union)
		}
	}
	// the case has links, problems, a step with a held record, and a held record the old pipeline told (the difference is there to be seen)
	if compared == 0 || problems == 0 || held == 0 || ofHeld == 0 || ofB == 0 {
		t.Fatalf("the case compares nothing: links %d (of the second source %d), problems %d, links of the step with a held record %d, problems of held records %d",
			compared, ofB, problems, held, ofHeld)
	}
}
