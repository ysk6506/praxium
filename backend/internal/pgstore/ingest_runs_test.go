package pgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	goslices "slices"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the shared tables of the runs (migration 0006) and of the digest of ingest_diff, on a real server.
// The helpers start with igr.

func igrSource(t *testing.T, admin *sql.DB, kg, source string) int {
	t.Helper()
	var no int
	if err := admin.QueryRowContext(context.Background(),
		`INSERT INTO source_state (kg_id, source_id) VALUES ($1, $2) RETURNING src_no`, kg, source).Scan(&no); err != nil {
		t.Fatal(err)
	}
	return no
}

func igrRun(admin *sql.DB, kg string, src int, stage string) error {
	ended := "NULL"
	if goslices.Contains(pgschema.RunOver(), stage) {
		ended = "now()"
	}
	class := "NULL"
	if stage == pgschema.RunFailed {
		class = "'" + pgschema.EndClassTransient + "'"
	}
	_, err := admin.ExecContext(context.Background(),
		fmt.Sprintf(`INSERT INTO sync_run (kg_id, src_no, stage, ended_at, end_class) VALUES ($1, $2, $3, %s, %s)`, ended, class), kg, src, stage)
	return err
}

// [REQ-682]
func TestIngestSharedTablesHaveTheColumnsTheNamesSay(t *testing.T) {
	_, admin := sxOpen(t)
	for table, want := range map[string][]string{
		pgschema.TableSyncRun:       pgschema.SyncRunColumns(),
		pgschema.TableSourceState:   pgschema.SourceStateColumns(),
		pgschema.TableSourceDataset: pgschema.SourceDatasetColumns(),
	} {
		if got := igtColumns(t, admin, "public", table); !goslices.Equal(got, want) {
			t.Errorf("%s has the columns %v, the names say %v", table, got, want)
		}
	}
	for _, seq := range []string{pgschema.SeqSyncRun, pgschema.SeqSource, pgschema.SeqDataset} {
		if sxInt(t, admin, `SELECT count(*) FROM pg_sequences WHERE schemaname = 'public' AND sequencename = $1`, seq) != 1 {
			t.Errorf("no sequence %s", seq)
		}
	}
	for _, idx := range []string{pgschema.IndexOneOpenRunPerSource, pgschema.IndexOneWorkingRunPerKG} {
		if sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1 AND indexdef LIKE 'CREATE UNIQUE INDEX%WHERE%'`, idx) != 1 {
			t.Errorf("%s is not a partial unique index", idx)
		}
	}
}

// [REQ-682]
func TestIngestTheGateOfTheRunsAllowsOneOpenRunPerSourceAndOneWorkingRunPerKG(t *testing.T) {
	_, admin := sxOpen(t)
	a, b, c := igrSource(t, admin, "kg-a", "s1"), igrSource(t, admin, "kg-a", "s2"), igrSource(t, admin, "kg-b", "s3")
	if err := igrRun(admin, "kg-a", a, pgschema.RunReceiving); err != nil {
		t.Fatal(err)
	}
	if err := igrRun(admin, "kg-a", a, pgschema.RunReceiving); igtErrCode(err) != "23505" {
		t.Errorf("a second open run of the same source was accepted: %v", err)
	}
	if err := igrRun(admin, "kg-a", a, pgschema.RunAwaitingConfirmation); igtErrCode(err) != "23505" {
		t.Errorf("a source that is receiving has another run that waits: %v", err)
	}
	// sources run side by side while they only receive, and one waiting run does not hold the gate of the KG.
	for _, r := range []struct {
		kg    string
		src   int
		stage string
	}{{"kg-a", b, pgschema.RunReceiving}, {"kg-b", c, pgschema.RunAwaitingConfirmation}} {
		if err := igrRun(admin, r.kg, r.src, r.stage); err != nil {
			t.Fatalf("%v: %v", r, err)
		}
	}
	sxRun(t, admin, `UPDATE sync_run SET stage = 'computing' WHERE src_no = $1`, a)
	if err := igrRun(admin, "kg-b", igrSource(t, admin, "kg-b", "s4"), pgschema.RunComputing); err != nil {
		t.Fatalf("a KG whose other run only waits cannot compute: %v", err)
	}
	// a KG has one run that works on it, whatever the stage of the work is.
	for _, stage := range pgschema.RunHoldsKG() {
		if _, err := admin.ExecContext(context.Background(), `UPDATE sync_run SET stage = $1 WHERE src_no = $2`, stage, b); igtErrCode(err) != "23505" {
			t.Errorf("a second run of the KG is %s: %v", stage, err)
		}
	}
	// a run that is over lets the source go, and a source can be run again.
	sxRun(t, admin, `UPDATE sync_run SET stage = 'succeeded', ended_at = now() WHERE src_no = $1`, a)
	if err := igrRun(admin, "kg-a", a, pgschema.RunReceiving); err != nil {
		t.Errorf("a source cannot be run again after a run that is over: %v", err)
	}
}

// [REQ-682]
func TestIngestARunKeepsToTheClosedStagesAndSaysHowItFailedInThreeWays(t *testing.T) {
	_, admin := sxOpen(t)
	src := igrSource(t, admin, "kg-a", "s1")
	for _, stage := range pgschema.RunStages() {
		if err := igrRun(admin, "kg-a", src, stage); err != nil && igtErrCode(err) != "23505" {
			t.Errorf("the stage %s is refused: %v", stage, err)
		}
	}
	if err := igrRun(admin, "kg-a", src, "paused"); igtErrCode(err) != "23514" {
		t.Errorf("a stage that is not one of the list: %v", err)
	}
	for _, class := range pgschema.EndClasses() {
		if _, err := admin.ExecContext(context.Background(),
			`INSERT INTO sync_run (kg_id, src_no, stage, ended_at, end_class) VALUES ('kg-a', $1, 'failed', now(), $2)`, src, class); err != nil {
			t.Errorf("the way %s of failing is refused: %v", class, err)
		}
	}
	for name, q := range map[string]string{
		"a failed run without a way":  `INSERT INTO sync_run (kg_id, src_no, stage, ended_at) VALUES ('kg-a', $1, 'failed', now())`,
		"a way for a run that is not": `INSERT INTO sync_run (kg_id, src_no, stage, ended_at, end_class) VALUES ('kg-a', $1, 'succeeded', now(), 'product')`,
		"a way that is not of three":  `INSERT INTO sync_run (kg_id, src_no, stage, ended_at, end_class) VALUES ('kg-a', $1, 'failed', now(), 'bad_luck')`,
		"an end without a time":       `INSERT INTO sync_run (kg_id, src_no, stage) VALUES ('kg-a', $1, 'canceled')`,
	} {
		if _, err := admin.ExecContext(context.Background(), q, src); igtErrCode(err) != "23514" {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a source takes its datasets and its runs with it.
	sxRun(t, admin, `INSERT INTO source_dataset (src_no, dataset_id) VALUES ($1, 'servers')`, src)
	sxRun(t, admin, `DELETE FROM source_state WHERE src_no = $1`, src)
	if sxInt(t, admin, `SELECT (SELECT count(*) FROM sync_run) + (SELECT count(*) FROM source_dataset)`) != 0 {
		t.Error("the runs or the datasets of a deleted source are still there")
	}
}

func igrDiffRows(t *testing.T, admin *sql.DB, schema string, chunk int, lines []pgschema.DiffLine) {
	t.Helper()
	for i, l := range lines {
		sxRun(t, admin, `INSERT INTO "`+schema+`".ingest_diff (ord, chunk_no, kind, type_no, entity_no, change_kind, old_hash, new_hash) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			chunk*100+i, chunk, l.Kind, l.TypeNo, l.EntityNo, l.Change, l.OldHash, l.NewHash)
	}
}

