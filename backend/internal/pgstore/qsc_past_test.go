package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the functions of a past time, the detail of an object, the set for a large apply and the change records of the query
// schema (ADR-0113). Their names start with Qsc.

// [REQ-626]
func TestQscTheFunctionsAndTheChangeViewsOfAGradeAreCutByTheSameSetOfAttributeNumbers(t *testing.T) {
	w := qscOpen(t)
	for _, grade := range qscGrades {
		w.as(t, grade, false, func(tx *sql.Tx) {
			surfaces := map[string][]string{
				"object_detail":   qscStrings(t, tx, `SELECT jsonb_object_keys(object_detail(1)->'attributes')`),
				"machine_at":      qscStrings(t, tx, `SELECT jsonb_object_keys(to_jsonb(r) - 'provenance') FROM machine_at(3) r`),
				"machine_at_prov": qscStrings(t, tx, `SELECT jsonb_object_keys(provenance) FROM machine_at(3)`),
				"value_change":    qscStrings(t, tx, `SELECT name FROM value_change`),
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
				if qscSeen[grade][name] && !qscHas(surfaces["object_detail"], name) {
					t.Errorf("%s: the detail lacks %s: %v", grade, name, surfaces["object_detail"])
				}
			}
			if qscSeen[grade]["coolant"] != qscHas(surfaces["value_change"], "coolant") {
				t.Errorf("%s: the changes of values and the coolant: %v", grade, surfaces["value_change"])
			}
		})
	}
}

// [REQ-575] [REQ-569]
func TestQscAFunctionOfAPastTimeAnswersForTheNumberAndRaisesAnErrorForOneThatCannotBeRead(t *testing.T) {
	w := qscOpen(t)
	// an object that was changed twice: three states, one in each interval
	qscRow(t, w, 4, 1, 3, qscDoc("S3"))
	for _, h := range []struct{ from, to int }{{1, 2}, {2, 3}} {
		sxRun(t, w.admin, fmt.Sprintf(`INSERT INTO %s (obj_no, type_no, name, min_grade, doc, content_hash, row_hash, seq_from, seq_to)
			VALUES (4, 1, 'obj4', 0, $1::jsonb, '\x00', '\x00', %d, %d)`, pgschema.Qualified(w.schema, pgschema.TableObjectHist), h.from, h.to), qscDoc(fmt.Sprintf("S%d", h.from)))
	}
	w.as(t, model.SensitivityConfidential, false, func(tx *sql.Tx) {
		for at, want := range map[string]string{"1": "S1", "2": "S2", "3": "S3"} {
			if got := qscStrings(t, tx, `SELECT serial FROM machine_at(`+at+`) WHERE id = 4`); len(got) != 1 || got[0] != want {
				t.Errorf("an object changed twice at %s: %v, want %s", at, got, want)
			}
		}
		now, then := qscColumns(t, tx, `SELECT * FROM machine LIMIT 0`), qscColumns(t, tx, `SELECT * FROM machine_at(3) LIMIT 0`)
		for _, col := range then {
			if !qscHas(now, col) {
				t.Errorf("the function of a past time has the column %s that the view has not", col)
			}
		}
		for _, col := range now {
			volatile := col == "life" || col == "life_at" || col == "temp" || col == "temp_at" || col == "coolant" || col == "coolant_at"
			if volatile == qscHas(then, col) {
				t.Errorf("the column %s: volatile %v, in the function %v", col, volatile, qscHas(then, col))
			}
		}
	})
	w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
		for at, want := range map[string]string{"2": qscSecretSerial, "3": "SN-NEW"} {
			if got := qscStrings(t, tx, `SELECT serial FROM machine_at(`+at+`) WHERE id = 1`); len(got) != 1 || got[0] != want {
				t.Errorf("machine_at(%s): %v, want %s", at, got, want)
			}
		}
		if got := qscStrings(t, tx, `SELECT id::text FROM machine_at(3) ORDER BY id`); strings.Join(got, ",") != "1,2,4" {
			t.Errorf("an object of an apply that is not readable is in the past: %v", got)
		}
	})
	sxRun(t, w.admin, `UPDATE `+pgschema.Qualified(w.schema, pgschema.TableHead)+` SET history_floor = 2`)
	for _, at := range []string{"1", "4", "99"} {
		w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
			_, err := tx.Exec(`SELECT * FROM machine_at(` + at + `)`)
			if pe, ok := err.(*pq.Error); !ok || string(pe.Code) != pgschema.CodeNotReadable {
				t.Errorf("machine_at(%s): %v (want %s)", at, err, pgschema.CodeNotReadable)
			}
		})
	}
	w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
		if _, err := tx.Exec(`SELECT * FROM machine_at(NULL)`); err == nil {
			t.Error("a null change number gives an answer")
		}
	})
}

// [REQ-574] [REQ-626]
func TestQscTheDetailOfAnObjectHasEveryAttributeTheGradeSeesWithItsEvidenceAndIsNullForAnObjectThatIsNotThere(t *testing.T) {
	w := qscOpen(t)
	w.as(t, model.SensitivityConfidential, false, func(tx *sql.Tx) {
		for path, want := range map[string]string{
			`attributes->'serial'->>'v'`:  "SN-NEW",
			`attributes->'serial'->>'b'`:  "configured",
			`attributes->'cpus'->>'v'`:    "8",
			`attributes->'temp'->>'v'`:    "41.5",
			`attributes->'coolant'->>'v'`: "77.5",
			`type`:                        "machine",
			`ref`:                         "ref-1",
		} {
			if got := qscStrings(t, tx, `SELECT (object_detail(1))`+detailPath(path)); len(got) != 1 || got[0] != want {
				t.Errorf("%s: %v, want %s", path, got, want)
			}
		}
		if got := qscStrings(t, tx, `SELECT coalesce(object_detail(12345)::text, 'null')`); len(got) != 1 || got[0] != "null" {
			t.Errorf("an object that is not there: %v", got)
		}
	})
}

