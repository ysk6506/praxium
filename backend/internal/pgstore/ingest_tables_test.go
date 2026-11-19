package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	goslices "slices"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the tables of the ingestion at scale (ADR-0095) on a real server. The helpers start with igt, so
// they never meet a helper of another test file of this folder.

func igtColumns(t *testing.T, admin *sql.DB, schema, table string) []string {
	t.Helper()
	rows, err := admin.QueryContext(context.Background(),
		`SELECT column_name FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`, schema, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func igtErrCode(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	for _, code := range []string{"23505", "23514", "23502"} {
		if strings.Contains(s, "pq: ") && strings.Contains(s, code) {
			return code
		}
	}
	return s
}

// igtExec runs statements as the person who owns the tables and reports the first that fails.
func igtExec(admin *sql.DB, stmts ...string) error {
	for _, q := range stmts {
		if _, err := admin.ExecContext(context.Background(), q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	return nil
}

func igtPutSource(t *testing.T, admin *sql.DB, kgNo, srcNo int64) {
	t.Helper()
	steps, err := pgschema.SourcePartitionSteps(kgNo, srcNo)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := admin.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if _, err := tx.ExecContext(context.Background(), s.SQL); err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s: %v", s.SQL, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// [REQ-680]
func TestIngestTablesOfANewKGAreLoggedAndHaveTheColumnsTheNamesSay(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	m := sxStandard(t, true)
	rec, err := st.CreateStorageSchema(ctx, sxKG(t, 1), m)
	if err != nil {
		t.Fatal(err)
	}
	schema := rec.Schema()
	for _, name := range pgschema.IngestTables() {
		kind := sxText(t, admin, `SELECT c.relkind::text || c.relpersistence::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2`, schema, name)
		want := "rp"
		if goslices.Contains(pgschema.SourceTables(), name) {
			want = "pp"
		}
		if kind != want {
			t.Errorf("%s is %q (relkind, persistence), want %q: a staged table is a logged one", name, kind, want)
		}
		if got, want := igtColumns(t, admin, schema, name), pgschema.ColumnsOf(name); !goslices.Equal(got, want) {
			t.Errorf("%s has the columns %v, the names say %v", name, got, want)
		}
	}
	plan, err := buildPlan(m, rec.KGNo)
	if err != nil {
		t.Fatal(err)
	}
	wantHash, _ := plan.Manifest.Hash()
	gotHash, _ := rec.Manifest.Hash()
	if gotHash != wantHash || rec.GeneratorVersion != pgschema.GeneratorVersion {
		t.Errorf("the manifest of a new KG is not the plan's: generator %d", rec.GeneratorVersion)
	}
	if err := st.CheckStorageSchema(ctx, sxKG(t, 1), m); err != nil {
		t.Fatalf("the check of the manifest fails: %v", err)
	}
	if sxInvalid(t, admin, schema) != 0 {
		t.Error("an index is invalid")
	}
	// the closed list of the kinds of problem is kept by the table.
	insert := `INSERT INTO "` + schema + `".record_problem (src_no, dataset_no, key, def_no, field, kind, reason) VALUES (1, 1, 'k', 1, 'serial', $1, 'r')`
	if _, err := admin.ExecContext(ctx, insert, pgschema.ProblemMissingName); err != nil {
		t.Errorf("a kind of the list is refused: %v", err)
	}
	if _, err := admin.ExecContext(ctx, insert, "made_up_kind"); igtErrCode(err) != "23514" {
		t.Errorf("a kind that is not in the list was accepted or failed another way: %v", err)
	}
}

// [REQ-680]
func TestIngestASourceHasPartitionsThatAreAttachedEmptiedAndDroppedOneByOne(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	rec, err := st.CreateStorageSchema(ctx, sxKG(t, 1), sxStandard(t, true))
	if err != nil {
		t.Fatal(err)
	}
	q := `"` + rec.Schema() + `".`
	raw := func(src int, key string) string {
		return fmt.Sprintf(`INSERT INTO %sraw_record (src_no, dataset_no, key, raw_hash, fields, decl_rev, opened_seq, fate) VALUES (%d, 1, '%s', '\x00', '{a}', 1, 1, 0)`, q, src, key)
	}
	stage := func(src int) string {
		return fmt.Sprintf(`INSERT INTO %sstage_record (src_no, dataset_no, page, row_no, key, change_kind) VALUES (%d, 1, 0, 1, 'k', 0)`, q, src)
	}
	if _, err := admin.ExecContext(ctx, raw(5, "a")); err == nil {
		t.Fatal("a raw record of a source that was not put to the KG was accepted")
	}
	igtPutSource(t, admin, rec.KGNo, 5)
	igtPutSource(t, admin, rec.KGNo, 6)
	if err := igtExec(admin, raw(5, "a"), raw(6, "a"), raw(6, "b"), stage(5), stage(6)); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, raw(5, "a")); igtErrCode(err) != "23505" {
		t.Errorf("the key of the raw records is not unique across the partition: %v", err)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_inherits i JOIN pg_class p ON p.oid = i.inhparent JOIN pg_namespace n ON n.oid = p.relnamespace
		WHERE n.nspname = $1 AND p.relkind = 'p'`, rec.Schema()); n != 2*len(pgschema.SourceTables()) {
		t.Errorf("%d partitions are attached", n)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = 'raw_record_p5' AND c.relpersistence = 'p'`, rec.Schema()); n != 1 {
		t.Error("the partition of the raw records is not a logged table")
	}
	trunc, err := pgschema.TruncateReceiveSQL(rec.KGNo, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := igtExec(admin, trunc); err != nil {
		t.Fatal(err)
	}
	count := func(table string) int { return sxInt(t, admin, `SELECT count(*) FROM `+q+table) }
	if count("stage_record") != 1 || count("raw_record") != 3 {
		t.Errorf("emptying a source's staged rows touched another source or the raw records: %d staged, %d raw", count("stage_record"), count("raw_record"))
	}
	drop, err := pgschema.DropSourcePartitionSteps(rec.KGNo, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range drop {
		if err := igtExec(admin, s.SQL); err != nil {
			t.Fatal(err)
		}
	}
	if count("raw_record") != 2 || sxInt(t, admin, `SELECT count(*) FROM pg_tables WHERE schemaname = $1 AND tablename LIKE '%\_p5'`, rec.Schema()) != 0 {
		t.Error("taking a source off the KG left its rows or its tables")
	}
}

// [REQ-680]
func TestIngestPuttingASourceToAKGDoesNotWaitForAnotherSourceThatIsBeingReceived(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	rec, err := st.CreateStorageSchema(ctx, sxKG(t, 1), sxStandard(t, true))
	if err != nil {
		t.Fatal(err)
	}
	igtPutSource(t, admin, rec.KGNo, 1)
	open, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = open.Rollback() }()
	if _, err := open.ExecContext(ctx, fmt.Sprintf(`INSERT INTO "%s".stage_record (src_no, dataset_no, page, row_no, key, change_kind) VALUES (1, 1, 0, 1, 'k', 0)`, rec.Schema())); err != nil {
		t.Fatal(err)
	}
	steps, err := pgschema.SourcePartitionSteps(rec.KGNo, 2)
	if err != nil {
		t.Fatal(err)
	}
	attach, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = attach.Rollback() }()
	if _, err := attach.ExecContext(ctx, `SET LOCAL lock_timeout = '3s'`); err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if _, err := attach.ExecContext(ctx, s.SQL); err != nil {
			t.Fatalf("the second source waited for the first and gave up: %s: %v", s.SQL, err)
		}
	}
	if err := attach.Commit(); err != nil {
		t.Fatal(err)
	}
}

// igtStripIngest turns a KG that this build made into one that the generator before made: the ingestion tables
// are dropped and the record says the generator 2.
func igtStripIngest(t *testing.T, admin *sql.DB, schema string, kg graph.KGID) {
	t.Helper()
	names := goslices.Clone(pgschema.IngestTables())
	goslices.Reverse(names)
	for _, n := range names {
		sxRun(t, admin, `DROP TABLE IF EXISTS "`+schema+`"."`+n+`" CASCADE`)
	}
	sxRun(t, admin, `UPDATE kg_schema_state SET generator_version = 2, manifest = jsonb_set(manifest, '{generator_version}', '2') WHERE kg_id = $1`, kg.String())
}

// [REQ-680]
func TestIngestAKGMadeByTheGeneratorBeforeIsAdjustedByAddingTheTablesWithoutClosingIt(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	m := sxStandard(t, true)
	kg := sxKG(t, 9)
	made, err := st.CreateKG(ctx, graph.KG{ID: kg, Name: "old", Role: graph.RoleTest, DefinitionSetHash: m.Registry.Hash(), CreatedAt: time.Unix(1000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.StorageSchema(ctx, kg)
	if err != nil {
		t.Fatal(err)
	}
	schema := rec.Schema()
	headBefore, err := st.Head(ctx, kg)
	if err != nil {
		t.Fatal(err)
	}
	igtStripIngest(t, admin, schema, kg)
	if err := st.CheckStorageSchema(ctx, kg, m); kernel.CodeOf(err) != CodeSchemaMismatch {
		t.Fatalf("a KG of the generator before matches the model: %v", err)
	}
	// a reader that has the KG's tables open and a lock timeout that would show a wait: the adjustment must
	// make tables beside it, not take the KG's tables from the reader.
	reader, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Rollback() }()
	if _, err := reader.ExecContext(ctx, `LOCK TABLE "`+schema+`".object, "`+schema+`".link IN ROW EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	got, err := st.AdjustStorageSchema(ctx, kg, m)
	if err != nil {
		t.Fatalf("the adjustment from the generator before: %v", err)
	}
	if got.State != "ok" || got.GeneratorVersion != pgschema.GeneratorVersion {
		t.Fatalf("the record after the adjustment: %+v", got)
	}
	for _, n := range pgschema.IngestTables() {
		if sxInt(t, admin, `SELECT count(*) FROM pg_tables WHERE schemaname = $1 AND tablename = $2`, schema, n) != 1 {
			t.Errorf("the table %s was not made", n)
		}
	}
	plan, err := buildPlan(m, rec.KGNo)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := plan.Manifest.Hash()
	if have, _ := got.Manifest.Hash(); have != want {
		t.Error("an adjusted KG has another manifest than a new one")
	}
	if err := st.CheckStorageSchema(ctx, kg, m); err != nil {
		t.Errorf("the adjusted schema does not match the model: %v", err)
	}
	if sxInvalid(t, admin, schema) != 0 {
		t.Error("an index is invalid")
	}
	headAfter, err := st.Head(ctx, kg)
	if err != nil || fmt.Sprintf("%+v", headAfter) != fmt.Sprintf("%+v", headBefore) || made.State != graph.StateBuilding {
		t.Errorf("the adjustment changed the head of the KG or closed it: %+v then %+v (%v)", headBefore, headAfter, err)
	}
	same := sxSnapshot(t, admin, schema)
	if _, err := st.AdjustStorageSchema(ctx, kg, m); err != nil || sxSnapshot(t, admin, schema) != same {
		t.Errorf("adjusting again changed something: %v", err)
	}
}

// igtOpenStandard opens a store that holds the standard model (so it can make KGs) and an administrator's
// connection to its database.
func igtOpenStandard(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dsn := NewTestDatabase(t)
	models, err := StandardModels(true)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(context.Background(), Config{DSN: dsn, Models: models})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return st, admin
}
