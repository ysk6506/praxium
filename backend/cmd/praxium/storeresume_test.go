package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgstore"
)

// rsmLeftHalfDone makes a database with a KG in it whose abandon (or restore) a server left half done, and returns the
// KG and the head it had before the work started.
func rsmLeftHalfDone(t *testing.T, restore bool) (dsn string, kg graph.KG, before graph.Head) {
	t.Helper()
	ctx := context.Background()
	dsn = pgstore.NewTestDatabase(t)
	st, err := pgstore.Open(ctx, pgstore.Config{DSN: dsn, LeaseClock: pgstore.LeaseClockCaller})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	fx, err := graph.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(11))
	if kg, err = graph.NewTestKG(ctx, st, fx.Reg, ids, clock, graph.KGSpec{Name: "rsm", Role: graph.RoleProduction}); err != nil {
		t.Fatal(err)
	}
	app, err := graph.NewApplier(st, fx.Reg, graph.NewTestKeyProvider("rsm"), clock, ids, graph.Options{Semantic: 1, ChunkRows: 7})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := app.Acquire(ctx, kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Apply(ctx, kg.ID, lease, fx.World(1, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if before, err = st.Head(ctx, kg.ID); err != nil {
		t.Fatal(err)
	}
	if restore {
		if _, err := app.Apply(ctx, kg.ID, lease, fx.World(1, 2, 2)); err != nil {
			t.Fatal(err)
		}
	} else {
		run, err := app.Begin(ctx, kg.ID, lease, fx.World(2, 2, 3))
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3 && !run.Done(); i++ {
			if err := run.Next(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	st.StopWorkAfter(1, kernel.Errorf(kernel.CodeInternal, "the server died"))
	if restore {
		_, err = app.Restore(ctx, kg.ID, before.AppliedSeq)
	} else {
		err = app.Abandon(ctx, kg.ID, lease)
	}
	if kernel.CodeOf(err) != kernel.CodeInternal {
		t.Fatalf("the work was not cut: %v", err)
	}
	return dsn, kg, before
}

// The server that starts on a database with a KG whose abandon or restore a server left half done ends that work
// before it answers, and says so in its log (ADR-0094).
//
// [REQ-270]
func TestTheServerEndsTheAbandonOrRestoreAKGWasLeftInTheMiddleOfWhenItStarts(t *testing.T) {
	for _, restore := range []bool{false, true} {
		name := map[bool]string{false: "abandon", true: "restore"}[restore]
		t.Run(name, func(t *testing.T) {
			dsn, kg, before := rsmLeftHalfDone(t, restore)
			d, _, stop := apiDeps(nil, nil)
			defer stop()
			d.env.ReadFile = os.ReadFile
			var logged bytes.Buffer
			st, closeStore, err := openStoreLogged(context.Background(), serveOptions{store: storePostgres, storeDSNFile: dsnFile(t, dsn)}, d, logger(&logged))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = closeStore() }()
			head, err := st.Head(context.Background(), kg.ID)
			if err != nil {
				t.Fatalf("the KG is still closed after the start: %v", err)
			}
			if head.AppliedSeq != before.AppliedSeq || head.ReadableThrough != before.ReadableThrough {
				t.Fatalf("the head is %+v after the work, it was %+v before it started", head, before)
			}
			if !strings.Contains(logged.String(), "left half done") || !strings.Contains(logged.String(), kg.ID.String()) {
				t.Fatalf("the log does not say which KG's work was ended: %s", logged.String())
			}
		})
	}
}

// A KG whose work cannot be ended does not keep the server down: the start is a warning and the KG stays as its head
// says.
//
// [REQ-270]
func TestTheServerStartsWithAWarningWhenTheWorkOfAKGCannotBeEnded(t *testing.T) {
	dsn, _, _ := rsmLeftHalfDone(t, true)
	d, _, stop := apiDeps(nil, nil)
	defer stop()
	d.env.ReadFile = os.ReadFile
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	var table string
	if err := admin.QueryRow(`SELECT quote_ident(schemaname) || '.' || quote_ident(tablename) FROM pg_tables WHERE tablename = 'object_hist'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`ALTER TABLE ` + table + ` RENAME TO object_hist_away`); err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	st, closeStore, err := openStoreLogged(context.Background(), serveOptions{store: storePostgres, storeDSNFile: dsnFile(t, dsn)}, d, logger(&logged))
	if err != nil {
		t.Fatalf("the server did not start: %v", err)
	}
	defer func() { _ = closeStore() }()
	if _, err := st.ListKGs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "could not end the work") {
		t.Fatalf("no warning in the log: %s", logged.String())
	}
}
