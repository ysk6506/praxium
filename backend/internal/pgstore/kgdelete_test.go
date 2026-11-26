package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the delete of a KG in steps (ADR-0125). The helpers start with kgd.

// errKgdStop is the error a test stops a delete with, as a server that dies there.
var errKgdStop = errors.New("the server stops here")

// kgdOpen opens a store of the light model (no query schema) on a new database the migrations ran on, and a connection to look
// at it; it returns the DSN too, for a second server.
func kgdOpen(t *testing.T) (*Store, *sql.DB, string) {
	t.Helper()
	dsn := pgconn.NewMigratedTestDatabase(t)
	st := kgdServer(t, dsn)
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return st, admin, dsn
}

// kgdServer opens another store of the light model on the database.
func kgdServer(t *testing.T, dsn string) *Store {
	t.Helper()
	models, err := StandardModels(false)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(context.Background(), Config{DSN: dsn, Models: models, LeaseClock: LeaseClockCaller})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// kgdState is what the database holds of the KG of number no: its row of the list, its mark, its storage schema, the tables of
// its computation and its sources.
func kgdState(t *testing.T, admin *sql.DB, kg graph.KGID, no int64) string {
	t.Helper()
	return fmt.Sprintf("listed=%d marked=%d schema=%d compute=%d sources=%d",
		sxInt(t, admin, `SELECT count(*) FROM kg_catalog WHERE kg_id = $1`, kg.String()),
		sxInt(t, admin, `SELECT count(*) FROM kg_deleting WHERE kg_id = $1`, kg.String()),
		sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, pgschema.SchemaName(no)),
		sxInt(t, admin, `SELECT count(*) FROM pg_tables WHERE schemaname = $1 AND tablename = ANY (string_to_array($2, ','))`,
			pgschema.SchemaName(no), strings.Join(pgschema.ComputeTables(), ",")),
		sxInt(t, admin, `SELECT count(*) FROM source_state WHERE kg_id = $1`, kg.String()))
}

// kgdClosed checks that every way in refuses a KG that is being deleted as one that is not there, and that the list leaves it
// out.
func kgdClosed(t *testing.T, st *Store, kg graph.KGID, name string) {
	t.Helper()
	ctx := context.Background()
	refused := func(what string, err error) {
		t.Helper()
		if kernel.CodeOf(err) != graph.CodeKGNotFound {
			t.Errorf("%s of a KG being deleted: %v, want %s", what, err, graph.CodeKGNotFound)
		}
	}
	_, err := st.GetKG(ctx, kg.String())
	refused("GetKG by id", err)
	_, err = st.GetKG(ctx, name)
	refused("GetKG by name", err)
	_, err = st.Head(ctx, kg)
	refused("Head", err)
	_, err = st.Generations(ctx, kg)
	refused("Generations", err)
	_, err = st.AcquireLease(ctx, graph.LeaseRequest{KG: kg, Owner: "w", TTL: time.Minute, Now: time.Unix(2000, 0)})
	refused("AcquireLease", err)
	kgs, err := st.ListKGs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kgs {
		if k.ID == kg {
			t.Errorf("the list has a KG being deleted: %+v", k)
		}
	}
}

// [REQ-684]
func TestKgdADeleteThatStopsInTheMiddleKeepsTheKGClosedAndEndsWhenCalledAgainOrWhenAServerStarts(t *testing.T) {
	ctx := context.Background()
	st, admin, dsn := kgdOpen(t)
	all := fmt.Sprintf("listed=1 marked=0 schema=1 compute=%d", len(pgschema.ComputeTables()))
	gone := "listed=0 marked=0 schema=0 compute=0 sources=0"

	// stopped before the last step: the sources and the tables of the computation are gone, the KG is closed.
	first := pgsrcKG(t, st, admin, 1, 2)
	st.StopDeleteAt(deleteStepLast, errKgdStop)
	if err := st.DeleteKG(ctx, first); !errors.Is(err, errKgdStop) {
		t.Fatalf("the delete that stops: %v", err)
	}
	if got := kgdState(t, admin, first, 1); got != "listed=1 marked=1 schema=1 compute=0 sources=0" {
		t.Errorf("after the stop before the last step: %s", got)
	}
	kgdClosed(t, st, first, "pgsrc1")
	if err := st.DeleteKG(ctx, first); err != nil {
		t.Fatalf("the delete called again: %v", err)
	}
	if got := kgdState(t, admin, first, 1); got != gone {
		t.Errorf("after the delete called again: %s", got)
	}
	if err := st.DeleteKG(ctx, first); kernel.CodeOf(err) != graph.CodeKGNotFound {
		t.Errorf("a KG that was deleted, deleted again: %v", err)
	}

	// stopped in the first transaction of the tables of the computation: a new server ends it when it starts.
	second := pgsrcKG(t, st, admin, 2, 1)
	st.StopDeleteAt(deleteStepCompute, errKgdStop)
	if err := st.DeleteKG(ctx, second); !errors.Is(err, errKgdStop) {
		t.Fatalf("the delete that stops: %v", err)
	}
	if got := kgdState(t, admin, second, 2); got != strings.Replace(all, "marked=0", "marked=1", 1)+" sources=0" {
		t.Errorf("after the stop in the tables of the computation: %s", got)
	}
	kgdClosed(t, st, second, "pgsrc2")
	// a server whose end of the delete fails too says so and still starts; the next one ends it.
	failing := kgdServer(t, dsn)
	failing.StopDeleteAt(deleteStepLast, errKgdStop)
	if resumed, err := failing.ResumeAllWork(ctx); !errors.Is(err, errKgdStop) || len(resumed) != 0 {
		t.Errorf("the server whose end of the delete fails ended %v: %v", resumed, err)
	}
	resumed, err := kgdServer(t, dsn).ResumeAllWork(ctx)
	if err != nil || len(resumed) != 1 || resumed[0] != second {
		t.Errorf("the server that starts ended %v: %v", resumed, err)
	}
	if got := kgdState(t, admin, second, 2); got != gone {
		t.Errorf("after the server started: %s", got)
	}
}

