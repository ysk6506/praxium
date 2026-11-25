package pgstore

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the query schema (ADR-0113). Their names start with Qsc.

var qscGrades = []model.Sensitivity{model.SensitivityOpen, model.SensitivityRestricted, model.SensitivityConfidential}

// qscSeen says which attributes (by name) a grade may see.
var qscSeen = map[model.Sensitivity]map[string]bool{
	model.SensitivityOpen:         {"serial": true, "temp": true, "bay": true, "fan": true},
	model.SensitivityRestricted:   {"serial": true, "temp": true, "bay": true, "fan": true, "cpus": true, "org.rack_owner": true},
	model.SensitivityConfidential: {"serial": true, "temp": true, "bay": true, "fan": true, "cpus": true, "boot": true, "coolant": true, "org.rack_owner": true},
}

var qscHiddenNames = []string{"cpus", "boot", "coolant", "org.rack_owner"}

// [REQ-573] [REQ-626]
func TestQscAGradeRoleSeesNoColumnOfAnAttributeOfAHigherGrade(t *testing.T) {
	w := qscOpen(t)
	for _, grade := range qscGrades {
		w.as(t, grade, false, func(tx *sql.Tx) {
			for _, view := range []string{"machine", "server"} {
				cols := qscColumns(t, tx, "SELECT * FROM "+view+" LIMIT 0")
				for name := range map[string]bool{"serial": true, "cpus": true, "boot": true, "temp": true, "coolant": true, "org.rack_owner": true} {
					if view == "server" && name == "bay" {
						continue
					}
					if got, want := qscHas(cols, name), qscSeen[grade][name]; got != want {
						t.Errorf("%s sees the column %s of %s: %v, want %v (%v)", grade, name, view, got, want, cols)
					}
				}
			}
			if cols := qscColumns(t, tx, "SELECT * FROM server LIMIT 0"); !qscHas(cols, "bay") || !qscHas(cols, "serial") {
				t.Errorf("%s: a server has the columns of the machine and its own: %v", grade, cols)
			}
		})
	}
}

// [REQ-626]
func TestQscEverySurfaceOfAGradeIsCutByTheSameSetOfAttributeNumbers(t *testing.T) {
	w := qscOpen(t)
	for _, grade := range qscGrades {
		w.as(t, grade, false, func(tx *sql.Tx) {
			surfaces := map[string][]string{
				"provenance":       qscStrings(t, tx, `SELECT jsonb_object_keys(provenance) FROM machine`),
				"attribute":        qscStrings(t, tx, `SELECT name FROM attribute`),
				"model_attribute":  qscStrings(t, tx, `SELECT name FROM model_attribute`),
				"model_type_attrs": qscStrings(t, tx, `SELECT name FROM model_attribute WHERE type = 'machine'`),
			}
			for surface, names := range surfaces {
				if len(names) == 0 {
					t.Errorf("%s: %s is empty", grade, surface)
				}
				for _, hidden := range qscHiddenNames {
					if qscHas(names, hidden) && !qscSeen[grade][hidden] {
						t.Errorf("%s: %s shows %s: %v", grade, surface, hidden, names)
					}
				}
			}
			for _, name := range []string{"cpus", "boot", "coolant"} {
				if qscSeen[grade][name] && !qscHas(surfaces["attribute"], name) {
					t.Errorf("%s: the view of the attributes lacks %s: %v", grade, name, surfaces["attribute"])
				}
			}
		})
	}
}

