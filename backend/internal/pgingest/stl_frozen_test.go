package pgingest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// What a run cleaned to the end leaves, held to the old pipeline and frozen (ADR-0128, decision 5 of the design lead): the old Runner and the
// new flow are given the same captures, the objects and the links that live after each run are compared, and what they are is written
// below as numbers and a hash of the ids, so that the comparison stands when the old pipeline is gone (I-7d).

// stlFrozen is what lives after each run of isoSteps: the objects, the links, and the first 16 hex digits of the hash of their sorted ids.
var stlFrozen = []struct {
	objects, links int
	hash           string
}{
	{8, 3, "88e0cc940de0d0e7"},
	{9, 4, "0521063a3b62c51e"},
}

// stlOldLive is what lives in the old pipeline's KG: the ids of the objects and of the links.
func stlOldLive(t *testing.T, o *rclOldPipeline) (objects, links []string) {
	t.Helper()
	snap, err := o.runner.Store.Snapshot(context.Background(), graph.ReadPoint{KG: o.kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	all, err := snap.ScanObjects(context.Background(), graph.ObjectID{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range all {
		if row.SeqTo == 0 {
			objects = append(objects, row.ID.String())
		}
	}
	for id := range o.rclLinks(t) {
		links = append(links, id)
	}
	sort.Strings(objects)
	sort.Strings(links)
	return objects, links
}

// stlHashOf is the first 16 hex digits of the hash of the ids, objects then links.
func stlHashOf(objects, links []string) string {
	h := kernel.HashBytes([]byte(strings.Join(objects, ",") + ";" + strings.Join(links, ",")))
	return h.String()[:16]
}

// [REQ-985]
func TestStlWhatLivesAfterARunCleanedToTheEndIsWhatTheOldPipelineLeavesAndIsFrozen(t *testing.T) {
	o := rclOldNew(t)
	s := rdfNew(t, pgiPlan(t, rclSourceA, rclMappingA))
	for n, texts := range isoSteps() {
		for name, text := range texts {
			o.fetch.text[name] = text
		}
		o.clock.Set(stlAt(n))
		if _, err := o.runner.Run(context.Background(), o.plans[0], o.kg.ID); err != nil {
			t.Fatalf("run %d: the old run: %v", n, err)
		}
		stlWhole(s, t, texts, stlAt(n), 2)
		oldObjects, oldLinks := stlOldLive(t, o)
		objects := expList(s.e, t, fmt.Sprintf(`SELECT ref FROM "s_%d".object WHERE retired_seq IS NULL ORDER BY ref`, s.ref.KGNo))
		links := expList(s.e, t, fmt.Sprintf(`SELECT ref FROM "s_%d".link ORDER BY ref`, s.ref.KGNo))
		if strings.Join(objects, " ") != strings.Join(oldObjects, " ") || strings.Join(links, " ") != strings.Join(oldLinks, " ") {
			t.Errorf("run %d: the old pipeline leaves\n%v\n%v\nthe new flow\n%v\n%v", n, oldObjects, oldLinks, objects, links)
		}
		got, want := stlFrozen[n], stlFrozen[n]
		got.objects, got.links, got.hash = len(objects), len(links), stlHashOf(objects, links)
		if got != want {
			t.Errorf("run %d: %+v lives, and %+v was frozen", n, got, want)
		}
	}
}
