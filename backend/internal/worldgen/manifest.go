package worldgen

import (
	"encoding/json"
	"io"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ManifestFormat names the manifest; ManifestFile is where it is written.
const (
	ManifestFormat = "praxium-worldgen-manifest"
	ManifestFile   = "manifest.json"
)

// ManifestCounts are the numbers of records.
type ManifestCounts struct {
	Objects              int64            `json:"objects"`
	Links                int64            `json:"links"`
	ObjectsNotIngestible int64            `json:"objects_not_ingestible"`
	LinksNotIngestible   int64            `json:"links_not_ingestible"`
	ObjectsByType        map[string]int64 `json:"objects_by_type"`
	LinksByType          map[string]int64 `json:"links_by_type"`
	ObjectsByLayer       map[string]int64 `json:"objects_by_layer"`
}

// LayoutInfo is the structure of the estate.
type LayoutInfo struct {
	Sites    int `json:"sites"`
	Halls    int `json:"halls"`
	Racks    int `json:"racks"`
	Servers  int `json:"servers"`
	Hosts    int `json:"hosts"`
	VMs      int `json:"vms"`
	Services int `json:"services"`
}

// SourceInfo says what a source delivers.
type SourceInfo struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Types       []string `json:"types"`
	Properties  []string `json:"properties"`
}

// ImperfectionReport is the flaws of the views: totals per kind and samples.
type ImperfectionReport struct {
	Counts  map[string]int64 `json:"counts"`
	ByFile  map[string]int64 `json:"by_file"`
	Samples []Imperfection   `json:"samples"`
}

// Manifest describes a run: parameters, counts, content hash, files, sources
// and imperfections (docs/design-docs/worldgen.md).
type Manifest struct {
	Format        string             `json:"format"`
	Version       int                `json:"version"`
	ModelVersion  int                `json:"model_version"`
	ModelHash     string             `json:"model_hash"`
	Params        Params             `json:"params"`
	Layout        LayoutInfo         `json:"layout"`
	Counts        ManifestCounts     `json:"counts"`
	Truth         *TruthInfo         `json:"truth,omitempty"`
	ContentHash   string             `json:"content_hash"`
	Files         []FileInfo         `json:"files"`
	Sources       []SourceInfo       `json:"sources,omitempty"`
	Imperfections ImperfectionReport `json:"imperfections"`
	ChangeEvents  int                `json:"change_events"`
	Shape         *ShapeCard         `json:"shape,omitempty"`
}

// RunOptions chooses what a run writes.
type RunOptions struct {
	Truth   bool // write truth.jsonl (else only count)
	Views   bool // write the source views
	Changes bool // write the change stream and the expected state
	Shape   bool // compute and write the shape card
}

// Run generates the world once, writes what the options ask for to the
// store, and writes and returns the manifest.
func (w *World) Run(st Store, opt RunOptions) (*Manifest, error) {
	m := &Manifest{Format: ManifestFormat, Version: FormatVersion, Params: w.P}
	var sink io.Writer = io.Discard
	if opt.Truth {
		f, err := st.Create(TruthFile)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		sink = f
	}
	tw := NewTruthWriter(sink)
	counts := &tw.Counts
	obs := []Observer{tw}
	var views *Views
	if opt.Views {
		views = NewViews(w.P, w, st)
		obs = append(obs, views)
	}
	var sc *ShapeCollector
	if opt.Shape {
		sc = NewShapeCollector()
		obs = append(obs, sc)
	}
	if err := w.Generate(obs...); err != nil {
		return nil, err
	}
	log := newImperfectionLog()
	if err := w.finishRun(m, st, opt, parts{tw, views, sc, log}); err != nil {
		return nil, err
	}
	w.fillCounts(m, counts)
	return m, w.writeManifest(st, m)
}

// parts are the writers of one run.
type parts struct {
	tw    *TruthWriter
	views *Views
	sc    *ShapeCollector
	log   *ImperfectionLog
}

