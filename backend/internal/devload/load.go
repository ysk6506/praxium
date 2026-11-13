package devload

import (
	"context"
	"io"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// DefaultBatchItems is the most objects or links one change batch holds.
const DefaultBatchItems = 5000

// leaseTTL is how long the loader keeps its lease between two batches. A load of a large world takes minutes, and
// nothing else writes to a KG that lives in a process of its own.
const leaseTTL = 12 * time.Hour

// sourceID names the source of the batches this loader writes.
const sourceID = "devload"

// Loader applies a truth file to a KG.
type Loader struct {
	Store graph.Store
	Reg   *model.Registry
	Clock kernel.Clock
	IDs   kernel.IDGenerator
	// Keys keeps the hash key of the KG; nil means the development key of the graph unit.
	Keys graph.KeyProvider
	// BatchItems bounds a batch; 0 means DefaultBatchItems.
	BatchItems int
}

// Report says what a load did. The counts of the truth are what the generator's manifest says (objects and
// links, and how many of them no source can deliver); the loaded counts are what went into the KG.
type Report struct {
	TruthObjects         int64  `json:"truth_objects"`
	TruthLinks           int64  `json:"truth_links"`
	ObjectsNotIngestible int64  `json:"objects_not_ingestible"`
	LinksNotIngestible   int64  `json:"links_not_ingestible"`
	ObjectsUnkeyed       int64  `json:"objects_unkeyed"`
	LinksDangling        int64  `json:"links_dangling"`
	Objects              int64  `json:"objects"`
	Links                int64  `json:"links"`
	VolatileValues       int64  `json:"volatile_values"`
	Identifiers          int64  `json:"identifiers"`
	IdentifiersInvalid   int64  `json:"identifiers_invalid"`
	Batches              int    `json:"batches"`
	Chunks               int    `json:"chunks"`
	ChangeSeq            int64  `json:"change_seq"`
	VersionID            string `json:"version_id"`
	GraphHash            string `json:"graph_hash"`
}

// NewKG creates the production KG a load fills: building until the first load seals.
func NewKG(ctx context.Context, store graph.Catalog, reg *model.Registry, ids kernel.IDGenerator, clock kernel.Clock, name string) (graph.KG, error) {
	id, err := kernel.NewID[graph.KGKind](ids, "kg")
	if err != nil {
		return graph.KG{}, err
	}
	return store.CreateKG(ctx, graph.KG{ID: id, Name: name, Role: graph.RoleProduction, State: graph.StateBuilding,
		DefinitionSetHash: reg.Hash(), CreatedAt: clock.Now()})
}

// Load reads the truth and applies it to the KG, then seals a version. The KG should be empty.
func (l Loader) Load(ctx context.Context, kg graph.KGID, truth io.Reader) (Report, error) {
	w, err := readWorld(l.Reg, truth)
	if err != nil {
		return Report{}, err
	}
	if err := w.resolveAll(); err != nil {
		return Report{}, err
	}
	rep := Report{TruthObjects: int64(len(w.objects)), TruthLinks: int64(len(w.edges))}
	keys := l.Keys
	if keys == nil {
		keys = graph.NewTestKeyProvider("praxium-dev-load")
	}
	applier, err := graph.NewApplier(l.Store, l.Reg, keys, l.Clock, l.IDs, graph.Options{Semantic: 1, LeaseTTL: leaseTTL})
	if err != nil {
		return rep, err
	}
	lease, err := applier.Acquire(ctx, kg)
	if err != nil {
		return rep, err
	}
	defer func() { _ = applier.Release(ctx, lease) }()
	r := &run{l: l, w: w, a: applier, kg: kg, lease: lease, rep: &rep,
		attrs: attrBuilder{reg: l.Reg, seen: l.Clock.Now().UTC().Format("2006-01-02T15:04:05Z")}}
	if err := r.objects(ctx); err != nil {
		return rep, err
	}
	if err := r.links(ctx); err != nil {
		return rep, err
	}
	return r.seal(ctx)
}

func (l Loader) size() int {
	if l.BatchItems > 0 {
		return l.BatchItems
	}
	return DefaultBatchItems
}
