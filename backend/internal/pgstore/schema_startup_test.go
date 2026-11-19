package pgstore

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// ssuOldKG makes a KG with the standard model and turns it into one that the generator before made.
func ssuOldKG(t *testing.T, st *Store, admin *sql.DB, n int) (graph.KGID, string) {
	t.Helper()
	m := sxStandard(t, true)
	kg := sxKG(t, n)
	if _, err := st.CreateKG(context.Background(), graph.KG{ID: kg, Name: "old" + strconv.Itoa(n), Role: graph.RoleTest, DefinitionSetHash: m.Registry.Hash(),
		CreatedAt: time.Unix(1000, 0)}); err != nil {
		t.Fatal(err)
	}
	rec, err := st.StorageSchema(context.Background(), kg)
	if err != nil {
		t.Fatal(err)
	}
	igtStripIngest(t, admin, rec.Schema(), kg)
	return kg, rec.Schema()
}

func ssuGenerator(t *testing.T, st *Store, kg graph.KGID) int {
	t.Helper()
	rec, err := st.StorageSchema(context.Background(), kg)
	if err != nil {
		t.Fatal(err)
	}
	return rec.GeneratorVersion
}

// [REQ-685]
func TestSchemaStartupAdjustsAKGOfTheGeneratorBeforeAndOpensIt(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	kg, _ := ssuOldKG(t, st, admin, 21)
	m := sxStandard(t, true)
	if err := st.CheckStorageSchema(ctx, kg, m); kernel.CodeOf(err) != CodeSchemaMismatch {
		t.Fatalf("the KG of the generator before matches: %v", err)
	}
	if _, err := st.Head(ctx, kg); err != nil {
		t.Fatal(err)
	}
	done, err := st.AdjustOldSchemas(ctx)
	if err != nil || len(done) != 1 || !done[0].Adjusted || done[0].KG != kg {
		t.Fatalf("the adjustment at the start: %+v, %v", done, err)
	}
	if got := ssuGenerator(t, st, kg); got != pgschema.GeneratorVersion {
		t.Errorf("the generator is %d after the start", got)
	}
	if err := st.CheckStorageSchema(ctx, kg, m); err != nil {
		t.Errorf("the adjusted KG is still refused: %v", err)
	}
	if _, err := st.Head(ctx, kg); err != nil {
		t.Errorf("the adjusted KG does not open: %v", err)
	}
	again, err := st.AdjustOldSchemas(ctx)
	if err != nil || len(again) != 0 {
		t.Errorf("a second start has something to adjust: %+v, %v", again, err)
	}
}

// [REQ-685]
func TestSchemaStartupLeavesAKGThatDiffersInMoreThanTheGeneratorRefused(t *testing.T) {
	ctx := context.Background()
	m := sxStandard(t, true)
	for name, change := range map[string]string{
		"the model differs":              `UPDATE kg_schema_state SET registry_hash = 'other' WHERE kg_id = $1`,
		"the difference is not additive": `UPDATE kg_schema_state SET manifest = jsonb_set(manifest, '{kg_no}', '777') WHERE kg_id = $1`,
	} {
		st, admin := igtOpenStandard(t)
		kg, schema := ssuOldKG(t, st, admin, 31)
		sxRun(t, admin, change, kg.String())
		done, err := st.AdjustOldSchemas(ctx)
		if err != nil || len(done) != 1 || done[0].Adjusted || done[0].Reason == "" {
			t.Fatalf("%s: %+v, %v", name, done, err)
		}
		if got := ssuGenerator(t, st, kg); got != 2 {
			t.Errorf("%s: the generator became %d", name, got)
		}
		if sxInt(t, admin, `SELECT count(*) FROM pg_tables WHERE schemaname = $1 AND tablename = $2`, schema, pgschema.TableRawRecord) != 0 {
			t.Errorf("%s: a table was made", name)
		}
		if err := st.CheckStorageSchema(ctx, kg, m); kernel.CodeOf(err) != CodeSchemaMismatch {
			t.Errorf("%s: the KG is not refused as before: %v", name, err)
		}
	}
}

// [REQ-685]
func TestSchemaStartupSkipsAKGThatAnotherRunIsBuilding(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	kg, _ := ssuOldKG(t, st, admin, 41)
	sxRun(t, admin, `UPDATE kg_schema_state SET state = 'building', online_since = now() WHERE kg_id = $1`, kg.String())
	done, err := st.AdjustOldSchemas(ctx)
	if err != nil || len(done) != 1 || done[0].Adjusted || !strings.Contains(done[0].Reason, "another run") {
		t.Fatalf("a KG that another run builds: %+v, %v", done, err)
	}
	if got := ssuGenerator(t, st, kg); got != 2 {
		t.Errorf("the generator became %d under the other run", got)
	}
	sxRun(t, admin, `UPDATE kg_schema_state SET online_since = now() - interval '2 hours' WHERE kg_id = $1`, kg.String())
	if done, err = st.AdjustOldSchemas(ctx); err != nil || len(done) != 1 || !done[0].Adjusted {
		t.Errorf("a run that said nothing for hours is not taken over: %+v, %v", done, err)
	}
}

// [REQ-685]
func TestSchemaStartupReportsAFailureAndLeavesANewerKGAlone(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	kg, schema := ssuOldKG(t, st, admin, 51)
	good, _ := ssuOldKG(t, st, admin, 53)
	newer, _ := ssuOldKG(t, st, admin, 52)
	sxRun(t, admin, `UPDATE kg_schema_state SET generator_version = 99 WHERE kg_id = $1`, newer.String())
	// a table of the same name that the plan makes stands in the way of the adjustment of the first KG
	sxRun(t, admin, `CREATE TABLE "`+schema+`"."`+pgschema.TableRawRecord+`" (x int)`)
	done, err := st.AdjustOldSchemas(ctx)
	if err == nil || !strings.Contains(err.Error(), kg.String()) {
		t.Fatalf("a failure to adjust: %+v, %v", done, err)
	}
	if len(done) != 1 || done[0].KG != good || !done[0].Adjusted {
		t.Errorf("the failure of one KG stopped the others: %+v", done)
	}
	if got := ssuGenerator(t, st, kg); got != 2 {
		t.Errorf("the generator of the failed KG is %d", got)
	}
	if got := ssuGenerator(t, st, newer); got != 99 {
		t.Errorf("a KG of a newer generator was touched: %d", got)
	}
}
