package pgcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/devload"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/memstore"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// pgrWorld is a development load of a generated world in a store, read by the current query engine.
type pgrWorld struct {
	store  graph.Store
	engine *query.Engine
	head   int64
}

func pgrLoad(t *testing.T, reg *model.Registry, store graph.Store, truth []byte) pgrWorld {
	t.Helper()
	ctx := context.Background()
	clock := kernel.NewManualClock(pgxStart)
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
	kg, err := devload.NewKG(ctx, store, reg, ids, clock, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (devload.Loader{Store: store, Reg: reg, Clock: clock, IDs: ids, BatchItems: 200}).Load(ctx, kg.ID, bytes.NewReader(truth)); err != nil {
		t.Fatal(err)
	}
	head, err := store.Head(ctx, kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := query.New(store, reg, clock)
	if err != nil {
		t.Fatal(err)
	}
	return pgrWorld{store: store, engine: engine, head: head.ReadableThrough}
}

// pgrJSON is an answer as text, so that two answers can be compared in whole: the body, and the parts of the envelope
// that come from the store (the change number, the counts of the evidence, what the answer holds).
func pgrJSON(t *testing.T, meta query.Envelope, data any) string {
	t.Helper()
	raw, err := json.Marshal(struct {
		Seq      int64                `json:"seq"`
		Evidence query.Evidence       `json:"evidence"`
		Complete query.ResultComplete `json:"complete"`
		Data     any                  `json:"data"`
	}{meta.AsOf.ChangeSeq, meta.Evidence, meta.ResultComplete, data})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Every read the current query engine makes (the rows of find, page by page and by type, the links around an object,
// the object with its attributes, identifiers and link counts) gives the same answer on the new PostgreSQL store as on
// the memory store, at the newest change number and at older ones.
//
// [REQ-258]
func TestPgrQueryReadsGiveTheSameRowsLinksAndPastStatesAsMemory(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	_, truth := pgxWorld(t, reg, 9, 500)
	mem := pgrLoad(t, reg, memstore.New(), truth)
	pg := pgrLoad(t, reg, pgstore.NewTestStore(t), truth)
	if mem.head != pg.head || mem.head < 3 {
		t.Fatalf("the heads differ or the load is too small to have a past: memory %d, postgres %d", mem.head, pg.head)
	}
	ctx := context.Background()
	points := []*int64{nil}
	for _, seq := range []int64{1, mem.head / 2, mem.head - 1} {
		points = append(points, &seq)
	}
	compared, withRows := 0, 0
	same := func(what string, ask func(e *query.Engine) (string, error)) {
		t.Helper()
		a, errA := ask(mem.engine)
		b, errB := ask(pg.engine)
		if (errA == nil) != (errB == nil) || kernel.CodeOf(errA) != kernel.CodeOf(errB) {
			t.Fatalf("%s: memory answered with %v, postgres with %v", what, errA, errB)
		}
		if a != b {
			t.Fatalf("%s differs:\nmemory   %.600s\npostgres %.600s", what, a, b)
		}
		compared++
	}
	for _, seq := range points {
		common := query.Common{KG: graph.MainAlias, ChangeSeq: seq}
		first, err := mem.engine.Find(ctx, query.FindInput{Common: common}, query.AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Data.CountsByType) < 3 {
			t.Fatalf("the world has %d types at %v", len(first.Data.CountsByType), seq)
		}
		var someone string
		for _, c := range first.Data.CountsByType[:3] {
			c := c
			in := query.FindInput{Common: common, ObjectType: c.ObjectType}
			in.Limit = 15
			in.Shape = query.ShapeItems
			same("find "+c.ObjectType, func(e *query.Engine) (string, error) {
				ans, err := e.Find(ctx, in, query.AllowAll())
				if err != nil {
					return "", err
				}
				if len(ans.Data.Items) > 0 && someone == "" {
					someone = ans.Data.Items[0].ObjectID
				}
				return pgrJSON(t, ans.Meta, ans.Data), nil
			})
		}
		if someone != "" {
			withRows++
		}
		for _, dir := range []string{"out", "in", "both"} {
			in := query.NeighborsInput{Common: common, ObjectID: someone, Direction: dir, MaxSteps: 2}
			same("neighbors "+dir, func(e *query.Engine) (string, error) {
				ans, err := e.Neighbors(ctx, in, query.AllowAll())
				if err != nil {
					return "", err
				}
				return pgrJSON(t, ans.Meta, ans.Data), nil
			})
		}
		get := query.GetInput{Common: common, ObjectID: someone, Include: []string{query.IncludeAttributes, query.IncludeIdentifiers, query.IncludeLinkCounts}}
		same("get", func(e *query.Engine) (string, error) {
			ans, err := e.Get(ctx, get, query.AllowAll())
			if err != nil {
				return "", err
			}
			return pgrJSON(t, ans.Meta, ans.Data), nil
		})
	}
	if withRows < 2 {
		t.Fatalf("only %d of the points had rows to follow", withRows)
	}
	if compared < 28 {
		t.Fatalf("only %d answers were compared", compared)
	}
}
