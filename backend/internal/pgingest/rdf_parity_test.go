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

// rdfLooseMapping is the mapping that filters nothing: the same feeds, and the rows a rule filtered out are used.
var rdfLooseMapping = strings.ReplaceAll(rdfParityMapping, `"in":["on"]`, `"in":["on","off"]`)

// rdfStep is what a source gave at one time: the rows of each dataset as "key/flag/note" (a key twice is a key that repeats), nil when
// the dataset was not given, and whether the read of a dataset stopped short (what it gave is then not used).
type rdfStep struct {
	// loose is the step run under a mapping that does not filter (the rows that were filtered are shown whole), the one before and
	// after under the mapping that filters: the same rows, so that a row whose hash is the same has only its filtering turned over.
	loose    bool
	hour     int
	racks    []string
	pods     []string
	racksCut bool
	podsCut  bool
}

func (s rdfStep) rdfRows(dataset string) []string {
	if dataset == "ds_racks" {
		return s.racks
	}
	return s.pods
}

func (s rdfStep) rdfCut(dataset string) bool {
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
	rows := f.on.rdfRows(ds.ID)
	if rows == nil {
		return ingest.Capture{}, kernel.Errorf(kernel.CodeNotFound, "not given")
	}
	text := rdfCSV(rows)
	if f.on.rdfCut(ds.ID) {
		return ingest.Capture{Dataset: ds.ID, Data: []byte(text), Hash: kernel.HashBytes([]byte(text)).String(), Complete: false, Reason: "cut"}, nil
	}
	return ingest.Capture{Dataset: ds.ID, Data: []byte(text), Hash: kernel.HashBytes([]byte(text)).String(), Complete: true}, nil
}

// rdfParity holds the old pipeline and the new comparison, run side by side.
type rdfParity struct {
	t      *testing.T
	runner ingest.Runner
	plan   *ingest.Plan
	// loosePlan is the mapping of a step that is loose; it has the datasets of plan and no filter.
	loosePlan *ingest.Plan
	looseAt   []bool
	// flips holds the counts and the time a record was opened to what a row whose filtering turned over makes of them: the old
	// pipeline calls it the same row and the comparison calls it changed (ADR-0102 decision 7).
	flips bool
	// beforeSettle, when it is set, is called after the comparison and before what the cleaning does: a later step's work.
	beforeSettle func()
	kg           graph.KG
	clock        *kernel.ManualClock
	fetch        *rdfFetcher
	states       *ingest.MemStateStore
	s            *rdfSetup
	alive        map[string]bool
	checks       int
	// size is the number of rows to a bucket of the comparison (0 is the default one) and runNos the number of the run of each step,
	// by which the old pipeline's run counts are told from the ones the comparison opens its records with.
	size   int
	runNos []int64
	// onlyRaw leaves the objects out of what is held to the old pipeline: two datasets that make the same objects (random captures
	// give both the same keys) are not what the model of rdfTrack follows.
	onlyRaw bool
}

func rdfNewParity(t *testing.T, size int) *rdfParity {
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
	looseMapping, err := ingest.ParseMapping(strings.NewReader(rdfLooseMapping))
	if err != nil {
		t.Fatal(err)
	}
	loosePlan, err := ingest.Compile(reg, src, looseMapping)
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
	return &rdfParity{t: t, size: size, plan: plan, loosePlan: loosePlan, kg: kg, clock: clock, fetch: fetch, states: states, alive: map[string]bool{},
		runner: ingest.Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, Fetcher: fetch, States: states},
		s:      rdfNew(t, pgiPlan(t, rdfParitySource, rdfParityMapping))}
}

// step runs both sides on one step and holds them to each other.
func (p *rdfParity) rdfStepOn(n int, st rdfStep) {
	p.t.Helper()
	at := rdfT0.Add(time.Duration(st.hour) * time.Hour)
	p.fetch.on = &st
	p.clock.Set(at)
	plan := p.plan
	if st.loose {
		plan = p.loosePlan
	}
	p.looseAt = append(p.looseAt, st.loose)
	oldRep, err := p.runner.Run(context.Background(), plan, p.kg.ID)
	if err != nil {
		p.t.Fatalf("step %d: the old run: %v", n, err)
	}
	rep := p.rdfNewRun(n, st, at, plan)
	for _, ds := range []string{"ds_racks", "ds_pods"} {
		p.rdfCompareDataset(n, ds, st, oldRep, rep)
	}
	if !p.onlyRaw {
		p.rdfCompareAlive(n)
	}
}