func (w *World) finishRun(m *Manifest, st Store, opt RunOptions, r parts) error {
	tw, views, sc, log := r.tw, r.views, r.sc, r.log
	info, err := tw.Close()
	if err != nil {
		return err
	}
	m.ContentHash = info.SHA256
	if opt.Truth {
		m.Truth = &info
		m.Files = append(m.Files, FileInfo{Name: TruthFile, Source: "truth", Format: "jsonl", Lines: info.Records, Bytes: info.Bytes, SHA256: info.SHA256})
	}
	if views != nil {
		files, err := views.Close()
		if err != nil {
			return err
		}
		m.Files = append(m.Files, files...)
		log = views.Log()
		m.Sources = sourceCatalog()
	}
	if opt.Changes {
		files, err := w.WriteChanges(st, log)
		if err != nil {
			return err
		}
		m.Files = append(m.Files, files...)
		m.ChangeEvents = int(w.P.Changes.Events)
	}
	m.Imperfections = ImperfectionReport{Counts: log.Counts, ByFile: log.ByFile, Samples: log.Samples}
	if sc != nil {
		m.Shape = sc.Card()
		b, _ := json.MarshalIndent(m.Shape, "", "  ")
		f, err := st.Create(ShapeFile)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func (w *World) fillCounts(m *Manifest, c *Counts) {
	m.ModelVersion = w.sch.reg.ModelVersion()
	m.ModelHash = w.sch.reg.Hash().String()
	racks, servers, hosts, vms, services := w.Lay.Counts()
	halls := 0
	for _, s := range w.Lay.Sites {
		halls += s.Halls
	}
	m.Layout = LayoutInfo{len(w.Lay.Sites), halls, racks, servers, hosts, vms, services}
	mc := ManifestCounts{Objects: c.TotalObjects, Links: c.TotalLinks, ObjectsNotIngestible: c.ObjectsNI, LinksNotIngestible: c.LinksNI,
		ObjectsByType: map[string]int64{}, LinksByType: map[string]int64{}, ObjectsByLayer: map[string]int64{}}
	for i, n := range c.Objects {
		if n > 0 {
			mc.ObjectsByType[typeNames[i]] = n
			mc.ObjectsByLayer[string(w.sch.types[i].layer)] += n
		}
	}
	for i, n := range c.Links {
		if n > 0 {
			mc.LinksByType[linkNames[i]] = n
		}
	}
	m.Counts = mc
}

func (w *World) writeManifest(st Store, m *Manifest) error {
	f, err := st.Create(ManifestFile)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		_ = f.Close()
		return kernel.Errorf(kernel.CodeInternal, "manifest: %v", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// sourceCatalog lists the invented sources and what each carries.
func sourceCatalog() []SourceInfo {
	c := []SourceInfo{
		{SrcRegister, "csv tables", "asset register: racks, servers and network devices, keyed by asset id", []string{"Rack", "PhysicalServer", "Switch", "Router"},
			[]string{"name", "total_u", "hostname", "serial_number", "manufacturer", "model", "power_state", "fabric_role", "management_address"}},
		{SrcVirt, "json api pages", "virtualization inventory: hosts and VMs with nested guests, NICs and disks; the batch half of VM facts", []string{"HypervisorHost", "VirtualMachine"},
			[]string{"hostname", "connection_state", "name", "vcpu_count", "memory_gib", "guest_os_family", "instance_uuid"}},
		{SrcNetconf, "device configuration text", "hierarchical configuration of switches and routers: interfaces, LAGs and ECMP groups", []string{"Switch", "Router", "Interface", "Lag", "EcmpGroup"},
			[]string{"hostname", "fabric_role", "if_name", "speed_mbps", "configured_max_paths"}},
		{SrcShow, "command-output tables", "fixed-column table of ECMP groups per site", []string{"EcmpGroup"}, []string{"name", "members"}},
		{SrcDeclared, "jsonl", "declared list of ECMP groups and their members", []string{"EcmpGroup"}, []string{"name", "members"}},
		{SrcFeed, "change stream jsonl", "added, modified and deleted entities with source time; the stream half of VM facts (power_state)", []string{"VirtualMachine", "MemoryModule", "links"},
			[]string{"power_state", "network_role", "placement"}},
	}
	sort.Slice(c, func(i, j int) bool { return c[i].Name < c[j].Name })
	return c
}
