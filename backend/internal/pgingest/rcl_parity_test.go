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
	for _, p := range [][2]string{{rclSourceA, rclMappingA}, {rclSourceB, rclMappingB}} {
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

// rclProblemsOf is the problems of the link kinds of a report, as kind/dataset/key.
func rclProblemsOf(r ingest.Report) []string {
	var out []string
	for _, p := range r.Problems {
		for _, k := range rclKinds {
			if p.Kind == k {
				out = append(out, p.Kind+"/"+p.Dataset+"/"+p.Key)
			}
		}
	}
	sort.Strings(out)
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
// differ), a server a rule holds, an empty cell, a rack that goes and comes back, and what the second source says about two more servers
// (it finds the racks and the servers the first source made, and makes the same ambiguous and missing ends).
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
	m := func(rows ...string) []string { return rows }
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
	k := rclTwo(t)
	compared, problems, held := 0, 0, 0
	for n, st := range rclSteps() {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		ta, tb := rclTextsA(st.racks, st.servers, st.vms), rclTextsB(st.racks, st.mounts)
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
			old, got := rclProblemsOf(report), rclProblems(s, t)
			problems += len(got)
			if strings.Join(old, " ") != strings.Join(got, " ") {
				t.Errorf("step %d source %d: the old pipeline reports %v, the new flow %v", n, i, old, got)
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
	if compared == 0 || problems == 0 || held == 0 {
		t.Fatalf("the case compares nothing: links %d, problems %d", compared, problems)
	}
}
