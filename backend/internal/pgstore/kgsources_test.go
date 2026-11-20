package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The test of the deletion of a KG that has sources put to it. The helpers start with pgsrc.

// pgsrcKG makes a KG of the standard model with the number its id says and puts sources to it with SQL.
func pgsrcKG(t *testing.T, st *Store, admin *sql.DB, n, sources int) graph.KGID {
	t.Helper()
	ctx := context.Background()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	id, err := kernel.ParseID[graph.KGKind](fmt.Sprintf("kg-%016x", n))
	if err != nil {
		t.Fatal(err)
	}
	kg, err := st.CreateKG(ctx, graph.KG{ID: id, Name: fmt.Sprintf("pgsrc%d", n), Role: graph.RoleTest, DefinitionSetHash: reg.Hash(), CreatedAt: time.Unix(1000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= sources; i++ {
		src := igrSource(t, admin, kg.ID.String(), fmt.Sprintf("src_%d", i))
		steps, err := pgschema.SourcePartitionSteps(int64(n), int64(src))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range steps {
			if _, err := admin.ExecContext(ctx, s.SQL); err != nil {
				t.Fatalf("%s: %v", s.Name, err)
			}
		}
		if err := igrRun(admin, kg.ID.String(), src, pgschema.RunReceiving); err != nil {
			t.Fatal(err)
		}
	}
	return kg.ID
}

// [REQ-684]
func TestPgsrcDeletingAKGOrDroppingItsSchemaTakesTheSourcesOffFirstEvenWithARunThatIsNotOver(t *testing.T) {
	ctx := context.Background()
	st, admin := sxOpen(t)
	first := pgsrcKG(t, st, admin, 1, 3)
	second := pgsrcKG(t, st, admin, 2, 2)
	if n := sxInt(t, admin, `SELECT count(*) FROM source_state`); n != 5 {
		t.Fatalf("%d sources", n)
	}
	if err := st.DeleteKG(ctx, first); err != nil {
		t.Fatal(err)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM source_state WHERE kg_id = $1`, first.String()); n != 0 {
		t.Errorf("%d sources of the deleted KG stayed", n)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM sync_run WHERE kg_id = $1`, first.String()); n != 0 {
		t.Errorf("%d runs of the deleted KG stayed", n)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM source_state WHERE kg_id = $1`, second.String()); n != 2 {
		t.Errorf("the other KG has %d sources", n)
	}
	if err := st.DropStorageSchema(ctx, second); err != nil {
		t.Fatal(err)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM source_state`); n != 0 {
		t.Errorf("%d sources stayed after the schema was dropped", n)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname IN ($1, $2)`, pgschema.SchemaName(1), pgschema.SchemaName(2)); n != 0 {
		t.Errorf("%d schemas stayed", n)
	}
}