// [REQ-684]
func TestKgdADeleteThatStopsInItsFirstTransactionLeavesTheKGOpenAndTheDropOfTheSchemaAloneGoesByTheSameSteps(t *testing.T) {
	ctx := context.Background()
	st, admin, _ := kgdOpen(t)
	all := fmt.Sprintf("listed=1 marked=0 schema=1 compute=%d", len(pgschema.ComputeTables()))
	gone := "listed=0 marked=0 schema=0 compute=0 sources=0"
	// stopped in its first transaction: nothing happened and the KG is open.
	third := pgsrcKG(t, st, admin, 1, 1)
	st.StopDeleteAt(deleteStepMark, errKgdStop)
	if err := st.DeleteKG(ctx, third); !errors.Is(err, errKgdStop) {
		t.Fatalf("the delete that stops: %v", err)
	}
	if got := kgdState(t, admin, third, 1); got != all+" sources=1" {
		t.Errorf("after the stop in the first transaction: %s", got)
	}
	if _, err := st.Head(ctx, third); err != nil {
		t.Errorf("the KG whose delete did not begin: %v", err)
	}
	// the drop of the storage schema alone goes by the same steps and leaves the row of the list, unmarked, as before.
	st.StopDeleteAt(deleteStepSource, errKgdStop)
	if err := st.DropStorageSchema(ctx, third); !errors.Is(err, errKgdStop) {
		t.Fatalf("the drop that stops: %v", err)
	}
	if got := kgdState(t, admin, third, 1); got != all+" sources=1" {
		t.Errorf("after the drop stopped at the source: %s", got)
	}
	st.StopDeleteAt(deleteStepQuery, errKgdStop)
	if err := st.DropStorageSchema(ctx, third); !errors.Is(err, errKgdStop) {
		t.Fatalf("the drop that stops: %v", err)
	}
	if got := kgdState(t, admin, third, 1); got != "listed=1 marked=0 schema=1 compute=0 sources=0" {
		t.Errorf("after the drop stopped at the query schema: %s", got)
	}
	if err := st.DropStorageSchema(ctx, third); err != nil {
		t.Fatal(err)
	}
	if got := kgdState(t, admin, third, 1); got != "listed=1 marked=0 schema=0 compute=0 sources=0" {
		t.Errorf("after the drop called again: %s", got)
	}
	if err := st.DeleteKG(ctx, third); err != nil {
		t.Fatalf("deleting the KG whose schema was dropped: %v", err)
	}
	if got := kgdState(t, admin, third, 1); got != gone {
		t.Errorf("after the delete: %s", got)
	}
}

