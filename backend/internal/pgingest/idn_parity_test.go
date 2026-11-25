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

// The identification does what the code that is there does (ADR-0108): the same captures, one run after another, are given to the
// pipeline that is there (ingest.Runner) and to the identification on a real server, and after every run the objects that are made
// are held to each other. The differences the ADR names are cases of their own and are held to what the ADR says.

// idnFetcher gives the old pipeline the rows of the step that is on.
type idnFetcher struct{ text string }

func (f *idnFetcher) Fetch(_ context.Context, _ string, ds ingest.Dataset) (ingest.Capture, error) {
	return ingest.Capture{Dataset: ds.ID, Data: []byte(f.text), Hash: kernel.HashBytes([]byte(f.text)).String(), Complete: true}, nil
}

// idnParityStep is one capture: the rows (asset, serial, manufacturer, uuid), and whether the objects must have the same ids in both
// pipelines (the rows that make an object together make it from one key then, and the key is the same).
type idnParityStep struct {
	rows  []string
	exact bool
	// groups is how many objects the step adds to the ones there are (the same in both pipelines).
	groups int
}

func idnParitySteps() []idnParityStep {
	return []idnParityStep{
		{rows: []string{"A,sn1,Zorvane,u1", "B,sn2,Zorvane,u2", "C,sn3,Zorvane,u3"}, exact: true, groups: 3},
		{rows: []string{"A,sn1,Zorvane,u1b", "B,sn2,Zorvane,u2", "C,sn3,Zorvane,u3", "D,sn4,Zorvane,u4"}, exact: true, groups: 1},
		{rows: []string{"A,sn1,Zorvane,u1b", "B,sn2,Zorvane,u2", "C,sn3,Zorvane,u3", "D,sn4,Zorvane,u4", "E,sn1,Zorvane,u9"}, exact: true, groups: 0},
		{rows: []string{"A,sn1,Zorvane,u1b", "B,sn2,Zorvane,u2", "C,sn3,Zorvane,u3", "D,sn4,Zorvane,u4", "E,sn1,Zorvane,u9", "F,snN,Zorvane,uN", "G,snN2,Zorvane,uN"}, groups: 1},
		{rows: []string{"A,sn1,Zorvane,u1b", "B,sn2,Zorvane,u2", "C,sn3,Zorvane,u3", "D,sn4,Zorvane,u4", "E,sn1,Zorvane,u9", "F,snN,Zorvane,uN", "G,snN2,Zorvane,uN", "H,sn5,Zorvane,u5"}, exact: true, groups: 1},
		{rows: []string{"A,sn1,Zorvane,u1b", "B,sn2,Zorvane,u2", "C,sn3,Zorvane,u3", "D,sn4,Zorvane,u4", "E,sn1,Zorvane,u9", "F,snN,Zorvane,uN", "G,snN2,Zorvane,uN", "H,sn5,Zorvane,u5", "J,sn2,Zorvane,u3"}, exact: true, groups: 0},
	}
}

// [REQ-885]
func TestIdnTheObjectsTheIdentificationMakesAreTheObjectsTheOldPipelineMakesForTheSameCaptures(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(idnServerSource))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(idnServerMapping))
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
	kg, err := store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "idn", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fetch := &idnFetcher{}
	runner := ingest.Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, Fetcher: fetch, States: ingest.NewMemStateStore()}
	s := rdfNew(t, pgiPlan(t, idnServerSource, idnServerMapping))
	total, checks := 0, 0
	for n, st := range idnParitySteps() {
		fetch.text = "asset_id,serial,manufacturer,uuid\n" + idnJoin(st.rows...)
		clock.Set(rdfT0.Add(time.Duration(n) * time.Hour))
		if _, err := runner.Run(context.Background(), plan, kg.ID); err != nil {
			t.Fatalf("step %d: the old run: %v", n, err)
		}
		l, _ := s.idnPipeline(t, pgiTexts{"ds_s": fetch.text}, rdfT0.Add(time.Duration(n)*time.Hour), 0)
		s.idnApply(t)
		s.rdfSettle(t, l)
		total += st.groups
		oldRefs := idnOldObjects(t, runner, kg.ID)
		newRefs := idnNewObjects(t, s)
		if len(oldRefs) != total || len(newRefs) != total {
			t.Errorf("step %d: %d objects in the old pipeline and %d in the identification, want %d", n, len(oldRefs), len(newRefs), total)
		}
		if st.exact && strings.Join(oldRefs, ",") != strings.Join(newRefs, ",") {
			t.Errorf("step %d: the ids are %v, the old pipeline makes %v", n, newRefs, oldRefs)
		}
		checks++
	}
	if checks != len(idnParitySteps()) {
		t.Fatal(checks)
	}
}

func idnOldObjects(t *testing.T, r ingest.Runner, kg graph.KGID) []string {
	t.Helper()
	snap, err := r.Store.Snapshot(context.Background(), graph.ReadPoint{KG: kg, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	all, err := snap.ScanObjects(context.Background(), graph.ObjectID{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, o := range all {
		if o.SeqTo == 0 {
			out = append(out, o.ID.String())
		}
	}
	sort.Strings(out)
	return out
}

func idnNewObjects(t *testing.T, s *rdfSetup) []string {
	t.Helper()
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT ref FROM "s_%d".object ORDER BY ref`, s.ref.KGNo))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}
