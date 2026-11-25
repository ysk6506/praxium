package pgstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-785]
func TestRdfAKGOfGeneratorFourGetsTheColumnsAndTheOwnerTableOfTheComparisonWhenItIsAdjustedAndKeepsWhatItHas(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	m := sxStandard(t, true)
	kg := sxKG(t, 11)
	if _, err := st.CreateKG(ctx, graph.KG{ID: kg, Name: "four", Role: graph.RoleTest, DefinitionSetHash: m.Registry.Hash(), CreatedAt: time.Unix(1000, 0)}); err != nil {
		t.Fatal(err)
	}
	rec, err := st.StorageSchema(ctx, kg)
	if err != nil {
		t.Fatal(err)
	}
	schema := rec.Schema()
	// what generator 4 made: the tables without the columns of the comparison, and problems that must have a definition.
	sxRun(t, admin, fmt.Sprintf(`ALTER TABLE "%s".record_wait DROP COLUMN expired`, schema))
	sxRun(t, admin, fmt.Sprintf(`ALTER TABLE "%s".calc_record DROP COLUMN page, DROP COLUMN row_no`, schema))
	for _, table := range []string{pgschema.TableRecordProblem, pgschema.TableCalcProblem} {
		sxRun(t, admin, fmt.Sprintf(`ALTER TABLE "%s"."%s" ALTER COLUMN def_no SET NOT NULL`, schema, table))
	}
	sxRun(t, admin, fmt.Sprintf(`DROP INDEX "%s".stage_record_key`, schema))
	sxRun(t, admin, fmt.Sprintf(`DROP TABLE "%s".calc_owner`, schema))
	sxRun(t, admin, fmt.Sprintf(`INSERT INTO "%s".record_wait (src_no, dataset_no, key, kind, runs, first_at) VALUES (1, 1, 'k', 0, 2, now())`, schema))
	sxRun(t, admin, `UPDATE kg_schema_state SET generator_version = 4, manifest = jsonb_set(manifest, '{generator_version}', '4') WHERE kg_id = $1`, kg.String())
	got, err := st.AdjustStorageSchema(ctx, kg, m)
	if err != nil {
		t.Fatalf("the adjustment from generator 4: %v", err)
	}
	if got.State != "ok" || got.GeneratorVersion != pgschema.GeneratorVersion {
		t.Fatalf("the record after the adjustment: %+v", got)
	}
	has := func(table, column, nullable string) int {
		return sxInt(t, admin, `SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3 AND is_nullable = $4`, schema, table, column, nullable)
	}
	if has("record_wait", "expired", "NO") != 1 || has("calc_record", "page", "YES") != 1 || has("calc_record", "row_no", "YES") != 1 ||
		has("record_problem", "def_no", "YES") != 1 || has("calc_problem", "def_no", "YES") != 1 {
		t.Error("the adjusted schema lacks a column of the comparison")
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = 'stage_record_key'`, schema); n != 1 {
		t.Errorf("the adjusted schema has no index of the rows received by key: %d", n)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'calc_owner' AND column_name = 'run_no'`, schema); n != 1 {
		t.Errorf("the adjusted schema has no table that says whose the computation is: %d", n)
	}
	if n := sxInt(t, admin, fmt.Sprintf(`SELECT count(*) FROM "%s".record_wait WHERE key = 'k' AND runs = 2 AND NOT expired`, schema)); n != 1 {
		t.Errorf("the wait that was there is not kept as it was: %d", n)
	}
	if err := st.CheckStorageSchema(ctx, kg, m); err != nil {
		t.Errorf("the adjusted schema does not match the model: %v", err)
	}
	if _, err := st.AdjustStorageSchema(ctx, kg, m); err != nil {
		t.Errorf("a second adjustment: %v", err)
	}
}
