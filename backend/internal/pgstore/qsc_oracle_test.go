package pgstore

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// The tests of the value of a hidden attribute (ADR-0113): a role of a grade must never get one, not even as the text of an error,
// as a yes or no, or as what a function of its own saw. The role is no superuser, and the function that watches is made by the
// superuser in a schema of the test, with the cost of a function that the planner runs first (a role of the grade can make no
// function: it has no TEMP right to rely on, so the test does not).

// qscProbe makes the function that watches: it writes down every value it is called with, and the role of each grade may call it.
func qscProbe(t *testing.T, w *qscWorld) {
	t.Helper()
	sxRun(t, w.admin, `CREATE SCHEMA qscprobe`)
	sxRun(t, w.admin, `CREATE TABLE qscprobe.hits (v text)`)
	for _, arg := range []string{"jsonb", "text"} {
		sxRun(t, w.admin, `CREATE FUNCTION qscprobe.see(j `+arg+`) RETURNS boolean LANGUAGE plpgsql COST 0.0001 SECURITY DEFINER SET search_path = pg_catalog
			AS $f$ BEGIN INSERT INTO qscprobe.hits VALUES (j::text); RETURN true; END $f$`)
	}
	for _, grade := range qscGrades {
		sxRun(t, w.admin, `GRANT USAGE ON SCHEMA qscprobe TO `+w.role(grade))
		sxRun(t, w.admin, `GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA qscprobe TO `+w.role(grade))
	}
}

// qscNoSuperuser says that the role the transaction runs as is a plain one: no superuser, no right to make roles or databases, and it
// does not skip the rules of rows.
func qscNoSuperuser(t *testing.T, tx *sql.Tx) {
	t.Helper()
	got := qscStrings(t, tx, `SELECT current_user || ' ' || (rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls)::text FROM pg_roles WHERE rolname = current_user`)
	if len(got) != 1 || !strings.HasSuffix(got[0], " false") || !strings.HasPrefix(got[0], "pq_") {
		t.Fatalf("the transaction does not run as a plain role of a grade: %v", got)
	}
}

// qscWatched runs the queries as the role of a grade and says what the function that watches was called with.
func (w *qscWorld) qscWatched(t *testing.T, grade model.Sensitivity, watermark bool, queries ...string) []string {
	t.Helper()
	var seen []string
	w.as(t, grade, watermark, func(tx *sql.Tx) {
		qscNoSuperuser(t, tx)
		for _, q := range queries {
			rows, err := tx.Query(q)
			if err != nil {
				t.Fatalf("[%s] %s: %v", grade, q, err)
			}
			for rows.Next() {
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("[%s] %s: %v", grade, q, err)
			}
			_ = rows.Close()
		}
		if _, err := tx.Exec(`RESET ROLE`); err != nil {
			t.Fatal(err)
		}
		seen = qscStrings(t, tx, `SELECT v FROM qscprobe.hits ORDER BY v`)
	})
	return seen
}

// qscHiddenFrom is what a grade must never see, as it would show in a value or in a row made into jsonb: the values of the attributes of
// a higher grade and their names.
var qscLowGrades = []model.Sensitivity{model.SensitivityOpen, model.SensitivityRestricted}

var qscHiddenFrom = map[model.Sensitivity][]string{
	model.SensitivityOpen:       {"boot-secret", `"boot"`, `"cpus"`, `"coolant"`, "rack_owner", "77.5"},
	model.SensitivityRestricted: {"boot-secret", `"boot"`, `"coolant"`, "77.5"},
}

// qscSurfaces are the queries that put what a surface gives to the function that watches, as a condition of the caller: the value of an
// attribute, the whole row of a view, the detail of an object, the object of a function of a past time.
var qscSurfaces = []string{
	`SELECT count(*) FROM attribute WHERE qscprobe.see(value)`,
	`SELECT count(*) FROM attribute a WHERE qscprobe.see(to_jsonb(a))`,
	`SELECT count(*) FROM machine m WHERE qscprobe.see(to_jsonb(m))`,
	`SELECT count(*) FROM server m WHERE qscprobe.see(to_jsonb(m))`,
	`SELECT count(*) FROM machine m WHERE qscprobe.see(m.provenance)`,
	`SELECT count(*) FROM object o WHERE qscprobe.see(to_jsonb(o))`,
	`SELECT count(*) FROM object o WHERE qscprobe.see(object_detail(o.id))`,
	`SELECT count(*) FROM machine_at(3) m WHERE qscprobe.see(to_jsonb(m))`,
	`SELECT count(*) FROM server_at(3) m WHERE qscprobe.see(to_jsonb(m))`,
	`SELECT count(*) FROM change c WHERE qscprobe.see(to_jsonb(c))`,
	`SELECT count(*) FROM identifier i WHERE qscprobe.see(to_jsonb(i))`,
	`SELECT count(*) FROM link l WHERE qscprobe.see(to_jsonb(l))`,
}