// [REQ-573]
func TestQscAGradeRoleHasNoRightOnAnotherKGOnTheStorageSchemaOrOnAHigherGrade(t *testing.T) {
	w := qscOpen(t)
	if _, err := w.st.CreateStorageSchema(context.Background(), sxKG(t, 2), qscModel(t)); err != nil {
		t.Fatal(err)
	}
	other := w.no + 1
	denied := []string{
		"SELECT 1 FROM " + pgschema.QuoteIdent(pgschema.QuerySchemaName(other, model.SensitivityOpen, false)) + ".machine",
		"SELECT 1 FROM " + pgschema.Qualified(w.schema, pgschema.TableObject),
		"SELECT 1 FROM " + pgschema.Qualified(w.schema, pgschema.TypeTable(1)),
		"SELECT 1 FROM " + pgschema.QuoteIdent(pgschema.QuerySchemaName(w.no, model.SensitivityConfidential, false)) + ".machine",
	}
	for _, q := range denied {
		w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
			_, err := tx.Exec(q)
			if pe, ok := err.(*pq.Error); !ok || pe.Code != "42501" {
				t.Errorf("%s: %v (want permission denied)", q, err)
			}
		})
	}
	w.as(t, model.SensitivityConfidential, false, func(tx *sql.Tx) {
		if _, err := tx.Exec("SELECT 1 FROM " + pgschema.QuoteIdent(pgschema.QuerySchemaName(w.no, model.SensitivityOpen, false)) + ".machine"); err != nil {
			t.Errorf("a grade cannot read the schema of a lower grade: %v", err)
		}
	})
	w.as(t, model.SensitivityConfidential, false, func(tx *sql.Tx) {
		if _, err := tx.Exec("SELECT 1 FROM " + pgschema.Qualified(w.schema, pgschema.TableObject)); err == nil {
			t.Error("the highest grade reads the storage schema")
		}
	})
}

// [REQ-573]
func TestQscEveryWriteOfAGradeRoleIsRefused(t *testing.T) {
	w := qscOpen(t)
	open := pgschema.QuoteIdent(pgschema.QuerySchemaName(w.no, model.SensitivityOpen, false))
	writes := []string{
		"INSERT INTO machine (id) VALUES (99)",
		"UPDATE machine SET name = 'x'",
		"DELETE FROM machine",
		"TRUNCATE " + open + ".machine",
		"CREATE TABLE " + open + ".mine (x int)",
		"CREATE VIEW " + open + ".mine AS SELECT 1",
		"DROP VIEW " + open + ".machine",
		"INSERT INTO " + pgschema.Qualified(w.schema, pgschema.TableObject) + " (obj_no) VALUES (99)",
		"CREATE TABLE public.mine (x int)",
	}
	for _, grade := range qscGrades {
		for _, q := range writes {
			w.as(t, grade, false, func(tx *sql.Tx) {
				if _, err := tx.Exec(q); err == nil {
					t.Errorf("%s: %s was accepted", grade, q)
				}
			})
		}
	}
	if n := sxInt(t, w.admin, `SELECT count(*) FROM `+pgschema.Qualified(w.schema, pgschema.TableObject)); n != 3 {
		t.Errorf("the objects are %d", n)
	}
}

// [REQ-574]
func TestQscATypeViewHasTheObjectsOfTheTypeAndOfEveryTypeBelowItOnce(t *testing.T) {
	w := qscOpen(t)
	w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
		ids := qscStrings(t, tx, `SELECT id::text FROM machine ORDER BY id`)
		if strings.Join(ids, ",") != "1,2,3" {
			t.Errorf("the machines: %v", ids)
		}
		servers := qscStrings(t, tx, `SELECT id::text FROM server ORDER BY id`)
		if strings.Join(servers, ",") != "2,3" {
			t.Errorf("the servers: %v", servers)
		}
		if got := qscStrings(t, tx, `SELECT serial FROM machine WHERE id = 1`); len(got) != 1 || got[0] != "SN-NEW" {
			t.Errorf("the serial: %v", got)
		}
		if got := qscStrings(t, tx, `SELECT min_grade FROM machine WHERE id = 1`); len(got) != 1 || got[0] != "fact" {
			t.Errorf("the evidence grade is shown as text: %v", got)
		}
		if got := qscStrings(t, tx, `SELECT temp::text FROM machine WHERE id = 1`); len(got) != 1 || got[0] != "41.5" {
			t.Errorf("the volatile value: %v", got)
		}
		if got := qscStrings(t, tx, `SELECT fan::text FROM server WHERE id = 2`); len(got) != 1 || got[0] != "1200" {
			t.Errorf("the volatile value of a type: %v", got)
		}
		if got := qscStrings(t, tx, `SELECT temp_at::text FROM machine WHERE id = 1`); len(got) != 1 {
			t.Errorf("the time of the volatile value: %v", got)
		}
	})
}

