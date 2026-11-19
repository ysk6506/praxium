package pgstore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

func sxOps(st *Store, kg graph.KGID, m SchemaModel) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"create": func() error { _, err := st.CreateStorageSchema(ctx, kg, m); return err },
		"adjust": func() error { _, err := st.AdjustStorageSchema(ctx, kg, m); return err },
		"stats":  func() error { _, err := st.FinishStorageStats(ctx, kg, m); return err },
		"drop":   func() error { return st.DropStorageSchema(ctx, kg) },
		"read":   func() error { _, err := st.StorageSchema(ctx, kg); return err },
		"check":  func() error { return st.CheckStorageSchema(ctx, kg, m) },
	}
}

func TestTheSchemaExecutorFailsWithAnErrorOnAClosedPoolAndOnDamagedRecords(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := sxKG(t, 40)
	m := sxSmall(t, sxSearch(1, 10))
	if _, err := st.CreateStorageSchema(ctx, kg, m); err != nil {
		t.Fatal(err)
	}
	sxRun(t, admin, `UPDATE kg_schema_state SET manifest = '[]'::jsonb`)
	for name, op := range sxOps(st, kg, m) {
		if err := op(); err == nil {
			t.Errorf("%s with a manifest that cannot be read: no error", name)
		}
	}
	sxRun(t, admin, `UPDATE kg_schema_state SET manifest = '{}'::jsonb`)
	sxRun(t, admin, `ALTER TABLE kg_schema_state RENAME TO kg_schema_state_away`)
	for name, op := range sxOps(st, kg, m) {
		if err := op(); err == nil || kernel.CodeOf(err) == kernel.CodeNotFound {
			t.Errorf("%s with the table of records gone: %v", name, err)
		}
	}
	sxRun(t, admin, `ALTER TABLE kg_schema_state_away RENAME TO kg_schema_state`)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for name, op := range sxOps(st, kg, m) {
		if err := op(); err == nil {
			t.Errorf("%s on a closed pool: no error", name)
		}
	}
}

func TestARunThatCannotGetTheLockOfTheSchemaWorkGivesUpWithAnError(t *testing.T) {
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	st, err := Open(ctx, Config{DSN: dsn, Limits: map[Role]Limits{pgconn.RoleMigration: {Statement: time.Minute, LockWait: 30 * time.Millisecond, IdleInTx: time.Minute}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	kg := sxKG(t, 41)
	m := sxSmall(t, sxSearch(1, 10))
	if _, err := st.CreateStorageSchema(ctx, kg, m); err != nil {
		t.Fatal(err)
	}
	holder, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, schemaLockPrefix+kg.String()); err != nil {
		t.Fatal(err)
	}
	for name, op := range sxOps(st, kg, m) {
		if name == "read" || name == "check" {
			continue
		}
		if err := op(); err == nil {
			t.Errorf("%s got through a lock that another run holds", name)
		}
	}
}

func TestAnAdjustmentThatCannotMakeATableOrBuildAnIndexLeavesTheSchemaUsable(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := sxKG(t, 42)
	rec, err := st.CreateStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10)))
	if err != nil {
		t.Fatal(err)
	}
	sxRun(t, admin, `CREATE TABLE `+pgschema.Qualified(rec.Schema(), "t_3")+` (a int)`)
	before := sxSnapshot(t, admin, rec.Schema())
	if _, err := st.AdjustStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10), "type")); err == nil {
		t.Fatal("a table that is in the way was made")
	}
	if sxSnapshot(t, admin, rec.Schema()) != before {
		t.Fatal("the failed adjustment left something")
	}
	failing := func(SchemaModel, int64) (pgschema.Plan, error) {
		return pgschema.Plan{}, kernel.Errorf(kernel.CodeInternal, "no plan")
	}
	if _, err := st.adjustStorageSchema(ctx, kg, sxSmall(t, sxSearch(1, 10)), failing); err == nil {
		t.Fatal("a plan that failed adjusted a schema")
	}
	sxRow(t, admin, rec.Schema(), "t_1", 1, `{"11":{"v":"not a number"}}`)
	more := sxSmall(t, sxSearch(1, 10, 1, 11))
	for i := 0; i < 2; i++ {
		if _, err := st.AdjustStorageSchema(ctx, kg, more); err == nil {
			t.Fatalf("run %d built an index on text that is not a number", i)
		}
		if sxText(t, admin, `SELECT state FROM kg_schema_state`) != "index_pending" {
			t.Fatalf("run %d: the state is not index_pending", i)
		}
	}
	if _, err := st.FinishStorageStats(ctx, kg, more); err != nil {
		t.Fatalf("a schema whose statistics are made: %v", err)
	}
}

func TestStatisticsThatCannotBeMadeAndATableThatIsInUseStopTheirRunWithAnError(t *testing.T) {
	ctx := context.Background()
	dsn := NewTestDatabase(t)
	st, err := Open(ctx, Config{DSN: dsn, Limits: map[Role]Limits{pgconn.RoleMigration: {Statement: time.Minute, LockWait: 30 * time.Millisecond, IdleInTx: time.Minute}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	kg := sxKG(t, 43)
	m := sxSmall(t, sxSearch(1, 10))
	m.WithStats = false
	rec, err := st.CreateStorageSchema(ctx, kg, m)
	if err != nil {
		t.Fatal(err)
	}
	table := pgschema.Qualified(rec.Schema(), "t_1")
	sxRun(t, admin, `CREATE STATISTICS `+pgschema.Qualified(rec.Schema(), "t_1_st_1")+` ON (doc->'1'->>'v'), (doc->'10'->>'v') FROM `+table)
	before := sxSnapshot(t, admin, rec.Schema())
	if _, err := st.FinishStorageStats(ctx, kg, m); err == nil {
		t.Fatal("statistics were made over a name that is taken")
	}
	if sxSnapshot(t, admin, rec.Schema()) != before {
		t.Fatal("the failed run left something")
	}
	holder, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx, `LOCK TABLE `+table+` IN ACCESS SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	if err := st.DropStorageSchema(ctx, kg); err == nil {
		t.Fatal("a schema was dropped under a table that is in use")
	}
}
