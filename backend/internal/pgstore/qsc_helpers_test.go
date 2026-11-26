package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The helpers of the tests of the query schema (ADR-0113). Their names start with qsc.

// qscWorld is a KG with a small model of two types (a machine and a server below it), two objects, the head, and one object of a
// large apply that is not readable yet.
type qscWorld struct {
	st     *Store
	admin  *sql.DB
	no     int64
	token  string
	schema string
}

const (
	qscSecretSerial = "SN-OPEN"
	qscCPUs         = 8
	qscSecretBoot   = "boot-secret"
)

// qscModel is the model: a machine (no 1) with a serial (open), a number of cpus (restricted), a boot password (confidential), a
// temperature (volatile, open) and a coolant level (volatile, confidential), and a server (no 2) below it with a bay (open).
func qscModel(t *testing.T) SchemaModel { return qscModelNamed(t, "serial") }

// qscModelNamed is the model with the serial of the machine under another name and the properties that are given added to the machine.
func qscModelNamed(t *testing.T, serialName string, extra ...model.Property) SchemaModel {
	t.Helper()
	common := model.Trait{
		ID: sxID[model.TraitKind](t, "tr.common"), Name: "Common",
		Properties: []model.Property{
			sxProp(t, "pr.common.name", "name", 1, "vt.string", model.ChangeSlow),
			sxProp(t, "pr.common.life", "life", 4, "vt.string", model.ChangeDynamic),
		},
	}
	serial := sxProp(t, "pr.machine.serial", serialName, 10, "vt.string", model.ChangeStatic)
	cpus := sxProp(t, "pr.machine.cpus", "cpus", 11, "vt.integer", model.ChangeSlow)
	cpus.Sensitivity = model.SensitivityRestricted
	boot := sxProp(t, "pr.machine.boot", "boot", 12, "vt.string", model.ChangeSlow)
	boot.Sensitivity = model.SensitivityConfidential
	temp := sxProp(t, "pr.machine.temp", "temp", 13, "vt.number", model.ChangeDynamic)
	coolant := sxProp(t, "pr.machine.coolant", "coolant", 14, "vt.number", model.ChangeDynamic)
	coolant.Sensitivity = model.SensitivityConfidential
	owner := sxProp(t, "pr.machine.rack_owner", "org.rack_owner", model.OrgNumberBase, "vt.string", model.ChangeSlow)
	owner.Namespace, owner.Sensitivity = model.NamespaceOrg, model.SensitivityRestricted
	extra = append([]model.Property{owner}, extra...)
	machine := sxType(t, "ot.machine", 1, "Machine", append([]model.Property{serial, cpus, boot, temp, coolant}, extra...)...)
	server := sxType(t, "ot.server", 2, "Server", sxProp(t, "pr.server.bay", "bay", 20, "vt.string", model.ChangeSlow),
		sxProp(t, "pr.server.fan", "fan", 21, "vt.integer", model.ChangeDynamic))
	server.Parent, server.Traits = machine.ID, nil
	reg, err := model.NewRegistry(model.Definitions{ModelVersion: 1, Traits: []model.Trait{common}, ObjectTypes: []model.ObjectType{machine, server}})
	if err != nil {
		t.Fatal(err)
	}
	return SchemaModel{Registry: reg, Search: pgschema.NewSearchSet(), WithStats: true}
}

func qscEntry(value any) string {
	return fmt.Sprintf(`{"v": %v, "g": 0, "b": "configured", "t": "2026-01-01T00:00:00Z", "r": "rec-1"}`, value)
}

func qscDoc(serial string, extra ...string) string {
	parts := []string{`"10": ` + qscEntry(`"`+serial+`"`), `"11": ` + qscEntry(qscCPUs), `"12": ` + qscEntry(`"`+qscSecretBoot+`"`)}
	return "{" + strings.Join(append(parts, extra...), ", ") + "}"
}

// qscRow puts an object into the registry and the table of its type.
func qscRow(t *testing.T, w *qscWorld, no, typeNo int, seqFrom int, doc string) {
	t.Helper()
	sxRun(t, w.admin, fmt.Sprintf(`INSERT INTO %s (obj_no, ref, type_no, name, min_grade, seq_from) VALUES ($1::bigint, 'ref-'||$1::bigint::text, $2, 'obj'||$1::bigint::text, 0, $3)`,
		pgschema.Qualified(w.schema, pgschema.TableObject)), no, typeNo, seqFrom)
	sxRun(t, w.admin, fmt.Sprintf(`INSERT INTO %s (obj_no, ref, name, min_grade, doc, content_hash, row_hash, seq_from)
		VALUES ($1::bigint, 'ref-'||$1::bigint::text, 'obj'||$1::bigint::text, 0, $2::jsonb, '\x00', '\x00', $3)`, pgschema.Qualified(w.schema, pgschema.TypeTable(typeNo))), no, doc, seqFrom)
}