// [REQ-626]
func TestQscAFunctionOfTheCallerThatTheCostPutsFirstNeverSeesTheValueOfAnAttributeOfAHigherGradeOnAnySurface(t *testing.T) {
	w := qscOpen(t)
	qscProbe(t, w)
	for _, watermark := range []bool{false, true} {
		for _, grade := range qscLowGrades {
			hidden := qscHiddenFrom[grade]
			seen := w.qscWatched(t, grade, watermark, qscSurfaces...)
			if len(seen) < 10 {
				t.Fatalf("[%s watermark %v] the function was called %d times: the surfaces were not read (%v)", grade, watermark, len(seen), seen)
			}
			for _, v := range seen {
				if grade == model.SensitivityOpen && v == "8" {
					t.Errorf("[%s watermark %v] the function saw the value of cpus: %s", grade, watermark, v)
				}
				for _, secret := range hidden {
					if strings.Contains(v, secret) {
						t.Errorf("[%s watermark %v] the function saw %s in %s", grade, watermark, secret, v)
					}
				}
			}
		}
		// the same function sees them when the grade is the one that may: the test can tell
		seen := w.qscWatched(t, model.SensitivityConfidential, watermark, qscSurfaces...)
		if !qscAny(seen, "boot-secret") || !qscAny(seen, `"boot"`) {
			t.Errorf("[confidential watermark %v] the function does not see the value that the grade may see: %v", watermark, seen)
		}
	}
}

func qscAny(list []string, part string) bool {
	for _, s := range list {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

// qscOracles are the conditions of a caller that fail on one value only, as text and as a yes or no: a cast of the value that is the
// confidential one to a number (the error shows the value), and a division by zero when the value has the length of the confidential
// one (the error says only that it exists). A grade that does not see the attribute must get neither.
var qscOracles = map[string]string{
	"cast":   `SELECT count(*) FROM attribute WHERE CASE WHEN (value #>> '{}') ~ '^boot' THEN (value #>> '{}')::int END > 0`,
	"length": `SELECT count(*) FROM attribute WHERE 1 / (length(value #>> '{}') - length('` + qscSecretBoot + `')) > 0`,
	"detail": `SELECT count(*) FROM object o, jsonb_each(object_detail(o.id) -> 'attributes') e WHERE 1 / (length(e.value ->> 'v') - length('` + qscSecretBoot + `')) > 0`,
	"keys":   `SELECT count(*) FROM machine m, jsonb_object_keys(m.provenance) k WHERE 1 / (length(k) - length('coolant')) > 0`,
}

var qscOracleNames = []string{"cast", "length", "detail", "keys"}

// [REQ-626]
func TestQscAnErrorOfAConditionOfTheCallerNeverShowsOrTellsTheValueOrTheNameOfAnAttributeOfAHigherGrade(t *testing.T) {
	w := qscOpen(t)
	for _, watermark := range []bool{false, true} {
		for _, name := range qscOracleNames {
			for _, grade := range qscLowGrades {
				w.as(t, grade, watermark, func(tx *sql.Tx) {
					qscNoSuperuser(t, tx)
					if _, err := tx.Exec(qscOracles[name]); err != nil {
						t.Errorf("[%s %s watermark %v] a condition of the caller fails on a value that the grade does not see: %v", grade, name, watermark, err)
					}
				})
			}
		}
		// the same conditions fail for the grade that sees the value: they are oracles, and the test can tell
		for _, name := range []string{"length", "detail", "keys"} {
			w.as(t, model.SensitivityConfidential, watermark, func(tx *sql.Tx) {
				if _, err := tx.Exec(qscOracles[name]); err == nil || !strings.Contains(err.Error(), "division by zero") {
					t.Errorf("[confidential %s watermark %v] the condition does not fail on the value of the grade: %v", name, watermark, err)
				}
			})
		}
		w.as(t, model.SensitivityConfidential, watermark, func(tx *sql.Tx) {
			if _, err := tx.Exec(qscOracles["cast"]); err == nil || !strings.Contains(err.Error(), qscSecretBoot) {
				t.Errorf("[confidential cast watermark %v] the cast does not show the value of the grade: %v", watermark, err)
			}
		})
	}
}
