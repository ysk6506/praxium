package pgstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// [REQ-880]
func TestIdnAKGOfGeneratorSixGetsTheHeldColumnsTheFeedOfABindTheWorkingTablesAndTheIndexesOfTheIdentificationWhenItIsAdjustedAndKeepsWhatItHas(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	m := sxStandard(t, true)
	kg := sxKG(t, 14)
	if _, err := st.CreateKG(ctx, graph.KG{ID: kg, Name: "six", Role: graph.RoleTest, DefinitionSetHash: m.Registry.Hash(), CreatedAt: time.Unix(1000, 0)}); err != nil {
		t.Fatal(err)
	}
	rec, err := st.StorageSchema(ctx, kg)
	if err != nil {
		t.Fatal(err)
	}
	schema := rec.Schema()
	src := igrSource(t, admin, kg.String(), "src_six")
	steps, err := pgschema.SourcePartitionSteps(rec.KGNo, int64(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		sxRun(t, admin, s.SQL)
	}
	// what generator 6 made: no column held in the computation, no feed in a bind, none of the four working tables, none of the indexes.
	for _, table := range []string{pgschema.TableCalcKey, pgschema.TableCalcEnd, pgschema.TableCalcClaim} {
		sxRun(t, admin, fmt.Sprintf(`ALTER TABLE "%s"."%s" DROP COLUMN held`, schema, table))
	}
	sxRun(t, admin, fmt.Sprintf(`ALTER TABLE "%s".calc_bind DROP COLUMN feed_no`, schema))
	for _, table := range []string{"calc_slot", "calc_label", "calc_klabel", "calc_ask", "calc_newobj"} {
		sxRun(t, admin, fmt.Sprintf(`DROP TABLE "%s"."%s"`, schema, table))
	}
	// (the index of a bind by its feed went with the column that was dropped)
	for _, index := range []string{"stage_key_page", "stage_end_page", "stage_claim_page", "calc_record_row", "calc_key_rid", "calc_end_rid", "calc_claim_rid",
		"calc_claim_value", "calc_bind_pair", "calc_newkey_key", "calc_ref_pair"} {
		sxRun(t, admin, fmt.Sprintf(`DROP INDEX IF EXISTS "%s"."%s"`, schema, index))
	}
	sxRun(t, admin, fmt.Sprintf(`INSERT INTO "%s".calc_bind (rid, key_ord, obj_no, is_new, round_no) VALUES (1, 0, 5, true, 1)`, schema))
	sxRun(t, admin, fmt.Sprintf(`INSERT INTO "%s".calc_key (rid, feed_no, key_ord, type_no, sig_hash) VALUES (1, 0, 0, 1, '\x01')`, schema))
	sxRun(t, admin, `UPDATE kg_schema_state SET generator_version = 6, manifest = jsonb_set(manifest, '{generator_version}', '6') WHERE kg_id = $1`, kg.String())
	got, err := st.AdjustStorageSchema(ctx, kg, m)
	if err != nil {
		t.Fatalf("the adjustment from generator 6: %v", err)
	}
	if got.State != "ok" || got.GeneratorVersion != pgschema.GeneratorVersion {
		t.Fatalf("the record after the adjustment: %+v", got)
	}
	for _, table := range []string{pgschema.TableCalcKey, pgschema.TableCalcEnd, pgschema.TableCalcClaim} {
		if n := sxInt(t, admin, `SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = 'held' AND is_nullable = 'NO'`, schema, table); n != 1 {
			t.Errorf("%s has no column held that is not null", table)
		}
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'calc_bind' AND column_name = 'feed_no' AND is_nullable = 'NO'`, schema); n != 1 {
		t.Error("calc_bind has no feed that is not null")
	}
	for _, table := range []string{"calc_slot", "calc_label", "calc_klabel", "calc_ask", "calc_newobj"} {
		if n := sxInt(t, admin, `SELECT count(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2`, schema, table); n != 1 {
			t.Errorf("no table %s", table)
		}
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'calc_slot' AND column_name = 'src_no'`, schema); n != 1 {
		t.Error("a slot does not say the source of its record")
	}
	for _, index := range []string{"stage_key_page", "stage_end_page", "stage_claim_page", "calc_record_row", "calc_key_rid", "calc_end_rid", "calc_claim_rid",
		"calc_claim_value", "calc_bind_pair", "calc_newkey_key", "calc_ref_pair", "calc_slot_pid", "calc_slot_key", "calc_label_pid", "calc_klabel_key", "calc_label_label", "calc_newobj_ref"} {
		if n := sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = $2`, schema, index); n != 1 {
			t.Errorf("no index %s", index)
		}
	}
	// the rows that were there stay, with the defaults of the columns that were added
	if n := sxInt(t, admin, fmt.Sprintf(`SELECT count(*) FROM "%s".calc_bind WHERE rid = 1 AND obj_no = 5 AND feed_no = 0`, schema)); n != 1 {
		t.Errorf("the bind that was there is not kept with feed 0: %d", n)
	}
	if n := sxInt(t, admin, fmt.Sprintf(`SELECT count(*) FROM "%s".calc_key WHERE rid = 1 AND NOT held`, schema)); n != 1 {
		t.Errorf("the key that was there is not kept as not held: %d", n)
	}
	if err := st.CheckStorageSchema(ctx, kg, m); err != nil {
		t.Errorf("the adjusted schema does not match the model: %v", err)
	}
	if _, err := st.AdjustStorageSchema(ctx, kg, m); err != nil {
		t.Errorf("a second adjustment: %v", err)
	}
}
