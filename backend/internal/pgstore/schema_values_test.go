package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the adjustment of a KG of the generator before version 5 (ADR-0101): the values that its prov holds are moved into the
// columns, and the name indexes are built. Their names start with svm.

// svmAttr is a volatile attribute of the manifest: where it is and of which kind.
type svmAttr struct {
	table  string
	typeNo int
	attrNo int
	kind   model.BaseKind
	common bool
}

// svmFind finds the first volatile attribute of each kind in the manifest of the KG, the common ones first.
func svmFind(rec SchemaRecord) map[model.BaseKind]svmAttr {
	out := map[model.BaseKind]svmAttr{}
	for _, c := range rec.Manifest.CommonState {
		if _, ok := out[c.Kind]; !ok {
			out[c.Kind] = svmAttr{pgschema.TableObjectState, 0, c.AttrNo, c.Kind, true}
		}
	}
	for _, ty := range rec.Manifest.Types {
		for _, c := range ty.State {
			if _, ok := out[c.Kind]; !ok {
				out[c.Kind] = svmAttr{pgschema.StateTable(ty.TypeNo), ty.TypeNo, c.AttrNo, c.Kind, false}
			}
		}
	}
	return out
}

// svmOldRow writes a row of values as the generator before kept it: the value in prov, the columns empty.
func svmOldRow(t *testing.T, admin *sql.DB, schema string, a svmAttr, objNo int, entry string) {
	t.Helper()
	table := pgschema.Qualified(schema, a.table)
	prov := fmt.Sprintf(`{"%d":%s}`, a.attrNo, entry)
	if a.common {
		sxRun(t, admin, `INSERT INTO `+table+` (obj_no, type_no, prov) VALUES ($1, 1, $2)`, objNo, prov)
		return
	}
	sxRun(t, admin, `INSERT INTO `+table+` (obj_no, prov) VALUES ($1, $2)`, objNo, prov)
}

