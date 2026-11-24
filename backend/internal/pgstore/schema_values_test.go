package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
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

// A KG of this generator whose indexes were not all built (a run was stopped in the middle) is finished by the start of a server, through
// AdjustOldSchemas, which is the path that a server takes.
// [REQ-763]
func TestSvmTheStartOfAServerFinishesTheIndexesOfAKGThatIsOfThisGeneratorButNotSettled(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	kg, schema := ssuOldKG(t, st, admin, 73)
	if _, err := st.AdjustStorageSchema(ctx, kg, sxStandard(t, true)); err != nil {
		t.Fatal(err)
	}
	rec, err := st.StorageSchema(ctx, kg)
	noErr(t, "record", err)
	if rec.GeneratorVersion != pgschema.GeneratorVersion {
		t.Fatalf("the KG is at the generator %d", rec.GeneratorVersion)
	}
	ty := rec.Manifest.Types[0].TypeNo
	fold := pgschema.Qualified(schema, pgschema.NameIndexFold(ty))
	sxRun(t, admin, `UPDATE pg_index SET indisvalid = false WHERE indexrelid = '`+fold+`'::regclass`)
	sxRun(t, admin, `DROP INDEX `+pgschema.Qualified(schema, pgschema.NameIndex(ty)))
	sxRun(t, admin, `UPDATE kg_schema_state SET state = 'index_pending' WHERE kg_id = $1`, kg.String())
	done, err := st.AdjustOldSchemas(ctx)
	noErr(t, "start", err)
	if len(done) != 1 || !done[0].Adjusted {
		t.Fatalf("the start did nothing about the KG: %+v", done)
	}
	if n := sxInvalid(t, admin, schema); n != 0 {
		t.Errorf("%d indexes are still invalid", n)
	}
	for _, name := range []string{pgschema.NameIndex(ty), pgschema.NameIndexFold(ty)} {
		if n := sxInt(t, admin, `SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = $2`, schema, name); n != 1 {
			t.Errorf("the index %s is not there after the start", name)
		}
	}
	if state := sxInt(t, admin, `SELECT count(*) FROM kg_schema_state WHERE kg_id = $1 AND state = 'ok'`, kg.String()); state != 1 {
		t.Error("the state is not ok after the start")
	}
	again, err := st.AdjustOldSchemas(ctx)
	if noErr(t, "start again", err); len(again) != 0 {
		t.Errorf("a settled KG was handled again: %+v", again)
	}
}

// A build that finds the storage schema made by another generator than its own refuses to write (ADR-0101), and forgets what it knew.
// [REQ-763]
func TestSvmAWriteIsRefusedWhenAnotherBuildAdjustedTheSchemaToAnotherGenerator(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	r.v2wApply(t, graph.Batch{Objects: []graph.ObjectUpsert{r.fx.Server("SN-000000", "M1")}})
	sxRun(t, r.admin, `UPDATE kg_schema_state SET generator_version = generator_version + 1 WHERE kg_id = $1`, r.kg.ID.String())
	_, err := r.app.Apply(r.ctx, r.kg.ID, r.lease, graph.Batch{Objects: []graph.ObjectUpsert{r.fx.Server("SN-000001", "M1")}})
	if kernel.CodeOf(err) != CodeSchemaMismatch {
		t.Fatalf("the write after another build adjusted the schema: %v", err)
	}
}

// A value whose attribute has no column is not thrown away: the KG is not adjusted and says so; a time seen that is no time stays in prov
// and leaves the column of the time null; the empty time and reference of an old entry are not carried over.
// [REQ-763]
func TestSvmTheAdjustmentKeepsWhatHasNoColumnAndTakesOldTimesAndEmptiesAsTheyAre(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	kg, schema := ssuOldKG(t, st, admin, 74)
	rec, err := st.StorageSchema(ctx, kg)
	noErr(t, "record", err)
	sxRun(t, admin, `UPDATE kg_schema_state SET generator_version = 4, manifest = jsonb_set(manifest, '{generator_version}', '4') WHERE kg_id = $1`, kg.String())
	for _, ty := range rec.Manifest.Types {
		for _, n := range []string{pgschema.NameIndex(ty.TypeNo), pgschema.NameIndexFold(ty.TypeNo)} {
			sxRun(t, admin, `DROP INDEX `+pgschema.Qualified(schema, n))
		}
	}
	enum := svmFind(rec)[model.BaseEnum]
	svmOldRow(t, admin, schema, enum, 701, `{"v":"down","g":"fact","b":"observed","s":"not a time","r":""}`)
	common := pgschema.Qualified(schema, pgschema.TableObjectState)
	sxRun(t, admin, `INSERT INTO `+common+` (obj_no, type_no, prov) VALUES (702, 1, '{"999999":{"v":"precious","g":"fact","b":"observed"}}')`)
	if _, err := st.AdjustOldSchemas(ctx); err == nil || !strings.Contains(err.Error(), "999999") {
		t.Fatalf("a value with no column was adjusted away, or the reason is not given: %v", err)
	}
	if got := ssuGenerator(t, st, kg); got != 4 {
		t.Fatalf("the generator is %d after a refused adjustment", got)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM `+common+` WHERE obj_no = 702 AND prov->'999999'->>'v' = 'precious'`); n != 1 {
		t.Error("the value with no column was lost")
	}
	sxRun(t, admin, `DELETE FROM `+common+` WHERE obj_no = 702`)
	if done, err := st.AdjustOldSchemas(ctx); err != nil || len(done) != 1 || !done[0].Adjusted {
		t.Fatalf("the adjustment: %+v, %v", done, err)
	}
	table := pgschema.Qualified(schema, enum.table)
	col, at := pgschema.StateColumn(enum.attrNo), pgschema.StateColumnAt(enum.attrNo)
	var value sql.NullString
	var atNull bool
	var prov string
	noErr(t, "row", admin.QueryRow(`SELECT `+col+`, `+at+` IS NULL, prov::text FROM `+table+` WHERE obj_no = 701`).Scan(&value, &atNull, &prov))
	if value.String != "down" || !atNull {
		t.Errorf("the value %q and the time null %v", value.String, atNull)
	}
	var entry map[string]map[string]any
	noErr(t, "prov", json.Unmarshal([]byte(prov), &entry))
	e := entry[strconv.Itoa(enum.attrNo)]
	if e["t"] != "not a time" || len(e) != 3 || e["g"] != "fact" || e["b"] != "observed" {
		t.Errorf("the entry is %v: it keeps the text of the time, and no empty reference", e)
	}
}