// [REQ-684]
func TestKgdANewKGWithTheNameOrTheIDOfOneBeingDeletedEndsThatDeleteFirst(t *testing.T) {
	ctx := context.Background()
	st, admin, _ := kgdOpen(t)
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	old := pgsrcKG(t, st, admin, 1, 1)
	st.StopDeleteAt(deleteStepLast, errKgdStop)
	if err := st.DeleteKG(ctx, old); !errors.Is(err, errKgdStop) {
		t.Fatalf("the delete that stops: %v", err)
	}
	byName := graph.KG{ID: sxID[graph.KGKind](t, "kg-00000000000000aa"), Name: "pgsrc1", Role: graph.RoleTest, DefinitionSetHash: reg.Hash(),
		CreatedAt: time.Unix(1000, 0)}
	// the delete it ends can fail too: then no KG is made.
	st.StopDeleteAt(deleteStepLast, errKgdStop)
	if _, err := st.CreateKG(ctx, byName); !errors.Is(err, errKgdStop) {
		t.Fatalf("a KG made while the delete before it fails: %v", err)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM kg_catalog WHERE kg_id = $1`, byName.ID.String()); n != 0 {
		t.Error("the KG was made though the delete before it failed")
	}
	made, err := st.CreateKG(ctx, byName)
	if err != nil {
		t.Fatalf("a KG of the name of one being deleted: %v", err)
	}
	if got := kgdState(t, admin, old, 1); got != "listed=0 marked=0 schema=0 compute=0 sources=0" {
		t.Errorf("the KG being deleted: %s", got)
	}
	if kg, err := st.GetKG(ctx, "pgsrc1"); err != nil || kg.ID != made.ID {
		t.Errorf("the name finds %v: %v", kg.ID, err)
	}
	// the same id: the KG that had it goes to the end, and the new one has a number of its own.
	st.StopDeleteAt(deleteStepCompute, errKgdStop)
	if err := st.DeleteKG(ctx, made.ID); !errors.Is(err, errKgdStop) {
		t.Fatalf("the delete that stops: %v", err)
	}
	again := byName
	again.Name = "another"
	if _, err := st.CreateKG(ctx, again); err != nil {
		t.Fatalf("a KG of the id of one being deleted: %v", err)
	}
	rec, err := st.StorageSchema(ctx, again.ID)
	if err != nil || rec.KGNo != 3 {
		t.Errorf("the new KG has the number %d: %v", rec.KGNo, err)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, pgschema.SchemaName(2)); n != 0 {
		t.Error("the schema of the KG that had the id is still there")
	}
}

// [REQ-683] [REQ-684]
func TestKgdTheQuerySchemaAndItsRolesGoInATransactionOfTheirOwnBeforeTheLastOne(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	kg := pgsrcKG(t, st, admin, 1, 0)
	token := sxText(t, admin, `SELECT token FROM install_token WHERE id = 1`)
	roles := strings.Join(pgschema.QueryRoleNames(token, 1), ",")
	names := strings.Join(pgschema.QuerySchemaNames(1), ",")
	count := func() string {
		return fmt.Sprintf("query=%d roles=%d storage=%d",
			sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = ANY (string_to_array($1, ','))`, names),
			sxInt(t, admin, `SELECT count(*) FROM pg_roles WHERE rolname = ANY (string_to_array($1, ','))`, roles),
			sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, pgschema.SchemaName(1)))
	}
	if got, want := count(), fmt.Sprintf("query=%d roles=%d storage=1", len(pgschema.QuerySchemaNames(1)), len(pgschema.QueryRoleNames(token, 1))); got != want || strings.HasPrefix(got, "query=0") {
		t.Fatalf("the KG has %s, want %s", got, want)
	}
	st.StopDeleteAt(deleteStepLast, errKgdStop)
	if err := st.DeleteKG(ctx, kg); !errors.Is(err, errKgdStop) {
		t.Fatalf("the delete that stops: %v", err)
	}
	if got := count(); got != "query=0 roles=0 storage=1" {
		t.Errorf("before the last step: %s", got)
	}
	if err := st.DeleteKG(ctx, kg); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != "query=0 roles=0 storage=0" {
		t.Errorf("after the delete: %s", got)
	}
}

// [REQ-684]
func TestKgdATableOfTheComputationThatARunIsWritingIsDroppedOnlyAfterTheTransactionOfTheRunEnds(t *testing.T) {
	ctx := context.Background()
	st, admin, _ := kgdOpen(t)
	kg := pgsrcKG(t, st, admin, 1, 1)
	table := pgschema.Qualified(pgschema.SchemaName(1), pgschema.TableCalcRecord)
	// a page of a run that writes a table of the computation holds this lock until its transaction ends.
	page, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = page.Rollback() }()
	if _, err := page.ExecContext(ctx, `LOCK TABLE `+table+` IN ROW EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- st.DeleteKG(ctx, kg) }()
	waiting := 0
	for i := 0; i < 3000 && waiting == 0; i++ {
		if waiting = sxInt(t, admin, `SELECT count(*) FROM pg_locks WHERE NOT granted AND relation = to_regclass($1)`, table); waiting == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if waiting == 0 {
		t.Fatal("the delete does not wait for the table the run is writing")
	}
	// the run went with its source before the tables of the computation, and the table is there while the run writes it.
	if got := kgdState(t, admin, kg, 1); got != fmt.Sprintf("listed=1 marked=1 schema=1 compute=%d sources=0", len(pgschema.ComputeTables())) {
		t.Errorf("while the run writes: %s", got)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM sync_run WHERE kg_id = $1`, kg.String()); n != 0 {
		t.Errorf("%d runs of the KG are left", n)
	}
	if err := page.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the delete after the run's transaction ended: %v", err)
	}
	if got := kgdState(t, admin, kg, 1); got != "listed=0 marked=0 schema=0 compute=0 sources=0" {
		t.Errorf("after the delete: %s", got)
	}
}