// [REQ-574] [REQ-576]
func TestQscACustomAttributeIsAColumnNamedLikeItIsAndTheModelViewsDescribeOnlyWhatTheGradeSees(t *testing.T) {
	w := qscOpen(t)
	for _, grade := range qscGrades {
		w.as(t, grade, false, func(tx *sql.Tx) {
			_, err := tx.Exec(`SELECT "org.rack_owner" FROM machine LIMIT 0`)
			if seen := qscSeen[grade]["org.rack_owner"]; seen != (err == nil) {
				t.Errorf("%s: the column of the custom attribute: %v", grade, err)
			}
		})
		w.as(t, grade, false, func(tx *sql.Tx) {
			if tx2 := qscStrings(t, tx, `SELECT name FROM model_attribute WHERE type = 'machine' AND name = 'org.rack_owner'`); (len(tx2) == 1) != qscSeen[grade]["org.rack_owner"] {
				t.Errorf("%s: the description of the custom attribute: %v", grade, tx2)
			}
			if got := qscStrings(t, tx, `SELECT name FROM model_type ORDER BY name`); strings.Join(got, ",") != "machine,server" {
				t.Errorf("%s: the types of the model: %v", grade, got)
			}
			if got := qscStrings(t, tx, `SELECT parent FROM model_type WHERE name = 'server'`); len(got) != 1 || got[0] != "machine" {
				t.Errorf("%s: the parent of a type: %v", grade, got)
			}
		})
	}
}

// [REQ-576]
func TestQscEveryViewAndColumnOfTheQuerySchemaHasAComment(t *testing.T) {
	w := qscOpen(t)
	missing := sxInt(t, w.admin, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname LIKE 'q\_%' AND c.relkind = 'v' AND obj_description(c.oid, 'pg_class') IS NULL`)
	if missing != 0 {
		t.Errorf("%d views have no comment", missing)
	}
	missing = sxInt(t, w.admin, `SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname LIKE 'q\_%' AND c.relkind = 'v' AND a.attnum > 0 AND NOT a.attisdropped AND col_description(c.oid, a.attnum) IS NULL`)
	if missing != 0 {
		t.Errorf("%d columns have no comment", missing)
	}
	missing = sxInt(t, w.admin, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname LIKE 'q\_%' AND obj_description(p.oid, 'pg_proc') IS NULL`)
	if missing != 0 {
		t.Errorf("%d functions have no comment", missing)
	}
}

// [REQ-572]
func TestQscAHigherGradeSchemaHoldsOnlyWhatDiffersFromTheGradeBelow(t *testing.T) {
	w := qscOpen(t)
	count := func(grade model.Sensitivity) int {
		return sxInt(t, w.admin, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relkind = 'v'`,
			pgschema.QuerySchemaName(w.no, grade, false))
	}
	open, restricted, confidential := count(model.SensitivityOpen), count(model.SensitivityRestricted), count(model.SensitivityConfidential)
	if open == 0 || restricted == 0 || confidential == 0 || restricted >= open || confidential >= open {
		t.Errorf("views: open %d restricted %d confidential %d", open, restricted, confidential)
	}
	if sxInt(t, w.admin, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = 'model_type'`,
		pgschema.QuerySchemaName(w.no, model.SensitivityRestricted, false)) != 0 {
		t.Error("a view that is the same for every grade is made again in a higher grade")
	}
}

// [REQ-574] [REQ-623]
func TestQscATypeViewReadsOneObjectByAnIndexAndTheViewOfAParentIsAUnionAllOfTheTablesOfItsTypes(t *testing.T) {
	w := qscOpen(t)
	sxRun(t, w.admin, `ANALYZE`)
	w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
		for _, q := range []string{"SET LOCAL enable_seqscan = off", "SET LOCAL enable_bitmapscan = off"} {
			if _, err := tx.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		plan := strings.Join(qscStrings(t, tx, `EXPLAIN SELECT serial FROM server WHERE id = 2`), "\n")
		if !strings.Contains(plan, "Index Scan") || strings.Contains(plan, "Seq Scan") {
			t.Errorf("one object of a type is not read by its key:\n%s", plan)
		}
		plan = strings.Join(qscStrings(t, tx, `EXPLAIN SELECT id FROM machine`), "\n")
		if !strings.Contains(plan, "Append") {
			t.Errorf("the view of a parent type is not a union of its tables:\n%s", plan)
		}
		for _, bad := range []string{"HashAggregate", "Unique", "Sort"} {
			if strings.Contains(plan, bad) {
				t.Errorf("the union of the types is made without a step to take the same row away (%s):\n%s", bad, plan)
			}
		}
	})
}

