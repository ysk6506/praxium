package pgstore

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The KG storage (ADR-0089, ADR-0094). A KG stands on its own in a storage schema of its own (s_<kg_no>): its head,
// its lease, its generations, its versions, its rows and its change records are there, so one transaction of the
// applier never leaves the schema. What is shared is the list of KGs (kg_catalog), the numbers of objects and
// links, the state of the schemas (kg_schema_state), the identity records and the number of the storage format.

// ModelProvider gives the model a KG is made from, by the hash of its definition set. A server holds the
// model it was built with; a KG whose storage schema was made from another model is refused (CodeSchemaMismatch).
type ModelProvider func(definitionSetHash kernel.Hash) (SchemaModel, error)

// StandardModels is the provider of the standard model: every hash gets it, so a KG is made from it when its
// definition set is the standard one (CreateKG refuses a KG whose hash is not the model's). withStats says
// whether the statistics objects are made together with the tables. A server passes true (it is what Open holds when it is given no
// model). False is the light model of the tests that are not about the query schema or the statistics: a KG made from it has the
// tables only, without the statistics objects and without the query schema (views, functions and roles; ADR-0113), which costs about
// two seconds for each KG. No file of a server calls it with false. The tests of other packages take TestOnlyModelsWithoutQuerySchema
// instead: this model with the statistics objects, less the query schema.
func StandardModels(withStats bool) (ModelProvider, error) {
	reg, err := model.LoadStandard()
	if err != nil {
		return nil, err
	}
	search, err := pgschema.DefaultSearchSet(reg)
	if err != nil {
		return nil, err
	}
	m := SchemaModel{Registry: reg, Search: search, WithStats: withStats, noQuery: !withStats}
	return func(kernel.Hash) (SchemaModel, error) { return m, nil }, nil
}

// LeaseClock says whose time decides that a lease has run out.
type LeaseClock int

// The clocks. The database's is the default and the one to use: servers whose clocks differ agree on it. The
// caller's is for tests, which move a manual clock.
const (
	LeaseClockDB LeaseClock = iota
	LeaseClockCaller
)

// CodeKGRestoring is a read or a write of a KG that is being restored: it is closed until the last step.
const CodeKGRestoring kernel.Code = "pgstore_kg_restoring"

// Store is the PostgreSQL implementation of graph.Store on the storage schemas. Make one with Open.
type Store struct {
	rt      router
	poll    time.Duration
	closers []func() error
	models  ModelProvider
	clock   LeaseClock
	cache   sync.Map // KG id text -> *kgAccess
	// hook is called between the transactions of an abandon or a restore. Only the tests of this unit set it, to
	// stop the work in the middle.
	hook func(stage string, seq int64) error
	// deleteSeen is called at the end of every transaction of a delete of a KG, in the transaction, with its step ("mark",
	// "source", "compute", "last"). Only the tests of this unit (and StopDeleteAt) set it, to count the locks a transaction
	// holds or to stop the delete in the middle.
	deleteSeen func(ctx context.Context, step string, tx *sql.Tx) error
	// record is called with every chunk that ApplyChunk wrote. Only tests set it (RecordChunks).
	record func(graph.ChunkWrite)
}

var _ graph.Store = (*Store)(nil)

// Open connects, checks the connection and brings the shared tables up to date with the migrations of this build
// (the connection base, pgconn, does all three). The DSN is never part of an error. The schemas of the KGs are made
// by CreateKG. cfg.Models is the model the server holds; when it is nil the standard model is held (with its
// statistics objects). Closing the Store closes the pool.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	models := cfg.Models
	if models == nil {
		var err error
		if models, err = StandardModels(true); err != nil {
			return nil, err
		}
	}
	pool, err := pgconn.Open(ctx, cfg.conn())
	if err != nil {
		return nil, err
	}
	sh := &shard{pool: pool}
	return &Store{rt: singleRouter{sh}, poll: cfg.poll(), closers: []func() error{pool.Close}, models: models, clock: cfg.LeaseClock}, nil
}

// Close closes the pool. The store cannot be used afterwards.
func (s *Store) Close() error {
	var first error
	for _, c := range s.closers {
		if err := c(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// kgAccess is what every access to a KG needs, found once: the shard, the schema, the model and what the schema
// was made from (D30: every access goes through the one place that makes it).
type kgAccess struct {
	sh     *shard
	id     graph.KGID
	cat    catalogRow
	rec    SchemaRecord
	model  SchemaModel
	common map[int]model.BaseKind
	state  map[int]map[int]model.BaseKind // type number -> the attributes of its own table of values
}

// t is the name of a table of the storage schema of the KG, quoted and qualified.
func (a *kgAccess) t(table string) string { return pgschema.Qualified(a.rec.Schema(), table) }

// sub puts the table in the place of the @ of a statement; a statement that starts with the table's name in the
// middle of its text (UPDATE @ SET ...) is one literal, as the check of the sessions reads it.
func (a *kgAccess) sub(query, table string) string { return strings.Replace(query, "@", a.t(table), 1) }

func (a *kgAccess) typeTable(typeNo int) string { return a.t(pgschema.TypeTable(typeNo)) }

// access resolves a KG to its shard, schema and model. The result is kept: a KG's number and the model it was
// made from do not change.
func (s *Store) access(ctx context.Context, kg graph.KGID) (*kgAccess, error) {
	if kg.IsZero() {
		return nil, kernel.Errorf(graph.CodeKGNotFound, "no KG %q", kg)
	}
	if v, ok := s.cache.Load(kg.String()); ok {
		return v.(*kgAccess), nil
	}
	sh, err := s.rt.locate(kg)
	if err != nil {
		return nil, err
	}
	a := &kgAccess{sh: sh, id: kg}
	err = sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT `+catalogColumns+` FROM kg_catalog WHERE kg_id = $1`, kg.String())
		var err error
		if a.cat, err = scanCatalog(row); err != nil {
			return notFound(err, "look a KG up", kg)
		}
		var ok bool
		if a.rec, ok, err = readRecord(ctx, tx, kg); err != nil {
			return err
		}
		if !ok {
			return kernel.Errorf(kernel.CodeNotFound, "the KG %s has no storage schema", kg)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if a.model, err = s.models(a.cat.definition); err != nil {
		return nil, err
	}
	a.common, a.state = valueKinds(a.rec.Manifest)
	s.cache.Store(kg.String(), a)
	return a, nil
}

// checked is the judgement of D19 for the KG: nil when its storage schema was made from the model the server
// holds. The paths of the apply and of the reads ask it before they work on the KG.
func (a *kgAccess) checked() error { return CheckSchemaMatch(a.rec, a.model) }

// valueKinds reads the tables of values out of a manifest: the common ones and those of each type.
func valueKinds(m pgschema.Manifest) (map[int]model.BaseKind, map[int]map[int]model.BaseKind) {
	common := map[int]model.BaseKind{}
	for _, c := range m.CommonState {
		common[c.AttrNo] = c.Kind
	}
	state := map[int]map[int]model.BaseKind{}
	for _, t := range m.Types {
		if len(t.State) == 0 {
			continue
		}
		state[t.TypeNo] = map[int]model.BaseKind{}
		for _, c := range t.State {
			state[t.TypeNo][c.AttrNo] = c.Kind
		}
	}
	return common, state
}

// forget drops what was kept of a KG (it was deleted).
func (s *Store) forget(kg graph.KGID) { s.cache.Delete(kg.String()) }