// qscOpen makes the KG: object 1 (a machine, changed at 3, it was another serial before), object 2 (a server), and object 3 (a server of an
// apply that is not readable yet, at 5), with the head at 3.
func qscOpen(t *testing.T) *qscWorld {
	t.Helper()
	st, admin := sxOpen(t)
	rec, err := st.CreateStorageSchema(context.Background(), sxKG(t, 1), qscModel(t))
	if err != nil {
		t.Fatal(err)
	}
	w := &qscWorld{st: st, admin: admin, no: rec.KGNo, schema: rec.Schema(), token: sxText(t, admin, `SELECT token FROM install_token`)}
	sxRun(t, admin, fmt.Sprintf(`INSERT INTO %s (id, state, hash_sum, input_digest, applied_seq, readable_through, history_floor) VALUES (1, 'serving', '\x00', '\x00', 5, 3, 0)`,
		pgschema.Qualified(w.schema, pgschema.TableHead)))
	qscRow(t, w, 1, 1, 3, qscDoc("SN-NEW"))
	qscRow(t, w, 2, 2, 1, qscDoc(qscSecretSerial, `"20": `+qscEntry(`"bay-1"`)))
	qscRow(t, w, 3, 2, 5, qscDoc("SN-LATE", `"20": `+qscEntry(`"bay-2"`)))
	sxRun(t, admin, fmt.Sprintf(`INSERT INTO %s (obj_no, type_no, name, min_grade, doc, content_hash, row_hash, seq_from, seq_to)
		VALUES (1, 1, 'obj1', 0, $1::jsonb, '\x00', '\x00', 1, 3)`, pgschema.Qualified(w.schema, pgschema.TableObjectHist)), qscDoc(qscSecretSerial))
	qscVolatile(t, w)
	qscChanges(t, w)
	return w
}

// qscVolatile gives object 1 a temperature and a coolant level.
func qscVolatile(t *testing.T, w *qscWorld) {
	t.Helper()
	sxRun(t, w.admin, fmt.Sprintf(`INSERT INTO %s (obj_no, type_no, %s, %s, prov) VALUES (1, 1, 41.5, 77.5, $1::jsonb)`,
		pgschema.Qualified(w.schema, pgschema.TableObjectState), pgschema.QuoteIdent(pgschema.StateColumn(13)), pgschema.QuoteIdent(pgschema.StateColumn(14))),
		`{"13": `+qscEntry(`41.5`)+`, "14": `+qscEntry(`77.5`)+`}`)
	sxRun(t, w.admin, fmt.Sprintf(`INSERT INTO %s (obj_no, %s, prov) VALUES (2, 1200, $1::jsonb)`,
		pgschema.Qualified(w.schema, pgschema.StateTable(2)), pgschema.QuoteIdent(pgschema.StateColumn(21))), `{"21": `+qscEntry(`1200`)+`}`)
}

// qscChanges writes one change that added object 1 and two changes of the volatile values.
func qscChanges(t *testing.T, w *qscWorld) {
	t.Helper()
	sxRun(t, w.admin, fmt.Sprintf(`INSERT INTO %s (change_seq, kind, source_id, apply_id, chunk_no, chunk_count, ingest_seq, attempt, applied_at, objects, links)
		VALUES (2, 'apply', 's', 'a', 0, 1, 1, 1, now(), 1, 0)`, pgschema.Qualified(w.schema, pgschema.TableChangeBatch)))
	sxRun(t, w.admin, fmt.Sprintf(`INSERT INTO %s (change_seq, entity_kind, entity_no, change_kind, ord) VALUES (2, %d, 1, %d, 0)`,
		pgschema.Qualified(w.schema, pgschema.TableChangeEntry), pgschema.EntityKindObject, pgschema.ChangeKindAdded))
	for _, no := range []int{13, 14} {
		sxRun(t, w.admin, fmt.Sprintf(`INSERT INTO %s (change_seq, obj_no, attr_no, value_mark) VALUES (2, 1, %d, 'm')`, pgschema.Qualified(w.schema, pgschema.TableLightChange), no))
	}
}

func (w *qscWorld) role(grade model.Sensitivity) string {
	return pgschema.QuoteIdent(pgschema.QueryRoleName(w.token, w.no, grade))
}

// as runs fn in a transaction as the role of a grade, with the search path of the grade; the transaction is rolled back.
func (w *qscWorld) as(t *testing.T, grade model.Sensitivity, watermark bool, fn func(tx *sql.Tx)) {
	t.Helper()
	tx, err := w.admin.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{"SET LOCAL ROLE " + w.role(grade), "SET LOCAL search_path = " + pgschema.SearchPath(w.no, grade, watermark)} {
		if _, err := tx.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	fn(tx)
}

// qscStrings reads the first column of a query as texts.
func qscStrings(t *testing.T, tx *sql.Tx, query string, args ...any) []string {
	t.Helper()
	rows, err := tx.Query(query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s.String)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// qscColumns are the names of the columns a query answers with.
func qscColumns(t *testing.T, tx *sql.Tx, query string) []string {
	t.Helper()
	rows, err := tx.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	return cols
}

func qscHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