// [REQ-623]
func TestQscTheLinksOfAnObjectAreReadFromTheIndexAloneByTheirNumber(t *testing.T) {
	w := qscOpen(t)
	link := pgschema.Qualified(w.schema, pgschema.TableLink)
	cols := sxText(t, w.admin, `SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'link'`, w.schema)
	if !strings.Contains(cols, "src") || !strings.Contains(cols, "dst") || !strings.Contains(cols, "type_no") {
		t.Fatalf("the columns of the links: %s", cols)
	}
	sxRun(t, w.admin, `VACUUM `+link)
	tx, err := w.admin.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(qscStrings(t, tx, `EXPLAIN SELECT dst FROM `+link+` WHERE src = 1 AND type_no = 1`), "\n")
	if !strings.Contains(plan, "Index Only Scan") {
		t.Errorf("the links of an object are not read from the index alone:\n%s", plan)
	}
}

// [REQ-573]
func TestQscDeletingAKGDropsItsQuerySchemasAndItsRoles(t *testing.T) {
	w := qscOpen(t)
	count := func(query string) int { return sxInt(t, w.admin, query, w.no) }
	if count(`SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'q\_'||$1::text||'\_%'`) < 3 {
		t.Fatalf("the schemas that the KG has: %d", count(`SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'q\_'||$1::text||'\_%'`))
	}
	roles := `SELECT count(*) FROM pg_roles WHERE rolname LIKE 'pq\_` + w.token + `\_'||$1::text||'\_%'`
	if count(roles) != 3 {
		t.Fatalf("the roles that the KG has: %d", count(roles))
	}
	if err := w.st.DeleteKG(context.Background(), sxKG(t, 1)); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'q\_'||$1::text||'\_%'`); n != 0 {
		t.Errorf("%d query schemas are left", n)
	}
	if n := count(roles); n != 0 {
		t.Errorf("%d roles are left", n)
	}
}

// [REQ-561]
func TestQscARenameOfAnAttributeBuildsEveryViewAgainWithTheNewName(t *testing.T) {
	w := qscOpen(t)
	tables := func() string {
		return sxText(t, w.admin, `SELECT string_agg(c.relname || ':' || c.relfilenode::text, ',' ORDER BY c.relname) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind = 'r' AND c.relname NOT LIKE 'model\_%'`, w.schema)
	}
	before := tables()
	// a rename alone leaves the hash of the model as it was (the names are labels), so the rename comes with an added attribute
	added := sxProp(t, "pr.machine.rack", "rack", 22, "vt.string", model.ChangeSlow)
	if _, err := w.st.AdjustStorageSchema(context.Background(), sxKG(t, 1), qscModelNamed(t, "serial_no", added)); err != nil {
		t.Fatal(err)
	}
	if after := tables(); after != before {
		t.Errorf("a rename wrote a table again:\n%s\n%s", before, after)
	}
	w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
		for _, view := range []string{"machine", "server"} {
			cols := qscColumns(t, tx, "SELECT * FROM "+view+" LIMIT 0")
			if qscHas(cols, "serial") || !qscHas(cols, "serial_no") {
				t.Errorf("%s after the rename: %v", view, cols)
			}
		}
		if !qscHas(qscStrings(t, tx, `SELECT name FROM attribute`), "serial_no") {
			t.Error("the view of the attributes keeps the old name")
		}
	})
}
