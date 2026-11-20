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
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The comparison does what the code that is there does (ADR-0102 decision 3): the same captures, one run after another and each at its
// own time, are given to the pipeline that is there (ingest.Runner, with its state and its KG) and to the comparison on a real
// server, and after every run the raw records, the waits, the counts and the objects that are still alive are held to each other.

const rdfParitySource = `{"id":"src_rdf","name":"rdf","adapter":"file","datasets":[
 {"id":"ds_racks","parser":"csv","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"rack_name"},{"name":"flag"},{"name":"note"}]},
 {"id":"ds_pods","parser":"csv","deletion":"none","natural_key":["asset_id"],"columns":[{"name":"asset_id"},{"name":"rack_name"},{"name":"flag"},{"name":"note"}]}
]}`

const rdfParityMapping = `{"refinements":[
 {"id":"r_racks","input":"ds_racks","ops":[{"op":"filter","column":"flag","in":["on"]}]},
 {"id":"r_pods","input":"ds_pods","ops":[{"op":"filter","column":"flag","in":["on"]}]}],
"feeds":[
 {"id":"f_rack","dataset":"r_racks","object_type":"Rack","creates_objects":true,"retires_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack_name"},{"attribute":"asset_tag","column":"asset_id"}],
  "identity":[{"key_name":"asset_tag","components":[{"property":"asset_tag","column":"asset_id"}],"priority":1}]},
 {"id":"f_pod","dataset":"r_pods","object_type":"Rack","creates_objects":true,"retires_objects":true,"basis":"declared",
  "attributes":[{"attribute":"name","column":"rack_name"},{"attribute":"asset_tag","column":"asset_id"}],
  "identity":[{"key_name":"asset_tag","components":[{"property":"asset_tag","column":"asset_id"}],"priority":1}]}
]}`

// rdfStep is what a source gave at one time: the rows of each dataset as "key/flag/note" (a key twice is a key that repeats), nil when
// the dataset was not given, and whether the read of a dataset stopped short (what it gave is then not used).
type rdfStep struct {
	hour     int
	racks    []string
	pods     []string
	racksCut bool
	podsCut  bool
}

func (s rdfStep) rows(dataset string) []string {
	if dataset == "ds_racks" {
		return s.racks
	}
	return s.pods
}

func (s rdfStep) cut(dataset string) bool {
	if dataset == "ds_racks" {
		return s.racksCut
	}
	return s.podsCut
}

func rdfCSV(rows []string) string {
	var b strings.Builder
	b.WriteString("asset_id,rack_name,flag,note\n")
	for _, r := range rows {
		f := strings.Split(r, "/")
		fmt.Fprintf(&b, "%s,n-%s,%s,%s\n", f[0], f[0], f[1], f[2])
	}
	return b.String()
}

// rdfFetcher gives the old pipeline the captures of the step that is on.
type rdfFetcher struct{ on *rdfStep }

func (f *rdfFetcher) Fetch(_ context.Context, _ string, ds ingest.Dataset) (ingest.Capture, error) {
	rows := f.on.rows(ds.ID)
	if rows == nil {
		return ingest.Capture{}, kernel.Errorf(kernel.CodeNotFound, "not given")
	}
	text := rdfCSV(rows)
	if f.on.cut(ds.ID) {
		return ingest.Capture{Dataset: ds.ID, Data: []byte(text), Hash: kernel.HashBytes([]byte(text)).String(), Complete: false, Reason: "cut"}, nil
	}
	return ingest.Capture{Dataset: ds.ID, Data: []byte(text), Hash: kernel.HashBytes([]byte(text)).String(), Complete: true}, nil
}

// rdfParity holds the old pipeline and the new comparison, run side by side.
type rdfParity struct {
	t      *testing.T
	runner ingest.Runner
	plan   *ingest.Plan
	kg     graph.KG
	clock  *kernel.ManualClock
	fetch  *rdfFetcher
	states *ingest.MemStateStore
	s      *rdfSetup
	alive  map[string]bool
	checks int
}

