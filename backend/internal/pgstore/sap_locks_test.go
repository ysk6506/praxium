package pgstore

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
)

// sapLockTaps keeps the number of locks that the transactions which moved a head held just before their COMMIT.
type sapLockTaps struct {
	mu    sync.Mutex
	locks []int
	on    bool
}

func (l *sapLockTaps) sapHook(ctx context.Context, tx *sql.Tx) error {
	l.mu.Lock()
	on := l.on
	l.mu.Unlock()
	if !on {
		return nil
	}
	var moved, held int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_xact_user_tables WHERE relname = 'head' AND n_tup_upd > 0`).Scan(&moved); err != nil || moved == 0 {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid()`).Scan(&held); err != nil {
		return err
	}
	l.mu.Lock()
	l.locks = append(l.locks, held)
	l.mu.Unlock()
	return nil
}

func (l *sapLockTaps) sapTake() []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.locks
	l.locks, l.on = nil, false
	return out
}

// sapMerge makes one chunk of every n chunks of the rows path.
func sapMerge(chunks []graph.ChunkWrite, n int) []graph.ChunkWrite {
	var out []graph.ChunkWrite
	for i := 0; i < len(chunks); i += n {
		var m graph.ChunkWrite
		for _, c := range chunks[i:min(i+n, len(chunks))] {
			m.Objects, m.Links, m.Identifiers, m.Volatile = append(m.Objects, c.Objects...), append(m.Links, c.Links...), append(m.Identifiers, c.Identifiers...), append(m.Volatile, c.Volatile...)
		}
		out = append(out, m)
	}
	return out
}

// The locks a chunk holds are those of the tables it writes and their indexes, and do not grow with the number of
// rows in it: a chunk of rows of many types holds more than one of few, and a chunk ten times as big holds what the
// same types hold. The numbers go to the log (ADR-0099).
//
// [REQ-728]
func TestSapLocksOfAChunkDoNotGrowWithItsRows(t *testing.T) {
	var worst, best = 0, 1 << 30
	for _, n := range []int{1, 3, 9} {
		taps := &sapLockTaps{}
		pair := sapStagedInto(t, 120, func(tb TB) (*Store, func()) { return sapTapStore(tb, taps), func() {} })
		pair.sapStageChunks(t, sapMerge(pair.first, n))
		taps.mu.Lock()
		taps.on = true
		taps.mu.Unlock()
		end := sapRun(t, pair.staged, "sap-run")
		held := taps.sapTake()
		rows := sapCount(t, pair.staged, `SELECT count(*) FROM {ingest_diff}`)
		hi, lo := 0, 1<<30
		for _, h := range held {
			hi, lo = max(hi, h), min(lo, h)
		}
		t.Logf("%d chunks of about %d lines: locks per transaction from %d to %d", end.ChunkCount, rows/end.ChunkCount, lo, hi)
		worst, best = max(worst, hi), min(best, lo)
		if len(held) != end.ChunkCount {
			t.Fatalf("%d transactions were counted for %d chunks", len(held), end.ChunkCount)
		}
	}
	if worst > 4*best+200 {
		t.Fatalf("the locks of a chunk grow with its rows: from %d to %d", best, worst)
	}
}

// sapTapStore opens a Store whose transactions are counted by the taps.
func sapTapStore(tb TB, taps *sapLockTaps) *Store {
	tb.Helper()
	pool := pgconn.NewTestPoolWith(tb, pgconn.Config{BeforeCommit: taps.sapHook})
	models, err := StandardModels(false)
	if err != nil {
		tb.Fatalf("models: %v", err)
	}
	return &Store{rt: singleRouter{&shard{pool: pool}}, poll: defaultPollInterval, models: models, clock: LeaseClockCaller}
}
