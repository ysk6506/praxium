package pgschema_test

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the control tables of a KG's storage schema (ADR-0089). Their names start with Control so that
// they never meet a name of another test file of this folder.

func controlPlan(t *testing.T) pgschema.Plan {
	t.Helper()
	plan, err := pgschema.Build(registry(t, smallDefs(t)), pgschema.Options{KGNo: 3})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestControlTablesAreInTheStorageSchemaOfTheKG(t *testing.T) {
	plan := controlPlan(t)
	got := strings.Join(plan.Tables(), ",")
	for _, name := range []string{pgschema.TableHead, pgschema.TableLease, pgschema.TableGenerationEvent, pgschema.TableVersion, pgschema.TableVersionHashPart} {
		if !strings.Contains(","+got+",", ","+name+",") {
			t.Fatalf("no table %s in %s", name, got)
		}
		if !strings.Contains(stepSQL(plan, name), `"s_3"."`+name+`"`) {
			t.Fatalf("the table %s is not made in the schema of the KG: %s", name, stepSQL(plan, name))
		}
	}
	head := stepSQL(plan, pgschema.TableHead)
	for _, col := range []string{"applied_seq", "readable_through", "generation", "history_floor", "work_seq", "rollback", "hash_sum"} {
		if !strings.Contains(head, col) {
			t.Fatalf("the head has no column %s", col)
		}
	}
	if !strings.Contains(head, "fillfactor = 50") {
		t.Fatalf("the head leaves no room in its pages: %s", head)
	}
}

func TestControlAppendOnlyTablesLeadTheirKeyWithTheChangeNumber(t *testing.T) {
	plan := controlPlan(t)
	cases := map[string]string{
		pgschema.TableObjectHist:  "PRIMARY KEY (seq_to, obj_no, seq_from)",
		pgschema.TableLinkHist:    "PRIMARY KEY (seq_to, link_no, seq_from)",
		pgschema.TableChangeEntry: "PRIMARY KEY (change_seq,",
		pgschema.TableLightChange: "PRIMARY KEY (change_seq,",
	}
	for table, key := range cases {
		if !strings.Contains(stepSQL(plan, table), key) {
			t.Fatalf("%s: no %q in %s", table, key, stepSQL(plan, table))
		}
		if strings.Contains(stepSQL(plan, table), "REFERENCES") {
			t.Fatalf("%s points at another table with a foreign key", table)
		}
	}
}

func TestControlReservesThePartitionPrefix(t *testing.T) {
	if !pgschema.IsReservedName(pgschema.ReservedPrefixPartition + "object_hist_1") {
		t.Fatal("the prefix of the partitions is not reserved")
	}
	if pgschema.GeneratorVersion != 12 {
		t.Fatalf("the generator version is %d", pgschema.GeneratorVersion)
	}
}
