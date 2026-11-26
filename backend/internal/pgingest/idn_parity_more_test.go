package pgingest

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// Two datasets, three kinds of key (the maker and serial, the uuid, and an identifier), a key that repeats, and a record that is not
// shown for so long that the comparison closes it: the same captures, a step after another, are given to the code that was there and to
// the identification, and the objects they have made are held to each other (ADR-0108).

const idnMoreSource = `{"id":"src_idm","name":"idm","adapter":"file","datasets":[
 {"id":"ds_a","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"},{"name":"uuid"},{"name":"mac"}]},
 {"id":"ds_b","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"serial"},{"name":"manufacturer"},{"name":"uuid"}]}]}`

const idnMoreMapping = `{"feeds":[
 {"id":"f_a","dataset":"ds_a","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identifiers":[{"scheme":"bmc_mac","column":"mac"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1},
              {"components":[{"property":"system_uuid","column":"uuid"}],"priority":2}]},
 {"id":"f_b","dataset":"ds_b","object_type":"PhysicalServer","creates_objects":true,"basis":"observed",
  "attributes":[{"attribute":"name","column":"asset_id"}],
  "identity":[{"components":[{"property":"manufacturer","column":"manufacturer"},{"property":"serial_number","column":"serial"}],"priority":1},
              {"components":[{"property":"system_uuid","column":"uuid"}],"priority":2}]}]}`

// idnDsFetcher gives the old pipeline the rows of each dataset of the step that is on.
type idnDsFetcher struct{ text map[string]string }

func (f *idnDsFetcher) Fetch(_ context.Context, _ string, ds ingest.Dataset) (ingest.Capture, error) {
	text := f.text[ds.ID]
	return ingest.Capture{Dataset: ds.ID, Data: []byte(text), Hash: kernel.HashBytes([]byte(text)).String(), Complete: true}, nil
}

// idnMoreStep is the rows of the two datasets at a step.
type idnMoreStep struct{ a, b []string }

func idnMoreSteps() []idnMoreStep {
	a := []string{"A,sn1,Zorvane,u1,aa:01", "B,sn2,Zorvane,u2,aa:02", "C,sn3,Zorvane,u3,aa:03"}
	b := []string{"X,sn1,Zorvane,uX,", "Y,sn9,Zorvane,u9,"}
	withD := append(append([]string{}, a...), "D,sn4,Zorvane,u4,aa:04")
	// the later row of a key is the record: A shows twice, and the second says another uuid
	repeated := append(append([]string{}, withD...), "A,sn1,Zorvane,u1b,aa:01")
	without := []string{a[0], a[2], withD[3]}
	return []idnMoreStep{
		{a, b}, {withD, b}, {repeated, b},
		// B is not shown any more: the third run without it, a day on, closes it
		{without, b}, {without, b}, {without, b},
		// and it is shown again, and a record of the other dataset gives the key of C
		{append(append([]string{}, withD...), "B,sn2,Zorvane,u2,aa:02"), append(append([]string{}, b...), "Z,sn3,Zorvane,uZ,")},
	}
}

func idnMoreTexts(rows []string, withMac bool) string {
	if withMac {
		return "asset_id,serial,manufacturer,uuid,mac\n" + idnJoin(rows...)
	}
	var out []string
	for _, r := range rows {
		parts := strings.Split(r, ",")
		out = append(out, strings.Join(parts[:4], ","))
	}
	return "asset_id,serial,manufacturer,uuid\n" + idnJoin(out...)
}

// idnEverMade is the ids of all the objects the old pipeline has made, the ones it has taken away too.
func idnEverMade(t *testing.T, r ingest.Runner, kg graph.KGID) []string {
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
		out = append(out, o.ID.String())
	}
	sort.Strings(out)
	return out
}

// [REQ-885]
func TestIdnTwoDatasetsThreeKindsOfKeyARepeatedKeyAndARecordThatIsClosedMakeTheObjectsTheOldPipelineMakes(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(idnMoreSource))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(idnMoreMapping))
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
	kg, err := store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "idm", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fetch := &idnDsFetcher{text: map[string]string{}}
	runner := ingest.Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, Fetcher: fetch, States: ingest.NewMemStateStore()}
	s := rdfNew(t, pgiPlan(t, idnMoreSource, idnMoreMapping))
	for n, st := range idnMoreSteps() {
		at := rdfT0.Add(time.Duration(n) * 13 * time.Hour)
		fetch.text["ds_a"], fetch.text["ds_b"] = idnMoreTexts(st.a, true), idnMoreTexts(st.b, false)
		clock.Set(at)
		if _, err := runner.Run(context.Background(), plan, kg.ID); err != nil {
			t.Fatalf("step %d: the old run: %v", n, err)
		}
		l, counts := idnPipeline(s, t, pgiTexts{"ds_a": fetch.text["ds_a"], "ds_b": fetch.text["ds_b"]}, at, 0)
		idnApply(s, t)
		s.rdfSettle(t, l)
		oldRefs, newRefs := idnEverMade(t, runner, kg.ID), idnNewObjects(t, s)
		if strings.Join(oldRefs, ",") != strings.Join(newRefs, ",") {
			t.Errorf("step %d: the identification has made %v, the old pipeline %v (%+v)", n, newRefs, oldRefs, counts)
		}
		if counts.Unresolved != 0 || counts.Unsettled != 0 {
			t.Errorf("step %d: %+v", n, counts)
		}
	}
}
