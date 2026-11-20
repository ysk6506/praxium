package pgstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-680]
func TestHeldAKGOfGeneratorThreeGetsTheColumnHeldOnItsTablesAndItsSourcePartitionsWhenItIsAdjusted(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	m := sxStandard(t, true)
	kg := sxKG(t, 9)
	if _, err := st.CreateKG(ctx, graph.KG{ID: kg, Name: "three", Role: graph.RoleTest, DefinitionSetHash: m.Registry.Hash(), CreatedAt: time.Unix(1000, 0)}); err != nil {
		t.Fatal(err)
	}
	rec, err := st.StorageSchema(ctx, kg)
	if err != nil {
		t.Fatal(err)
	}
	schema := rec.Schema()
	src := igrSource(t, admin, kg.String(), "src_one")
	steps, err := pgschema.SourcePartitionSteps(rec.KGNo, int64(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		sxRun(t, admin, s.SQL)
	}
	sxRun(t, admin, fmt.Sprintf(`INSERT INTO "%s"."stage_key_p%d" (src_no, dataset_no, page, row_no, feed_no, key_ord, type_no, sig_hash) VALUES (%d, 1, 0, 0, 1, 0, 1, '\x01')`, schema, src, src))
	// what generator 3 made: the three tables without the column held.
	for _, table := range []string{pgschema.TableStageKey, pgschema.TableStageEnd, pgschema.TableStageClaim} {
		sxRun(t, admin, fmt.Sprintf(`ALTER TABLE "%s"."%s" DROP COLUMN held`, schema, table))
	}
	sxRun(t, admin, `UPDATE kg_schema_state SET generator_version = 3, manifest = jsonb_set(manifest, '{generator_version}', '3') WHERE kg_id = $1`, kg.String())
	got, err := st.AdjustStorageSchema(ctx, kg, m)
	if err != nil {
		t.Fatalf("the adjustment from generator 3: %v", err)
	}
	if got.State != "ok" || got.GeneratorVersion != pgschema.GeneratorVersion {
		t.Fatalf("the record after the adjustment: %+v", got)
	}
	for _, table := range []string{pgschema.TableStageKey, pgschema.TableStageEnd, pgschema.TableStageClaim} {
		for _, name := range []string{table, fmt.Sprintf("%s_p%d", table, src)} {
			n := sxInt(t, admin, `SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = 'held' AND is_nullable = 'NO'`, schema, name)
			if n != 1 {
				t.Errorf("%s has no column held that is not null", name)
			}
		}
	}
	if n := sxInt(t, admin, fmt.Sprintf(`SELECT count(*) FROM "%s"."stage_key_p%d" WHERE NOT held`, schema, src)); n != 1 {
		t.Errorf("the row that was there is not held=false: %d", n)
	}
	if err := st.CheckStorageSchema(ctx, kg, m); err != nil {
		t.Errorf("the adjusted schema does not match the model: %v", err)
	}
	// adjusting again changes nothing.
	if _, err := st.AdjustStorageSchema(ctx, kg, m); err != nil {
		t.Errorf("a second adjustment: %v", err)
	}
}