// newRun receives the step the way the receiving does, runs the comparison at the time of the step and does what the cleaning will.
func (p *rdfParity) rdfNewRun(n int, st rdfStep, at time.Time, plan *ingest.Plan) ingest.RawDiffReport {
	p.t.Helper()
	s := p.s
	texts := pgiTexts{}
	for _, ds := range []string{"ds_racks", "ds_pods"} {
		if rows := st.rdfRows(ds); rows != nil {
			texts[ds] = rdfCSV(rows)
		}
	}
	l := s.e.open(p.t, s.kg, s.plan.Source.ID, "server-a")
	if _, err := s.e.st.Receive(s.e.ctx, l, ingest.Intake{Reader: texts, Plan: plan}); err != nil {
		p.t.Fatalf("step %d: %v", n, err)
	}
	for _, ds := range []string{"ds_racks", "ds_pods"} {
		if st.rdfCut(ds) {
			s.e.exec(p.t, `UPDATE sync_run SET counts = jsonb_set(counts, '{receive,datasets}', (SELECT jsonb_agg(CASE WHEN d->>'dataset' = $2 THEN d || '{"coverage":"incomplete","complete":false}' ELSE d END)
				FROM jsonb_array_elements(counts->'receive'->'datasets') d)) WHERE run_no = $1`, l.No, ds)
		}
	}
	p.runNos = append(p.runNos, l.No)
	start := ingest.RawDiffStart{Now: at, BucketRows: p.size}
	for _, d := range s.plan.Source.Datasets {
		start.Datasets = append(start.Datasets, ingest.RawDiffDataset{Dataset: d.ID, Retire: d.Deletion == ingest.DeletionAbsentInFull})
	}
	rep := s.rdfRun(p.t, l, start)
	p.rdfTrack(st, s.rdfRecords(p.t), s.rdfCalcWaits(p.t))
	if p.beforeSettle != nil {
		p.beforeSettle()
	}
	s.rdfSettle(p.t, l)
	return rep
}

// track follows which objects the comparison leaves alive: a shown row that is not filtered makes its object, a record that a rule
// filters keeps the object that is there until the wait is up (shown or not), and a record that is closed takes its object away.
func (p *rdfParity) rdfTrack(st rdfStep, recs []rdfRecord, waits []rdfWaitOut) {
	for _, ds := range []string{"ds_racks", "ds_pods"} {
		if st.rdfRows(ds) == nil || st.rdfCut(ds) {
			continue
		}
		for _, row := range st.rdfRows(ds) {
			f := strings.Split(row, "/")
			if f[1] == "on" || st.loose {
				p.alive[ds+"/"+f[0]] = true
			}
		}
		for _, w := range waits {
			if w.Dataset == ds && w.Kind == pgschema.WaitFiltered && w.Expired {
				p.alive[ds+"/"+w.Key] = false
			}
		}
		for _, r := range recs {
			if r.Dataset == ds && r.Why == pgschema.CalcWhyClosed {
				p.alive[ds+"/"+r.Key] = false
			}
		}
	}
}

func (p *rdfParity) rdfCompareDataset(n int, ds string, st rdfStep, oldRep ingest.Report, rep ingest.RawDiffReport) {
	p.t.Helper()
	state, err := p.states.Load(context.Background(), p.plan.Source.ID)
	if err != nil {
		p.t.Fatal(err)
	}
	old := state.Datasets[ds]
	wantUnseen, wantFiltered := map[string]string{}, map[string]string{}
	if old != nil {
		for k, u := range old.Unseen {
			wantUnseen[k] = fmt.Sprintf("%d@%s", u.Runs, u.First)
		}
		for k, u := range old.Filtered {
			wantFiltered[k] = fmt.Sprintf("%d@%s", u.Runs, u.First)
		}
	}
	p.rdfCompareRaw(n, ds, st, old)
	p.rdfCompareWaits(n, ds, map[int16]map[string]string{pgschema.WaitUnseen: wantUnseen, pgschema.WaitFiltered: wantFiltered})
	p.rdfCompareCounts(n, ds, oldRep, rep)
	p.checks++
}