func rdfNewParity(t *testing.T) *rdfParity {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(rdfParitySource))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(rdfParityMapping))
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
	kg, err := store.CreateKG(context.Background(), graph.KG{ID: kgid, Name: "rdf", Role: graph.RoleProduction, State: graph.StateBuilding, DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fetch := &rdfFetcher{}
	states := ingest.NewMemStateStore()
	return &rdfParity{t: t, plan: plan, kg: kg, clock: clock, fetch: fetch, states: states, alive: map[string]bool{},
		runner: ingest.Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, Fetcher: fetch, States: states},
		s:      rdfNew(t, pgiPlan(t, rdfParitySource, rdfParityMapping))}
}

// step runs both sides on one step and holds them to each other.
func (p *rdfParity) step(n int, st rdfStep) {
	p.t.Helper()
	at := rdfT0.Add(time.Duration(st.hour) * time.Hour)
	p.fetch.on = &st
	p.clock.Set(at)
	oldRep, err := p.runner.Run(context.Background(), p.plan, p.kg.ID)
	if err != nil {
		p.t.Fatalf("step %d: the old run: %v", n, err)
	}
	rep := p.newRun(n, st, at)
	for _, ds := range []string{"ds_racks", "ds_pods"} {
		p.compareDataset(n, ds, st, oldRep, rep)
	}
	p.compareAlive(n)
}

// newRun receives the step the way the receiving does, runs the comparison at the time of the step and does what the cleaning will.
func (p *rdfParity) newRun(n int, st rdfStep, at time.Time) ingest.RawDiffReport {
	p.t.Helper()
	s := p.s
	texts := pgiTexts{}
	for _, ds := range []string{"ds_racks", "ds_pods"} {
		if rows := st.rows(ds); rows != nil {
			texts[ds] = rdfCSV(rows)
		}
	}
	l := s.e.open(p.t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: s.plan}); err != nil {
		p.t.Fatalf("step %d: %v", n, err)
	}
	for _, ds := range []string{"ds_racks", "ds_pods"} {
		if st.cut(ds) {
			s.e.exec(p.t, `UPDATE sync_run SET counts = jsonb_set(counts, '{receive,datasets}', (SELECT jsonb_agg(CASE WHEN d->>'dataset' = $2 THEN d || '{"coverage":"incomplete","complete":false}' ELSE d END)
				FROM jsonb_array_elements(counts->'receive'->'datasets') d)) WHERE run_no = $1`, l.No, ds)
		}
	}
	start := ingest.RawDiffStart{Now: at}
	for _, d := range s.plan.Source.Datasets {
		start.Datasets = append(start.Datasets, ingest.RawDiffDataset{Dataset: d.ID, Retire: d.Deletion == ingest.DeletionAbsentInFull})
	}
	rep := s.run(p.t, l, start)
	p.track(st, s.records(p.t), s.calcWaits(p.t))
	s.settle(p.t, l)
	return rep
}

// track follows which objects the comparison leaves alive: a shown row that is not filtered makes its object, a shown row that is
// filtered keeps the object that is there until the wait is up, and a record that is closed takes its object away.
func (p *rdfParity) track(st rdfStep, recs []rdfRecord, waits []rdfWaitOut) {
	for _, ds := range []string{"ds_racks", "ds_pods"} {
		if st.rows(ds) == nil || st.cut(ds) {
			continue
		}
		expired := map[string]bool{}
		for _, w := range waits {
			if w.Dataset == ds && w.Kind == pgschema.WaitFiltered {
				expired[w.Key] = w.Expired
			}
		}
		for _, row := range st.rows(ds) {
			f := strings.Split(row, "/")
			key := ds + "/" + f[0]
			if f[1] == "on" {
				p.alive[key] = true
			} else {
				p.alive[key] = p.alive[key] && !expired[f[0]]
			}
		}
		for _, r := range recs {
			if r.Dataset == ds && r.Why == pgschema.CalcWhyClosed {
				p.alive[ds+"/"+r.Key] = false
			}
		}
	}
}

