package pgcheck

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
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
	"github.com/ysk6506/praxium/backend/internal/pgstore"
	"github.com/ysk6506/praxium/backend/internal/query"
)

//go:embed testdata/source.json
var pgxSource string

//go:embed testdata/mapping.json
var pgxMapping string

var pgxStart = time.Date(2031, 3, 1, 0, 0, 0, 0, time.UTC)

// pgxIngest runs the current ingestion of the example definitions over the files into a store.
func pgxIngest(t *testing.T, reg *model.Registry, store graph.Store, files pgxExports) (ingest.Report, graph.KG) {
	t.Helper()
	src, err := ingest.ParseSource(strings.NewReader(pgxSource))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := ingest.ParseMapping(strings.NewReader(pgxMapping))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ingest.Compile(reg, src, mp)
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(pgxStart)
	ids := kernel.NewSequentialIDs()
	ctx := context.Background()
	kg, err := devload.NewKG(ctx, store, reg, ids, clock, "ingested")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, d := range plan.Source.Datasets {
		names[d.ID] = d.ID
	}
	open := func(name string) (io.ReadCloser, error) {
		text, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("no file %s", name)
		}
		return io.NopCloser(strings.NewReader(text)), nil
	}
	r := ingest.Runner{Store: store, Reg: reg, Clock: clock, IDs: ids, States: ingest.NewMemStateStore(),
		Fetcher: filesource.Source{Open: open, Files: names}}
	rep, err := r.Run(ctx, plan, kg.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return rep, kg
}

// The current ingestion, from the files to the end, gives the same graph hash on the new PostgreSQL store as on
// the memory store, and a second run over the same files changes nothing in either.
//
// [REQ-265]
func TestPgxIngestionGivesTheSameGraphHashAsMemory(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	files, _ := pgxWorld(t, reg, 7, 500)
	pg := pgstore.NewTestStoreWith(t, pgstore.LeaseClockCaller)
	mem := memstore.New()
	repPG, kgPG := pgxIngest(t, reg, pg, files)
	repMem, _ := pgxIngest(t, reg, mem, files)
	if repPG.GraphHash == "" || repPG.GraphHash != repMem.GraphHash {
		t.Fatalf("graph hash: postgres %q, memory %q", repPG.GraphHash, repMem.GraphHash)
	}
	if repPG.Objects != repMem.Objects || repPG.Objects == 0 {
		t.Fatalf("objects: postgres %d, memory %d", repPG.Objects, repMem.Objects)
	}
	head, err := pg.Head(context.Background(), kgPG.ID)
	if err != nil || head.AppliedSeq == 0 {
		t.Fatalf("head %+v, %v", head, err)
	}
}

// After a development load the current query engine answers the same on the new store as on the memory store.
//
// [REQ-258]
func TestPgxQueriesAnswerTheSameAsMemory(t *testing.T) {
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	_, truth := pgxWorld(t, reg, 9, 500)
	ctx := context.Background()
	clock := kernel.NewManualClock(pgxStart)
	engines := map[string]*query.Engine{}
	for name, store := range map[string]graph.Store{"memory": memstore.New(), "postgres": pgstore.NewTestStoreWith(t, pgstore.LeaseClockCaller)} {
		ids := kernel.NewRandomIDs(kernel.NewSeededRng(5))
		kg, err := devload.NewKG(ctx, store, reg, ids, clock, "dev")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := (devload.Loader{Store: store, Reg: reg, Clock: clock, IDs: ids, BatchItems: 200}).Load(ctx, kg.ID, bytes.NewReader(truth)); err != nil {
			t.Fatal(err)
		}
		if engines[name], err = query.New(store, reg, clock); err != nil {
			t.Fatal(err)
		}
	}
	find := func(e *query.Engine, seq *int64) any {
		ans, err := e.Find(ctx, query.FindInput{Common: query.Common{KG: graph.MainAlias, ChangeSeq: seq}}, query.AllowAll())
		if err != nil {
			t.Fatal(err)
		}
		return ans.Data.CountsByType
	}
	first := int64(1)
	for _, seq := range []*int64{nil, &first} {
		a, b := fmt.Sprint(find(engines["memory"], seq)), fmt.Sprint(find(engines["postgres"], seq))
		if a != b || a == "[]" {
			t.Fatalf("counts differ at %v:\nmemory   %s\npostgres %s", seq, a, b)
		}
	}
}