// rdfCompareRaw holds the raw records of a dataset, each with its content, its hash, its fate and the run that opened it, to the
// open records of the old pipeline.
func (p *rdfParity) rdfCompareRaw(n int, ds string, st rdfStep, old *ingest.DatasetState) {
	p.t.Helper()
	s := p.s
	var wantKeys []string
	wantRows := map[string]string{}
	if old != nil {
		for k, rec := range old.Open {
			wantKeys = append(wantKeys, k)
			loose, opened := p.looseAt[rec.FromRun-1], p.runNos[rec.FromRun-1]
			if p.flips {
				// every record is shown in every step, so it is as filtered as this step's mapping says; and the time it was opened
				// at is not held to the old pipeline, which does not open a record again for a row whose hash is the same
				loose, opened = st.loose, 0
			}
			fate := pgschema.FateUsed
			if rec.Fields["flag"] != "on" && !loose {
				fate = pgschema.FateFiltered
			}
			wantRows[k] = fmt.Sprintf("%s|%s|%s|fate %d|opened by run %d", rec.Fields["asset_id"], rec.Fields["rack_name"], rec.Fields["flag"]+"/"+rec.Fields["note"]+"|"+rec.Hash, fate, opened)
		}
	}
	sort.Strings(wantKeys)
	rows, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT key, array_to_string(fields, '|'), encode(raw_hash, 'hex'), fate, opened_seq FROM %s WHERE dataset_no = $1 ORDER BY key`, pgiTable(s.ref, pgschema.TableRawRecord)), s.rdfNo(ds))
	if err != nil {
		p.t.Fatal(err)
	}
	var gotKeys []string
	gotRows := map[string]string{}
	for rows.Next() {
		var k, fields, hash string
		var fate int16
		var opened int64
		if err := rows.Scan(&k, &fields, &hash, &fate, &opened); err != nil {
			p.t.Fatal(err)
		}
		gotKeys = append(gotKeys, k)
		parts := strings.Split(fields, "|")
		if p.flips {
			opened = 0
		}
		gotRows[k] = fmt.Sprintf("%s|%s|%s|fate %d|opened by run %d", parts[0], parts[1], parts[2]+"/"+parts[3]+"|"+hash, fate, opened)
	}
	_ = rows.Close()
	if strings.Join(gotKeys, ",") != strings.Join(wantKeys, ",") {
		p.t.Errorf("step %d, %s: the raw records are %v, the old pipeline keeps %v", n, ds, gotKeys, wantKeys)
	}
	for _, k := range wantKeys {
		if gotRows[k] != wantRows[k] {
			p.t.Errorf("step %d, %s, %s: the raw record is %s, the old pipeline keeps %s", n, ds, k, gotRows[k], wantRows[k])
		}
	}
}

// rdfCompareWaits holds the waits of a dataset, by kind, with their runs and first times, to those the old pipeline counts.
func (p *rdfParity) rdfCompareWaits(n int, ds string, wants map[int16]map[string]string) {
	p.t.Helper()
	s := p.s
	for kind, want := range wants {
		got := map[string]string{}
		ws, err := s.e.admin.QueryContext(s.e.ctx, fmt.Sprintf(`SELECT key, runs, first_at FROM "s_%d".record_wait WHERE dataset_no = $1 AND kind = $2`, s.ref.KGNo), s.rdfNo(ds), kind)
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
}

func (p *rdfParity) rdfCompareCounts(n int, ds string, oldRep ingest.Report, rep ingest.RawDiffReport) {
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
	if p.flips {
		// a row whose filtering turned over is changed to the comparison and the same to the old pipeline: the two are held together
		got.Modified, got.Unchanged, old.Modified, old.Unchanged = got.Modified+got.Unchanged, 0, old.Modified+old.Unchanged, 0
	}
	if got.Added != old.Added || got.Modified != old.Modified || got.Unchanged != old.Unchanged || got.Deleted != old.Deleted || got.Unseen != old.Unseen {
		p.t.Errorf("step %d, %s: the counts are %+v, the old pipeline counts %+v", n, ds, got, old)
	}
}

// compareAlive holds the objects the old pipeline leaves alive to those the comparison leaves alive.
func (p *rdfParity) rdfCompareAlive(n int) {
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
		// a row that is filtered, then not shown, then filtered again, and so on: the wait for it goes on while its record is open, so
		// it is let go of in the run that is the third and long enough (the one that does not show it), and the pod's, which never
		// retires what is gone, is counted and never let go of
		{hour: 200, racks: []string{"a/on/1", "q/on/1"}, pods: []string{"p1/on/5", "pq/on/1"}},
		{hour: 210, racks: []string{"a/on/1", "q/off/1"}, pods: []string{"p1/on/5", "pq/off/1"}},
		{hour: 220, racks: []string{"a/on/1"}, pods: []string{"p1/on/5"}},
		{hour: 230, racks: []string{"a/on/1", "q/off/1"}, pods: []string{"p1/on/5", "pq/off/1"}},
		{hour: 240, racks: []string{"a/on/1"}, pods: []string{"p1/on/5"}},
		{hour: 250, racks: []string{"a/on/1", "q/off/1"}, pods: []string{"p1/on/5", "pq/off/1"}},
		{hour: 260, racks: []string{"a/on/1"}, pods: []string{"p1/on/5"}},
		{hour: 270, racks: []string{"a/on/1", "q/on/2"}, pods: []string{"p1/on/5", "pq/on/2"}},
	}
}

// [REQ-784]
func TestRdfTheComparisonGivesTheRawRecordsTheWaitsTheCountsAndTheLiveObjectsTheOldPipelineGivesForTheSameCaptures(t *testing.T) {
	// the table is run in one bucket and in buckets of one row, which must give the same as the old pipeline each
	for _, size := range []int{0, 1} {
		p := rdfNewParity(t, size)
		steps := rdfParitySteps()
		for n, st := range steps {
			p.rdfStepOn(n, st)
		}
		if p.checks != 2*len(steps) {
			t.Fatalf("%d comparisons for %d steps", p.checks, len(steps))
		}
	}
}
