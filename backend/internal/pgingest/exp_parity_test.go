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

// The objects that the old pipeline retires and the candidates the expansion finds are held to each other: the same captures, a step
// after another, are given to the code that was there and to the new flow (the same ones the identification is held to the old code
// by, ADR-0108), with the feeds that retire.

// expLiveOld is the ids of the objects of the old pipeline's KG that are not retired, of the ids it has made.
func expLiveOld(t *testing.T, r ingest.Runner, kg graph.KGID, made map[string]bool) []string {
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
		oid, err := kernel.ParseID[graph.ObjectKind](id)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, err := snap.Object(context.Background(), oid); err != nil {
			t.Fatal(err)
		} else if ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// [REQ-891] [REQ-897]
func TestExpTheCandidatesForRetirementAreTheObjectsTheOldPipelineRetiresInTheSameCaptures(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	mapping := strings.ReplaceAll(idnMoreMapping, `"creates_objects":true,`, `"creates_objects":true,"retires_objects":true,`)
	src, err := ingest.ParseSource(strings.NewReader(idnMoreSource))
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
	kg, err := store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "exp", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fetch := &idnDsFetcher{text: map[string]string{}}
	runner := ingest.Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, Fetcher: fetch, States: ingest.NewMemStateStore()}
	s := rdfNew(t, pgiPlan(t, idnMoreSource, mapping))
	made, retiredOld, retiredNew := map[string]bool{}, 0, 0
	for n, st := range idnMoreSteps() {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		fetch.text["ds_a"], fetch.text["ds_b"] = idnMoreTexts(st.a, true), idnMoreTexts(st.b, false)
		clock.Set(at)
		rep, err := runner.Run(context.Background(), plan, kg.ID)
		if err != nil {
			t.Fatalf("step %d: the old run: %v", n, err)
		}
		l, _ := idnPipeline(s, t, pgiTexts{"ds_a": fetch.text["ds_a"], "ds_b": fetch.text["ds_b"]}, at, 0)
		counts, err := ingest.RunExpand(s.e.ctx, s.e.st, l, ingest.ExpandStart{Cards: plan.ExpandCards(), Feeds: expFeeds(s)})
		if err != nil {
			t.Fatal(err)
		}
		expApply(s, t)
		s.rdfSettle(t, l)
		oldLive, newLive := expLiveOld(t, runner, kg.ID, made), expList(s.e, t, fmt.Sprintf(`SELECT ref FROM "s_%d".object WHERE retired_seq IS NULL ORDER BY ref`, s.ref.KGNo))
		if strings.Join(oldLive, ",") != strings.Join(newLive, ",") {
			t.Errorf("step %d: the objects that are kept are %v, the old pipeline keeps %v (%+v)", n, newLive, oldLive, counts)
		}
		if counts.Retiring != rep.RetiredObjects {
			t.Errorf("step %d: %d objects are candidates, the old pipeline retired %d", n, counts.Retiring, rep.RetiredObjects)
		}
		retiredOld, retiredNew = retiredOld+rep.RetiredObjects, retiredNew+counts.Retiring
	}
	// the captures retire an object, and it is shown again (the object is the one it was)
	if retiredOld == 0 || retiredNew != retiredOld {
		t.Errorf("the old pipeline retired %d objects in all, the expansion found %d", retiredOld, retiredNew)
	}
}