func detailPath(path string) string {
	if strings.HasPrefix(path, "attributes") {
		return "->'attributes'->" + strings.TrimPrefix(path, "attributes->")
	}
	return "->>'" + path + "'"
}

// [REQ-561] [REQ-575]
func TestQscARenameWithAnAddedAttributeBuildsTheFunctionOfAPastTimeAgainWithTheNewName(t *testing.T) {
	w := qscOpen(t)
	added := sxProp(t, "pr.machine.rack", "rack", 22, "vt.string", model.ChangeSlow)
	if _, err := w.st.AdjustStorageSchema(context.Background(), sxKG(t, 1), qscModelNamed(t, "serial_no", added)); err != nil {
		t.Fatal(err)
	}
	w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
		if got := qscStrings(t, tx, `SELECT serial_no FROM machine_at(3) WHERE id = 1`); len(got) != 1 || got[0] != "SN-NEW" {
			t.Errorf("the function of a past time after the rename: %v", got)
		}
	})
}

// [REQ-573] [REQ-627]
func TestQscAGradeRoleHasNoRightOnTheSetForALargeApplyOfAHigherGrade(t *testing.T) {
	w := qscOpen(t)
	w.as(t, model.SensitivityOpen, true, func(tx *sql.Tx) {
		_, err := tx.Exec("SELECT 1 FROM " + pgschema.QuoteIdent(pgschema.QuerySchemaName(w.no, model.SensitivityConfidential, true)) + ".machine")
		if pe, ok := err.(*pq.Error); !ok || pe.Code != "42501" {
			t.Errorf("the set of a higher grade for a large apply: %v (want permission denied)", err)
		}
	})
}

// [REQ-627]
func TestQscTheSetForALargeApplyHasOnlyWhatIsReadableThroughTheWatermark(t *testing.T) {
	w := qscOpen(t)
	for _, c := range []struct {
		watermark bool
		want      string
	}{{false, "1,2,3"}, {true, "1,2"}} {
		w.as(t, model.SensitivityOpen, c.watermark, func(tx *sql.Tx) {
			if got := qscStrings(t, tx, `SELECT id::text FROM machine ORDER BY id`); strings.Join(got, ",") != c.want {
				t.Errorf("watermark %v: %v, want %s", c.watermark, got, c.want)
			}
			if got := qscStrings(t, tx, `SELECT serial FROM machine WHERE id = 1`); len(got) != 1 || got[0] != "SN-NEW" {
				t.Errorf("watermark %v: %v", c.watermark, got)
			}
		})
	}
	sxRun(t, w.admin, `UPDATE `+pgschema.Qualified(w.schema, pgschema.TableHead)+` SET readable_through = 2`)
	w.as(t, model.SensitivityOpen, true, func(tx *sql.Tx) {
		if got := qscStrings(t, tx, `SELECT serial FROM machine WHERE id = 1`); len(got) != 1 || got[0] != qscSecretSerial {
			t.Errorf("the set for a large apply reads the row that was there at the watermark: %v", got)
		}
	})
}

// [REQ-575] [REQ-627]
func TestQscTheStandardModelHasAFunctionOfAPastTimeForEveryTypeTheViewsOfTheChangesAndTheSixSchemas(t *testing.T) {
	st, admin := sxOpen(t)
	m := sxStandard(t, false)
	rec, err := st.CreateStorageSchema(context.Background(), sxKG(t, 1), m)
	if err != nil {
		t.Fatal(err)
	}
	open := pgschema.QuerySchemaName(rec.KGNo, model.SensitivityOpen, false)
	for _, v := range []string{"kg_version", "change", "value_change"} {
		if sxInt(t, admin, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind = 'v'`, open, v) != 1 {
			t.Errorf("no view %s", v)
		}
	}
	for _, ot := range m.Registry.ObjectTypes() {
		name := pgschema.AtFunctionName(pgschema.TypeViewName(ot.Name))
		if sxInt(t, admin, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = $1 AND p.proname = $2`, open, name) != 1 {
			t.Errorf("no function %s", name)
		}
	}
	if sxInt(t, admin, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = $1 AND p.proname = 'object_detail'`, open) != 1 {
		t.Error("no function object_detail")
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'q\_'||$1::text||'\_%'`, rec.KGNo); n != 6 {
		t.Errorf("the KG has %d query schemas", n)
	}
}

// [REQ-569]
func TestQscTheChangeRecordsAreViewsOfTheKGAndTheVersionsOfIt(t *testing.T) {
	w := qscOpen(t)
	w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
		if got := qscStrings(t, tx, `SELECT entity||':'||change||':'||entity_id FROM change`); len(got) != 1 || got[0] != "object:added:1" {
			t.Errorf("the change records: %v", got)
		}
		if got := qscStrings(t, tx, `SELECT count(*)::text FROM kg_version`); len(got) != 1 {
			t.Errorf("the versions: %v", got)
		}
	})
}