func (p *rdfParity) compareDataset(n int, ds string, st rdfStep, oldRep ingest.Report, rep ingest.RawDiffReport) {
	p.t.Helper()
	s := p.s
	state, err := p.states.Load(context.Background(), p.plan.Source.ID)
	if err != nil {
		p.t.Fatal(err)
	}
	old := state.Datasets[ds]
	var wantKeys []string
	wantUnseen, wantFiltered := map[string]string{}, map[string]string{}
	if old != nil {
		for k := range old.Open {
			wantKeys = append(wantKeys, k)
		}
		for k, u := range old.Unseen {
			wantUnseen[k] = fmt.Sprintf("%d@%s", u.Runs, u.First)
		}
		for k, u := range old.Filtered {
			wantFiltered[k] = fmt.Sprintf("%d@%s", u.Runs, u.First)
		}
	}
	sort.Strings(wantKeys)
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT key FROM %s WHERE dataset_no = $1 ORDER BY key`, pgiTable(s.ref, pgschema.TableRawRecord)), s.no(ds))
	if err != nil {
		p.t.Fatal(err)
	}
	var gotKeys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			p.t.Fatal(err)
		}
		gotKeys = append(gotKeys, k)
	}
	_ = rows.Close()
	if strings.Join(gotKeys, ",") != strings.Join(wantKeys, ",") {
		p.t.Errorf("step %d, %s: the raw records are %v, the old pipeline keeps %v", n, ds, gotKeys, wantKeys)
	}
	for kind, want := range map[int16]map[string]string{pgschema.WaitUnseen: wantUnseen, pgschema.WaitFiltered: wantFiltered} {
		got := map[string]string{}
		ws, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT key, runs, first_at FROM "s_%d".record_wait WHERE dataset_no = $1 AND kind = $2`, s.ref.KGNo), s.no(ds), kind)
		if err != nil {
			p.t.Fatal(err)
		}
		for ws.Next() {
			var k string
			var runs int
			var first time.Time
			if err := ws.Scan(&k, &runs, &first); err != nil {
				p.t.Fatal(err)
			}
			got[k] = fmt.Sprintf("%d@%s", runs, first.UTC().Format(time.RFC3339))
		}
		_ = ws.Close()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			p.t.Errorf("step %d, %s, wait kind %d: %v, the old pipeline counts %v", n, ds, kind, got, want)
		}
	}
	p.compareCounts(n, ds, oldRep, rep)
	p.checks++
}

func (p *rdfParity) compareCounts(n int, ds string, oldRep ingest.Report, rep ingest.RawDiffReport) {
	p.t.Helper()
	var old ingest.RawChanges
	for _, d := range oldRep.Datasets {
		if d.Dataset == ds {
			old = d.Raw
		}
	}
	var got ingest.RawDiffCounts
	for _, c := range rep.Counts {
		if c.Dataset == ds {
			got = c
		}
	}
	if got.Added != old.Added || got.Modified != old.Modified || got.Unchanged != old.Unchanged || got.Deleted != old.Deleted || got.Unseen != old.Unseen {
		p.t.Errorf("step %d, %s: the counts are %+v, the old pipeline counts %+v", n, ds, got, old)
	}
}