// [REQ-681]
func TestIngestTheDigestOfAChunkInSQLIsTheDigestTheGoCodeGives(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	rec, err := st.CreateStorageSchema(ctx, sxKG(t, 1), sxStandard(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := func(s string) []byte { x := sha256.Sum256([]byte(s)); return x[:] }
	chunks := [][]pgschema.DiffLine{
		{
			{Kind: pgschema.DiffKindObject, TypeNo: 3, EntityNo: 258, Change: pgschema.ChangeKindAdded, NewHash: h("a")},
			{Kind: pgschema.DiffKindObject, TypeNo: 3, EntityNo: 259, Change: pgschema.ChangeKindChanged, OldHash: h("a"), NewHash: h("b")},
			{Kind: pgschema.DiffKindLink, TypeNo: 32767, EntityNo: 1 << 40, Change: pgschema.ChangeKindRetired, OldHash: h("b")},
		},
		{{Kind: pgschema.DiffKindIdentifier, TypeNo: 1, EntityNo: 9223372036854775807, Change: pgschema.ChangeKindAdded, NewHash: h("c")}},
	}
	var chain [32]byte
	for i, lines := range chunks {
		igrDiffRows(t, admin, rec.Schema(), i, lines)
		want, err := pgschema.ChunkDigest(lines)
		if err != nil {
			t.Fatal(err)
		}
		var got []byte
		if err := admin.QueryRowContext(ctx, pgschema.ChunkDigestSQL(rec.KGNo), i).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want[:]) {
			t.Errorf("the chunk %d is %x in SQL and %x in Go", i, got, want)
		}
		chain = pgschema.ChainDigest(chain, want)
	}
	if whole, _ := pgschema.InputDigest(chunks); whole != chain {
		t.Error("the digest of the chunks is not the chain")
	}
	var none []byte
	if err := admin.QueryRowContext(ctx, pgschema.ChunkDigestSQL(rec.KGNo), 7).Scan(&none); err != nil || none != nil {
		t.Errorf("a chunk with no row has a digest %x (%v)", none, err)
	}
	sig := make([]byte, 16)
	for i := range sig {
		sig[i] = byte(0xF0 + i)
	}
	var key []byte
	if err := admin.QueryRowContext(ctx, `SELECT `+pgschema.KeyRowHashExpr("k")+` FROM (SELECT 7::smallint AS type_no, 99::bigint AS ctx_no, $1::bytea AS sig_hash) k`, sig).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if want, _ := pgschema.KeyRowHash(7, 99, sig); !bytes.Equal(key, want[:]) {
		t.Errorf("the hash of a key is %x in SQL and %x in Go", key, want)
	}
}

// igrHeldLocks is the number of locks the connection of the transaction holds.
func igrHeldLocks(t *testing.T, tx *sql.Tx) int {
	t.Helper()
	var n int
	if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid()`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// igrLocksOf runs the statements in one transaction, commits it and says how many locks it held at the end.
func igrLocksOf(t *testing.T, admin *sql.DB, stmts ...string) int {
	t.Helper()
	ctx := context.Background()
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	n := igrHeldLocks(t, tx)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return n
}

func igrSQL(steps []pgschema.Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.SQL)
	}
	return out
}

// [REQ-683]
func TestIngestMakingAndDroppingAKGOfTheStandardModelFitsInTheDefaultLockTable(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	var perTx, conns, prepared int
	if err := admin.QueryRowContext(ctx, `SELECT current_setting('max_locks_per_transaction')::int, current_setting('max_connections')::int, current_setting('max_prepared_transactions')::int`).Scan(&perTx, &conns, &prepared); err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(sxStandard(t, true), 1)
	if err != nil {
		t.Fatal(err)
	}
	// The lock table is shared by every connection: its size is the setting times the connections, and one
	// transaction takes what it needs from it. A KG must leave room for the others, so it may take at most
	// 80% of the table of the default settings (64 for each of the 100 connections).
	limit := 64 * 100 * 8 / 10
	made := igrLocksOf(t, admin, igrSQL(plan.Steps)...)
	t.Logf("making a KG of the standard model: %d locks held at the end; this server's lock table holds %d (%d x (%d + %d))", made, perTx*(conns+prepared), perTx, conns, prepared)
	if made > limit {
		t.Errorf("making a KG holds %d locks, more than %d (80%% of the lock table of the default settings)", made, limit)
	}
	put, err := pgschema.SourcePartitionSteps(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n := igrLocksOf(t, admin, igrSQL(put)...); n > 64 {
		t.Errorf("putting a source to a KG holds %d locks", n)
	} else {
		t.Logf("putting a source to a KG: %d locks", n)
	}
	takeOff, _ := pgschema.DropSourcePartitionSteps(1, 1)
	if n := igrLocksOf(t, admin, igrSQL(takeOff)...); n > 100 {
		t.Errorf("taking a source off a KG holds %d locks", n)
	} else {
		t.Logf("taking a source off a KG: %d locks", n)
	}
	// Dropping the whole schema in one transaction is kept as a number only: it locks every relation and the types of
	// its tables, and the product does not do it (ADR-0125).
	t.Logf("dropping the schema of a KG in one transaction: %d locks held", igrLocksOf(t, admin, `DROP SCHEMA "s_1" CASCADE`))
	// A KG that still has its sources costs more to drop in one transaction: each source adds about 68. A drop
	// that keeps to the default lock table takes the sources off one by one first (ADR-0095).
	plan2, err := buildPlan(sxStandard(t, true), 2)
	if err != nil {
		t.Fatal(err)
	}
	igrLocksOf(t, admin, igrSQL(plan2.Steps)...)
	var sourceSteps []string
	for i := int64(1); i <= 9; i++ {
		more, _ := pgschema.SourcePartitionSteps(2, i)
		sourceSteps = append(sourceSteps, igrSQL(more)...)
	}
	igrLocksOf(t, admin, sourceSteps...)
	t.Logf("dropping the schema of a KG that has 9 sources in one transaction: %d locks held", igrLocksOf(t, admin, `DROP SCHEMA "s_2" CASCADE`))
	// What the product does to delete a KG (ADR-0125), a KG of the server's model (its query schema too) with no source and one
	// with 9: every transaction of DeleteKG is counted at its end, when it holds all it took, and none may take more than 80%.
	igrDeleteFits(t, st, admin, limit, 1, 0)
	igrDeleteFits(t, st, admin, limit, 2, 9)
}

// igrDeleteFits makes the KG of number no with that many sources and deletes it with DeleteKG, counting the locks every
// transaction of the delete holds at its end (when it holds all it took): none may hold more than limit.
func igrDeleteFits(t *testing.T, st *Store, admin *sql.DB, limit, no, sources int) {
	t.Helper()
	kg := pgsrcKG(t, st, admin, no, sources)
	steps := map[string]int{}
	most := map[string]int{}
	st.deleteSeen = func(_ context.Context, step string, tx *sql.Tx) error {
		n := igrHeldLocks(t, tx)
		steps[step]++
		most[step] = max(most[step], n)
		if n > limit {
			t.Errorf("deleting a KG of %d sources: a transaction of the step %q holds %d locks, more than %d", sources, step, n, limit)
		}
		return nil
	}
	defer func() { st.deleteSeen = nil }()
	if err := st.DeleteKG(context.Background(), kg); err != nil {
		t.Fatal(err)
	}
	t.Logf("deleting a KG of %d sources: the most locks a transaction held, by step: %v (transactions: %v)", sources, most, steps)
	if steps[deleteStepMark] != 1 || steps[deleteStepSource] != sources || steps[deleteStepCompute] < 1 || steps[deleteStepQuery] != 1 || steps[deleteStepLast] != 1 {
		t.Errorf("deleting a KG of %d sources ran the transactions %v", sources, steps)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, pgschema.SchemaName(int64(no))); n != 0 {
		t.Errorf("the schema of the KG of %d sources is still there", sources)
	}
}
