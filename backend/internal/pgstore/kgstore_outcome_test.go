package pgstore

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// A write whose COMMIT gets no answer is decided by the head, on the new storage too. The names start with v2o.

// v2oStore opens a StoreV2 on a new test database. After arm() is called, the next transaction that moves a head has
// its backend ended by the server just before its COMMIT.
func v2oStore(tb TB) (*StoreV2, func()) {
	tb.Helper()
	armed := new(atomic.Bool)
	var pool *pgconn.Pool
	hook := func(ctx context.Context, tx *sql.Tx) error {
		if !armed.Load() {
			return nil
		}
		var moved int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_xact_user_tables WHERE relname = 'head' AND n_tup_upd > 0`).Scan(&moved); err != nil || moved == 0 {
			return err
		}
		armed.Store(false)
		var pid int
		if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		return pool.TxOn(ctx, pgconn.ReadPool, RoleAPIRead, true, func(other *sql.Tx) error {
			if _, err := other.ExecContext(ctx, `SELECT pg_terminate_backend($1)`, pid); err != nil {
				return err
			}
			for i := 0; i < 500; i++ {
				var n int
				if err := other.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&n); err != nil || n == 0 {
					return err
				}
				time.Sleep(10 * time.Millisecond)
			}
			return nil
		})
	}
	pool = pgconn.NewTestPoolWith(tb, pgconn.Config{BeforeCommit: hook})
	models, err := StandardModels(false)
	if err != nil {
		tb.Fatalf("models: %v", err)
	}
	return &StoreV2{rt: singleRouter{&shard{pool: pool}}, poll: defaultPollInterval, models: models, clock: LeaseClockCaller}, func() { armed.Store(true) }
}

func TestV2oAnApplyWhoseCommitGotNoAnswerIsCheckedAgainstTheHeadAndCanBeRepeated(t *testing.T) {
	st, arm := v2oStore(t)
	ctx := context.Background()
	fx, err := graph.NewFixture()
	noErr(t, "fixture", err)
	clock := kernel.NewManualClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := kernel.NewRandomIDs(kernel.NewSeededRng(3))
	id, err := kernel.NewID[graph.KGKind](ids, "kg")
	noErr(t, "id", err)
	kg, err := st.CreateKG(ctx, graph.KG{ID: id, Name: "o", Role: graph.RoleTest, DefinitionSetHash: fx.Reg.Hash(), CreatedAt: clock.Now()})
	noErr(t, "create", err)
	app, err := graph.NewApplier(st, fx.Reg, graph.NewTestKeyProvider("o"), clock, ids, graph.Options{Semantic: 1})
	noErr(t, "applier", err)
	lease, err := app.Acquire(ctx, kg.ID)
	noErr(t, "lease", err)
	arm()
	_, err = app.Apply(ctx, kg.ID, lease, fx.World(1, 1, 3))
	wantCode(t, "a commit that never happened", err, pgconn.CodeConnectionLost)
	h0, err := st.Head(ctx, kg.ID)
	if err != nil || h0.AppliedSeq != 0 {
		t.Fatalf("the head after the lost commit: %+v %v", h0, err)
	}
	res, err := app.Apply(ctx, kg.ID, lease, fx.World(1, 1, 3))
	noErr(t, "repeat", err)
	if res.Duplicate {
		t.Fatal("the repeat was taken for a duplicate of a write that never happened")
	}
	h1, err := st.Head(ctx, kg.ID)
	if err != nil || h1.AppliedSeq == 0 {
		t.Fatalf("the head after the repeat: %+v %v", h1, err)
	}
}