// [REQ-763]
func TestSvmAKGThatKeptItsValuesInProvHasThemMovedIntoTheColumnsAndTheNameIndexesBuilt(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	kg, schema := ssuOldKG(t, st, admin, 71)
	rec, err := st.StorageSchema(ctx, kg)
	noErr(t, "record", err)
	// the KG as the generator 4 made it: no name indexes
	for _, ty := range rec.Manifest.Types {
		for _, n := range []string{pgschema.NameIndex(ty.TypeNo), pgschema.NameIndexFold(ty.TypeNo)} {
			sxRun(t, admin, `DROP INDEX `+pgschema.Qualified(schema, n))
		}
	}
	sxRun(t, admin, `UPDATE kg_schema_state SET generator_version = 4, manifest = jsonb_set(manifest, '{generator_version}', '4') WHERE kg_id = $1`, kg.String())
	attrs := svmFind(rec)
	seen := `"s":"2030-01-02T03:04:05.5Z"`
	rows := []struct {
		kind  model.BaseKind
		no    int
		entry string
		value any
		at    string
	}{
		{model.BaseEnum, 1, `{"v":"down","g":"fact","b":"observed",` + seen + `,"r":"src/1"}`, "down", "2030-01-02T03:04:05.5Z"},
		{model.BaseEnum, 2, `{"v":null,"g":"unknown","b":"observed","s":"","r":""}`, nil, ""},
		{model.BaseNumber, 3, `{"v":"12.5","g":"assumed","b":"configured",` + seen + `,"r":""}`, "12.5", "2030-01-02T03:04:05.5Z"},
		{model.BaseNumber, 4, `{"v":7,"g":"fact","b":"observed","s":"","r":""}`, int64(7), ""},
		{model.BaseQuantity, 5, `{"v":"64.5","g":"fact","b":"observed",` + seen + `,"r":"x"}`, "64.5", "2030-01-02T03:04:05.5Z"},
		{model.BaseTime, 6, `{"v":"2031-02-03T04:05:06Z","g":"fact","b":"observed",` + seen + `,"r":""}`, "2031-02-03T04:05:06Z", "2030-01-02T03:04:05.5Z"},
	}
	for _, r := range rows {
		a, ok := attrs[r.kind]
		if !ok {
			t.Fatalf("the standard model has no volatile attribute of the kind %s", r.kind)
		}
		svmOldRow(t, admin, schema, a, r.no+500, r.entry)
	}
	// rows that the step leaves alone: a row with no entry, and one whose entries are already of the new form
	enum := attrs[model.BaseEnum]
	svmOldRow(t, admin, schema, enum, 600, `{"g":"fact","b":"observed","t":"2030-01-02T03:04:05Z"}`)
	sxRun(t, admin, `UPDATE `+pgschema.Qualified(schema, enum.table)+` SET `+pgschema.StateColumn(enum.attrNo)+` = 'kept' WHERE obj_no = 600`)
	sxRun(t, admin, `INSERT INTO `+pgschema.Qualified(schema, pgschema.TableObjectState)+` (obj_no, type_no) VALUES (601, 1)`)

	done, err := st.AdjustOldSchemas(ctx)
	if err != nil || len(done) != 1 || !done[0].Adjusted {
		t.Fatalf("the adjustment: %+v, %v", done, err)
	}
	if got := ssuGenerator(t, st, kg); got != pgschema.GeneratorVersion {
		t.Fatalf("the generator is %d", got)
	}
	reader, err := st.access(ctx, kg)
	noErr(t, "access", err)
	fx, err := graph.NewFixture()
	noErr(t, "fixture", err)
	check := func(when string) {
		for _, r := range rows {
			a := attrs[r.kind]
			table := pgschema.Qualified(schema, a.table)
			col, at := pgschema.StateColumn(a.attrNo), pgschema.StateColumnAt(a.attrNo)
			var text sql.NullString
			var prov string
			noErr(t, "row", admin.QueryRow(`SELECT `+valueRead(r.kind, col)+`, prov::text FROM `+table+` WHERE obj_no = $1`, r.no+500).Scan(&text, &prov))
			got, err := valueFrom(r.kind, text)
			noErr(t, "value", err)
			if !vcolSame(got, r.value) {
				t.Errorf("%s: row %d (%s) has the value %v, wanted %v", when, r.no, r.kind, got, r.value)
			}
			if strings.Contains(prov, `"v"`) || strings.Contains(prov, `"s"`) || !strings.Contains(prov, `"g"`) {
				t.Errorf("%s: row %d: prov is %s", when, r.no, prov)
			}
			var same sql.NullBool
			noErr(t, "time", admin.QueryRow(`SELECT `+at+` = NULLIF($2, '')::timestamptz FROM `+table+` WHERE obj_no = $1`, r.no+500, r.at).Scan(&same))
			if (r.at != "" && (!same.Valid || !same.Bool)) || (r.at == "" && same.Valid) {
				t.Errorf("%s: row %d: the time seen is not %q", when, r.no, r.at)
			}
			// the reader gives the value and what is said about it, as the writer would have kept them
			var row graph.VolatileRow
			var found bool
			oid := fx.ServerID("SVM-" + strconv.Itoa(r.no))
			noErr(t, "read", reader.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
				var err error
				row, found, err = (&kgSnapshot{a: reader}).volatileOf(ctx, tx, regEntry{ref: oid.String(), objNo: int64(r.no + 500), typeNo: a.typeNo}, oid, a.attrNo)
				return err
			}))
			if !found || !vcolSame(row.Value, r.value) || row.Grade == "" || (r.at != "" && row.SeenAt == "") {
				t.Errorf("%s: row %d is read as %+v (found %v)", when, r.no, row, found)
			}
		}
		var kept sql.NullString
		noErr(t, "kept", admin.QueryRow(`SELECT `+pgschema.StateColumn(enum.attrNo)+`::text FROM `+pgschema.Qualified(schema, enum.table)+` WHERE obj_no = 600`).Scan(&kept))
		if kept.String != "kept" {
			t.Errorf("%s: a row that is of the new form was moved again: %v", when, kept)
		}
		if n := sxInt(t, admin, `SELECT count(*) FROM `+pgschema.Qualified(schema, pgschema.TableObjectState)+` WHERE obj_no = 601 AND prov = '{}'::jsonb`); n != 1 {
			t.Errorf("%s: a row with no entry changed", when)
		}
	}
	check("after the adjustment")
	for _, ty := range rec.Manifest.Types {
		for _, n := range []string{pgschema.NameIndex(ty.TypeNo), pgschema.NameIndexFold(ty.TypeNo)} {
			if c := sxInt(t, admin, `SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = $1 AND c.relname = $2 AND i.indisvalid`, schema, n); c != 1 {
				t.Fatalf("the index %s is not built and valid", n)
			}
		}
	}
	if err := st.CheckStorageSchema(ctx, kg, sxStandard(t, true)); err != nil {
		t.Errorf("the adjusted KG is refused: %v", err)
	}
	// the statements of the step again: what is moved is not moved twice
	now, err := st.StorageSchema(ctx, kg)
	noErr(t, "record", err)
	old := now.Manifest
	old.GeneratorVersion = 4
	delta, err := pgschema.Diff(old, now.Manifest)
	noErr(t, "diff", err)
	ran := 0
	for _, s := range delta.Steps {
		if s.What == pgschema.WhatData {
			sxRun(t, admin, s.SQL)
			ran++
		}
	}
	if ran == 0 {
		t.Fatal("the step has no statement that moves values")
	}
	check("after the statements again")
}

// [REQ-763]
func TestSvmAnIndexThatAnAdjustmentLeftInvalidIsBuiltAgainByTheNextRun(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	kg, schema := ssuOldKG(t, st, admin, 72)
	rec, err := st.StorageSchema(ctx, kg)
	noErr(t, "record", err)
	ty := rec.Manifest.Types[0].TypeNo
	fold := pgschema.Qualified(schema, pgschema.NameIndexFold(ty))
	sxRun(t, admin, `UPDATE pg_index SET indisvalid = false WHERE indexrelid = '`+fold+`'::regclass`)
	sxRun(t, admin, `DROP INDEX `+pgschema.Qualified(schema, pgschema.NameIndex(ty)))
	sxRun(t, admin, `UPDATE kg_schema_state SET state = 'index_pending' WHERE kg_id = $1`, kg.String())
	if _, err := st.AdjustStorageSchema(ctx, kg, sxStandard(t, true)); err != nil {
		t.Fatal(err)
	}
	if n := sxInvalid(t, admin, schema); n != 0 {
		t.Errorf("%d indexes are still invalid", n)
	}
	for _, name := range []string{pgschema.NameIndex(ty), pgschema.NameIndexFold(ty)} {
		if n := sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = $2`, schema, name); n != 1 {
			t.Errorf("the index %s is not there after the run", name)
		}
	}
}
