package graphcheck

import (
	"context"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// TB is the small part of *testing.T the conformance suite uses. A non-test
// file may not import "testing", so the suite asks for this instead and the
// adapter's own test passes its *testing.T.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
	Logf(format string, args ...any)
	Cleanup(func())
}

// Factory returns a new, empty store. The suite calls it once per case.
type Factory func(tb TB) graph.Store

// Case is one case of the conformance suite: what any adapter of the storage
// port must do. An adapter's test runs every case as a subtest:
//
//	for _, c := range graphcheck.Cases() {
//		t.Run(c.Name, func(t *testing.T) { c.Run(t, factory) })
//	}
type Case struct {
	Name string
	Run  func(tb TB, newStore Factory)
}

// Cases returns the whole suite in a fixed order.
func Cases() []Case {
	var all []Case
	for _, group := range [][]Case{applyCases(), chunkCases(), leaseCases(), identityCases(), readCases(), feedCases(), sealCases()} {
		all = append(all, group...)
	}
	return all
}

var epoch = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

// env is what a case works in: a store, a clock and ids that are the same
// every run, an applier that holds the lease, and a graph.KG with the fixture's
// registry.
type env struct {
	tb    TB
	ctx   context.Context
	store graph.Store
	fx    *graph.Fixture
	clock *kernel.ManualClock
	ids   kernel.IDGenerator
	a     *graph.Applier
	kg    graph.KG
	lease graph.Lease
}

func newEnv(tb TB, factory Factory, opts graph.Options) *env {
	tb.Helper()
	fx, err := graph.NewFixture()
	if err != nil {
		tb.Fatalf("fixture: %v", err)
	}
	e := &env{tb: tb, ctx: context.Background(), store: factory(tb), fx: fx,
		clock: kernel.NewManualClock(epoch), ids: kernel.NewRandomIDs(kernel.NewSeededRng(7))}
	e.kg, err = graph.NewTestKG(e.ctx, e.store, fx.Reg, e.ids, e.clock, graph.KGSpec{Name: "prod", Role: graph.RoleProduction})
	if err != nil {
		tb.Fatalf("create KG: %v", err)
	}
	if opts.Semantic == 0 {
		opts.Semantic = 1
	}
	opts.VerifyOnSeal = true
	e.a = e.applier(opts)
	e.lease, err = e.a.Acquire(e.ctx, e.kg.ID)
	if err != nil {
		tb.Fatalf("acquire: %v", err)
	}
	return e
}

func (e *env) applier(opts graph.Options) *graph.Applier {
	e.tb.Helper()
	a, err := graph.NewApplier(e.store, e.fx.Reg, graph.NewTestKeyProvider("conformance"), e.clock, e.ids, opts)
	if err != nil {
		e.tb.Fatalf("applier: %v", err)
	}
	return a
}

func (e *env) apply(b graph.Batch) graph.Result {
	e.tb.Helper()
	res, err := e.a.Apply(e.ctx, e.kg.ID, e.lease, b)
	if err != nil {
		e.tb.Fatalf("apply: %v", err)
	}
	return res
}

func (e *env) begin(b graph.Batch) *graph.Run {
	e.tb.Helper()
	run, err := e.a.Begin(e.ctx, e.kg.ID, e.lease, b)
	if err != nil {
		e.tb.Fatalf("begin: %v", err)
	}
	return run
}

func (e *env) step(run *graph.Run) {
	e.tb.Helper()
	if err := run.Next(e.ctx); err != nil {
		e.tb.Fatalf("chunk: %v", err)
	}
}

func (e *env) head() graph.Head {
	e.tb.Helper()
	h, err := e.store.Head(e.ctx, e.kg.ID)
	if err != nil {
		e.tb.Fatalf("head: %v", err)
	}
	return h
}

func (e *env) snap(kind graph.ReadKind, seq int64) graph.Snapshot {
	e.tb.Helper()
	s, err := e.store.Snapshot(e.ctx, graph.ReadPoint{KG: e.kg.ID, Kind: kind, Seq: seq})
	if err != nil {
		e.tb.Fatalf("snapshot: %v", err)
	}
	return s
}

func (e *env) object(s graph.Snapshot, id graph.ObjectID) (graph.ObjectRow, bool) {
	e.tb.Helper()
	row, ok, err := s.Object(e.ctx, id)
	if err != nil {
		e.tb.Fatalf("object: %v", err)
	}
	return row, ok
}

func (e *env) count(s graph.Snapshot) (objects, links int) {
	e.tb.Helper()
	rows, err := s.ScanObjects(e.ctx, graph.ObjectID{}, 1<<20)
	if err != nil {
		e.tb.Fatalf("scan objects: %v", err)
	}
	lrows, err := s.ScanLinks(e.ctx, graph.LinkID{}, 1<<20)
	if err != nil {
		e.tb.Fatalf("scan links: %v", err)
	}
	return len(rows), len(lrows)
}

func (e *env) verify() {
	e.tb.Helper()
	if err := e.a.Verify(e.ctx, e.kg.ID); err != nil {
		e.tb.Fatalf("verify: %v", err)
	}
}

// wantCode checks that err carries code.
func wantCode(tb TB, what string, err error, code kernel.Code) {
	tb.Helper()
	if err == nil {
		tb.Fatalf("%s: no error, wanted %s", what, code)
		return
	}
	if got := kernel.CodeOf(err); got != code {
		tb.Fatalf("%s: code %s (%v), wanted %s", what, got, err, code)
	}
}

func check(tb TB, ok bool, format string, args ...any) {
	tb.Helper()
	if !ok {
		tb.Errorf(format, args...)
	}
}

func fatalIf(tb TB, err error, what string) {
	tb.Helper()
	if err != nil {
		tb.Fatalf("%s: %v", what, err)
	}
}

func fact(v any) graph.Attr {
	return graph.Attr{Value: v, Grade: model.GradeFact, Basis: model.BasisConfigured}
}
