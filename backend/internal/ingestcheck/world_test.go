package ingestcheck

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/filesource"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/worldgen"
)

//go:embed testdata/source.json
var sourceJSON string

//go:embed testdata/mapping.json
var mappingJSON string

var start = time.Date(2031, 3, 1, 0, 0, 0, 0, time.UTC)

// exports are the source export files made from the generated world: one asset ledger (CSV) with racks and servers,
// a virtualization inventory (JSON Lines) with hosts and virtual machines, and a power report (CSV) that gives a
// second value for the virtual machines.
type exports map[string]string

// world makes the files and the truth of a generated world. Without flaws the files carry every row.
func world(t testing.TB, reg *model.Registry, seed uint64, scale int64, tweak func(*worldgen.Params)) (exports, []byte) {
	t.Helper()
	p := worldgen.DefaultParams(seed, scale)
	if tweak != nil {
		tweak(&p)
	}
	w, err := worldgen.Build(reg, p)
	if err != nil {
		t.Fatal(err)
	}
	st := worldgen.NewMemStore()
	if _, err := w.Run(st, worldgen.RunOptions{Truth: true, Views: true}); err != nil {
		t.Fatal(err)
	}
	truth := st.Files[worldgen.TruthFile].Bytes()
	ex := exports{
		"ds_racks":    st.Files["racks.csv"].String(),
		"ds_servers":  st.Files["servers.csv"].String(),
		"ds_hosts":    pagesToLines(t, st, "virt/hosts-"),
		"ds_vms":      pagesToLines(t, st, "virt/vms-"),
		"ds_vm_power": powerReport(t, truth),
	}
	return ex, truth
}

// pagesToLines turns the JSON pages of the virtualization view into one line per item, in page order.
func pagesToLines(t testing.TB, st *worldgen.MemStore, prefix string) string {
	t.Helper()
	var names []string
	for n := range st.Files {
		if strings.HasPrefix(n, prefix) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var out strings.Builder
	for _, n := range names {
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(st.Files[n].Bytes(), &page); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		for _, it := range page.Items {
			out.Write(it)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// powerReport is a second export that names the same virtual machines by their instance uuid.
func powerReport(t testing.TB, truth []byte) string {
	t.Helper()
	var out strings.Builder
	out.WriteString("instance_uuid,power_state\n")
	for _, line := range bytes.Split(truth, []byte("\n")) {
		var rec struct {
			Type  string            `json:"type"`
			Props map[string]any    `json:"props"`
			Ids   map[string]string `json:"identifiers"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "ot.virtual_machine" {
			continue
		}
		if id, ok := rec.Ids["is.vm_instance_uuid"]; ok {
			fmt.Fprintf(&out, "%s,%v\n", id, rec.Props["power_state"])
		}
	}
	return out.String()
}

// pipeline is the ingestion of the example definitions into a fresh reference store.
type pipeline struct {
	t      *testing.T
	reg    *model.Registry
	store  *memstore.Store
	kg     graph.KG
	plan   *ingest.Plan
	runner ingest.Runner
	clock  *kernel.ManualClock
	files  exports
}

func newPipeline(t *testing.T, files exports) *pipeline {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	src, err := ingest.ParseSource(strings.NewReader(sourceJSON))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(mappingJSON))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ingest.Compile(reg, src, mp)
	if err != nil {
		t.Fatal(err)
	}
	p := &pipeline{t: t, reg: reg, plan: plan, store: memstore.New(), clock: kernel.NewManualClock(start), files: files}
	ids := kernel.NewSequentialIDs()
	if p.kg, err = devload.NewKG(context.Background(), p.store, reg, ids, p.clock, "ingested"); err != nil {
		t.Fatal(err)
	}
	p.runner = ingest.Runner{Store: p.store, Reg: reg, Clock: p.clock, IDs: ids, States: ingest.NewMemStateStore()}
	p.runner.Fetcher = filesource.Source{Open: p.open, Files: names(plan)}
	return p
}

func names(plan *ingest.Plan) map[string]string {
	m := map[string]string{}
	for _, d := range plan.Source.Datasets {
		m[d.ID] = d.ID
	}
	return m
}

func (p *pipeline) open(name string) (io.ReadCloser, error) {
	text, ok := p.files[name]
	if !ok {
		return nil, fmt.Errorf("no file %s", name)
	}
	return io.NopCloser(strings.NewReader(text)), nil
}

func (p *pipeline) run() ingest.Report {
	p.t.Helper()
	rep, err := p.runner.Run(context.Background(), p.plan, p.kg.ID)
	if err != nil {
		p.t.Fatalf("run: %v", err)
	}
	return rep
}

// developed loads the truth with the development loader into a store of its own.
func developed(t testing.TB, reg *model.Registry, truth []byte) (*memstore.Store, graph.KG) {
	t.Helper()
	clock := kernel.NewManualClock(start)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	store := memstore.New()
	kg, err := devload.NewKG(context.Background(), store, reg, ids, clock, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (devload.Loader{Store: store, Reg: reg, Clock: clock, IDs: ids}).Load(context.Background(), kg.ID, bytes.NewReader(truth)); err != nil {
		t.Fatal(err)
	}
	return store, kg
}
