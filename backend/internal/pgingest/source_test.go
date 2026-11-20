package pgingest

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// pgiPartitions is how many partitions of the tables of the sources the KG has for a source.
func pgiPartitions(t *testing.T, e *pgiEnv, ref SourceRef) int {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname LIKE '%\_p' || $2::text`, pgschema.SchemaName(ref.KGNo), ref.SrcNo)
}

// [REQ-703]
func TestPgiASourcePutToAKGHasItsRowsAndItsPartitionsAndPuttingItAgainChangesNothingButTheDatasetsItAdds(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	if ref.KGNo != 1 || ref.SrcNo == 0 || len(ref.Datasets) != 3 || pgiPartitions(t, e, ref) != len(pgschema.SourceTables()) {
		t.Fatalf("%+v with %d partitions", ref, pgiPartitions(t, e, ref))
	}
	if got := e.text(t, `SELECT full_receive || ' ' || full_compute FROM source_state WHERE src_no = $1`, ref.SrcNo); got != "true true" {
		t.Fatalf("a source that has had no run is new, both marks are set: %s", got)
	}
	again, err := e.st.AttachSource(e.ctx, kg.ID, plan.Source.ID, append(pgiDatasets(plan), "ds_more"))
	if err != nil || again.SrcNo != ref.SrcNo || len(again.Datasets) != 4 || again.Datasets["ds_places"] != ref.Datasets["ds_places"] {
		t.Fatalf("%+v %v", again, err)
	}
	if n := pgiPartitions(t, e, ref); n != len(pgschema.SourceTables()) {
		t.Errorf("%d partitions", n)
	}
	// a partition that was made and not attached (a call that was cut off between its two statements) is attached by the next call.
	e.exec(t, fmt.Sprintf(`ALTER TABLE "s_1".stage_claim DETACH PARTITION "s_1".stage_claim_p%d`, ref.SrcNo))
	if n := pgiPartitions(t, e, ref); n != len(pgschema.SourceTables())-1 {
		t.Fatalf("%d partitions", n)
	}
	e.attach(t, kg, plan)
	if n := pgiPartitions(t, e, ref); n != len(pgschema.SourceTables()) {
		t.Errorf("%d partitions", n)
	}
	other, err := e.st.AttachSource(e.ctx, kg.ID, "src_two", []string{"ds_a"})
	if err != nil || other.SrcNo == ref.SrcNo || other.Datasets["ds_a"] == ref.Datasets["ds_places"] {
		t.Fatalf("a second source has numbers of its own: %+v %v", other, err)
	}
	for name, fn := range map[string]func() error{
		"no datasets": func() error { _, err := e.st.AttachSource(e.ctx, kg.ID, "src_x", nil); return err },
		"no source":   func() error { _, err := e.st.AttachSource(e.ctx, kg.ID, "", []string{"d"}); return err },
	} {
		if err := fn(); !pgiCodeIs(err, kernel.CodeInvalidValue) {
			t.Errorf("%s: %v", name, err)
		}
	}
	unknown, _ := kernel.ParseID[graph.KGKind]("kg-00000000000000ff")
	if _, err := e.st.AttachSource(e.ctx, unknown, "src_x", []string{"d"}); !pgiCodeIs(err, graph.CodeKGNotFound) {
		t.Errorf("a KG that is not there: %v", err)
	}
}

// [REQ-703]
func TestPgiTakingASourceOffDropsItsPartitionsAndRowsInChunksAndRefusesASourceWithARunThatIsNotOver(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	ref := e.attach(t, kg, plan)
	keep, err := e.st.AttachSource(e.ctx, kg.ID, "src_two", []string{"ds_a"})
	if err != nil {
		t.Fatal(err)
	}
	// rows of the runs of the two sources in the tables that are not partitioned: more than a chunk of one source.
	q := func(table string) string { return pgschema.TableRef(ref.KGNo, table) }
	e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, kind, runs, first_at) SELECT $1, 1, 'k' || g, 0, 1, now() FROM generate_series(1, %d) g`, q(pgschema.TableRecordWait), chunkRows+5), ref.SrcNo)
	e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, kind, runs, first_at) VALUES ($1, 1, 'keep', 0, 1, now())`, q(pgschema.TableRecordWait)), keep.SrcNo)
	e.exec(t, fmt.Sprintf(`INSERT INTO %s (src_no, dataset_no, key, def_no, field, kind, reason) VALUES ($1, 1, 'k', 1, 'f', 'cell_error', 'r')`, q(pgschema.TableRecordProblem)), ref.SrcNo)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	if err := e.st.DetachSource(e.ctx, kg.ID, plan.Source.ID); !pgiCodeIs(err, ingest.CodeRunOpen) {
		t.Fatalf("a source with a run that is not over: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM `+q(pgschema.TableRecordWait)+` WHERE src_no = $1`, ref.SrcNo); n != chunkRows+5 {
		t.Fatalf("the refused call deleted rows: %d left", n)
	}
	if err := e.st.EndRun(e.ctx, lease, ingest.RunEnd{Stage: ingest.RunCanceled}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.DetachSource(e.ctx, kg.ID, plan.Source.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM `+q(pgschema.TableRecordWait)+` WHERE src_no = $1`, ref.SrcNo); n != 0 {
		t.Errorf("%d rows of the source stayed", n)
	}
	if n := e.count(t, `SELECT count(*) FROM `+q(pgschema.TableRecordWait)+` WHERE src_no = $1`, keep.SrcNo); n != 1 {
		t.Errorf("the rows of the other source: %d", n)
	}
	if n := pgiPartitions(t, e, ref); n != 0 {
		t.Errorf("%d partitions stayed", n)
	}
	if n := pgiPartitions(t, e, keep); n != len(pgschema.SourceTables()) {
		t.Errorf("the other source has %d partitions", n)
	}
	for _, table := range []string{"source_state", "source_dataset", "sync_run"} {
		if n := e.count(t, `SELECT count(*) FROM `+table+` WHERE src_no = $1`, ref.SrcNo); n != 0 {
			t.Errorf("%s still has %d rows of the source", table, n)
		}
	}
	if err := e.st.DetachSource(e.ctx, kg.ID, plan.Source.ID); !pgiCodeIs(err, kernel.CodeNotFound) {
		t.Errorf("a source that is not put to the KG: %v", err)
	}
	// putting it again gives it new numbers.
	again := e.attach(t, kg, plan)
	if again.SrcNo <= keep.SrcNo {
		t.Errorf("the number of a source is not given again: %d", again.SrcNo)
	}
}

// pgiLocks is the number of locks the connection of a transaction holds.
func pgiLocks(t *testing.T, tx *sql.Tx) int {
	t.Helper()
	var n int
	if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid()`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// [REQ-683]
func TestPgiPuttingASourceToAKGAndTakingItOffHoldFewLocksAndDeletingAKGOfManySourcesFitsTheDefaultLockTable(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	var perTx int
	if err := e.admin.QueryRowContext(e.ctx, `SELECT current_setting('max_locks_per_transaction')::int`).Scan(&perTx); err != nil {
		t.Fatal(err)
	}
	measure := func(fn func(tx *sql.Tx) error) int {
		tx, err := e.admin.BeginTx(e.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := fn(tx); err != nil {
			t.Fatal(err)
		}
		return pgiLocks(t, tx)
	}
	var ref SourceRef
	put := measure(func(tx *sql.Tx) (err error) {
		ref, err = attachTx(e.ctx, tx, kg.ID, plan.Source.ID, pgiDatasets(plan))
		return err
	})
	// the lock table is shared by all connections (max_locks_per_transaction times the connections), so what matters is that
	// one source costs a bounded number, not a number that grows with the tables of the KG.
	if put > 100 {
		t.Errorf("putting a source to a KG holds %d locks, more than 100", put)
	}
	// the number of the source of the rolled back call is not in use, so the source is put again and that one is taken off.
	ref = e.attach(t, kg, plan)
	off := measure(func(tx *sql.Tx) error { return dropTx(e.ctx, tx, ref.KGNo, ref.SrcNo) })
	// the index of the rows received by key (ADR-0102) is one more relation to drop
	if off > 110 {
		t.Errorf("taking a source off a KG holds %d locks, more than 110", off)
	}
	t.Logf("putting a source to a KG: %d locks; taking it off: %d (max_locks_per_transaction is %d here)", put, off, perTx)
	// a KG of 30 sources, more than the 26 or so that one transaction can drop at the default of 64: deleting it takes the
	// sources off one by one, a transaction each, and then drops the schema.
	for i := 2; i <= 30; i++ {
		if _, err := e.st.AttachSource(e.ctx, kg.ID, fmt.Sprintf("src_%d", i), []string{"ds_a"}); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM source_state WHERE kg_id = $1`, kg.ID.String()); n != 30 {
		t.Fatalf("%d sources", n)
	}
	drop := measure(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `DROP SCHEMA "s_1" CASCADE`)
		return err
	})
	t.Logf("dropping the schema of a KG that still has 30 sources holds %d locks (the lock table of this server has %d for each of its connections)", drop, perTx)
	if err := e.pst.DeleteKG(e.ctx, kg.ID); err != nil {
		t.Fatalf("deleting a KG of 30 sources: %v", err)
	}
	for _, table := range []string{"source_state", "source_dataset", "sync_run"} {
		if n := e.count(t, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s has %d rows of a KG that was deleted", table, n)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM pg_namespace WHERE nspname = 's_1'`); n != 0 {
		t.Error("the schema of the KG is still there")
	}
}

// [REQ-684]
func TestPgiDeletingAKGTakesItsSourcesOffFirstWithTheirRunsAndAKGWithNoSourcesIsDeletedAsBefore(t *testing.T) {
	e := pgiOpen(t)
	plan := pgiPlan(t, pgiSourceJSON, pgiMappingJSON)
	kg := e.kg(t, 1)
	bare := e.kg(t, 2)
	ref := e.attach(t, kg, plan)
	lease := e.open(t, kg, plan.Source.ID, "server-a")
	if _, err := e.st.Receive(e.ctx, lease, ingest.Intake{Reader: pgiAll(), Plan: plan}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM `+pgiTable(ref, pgschema.TableStageRecord)); n != 7 {
		t.Fatalf("%d rows are staged", n)
	}
	// a run that is not over does not keep a KG from being deleted: the KG is going.
	if err := e.pst.DeleteKG(e.ctx, kg.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM sync_run`) + e.count(t, `SELECT count(*) FROM source_state`); n != 0 {
		t.Errorf("%d rows of the runs and sources of the KG stayed", n)
	}
	if err := e.pst.DeleteKG(e.ctx, bare.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.pst.DeleteKG(e.ctx, bare.ID); !pgiCodeIs(err, graph.CodeKGNotFound) {
		t.Errorf("a KG that is not there: %v", err)
	}
	// the drop of the storage schema alone takes the sources off too.
	again := e.kg(t, 3)
	e.attach(t, again, plan)
	if err := e.pst.DropStorageSchema(e.ctx, again.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM source_state`); n != 0 {
		t.Errorf("%d sources stayed", n)
	}
	_ = time.Second
}
