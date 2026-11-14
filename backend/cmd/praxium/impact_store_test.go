package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/cli"
	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// itStoreAnswers loads the truth into a store and asks the impact questions that pick their objects by type.
func itStoreAnswers(t *testing.T, store graph.Store, truth []byte) []string {
	t.Helper()
	ctx := context.Background()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	w := wiring{deps: fixedDeps()}
	if _, err := loadTruthFrom(ctx, store, reg, w, bytes.NewReader(truth), 400); err != nil {
		t.Fatal(err)
	}
	eng, err := query.New(store, reg, w.clock)
	if err != nil {
		t.Fatal(err)
	}
	common := query.Common{KG: graph.MainAlias, Limit: query.MaxLimit, Shape: query.ShapeItems}
	first := func(typ string, n int) []string {
		a, err := eng.Find(ctx, query.FindInput{Common: common, ObjectType: typ}, query.AllowAll())
		if err != nil || len(a.Data.Items) < n {
			t.Fatalf("find %s: %v", typ, err)
		}
		var ids []string
		for _, it := range a.Data.Items[:n] {
			ids = append(ids, it.ObjectID)
		}
		return ids
	}
	inputs := []query.ImpactInput{
		{Dead: first("RackPdu", 1)},
		{Dead: first("RackPdu", 2), Mode: query.ModeWithAssumptions},
		{Dead: first("Switch", 1), Mode: query.ModeFactsOnly},
		{SiteFailure: first("Site", 1), MaxItems: 200},
		{Dead: first("RackPdu", 2), MaxHops: 3},
		{Direction: query.DirectionUpstream, TargetObjectID: first("VirtualMachine", 1)[0]},
	}
	var out []string
	for _, in := range inputs {
		in.Common = common
		a, err := eng.Impact(ctx, in, query.AllowAll())
		if err != nil {
			t.Fatalf("impact: %v", err)
		}
		raw, err := json.Marshal(struct {
			Data   query.ImpactData
			Result query.ResultComplete
			Limits []string
			Warn   []string
		}{a.Data, a.Meta.ResultComplete, a.Meta.ModelLimits, a.Meta.Warnings})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(raw))
	}
	return out
}

// The same questions on the same world give the same answers on the in-memory store, the file store and PostgreSQL.
//
// [REQ-300] [REQ-305]
func TestImpactAnswersAreTheSameOnTheMemoryTheFileAndThePostgresStore(t *testing.T) {
	truth, err := os.ReadFile(writeWorld(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mem, closeMem, err := openStore(ctx, serveOptions{store: storeMemory}, wiring{deps: fixedDeps()})
	if err != nil {
		t.Fatal(err)
	}
	defer closeMem()
	file, closeFile, err := openFile(serveOptions{store: storeFile, storeDataDir: t.TempDir()}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer closeFile()
	dsn := pgstore.NewTestDatabase(t)
	pg, closePG, err := openStore(ctx, serveOptions{store: storePostgres, storeDSNFile: "/dsn"},
		wiring{deps: fixedDeps(), env: itDSNEnv(dsn)})
	if err != nil {
		t.Fatal(err)
	}
	defer closePG()
	want := itStoreAnswers(t, mem, truth)
	for name, store := range map[string]graph.Store{"file": file, "postgres": pg} {
		got := itStoreAnswers(t, store, truth)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("question %d on the %s store differs from the in-memory store:\n%.600s\n%.600s", i, name, got[i], want[i])
			}
		}
	}
	for i, a := range want {
		var probe struct {
			Data struct {
				FactsOnly       *struct{ Items []any }
				WithAssumptions *struct{ Items []any }
				Upstream        *struct{ Total int }
			}
		}
		if err := json.Unmarshal([]byte(a), &probe); err != nil {
			t.Fatal(err)
		}
		d := probe.Data
		if d.FactsOnly != nil && len(d.FactsOnly.Items) == 0 && d.WithAssumptions != nil && len(d.WithAssumptions.Items) == 0 || d.Upstream != nil && d.Upstream.Total == 0 {
			t.Fatalf("question %d found nothing: the comparison would prove nothing", i)
		}
	}
}

// itDSNEnv gives the DSN as the content of a file the store reads.
func itDSNEnv(dsn string) cli.Env {
	return cli.Env{ReadFile: func(string) ([]byte, error) { return []byte(dsn), nil }}
}
