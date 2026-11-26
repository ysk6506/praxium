package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the making of a KG in steps and of the marks of its query schemas (ADR-0126). The helpers start with kgm.

// errKgmStop is the error a test stops a making with.
var errKgmStop = errors.New("the making stops here")

// kgmOpen opens a store that holds the model *m on a new empty database (the query schemas make roles named by its token), and a
// connection to look at it; it returns the DSN too, for another server.
func kgmOpen(t *testing.T, m *SchemaModel) (*Store, *sql.DB, string) {
	t.Helper()
	sxParallel(t)
	dsn := NewTestDatabase(t)
	st := kgmServer(t, dsn, m)
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return st, admin, dsn
}

// kgmServer opens a store on the database that holds the model *m for every definition set: a test changes *m to give the next
// server another model.
func kgmServer(t *testing.T, dsn string, m *SchemaModel) *Store {
	t.Helper()
	st, err := Open(context.Background(), Config{DSN: dsn, Models: func(kernel.Hash) (SchemaModel, error) { return *m, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// kgmKG is a KG of the model, of the id and the name n gives.
func kgmKG(t *testing.T, m SchemaModel, n int) graph.KG {
	t.Helper()
	return graph.KG{ID: sxKG(t, n), Name: fmt.Sprintf("kgm%d", n), Role: graph.RoleTest, DefinitionSetHash: m.Registry.Hash(), CreatedAt: time.Unix(1000, 0)}
}

// kgmGone is what kgmLeft says of a KG that left nothing.
const kgmGone = "listed=0 making=0 deleting=0 record=0 storage=0 query=0 roles=0"

// kgmLeft is what the database holds of the KG of number no: its row of the list, its marks, its record, its storage schema, its
// query schemas and its roles.
func kgmLeft(t *testing.T, admin *sql.DB, kg graph.KGID, no int64) string {
	t.Helper()
	token := sxText(t, admin, `SELECT token FROM install_token WHERE id = 1`)
	return fmt.Sprintf("listed=%d making=%d deleting=%d record=%d storage=%d query=%d roles=%d",
		sxInt(t, admin, `SELECT count(*) FROM kg_catalog WHERE kg_id = $1`, kg.String()),
		sxInt(t, admin, `SELECT count(*) FROM kg_making WHERE kg_id = $1`, kg.String()),
		sxInt(t, admin, `SELECT count(*) FROM kg_deleting WHERE kg_id = $1`, kg.String()),
		sxInt(t, admin, `SELECT count(*) FROM kg_schema_state WHERE kg_id = $1`, kg.String()),
		sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, pgschema.SchemaName(no)),
		sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = ANY (string_to_array($1, ','))`, strings.Join(pgschema.QuerySchemaNames(no), ",")),
		sxInt(t, admin, `SELECT count(*) FROM pg_roles WHERE rolname = ANY (string_to_array($1, ','))`, strings.Join(pgschema.QueryRoleNames(token, no), ",")))
}

// kgmStopAt makes the store stop at its n-th transaction of a query schema with errKgmStop. With cancel, the context of the work is
// cancelled there too, as a server that dies does: what was made cannot be deleted by the making.
func kgmStopAt(st *Store, n int, cancel context.CancelFunc) {
	seen := 0
	st.schemaSeen = func(_ context.Context, step string, _ *sql.Tx) error {
		if step != schemaStepQuery && step != schemaStepFresh {
			return nil
		}
		if seen++; seen != n {
			return nil
		}
		if cancel != nil {
			cancel()
		}
		return errKgmStop
	}
}

// kgmCount makes the store count the transactions of its makings and adjustments by step.
func kgmCount(st *Store) map[string]int {
	var mu sync.Mutex
	steps := map[string]int{}
	st.schemaSeen = func(_ context.Context, step string, _ *sql.Tx) error {
		mu.Lock()
		defer mu.Unlock()
		steps[step]++
		return nil
	}
	return steps
}

// [REQ-920]
func TestKgmAMakingThatFailsAtAnyQuerySchemaDeletesWhatItMadeAndReturnsItsError(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	st, admin, _ := kgmOpen(t, &m)
	for i, at := range []int{1, 4, 6} {
		kg := kgmKG(t, m, 10+i)
		kgmStopAt(st, at, nil)
		if _, err := st.CreateKG(ctx, kg); !errors.Is(err, errKgmStop) {
			t.Fatalf("a making stopped at the query schema %d: %v", at, err)
		}
		if got := kgmLeft(t, admin, kg.ID, int64(i+1)); got != kgmGone {
			t.Errorf("a making stopped at the query schema %d left %s", at, got)
		}
	}
	st.schemaSeen = nil
	// a storage schema alone, whose fifth query schema has a statement that PostgreSQL refuses
	broken := func(m SchemaModel, no int64) (pgschema.Plan, error) {
		p, err := buildPlan(m, no)
		for i, st := range p.Query {
			if st.What == pgschema.WhatGrant && st.Name == pgschema.QuerySchemaNames(no)[4] {
				p.Query[i].SQL = "GRANT nothing"
			}
		}
		return p, err
	}
	if _, err := st.createStorageSchema(ctx, sxKG(t, 20), m, broken); err == nil || !strings.Contains(err.Error(), "the grant "+pgschema.QuerySchemaNames(4)[4]) {
		t.Fatalf("a storage schema with a query schema that cannot be made: %v", err)
	}
	if got := kgmLeft(t, admin, sxKG(t, 20), 4); got != kgmGone {
		t.Errorf("the storage schema whose fifth query schema failed left %s", got)
	}
	// nothing of them stands in the way of a KG of the same name
	if _, err := st.CreateKG(ctx, kgmKG(t, m, 10)); err != nil {
		t.Fatal(err)
	}
	if got := kgmLeft(t, admin, sxKG(t, 10), 5); got != "listed=1 making=0 deleting=0 record=1 storage=1 query=6 roles=3" {
		t.Errorf("the KG made after them: %s", got)
	}
}

// [REQ-920]
func TestKgmAMakingWhoseServerStopsIsClosedAndIsDeletedOnlyOnceItsTermIsOver(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	st, admin, dsn := kgmOpen(t, &m)
	kg := kgmKG(t, m, 1)
	stopped, cancel := context.WithCancel(ctx)
	kgmStopAt(st, 2, cancel)
	if _, err := st.CreateKG(stopped, kg); !errors.Is(err, errKgmStop) {
		t.Fatalf("the making that stops: %v", err)
	}
	st.schemaSeen = nil
	half := "listed=1 making=1 deleting=0 record=1 storage=1 query=1 roles=3"
	if got := kgmLeft(t, admin, kg.ID, 1); got != half {
		t.Fatalf("the making whose server stopped left %s", got)
	}
	kgdClosed(t, st, kg.ID, kg.Name)
	// before its term is over: a server that starts leaves it, and a KG of its name is a conflict
	if resumed, err := kgmServer(t, dsn, &m).ResumeAllWork(ctx); err != nil || len(resumed) != 0 {
		t.Errorf("a server that starts within the term ended %v: %v", resumed, err)
	}
	again := kgmKG(t, m, 2)
	again.Name = kg.Name
	if _, err := st.CreateKG(ctx, again); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Errorf("a KG of the name of one being made: %v", err)
	}
	if got := kgmLeft(t, admin, kg.ID, 1); got != half {
		t.Errorf("within the term: %s", got)
	}
	// once the term is over, the KG of its name deletes it first
	sxRun(t, admin, `UPDATE kg_making SET expires_at = now() - interval '1 second'`)
	made, err := st.CreateKG(ctx, again)
	if err != nil {
		t.Fatalf("a KG of the name of one whose term is over: %v", err)
	}
	if got := kgmLeft(t, admin, kg.ID, 1); got != kgmGone {
		t.Errorf("the making whose term is over: %s", got)
	}
	if got, err := st.GetKG(ctx, kg.Name); err != nil || got.ID != made.ID {
		t.Errorf("the name finds %v: %v", got.ID, err)
	}
	// and a server that starts deletes one whose term is over
	other := kgmKG(t, m, 3)
	stopped, cancel = context.WithCancel(ctx)
	kgmStopAt(st, 5, cancel)
	if _, err := st.CreateKG(stopped, other); !errors.Is(err, errKgmStop) {
		t.Fatalf("the making that stops: %v", err)
	}
	st.schemaSeen = nil
	sxRun(t, admin, `UPDATE kg_making SET expires_at = now() - interval '1 second'`)
	resumed, err := kgmServer(t, dsn, &m).ResumeAllWork(ctx)
	if err != nil || len(resumed) != 1 || resumed[0] != other.ID {
		t.Errorf("the server that starts after the term ended %v: %v", resumed, err)
	}
	if got := kgmLeft(t, admin, other.ID, 3); got != kgmGone {
		t.Errorf("after the server started: %s", got)
	}
}

// [REQ-920]
func TestKgmAMakingThatRunsPastItsTermOrWhoseKGIsDeletedStopsAndLeavesNothing(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	st, admin, _ := kgmOpen(t, &m)
	for i, c := range []struct {
		what, statement string
		code            kernel.Code
	}{
		{"the term is over", `UPDATE kg_making SET expires_at = now() - interval '1 second' WHERE kg_id = $1`, kernel.CodeConflict},
		{"the KG is being deleted", `INSERT INTO kg_deleting (kg_id, started_at) VALUES ($1, now())`, graph.CodeKGNotFound},
	} {
		kg := kgmKG(t, m, 1+i)
		seen := 0
		st.schemaSeen = func(ctx context.Context, step string, tx *sql.Tx) error {
			if step == schemaStepQuery {
				if seen++; seen == 2 {
					_, err := tx.ExecContext(ctx, c.statement, kg.ID.String())
					return err
				}
			}
			return nil
		}
		if _, err := st.CreateKG(ctx, kg); kernel.CodeOf(err) != c.code {
			t.Errorf("%s: the making ended with %v, want %s", c.what, err, c.code)
		}
		if got := kgmLeft(t, admin, kg.ID, int64(1+i)); got != kgmGone {
			t.Errorf("%s: the making left %s", c.what, got)
		}
	}
}

// [REQ-920]
func TestKgmAStorageSchemaThatAnotherRunIsMakingIsEndedByARunOfTheSameModelAndOneWhoseTermIsOverIsMadeAnew(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	st, admin, _ := kgmOpen(t, &m)
	id := sxKG(t, 1)
	sh, err := st.rt.locate(id)
	if err != nil {
		t.Fatal(err)
	}
	stopped, cancel := context.WithCancel(ctx)
	kgmStopAt(st, 2, cancel)
	if _, err := st.CreateStorageSchema(stopped, id, m); !errors.Is(err, errKgmStop) {
		t.Fatalf("the making that stops: %v", err)
	}
	st.schemaSeen = nil
	// a making within its term is not deleted as one that stopped
	if done, err := st.unmake(ctx, sh, id, true); done || err != nil {
		t.Errorf("a making within its term was deleted: %v %v", done, err)
	}
	rec, err := st.CreateStorageSchema(ctx, id, m)
	if err != nil {
		t.Fatalf("a run of the same model: %v", err)
	}
	if got := kgmLeft(t, admin, id, rec.KGNo); rec.KGNo != 1 || got != "listed=0 making=0 deleting=0 record=1 storage=1 query=6 roles=3" {
		t.Errorf("the run of the same model ended the making as %d: %s", rec.KGNo, got)
	}
	// one whose term is over: the first transaction of a run at that moment refuses it, and a run after deletes it and makes it anew
	other := sxKG(t, 2)
	stopped, cancel = context.WithCancel(ctx)
	kgmStopAt(st, 1, cancel)
	if _, err := st.CreateStorageSchema(stopped, other, m); !errors.Is(err, errKgmStop) {
		t.Fatalf("the making that stops: %v", err)
	}
	st.schemaSeen = nil
	sxRun(t, admin, `UPDATE kg_making SET expires_at = now() - interval '1 second'`)
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createInTx(ctx, tx, other, m, buildPlan); kernel.CodeOf(err) != kernel.CodeConflict {
		t.Errorf("the first transaction of a run that meets a making whose term is over: %v", err)
	}
	_ = tx.Rollback()
	again, err := st.CreateStorageSchema(ctx, other, m)
	if err != nil || again.KGNo != 3 {
		t.Fatalf("a run after the term made %d: %v", again.KGNo, err)
	}
	// the record is the one of the new number; nothing of the number 2 is left
	if got := kgmLeft(t, admin, other, 2); got != "listed=0 making=0 deleting=0 record=1 storage=0 query=0 roles=0" {
		t.Errorf("the making whose term was over: %s", got)
	}
}

// [REQ-920]
func TestKgmAnExecutorThatMayNotMakeRolesMakesNoKGAndLeavesNothing(t *testing.T) {
	ctx := context.Background()
	sxParallel(t)
	dsn := NewTestDatabase(t)
	root, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	owner := "kgm_own_" + sxText(t, root, `SELECT substr(md5(random()::text), 1, 10)`)
	sxRun(t, root, `CREATE ROLE `+owner+` LOGIN NOCREATEROLE NOSUPERUSER`)
	sxRun(t, root, `ALTER DATABASE `+pgschema.QuoteIdent(sxText(t, root, `SELECT current_database()`))+` OWNER TO `+owner)
	own := strings.Replace(dsn, "user=praxium", "user="+owner, 1)
	if own == dsn {
		t.Fatalf("the DSN of the test server has no user to replace")
	}
	m := qscModel(t)
	st := kgmServer(t, own, &m)
	kg := kgmKG(t, m, 1)
	if _, err := st.CreateKG(ctx, kg); err == nil || !strings.Contains(err.Error(), "right to make roles") {
		t.Fatalf("a KG made by an executor that may not make roles: %v", err)
	}
	if got := kgmLeft(t, root, kg.ID, 1); got != kgmGone {
		t.Errorf("the making left %s", got)
	}
}

// [REQ-920] [REQ-921]
func TestKgmTheTransactionsOfTheMakingFailInATransactionThatIsOver(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	rec, err := st.CreateStorageSchema(ctx, sxKG(t, 1), qscModel(t))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := queryMark(ctx, tx, "q_1_open"); err == nil {
		t.Error("a mark is read in a transaction that is over")
	}
	if err := partGoesOn(ctx, tx, sxKG(t, 1), true); err == nil {
		t.Error("a part goes on in a transaction that is over")
	}
	if _, err := joinMaking(ctx, tx, rec, qscModel(t), buildPlan); kernel.CodeOf(err) == kernel.CodeConflict || err == nil {
		t.Errorf("a making is joined in a transaction that is over: %v", err)
	}
}

// [REQ-920]
func TestKgmADeleteOfAMakingThatFailsLeavesItForTheNextOneAndAStrayMarkIsAFailure(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	st, admin, _ := kgmOpen(t, &m)
	kg := kgmKG(t, m, 1)
	stopped, cancel := context.WithCancel(ctx)
	kgmStopAt(st, 1, cancel)
	if _, err := st.CreateKG(stopped, kg); !errors.Is(err, errKgmStop) {
		t.Fatalf("the making that stops: %v", err)
	}
	alone := sxKG(t, 2)
	stopped, cancel = context.WithCancel(ctx)
	kgmStopAt(st, 1, cancel)
	if _, err := st.CreateStorageSchema(stopped, alone, m); !errors.Is(err, errKgmStop) {
		t.Fatalf("the making of a storage schema that stops: %v", err)
	}
	st.schemaSeen = nil
	sxRun(t, admin, `UPDATE kg_making SET expires_at = now() - interval '1 second'`)
	// a delete of a stopped making that fails is said by the run that would make it again, and by a server that starts; the KG whose
	// delete stopped after its mark is ended as a delete by the same start
	st.StopDeleteAt(deleteStepLast, errKgmStop)
	if _, err := st.CreateStorageSchema(ctx, alone, m); !errors.Is(err, errKgmStop) {
		t.Errorf("a storage schema made while the delete before it fails: %v", err)
	}
	st.StopDeleteAt(deleteStepLast, errKgmStop)
	if resumed, err := st.ResumeAllWork(ctx); !errors.Is(err, errKgmStop) || len(resumed) != 2 || resumed[0] != alone || resumed[1] != kg.ID {
		t.Errorf("the server whose first delete fails ended %v: %v", resumed, err)
	}
	st.deleteSeen = nil
	if _, err := st.CreateKG(ctx, kg); err != nil {
		t.Fatalf("the KG made after the deletes: %v", err)
	}
	if _, err := st.CreateStorageSchema(ctx, alone, m); err != nil {
		t.Fatalf("the storage schema made after the deletes: %v", err)
	}
	// a mark with nothing under it is not deleted (there is no making to end), and a making of the same id fails on it
	sh, err := st.rt.locate(kg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done, err := st.unmake(ctx, sh, sxKG(t, 9), false); done || err != nil {
		t.Errorf("a KG without a mark was deleted: %v %v", done, err)
	}
	sxRun(t, admin, `INSERT INTO kg_making (kg_id, expires_at) VALUES ($1, now() + interval '1 hour')`, sxKG(t, 9).String())
	if _, err := st.CreateStorageSchema(ctx, sxKG(t, 9), m); err == nil || !strings.Contains(err.Error(), "being made") {
		t.Errorf("a making over a stray mark: %v", err)
	}
	// steps that name no schema, and a schema the steps do not make, are refused before and at the mark
	if err := st.makeQuerySchemas(ctx, sh, kg.ID, pgschema.Plan{Query: []pgschema.Step{{What: pgschema.WhatView, Name: "v", SQL: "SELECT 1"}}}, false); kernel.CodeOf(err) != kernel.CodeInternal {
		t.Errorf("steps that name no schema: %v", err)
	}
	none := pgschema.Plan{Query: []pgschema.Step{{What: pgschema.WhatSchema, Name: "q_kgm_none", SQL: "SELECT 1"}}}
	if err := st.makeQuerySchemas(ctx, sh, kg.ID, none, false); err == nil || !strings.Contains(err.Error(), "mark a query schema") {
		t.Errorf("a schema the steps do not make: %v", err)
	}
}

// [REQ-921]
func TestKgmAServerThatStartsAndCannotMakeAStaleQuerySchemaSaysSoAndTheNextOneMakesIt(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	st, admin, _ := kgmOpen(t, &m)
	kg := kgmKG(t, m, 1)
	if _, err := st.CreateKG(ctx, kg); err != nil {
		t.Fatal(err)
	}
	sxRun(t, admin, `COMMENT ON SCHEMA `+pgschema.QuoteIdent(pgschema.QuerySchemaNames(1)[2])+` IS NULL`)
	kgmStopAt(st, 1, nil)
	if done, err := st.AdjustOldSchemas(ctx); err == nil || !strings.Contains(err.Error(), errKgmStop.Error()) || len(done) != 0 {
		t.Errorf("a start whose query schema fails: %+v, %v", done, err)
	}
	// a token that is not one stops the start before any query schema
	sxRun(t, admin, `ALTER TABLE install_token DROP CONSTRAINT install_token_token_check`)
	token := sxText(t, admin, `SELECT token FROM install_token`)
	sxRun(t, admin, `UPDATE install_token SET token = 'NOT-A-TOKEN'`)
	steps := kgmCount(st)
	if _, err := st.AdjustOldSchemas(ctx); err == nil || len(steps) != 0 {
		t.Errorf("a start with a token that is not one: %v, %v", err, steps)
	}
	sxRun(t, admin, `UPDATE install_token SET token = $1`, token)
	if done, err := st.AdjustOldSchemas(ctx); err != nil || len(done) != 1 || steps[schemaStepQuery] != 1 {
		t.Errorf("the next start: %+v, %v, %v", done, err, steps)
	}
}