// compareAlive holds the objects the old pipeline leaves alive to those the comparison leaves alive.
func (p *rdfParity) compareAlive(n int) {
	p.t.Helper()
	snap, err := p.runner.Store.Snapshot(context.Background(), graph.ReadPoint{KG: p.kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		p.t.Fatal(err)
	}
	all, err := snap.ScanObjects(context.Background(), graph.ObjectID{}, 1000)
	if err != nil {
		p.t.Fatal(err)
	}
	var live []string
	for _, o := range all {
		if o.SeqTo == 0 {
			live = append(live, o.Name)
		}
	}
	var want []string
	for key, ok := range p.alive {
		if ok {
			want = append(want, "n-"+key[strings.Index(key, "/")+1:])
		}
	}
	sort.Strings(live)
	sort.Strings(want)
	if strings.Join(live, ",") != strings.Join(want, ",") {
		p.t.Errorf("step %d: the objects that are alive are %v, the old pipeline keeps %v", n, want, live)
	}
}

// rdfParitySteps is the table of captures: a key that repeats, a key that is gone for a while and comes back, one that stays gone, a
// row that is filtered and then freed and then shown whole again, a dataset that is not given, one that is cut, and a dataset that
// never retires what is gone.
func rdfParitySteps() []rdfStep {
	return []rdfStep{
		{hour: 0, racks: []string{"a/on/1", "b/on/1", "b/on/2", "c/on/1", "d/on/1", "e/on/1", "f/on/1"}, pods: []string{"p1/on/1", "p2/on/1", "p3/on/1"}},
		{hour: 1, racks: []string{"a/on/1", "b/on/2", "c/on/1", "d/off/1", "e/off/1"}, pods: []string{"p1/on/1"}},
		{hour: 2, racks: []string{"a/on/1", "b/on/2", "d/off/1", "e/off/1"}},
		{hour: 3, racks: []string{"a/on/9"}, racksCut: true, pods: []string{"p1/on/1", "p2/on/1"}},
		{hour: 30, racks: []string{"a/on/1", "b/on/2", "d/off/1", "e/on/1", "f/on/1"}, pods: []string{"p1/on/5", "p3/off/1"}},
		{hour: 31, racks: []string{"a/on/1", "b/on/2", "d/off/1", "f/on/1"}, pods: []string{"p1/on/5", "p2/on/1", "p3/off/1"}},
		{hour: 52, racks: []string{"a/on/1", "d/off/1", "f/on/1", "g/on/1", "g/on/2"}, pods: []string{"p1/on/5", "p3/off/1"}},
		{hour: 53, racks: []string{"a/on/1", "d/off/2", "g/on/2"}, pods: []string{"p1/on/5", "p3/off/1"}},
		{hour: 60, racks: []string{"a/on/1", "d/on/1", "g/on/2", "h/off/1"}, pods: []string{"p1/on/5", "p3/off/1"}},
		{hour: 61, racks: []string{"a/on/1", "d/on/1", "g/on/2", "h/on/1"}, pods: []string{"p3/on/1"}},
		{hour: 90, racks: []string{"a/on/1"}, pods: []string{"p1/on/5"}},
		{hour: 91, racks: []string{"a/on/1"}, pods: []string{"p1/on/5"}},
		{hour: 120, racks: []string{"a/on/1", "x/off/1", "y/on/1"}, pods: []string{"p1/on/5"}},
		{hour: 121, racks: []string{"a/on/1", "x/off/1"}, pods: []string{"p1/on/5"}},
		{hour: 122, racks: []string{"a/on/1", "x/off/1"}, pods: []string{"p1/on/5"}},
		{hour: 150, racks: []string{"a/on/1", "x/off/1", "y/on/1"}},
		{hour: 151, racks: []string{"a/on/1", "x/on/1", "y/on/1"}, pods: []string{"p1/on/5"}},
	}
}

// [REQ-784]
func TestRdfTheComparisonGivesTheRawRecordsTheWaitsTheCountsAndTheLiveObjectsTheOldPipelineGivesForTheSameCaptures(t *testing.T) {
	p := rdfNewParity(t)
	steps := rdfParitySteps()
	for n, st := range steps {
		p.step(n, st)
	}
	if p.checks != 2*len(steps) {
		t.Fatalf("%d comparisons for %d steps", p.checks, len(steps))
	}
}
