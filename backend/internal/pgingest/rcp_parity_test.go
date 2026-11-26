package pgingest

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// The objects the new flow stages and the ones the old pipeline makes from the same captures are held to each other (ADR-0115): the
// same captures, a step after another, are given to the code that was there and to the new flow. What is different is listed in
// rcpKnownDifferences, one entry for each row of the table of the ADR.

// rcpApply does to the registry what the later steps will do with what the computation staged, so that the next run finds it: the
// objects the run made, with the names and the first keys they were staged with, and the grades.
func rcpApply(s *rdfSetup, t *testing.T) {
	t.Helper()
	expApply(s, t)
	s.e.exec(t, fmt.Sprintf(`UPDATE "s_%[1]d".object o SET name = x.name, min_grade = x.min_grade, first_key = COALESCE(o.first_key, x.first_key)
		FROM "s_%[1]d".stage_object x WHERE x.obj_no = o.obj_no`, s.ref.KGNo))
}

// rcpOld is the objects of the old pipeline's KG, by id, and the ids it has made.
func rcpOld(t *testing.T, r ingest.Runner, kg graph.KGID, ids []string) map[string]graph.ObjectRow {
	t.Helper()
	snap, err := r.Store.Snapshot(context.Background(), graph.ReadPoint{KG: kg, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]graph.ObjectRow{}
	for _, id := range ids {
		oid, err := kernel.ParseID[graph.ObjectKind](id)
		if err != nil {
			t.Fatal(err)
		}
		if row, ok, err := snap.Object(context.Background(), oid); err != nil {
			t.Fatal(err)
		} else if ok {
			out[id] = row
		}
	}
	return out
}

// rcpAllIDs is every object id the old KG has now.
func rcpAllIDs(t *testing.T, r ingest.Runner, kg graph.KGID, made map[string]bool) []string {
	t.Helper()
	snap, err := r.Store.Snapshot(context.Background(), graph.ReadPoint{KG: kg, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	all, err := snap.ScanObjects(context.Background(), graph.ObjectID{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range all {
		made[o.ID.String()] = true
	}
	var out []string
	for id := range made {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func rcpJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return v
}

// rcpWithoutTime is a document of attributes without the time each was seen (not hashed).
func rcpWithoutTime(doc any) any {
	m, _ := doc.(map[string]any)
	out := map[string]any{}
	for no, v := range m {
		e, _ := v.(map[string]any)
		cp := map[string]any{}
		for k, x := range e {
			if k != "t" {
				cp[k] = x
			}
		}
		out[no] = cp
	}
	return out
}

// rcpDiff says how the staged row of an object differs from the row the old pipeline made of it: the names of the parts.
func rcpDiff(t *testing.T, old graph.ObjectRow, got rcpObject) []string {
	t.Helper()
	var out []string
	if old.Name != got.Name {
		out = append(out, "name")
	}
	if g, _ := graph.GradeNo(old.MinGrade); g != got.Grade {
		out = append(out, "grade")
	}
	oldDoc, err := graph.EncodeAttrs(old.Attrs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rcpWithoutTime(rcpJSON(t, oldDoc)), rcpWithoutTime(rcpJSON(t, []byte(got.Doc)))) {
		out = append(out, "doc")
	} else if !reflect.DeepEqual(rcpJSON(t, oldDoc), rcpJSON(t, []byte(got.Doc))) {
		out = append(out, "source_time")
	}
	oldRefs, err := graph.EncodeRefs(old.SourceRefs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rcpJSON(t, oldRefs), rcpJSON(t, []byte(got.SourceRefs))) {
		out = append(out, "source_refs")
	}
	if old.ContentHash.String() != got.Content {
		out = append(out, "content_hash")
	}
	return out
}

// rcpOldPipeline is the old pipeline of a plan with its KG, and what it needs to be given a capture of each dataset.
type rcpOldPipeline struct {
	runner ingest.Runner
	kg     graph.KG
	plan   *ingest.Plan
	fetch  *idnDsFetcher
	clock  *kernel.ManualClock
	made   map[string]bool
}

func rcpOldNew(t *testing.T, source, mapping string) *rcpOldPipeline {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(mapping))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ingest.Compile(reg, src, mp)
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(rdfT0)
	ids := kernel.NewSequentialIDs()
	store := graph.NewScratchStore()
	kgid, _ := kernel.NewID[graph.KGKind](ids, "kg")
	kg, err := store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "rcp", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fetch := &idnDsFetcher{text: map[string]string{}}
	return &rcpOldPipeline{runner: ingest.Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, Fetcher: fetch, States: ingest.NewMemStateStore()}, kg: kg, plan: plan, fetch: fetch, clock: clock, made: map[string]bool{}}
}

// rcpRunAt gives a capture to the old pipeline and to the new flow (up to the computation of the objects, with the time of the run as the
// time the values were seen) and gives the old objects and the staged ones.
func rcpRunAt(t *testing.T, o *rcpOldPipeline, s *rdfSetup, texts pgiTexts, at time.Time) (map[string]graph.ObjectRow, map[string]rcpObject, ingest.RecomputeCounts, ingest.RunLease) {
	t.Helper()
	for name, text := range texts {
		o.fetch.text[name] = text
	}
	o.clock.Set(at)
	if _, err := o.runner.Run(context.Background(), o.plan, o.kg.ID); err != nil {
		t.Fatalf("the old run: %v", err)
	}
	l, _ := idnPipeline(s, t, texts, at, 0)
	rcpExpand(s, t, l, false)
	start := rcpStart(s, 0, 0)
	start.Seen = at.UTC().Format(time.RFC3339)
	counts, err := ingest.RunRecompute(s.e.ctx, s.e.st, l, start, ingest.NewObjectComputer(map[int64]*ingest.Plan{s.ref.SrcNo: s.plan}, rcpHasher(t, s), start))
	if err != nil {
		t.Fatal(err)
	}
	return rcpOld(t, o.runner, o.kg.ID, rcpAllIDs(t, o.runner, o.kg.ID, o.made)), rcpStaged(s, t), counts, l
}

// rcpKnownDifferences is where the staged row may differ from the one of the old pipeline, by the part of the row and the case that is
// the reason, each a row of the table of the differences in the ADR (the number is the row of the table).
var rcpKnownDifferences = map[string]string{
	// row 1 of the table of the ADR: the old pipeline keeps the time the value was first seen; the new flow writes the time of the run
	"source_time": "the time a value was seen is the time of the run that computes it (it is not hashed)",
}

// [REQ-934]
func TestRcpTheStagedObjectsAreTheOnesTheOldPipelineMakesFromTheSameCaptures(t *testing.T) {
	o := rcpOldNew(t, idnMoreSource, idnMoreMapping)
	s := rdfNew(t, pgiPlan(t, idnMoreSource, idnMoreMapping))
	compared, differing := 0, map[string][]string{}
	for n, st := range idnMoreSteps() {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		old, staged, counts, l := rcpRunAt(t, o, s, pgiTexts{"ds_a": idnMoreTexts(st.a, true), "ds_b": idnMoreTexts(st.b, false)}, at)
		for ref, got := range staged {
			row, ok := old[ref]
			if !ok {
				t.Errorf("step %d: the object %s is staged and the old pipeline has no such object", n, ref)
				continue
			}
			compared++
			if d := rcpDiff(t, row, got); len(d) > 0 {
				differing[fmt.Sprintf("step %d %s", n, ref)] = d
				oldDoc, _ := graph.EncodeAttrs(row.Attrs)
				t.Logf("step %d %s %v\n old name=%q doc=%s refs=%v\n new name=%q doc=%s refs=%s", n, ref, d, row.Name, oldDoc, row.SourceRefs, got.Name, got.Doc, got.SourceRefs)
			}
		}
		if counts.Objects != len(staged) {
			t.Errorf("step %d: the report says %d objects and %d are staged", n, counts.Objects, len(staged))
		}
		rcpApply(s, t)
		s.rdfSettle(t, l)
	}
	if compared == 0 {
		t.Fatal("no object was compared")
	}
	for where, parts := range differing {
		for _, p := range parts {
			if _, known := rcpKnownDifferences[p]; !known {
				t.Errorf("%s differs in %s from the old pipeline, and the ADR has no row for it", where, p)
			}
		}
	}
}
