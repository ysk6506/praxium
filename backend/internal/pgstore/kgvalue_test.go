package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the volatile values in their columns (ADR-0101). Their names start with vcol.

// vcolCase is a value of a kind and what is read back from its column.
type vcolCase struct {
	kind model.BaseKind
	in   any
	out  any
}

func vcolCases() []vcolCase {
	return []vcolCase{
		{model.BaseString, "text with 'quotes' and a \"double\" one", "text with 'quotes' and a \"double\" one"},
		{model.BaseEnum, "up", "up"},
		{model.BaseRef, "ref-1", "ref-1"},
		{model.BaseInteger, int64(-9007199254740993), int64(-9007199254740993)},
		{model.BaseBoolean, true, true},
		{model.BaseBoolean, false, false},
		{model.BaseNumber, "12.5", "12.5"},
		{model.BaseNumber, int64(5), int64(5)},
		{model.BaseNumber, "-0.25", "-0.25"},
		{model.BaseQuantity, "64.5", "64.5"},
		{model.BaseQuantity, "1.10", "1.10"}, // a numeric keeps its scale
		{model.BaseQuantity, int64(3), int64(3)},
		{model.BaseTime, "2030-01-01T00:00:00Z", "2030-01-01T00:00:00Z"},
		{model.BaseTime, "2030-01-01T00:00:00.5Z", "2030-01-01T00:00:00.5Z"},
		{model.BaseTime, "2030-01-01T00:00:00.123456Z", "2030-01-01T00:00:00.123456Z"},
		{model.BaseList, []any{"a", int64(2), true}, []any{"a", int64(2), true}},
		{model.BaseMap, map[string]any{"k": "v", "n": int64(4)}, map[string]any{"k": "v", "n": int64(4)}},
	}
}

func vcolSame(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// A value of every kind goes into the type of its column and comes back as it went in, in the form the port has for the kind.
// [REQ-760]
func TestVcolEveryKindOfValueGoesThroughItsColumnAndComesBack(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	for _, c := range vcolCases() {
		text, ok, err := valueText(c.kind, c.in)
		if err != nil || !ok {
			t.Fatalf("%s %v: %v %v", c.kind, c.in, ok, err)
		}
		cast, err := valueCast(c.kind, "$1")
		noErr(t, "cast", err)
		var back sql.NullString
		noErr(t, "round trip", w.admin.QueryRowContext(ctx, `SELECT `+valueRead(c.kind, "("+cast+")"), text).Scan(&back))
		got, err := valueFrom(c.kind, back)
		noErr(t, "from", err)
		if !vcolSame(got, c.out) {
			t.Errorf("%s: %#v went in and %#v came back (the column read %q)", c.kind, c.in, got, back.String)
		}
	}
	if v, err := valueFrom(model.BaseString, sql.NullString{}); err != nil || v != nil {
		t.Errorf("a null is a value that is not there: %v %v", v, err)
	}
	if _, ok, err := valueText(model.BaseString, nil); err != nil || ok {
		t.Errorf("no value is sent as null: %v %v", ok, err)
	}
	for _, bad := range []vcolCase{{model.BaseInteger, "5", nil}, {model.BaseBoolean, int64(1), nil}, {model.BaseString, int64(1), nil}, {model.BaseQuantity, map[string]any{"value": "1"}, nil}} {
		if _, _, err := valueText(bad.kind, bad.in); err == nil {
			t.Errorf("%s: %#v was taken", bad.kind, bad.in)
		}
	}
}

// vcolRegister puts an object of the type in the registry (and its row in the table of the type, so it is live).
func vcolRegister(t *testing.T, w *v2rWorld, no int, id graph.ObjectID, typeNo int) {
	t.Helper()
	v2rExec(t, w.admin, `INSERT INTO `+w.a.t("object")+` (obj_no, ref, type_no, name, min_grade, seq_from) VALUES ($1,$2,$3,'n',0,1)`, no, id.String(), typeNo)
}

type vcolWhere struct {
	typeNo, attrNo int
	kind           model.BaseKind
	common         bool
}

// vcolColumns lists the columns of the table of values of the standard model, one for each kind that has one, the common ones
// first, in the order of the numbers (a map is never walked in its own order).
func vcolColumns(w *v2rWorld) []vcolWhere {
	var out []vcolWhere
	seen := map[model.BaseKind]bool{}
	var attrs []int
	for no := range w.a.common {
		attrs = append(attrs, no)
	}
	sort.Ints(attrs)
	for _, no := range attrs {
		if k := w.a.common[no]; !seen[k] {
			seen[k] = true
			out = append(out, vcolWhere{w.serverNo, no, k, true})
		}
	}
	var types []int
	for no := range w.a.state {
		types = append(types, no)
	}
	sort.Ints(types)
	for _, typeNo := range types {
		attrs = attrs[:0]
		for no := range w.a.state[typeNo] {
			attrs = append(attrs, no)
		}
		sort.Ints(attrs)
		for _, no := range attrs {
			if k := w.a.state[typeNo][no]; !seen[k] {
				seen[k] = true
				out = append(out, vcolWhere{typeNo, no, k, false})
			}
		}
	}
	return out
}

// The volatile attributes of the standard model, one of each kind it has, are written by the writer of the chunks and read by the reader
// of the snapshots: the value is in the column of the attribute and not in prov, and what is said about it is in prov.
// [REQ-760]
func TestVcolTheWriterPutsTheValueInItsColumnAndTheReaderGivesItBack(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	fx, err := graph.NewFixture()
	noErr(t, "fixture", err)
	cols := vcolColumns(w)
	kinds := map[model.BaseKind]bool{}
	for _, c := range cols {
		kinds[c.kind] = true
	}
	for _, k := range []model.BaseKind{model.BaseEnum, model.BaseNumber, model.BaseQuantity, model.BaseTime} {
		if !kinds[k] {
			t.Fatalf("the standard model has no volatile attribute of the kind %s: the test no longer reaches it", k)
		}
	}
	for i, c := range cols {
		no := 100 + i
		id := fx.ServerID("VCOL-" + strconv.Itoa(i))
		vcolRegister(t, w, no, id, c.typeNo)
		for _, vc := range vcolCases() {
			if vc.kind != c.kind {
				continue
			}
			item := volatileItem{objNo: int64(no), typeNo: c.typeNo, attrNo: c.attrNo, value: vc.in,
				doc: provDoc{Grade: "fact", Basis: "observed", Seen: "2030-02-03T04:05:06.25Z", Ref: "src/1"}}
			noErr(t, "write", w.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
				ch := &kgChunk{writer: newWriter(ctx, tx, w.kg.ID), a: w.a, seq: 1}
				ch.writeItems([]volatileItem{item})
				return ch.err
			}))
			var got graph.VolatileRow
			var found bool
			noErr(t, "read", w.a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
				var err error
				got, found, err = (&kgSnapshot{a: w.a}).volatileOf(ctx, tx, regEntry{ref: id.String(), objNo: int64(no), typeNo: c.typeNo}, id, c.attrNo)
				return err
			}))
			if !found || !vcolSame(got.Value, vc.out) || got.Grade != model.GradeFact || got.Basis != model.BasisObserved || got.SeenAt != "2030-02-03T04:05:06.25Z" || got.Ref != "src/1" {
				t.Errorf("%s %v: read back %+v (found %v), wanted the value %v", c.kind, vc.in, got, found, vc.out)
			}
		}
		table := w.a.t(valueTable(c.common, c.typeNo))
		var prov, atText string
		col, at := pgschema.StateColumn(c.attrNo), pgschema.StateColumnAt(c.attrNo)
		noErr(t, "stored", w.admin.QueryRow(`SELECT prov::text, to_char(`+at+` AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US') FROM `+table+` WHERE obj_no = $1`, no).Scan(&prov, &atText))
		var entry map[string]map[string]any
		noErr(t, "prov", json.Unmarshal([]byte(prov), &entry))
		e := entry[strconv.Itoa(c.attrNo)]
		if _, has := e["v"]; has || len(e) != 4 || e["g"] != "fact" || e["b"] != "observed" || e["t"] != "2030-02-03T04:05:06.25Z" || e["r"] != "src/1" {
			t.Errorf("%s: prov is %s: it must have the grade, the basis, the time seen and the reference, and no value", c.kind, prov)
		}
		if atText != "2030-02-03T04:05:06.250000" {
			t.Errorf("%s: the column %s is %q", c.kind, at, atText)
		}
		if n := vcolCount(t, w, `SELECT count(*) FROM `+table+` WHERE obj_no = $1 AND `+col+` IS NOT NULL`, no); n != 1 {
			t.Errorf("%s: the column %s has no value", c.kind, col)
		}
	}
}

func vcolCount(t *testing.T, w *v2rWorld, query string, args ...any) int {
	t.Helper()
	var n int
	noErr(t, query, w.admin.QueryRow(query, args...).Scan(&n))
	return n
}

// An unknown value (no value, grade unknown) and an entry with no time seen keep the column null, and the entry is still there, so the
// reader says "there is a value of the attribute and it is unknown" and not "there is none".
// [REQ-760]
func TestVcolAnUnknownValueIsAnEntryWithAnEmptyColumn(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	fx, err := graph.NewFixture()
	noErr(t, "fixture", err)
	id := fx.ServerID("VCOL-N")
	vcolRegister(t, w, 7, id, w.serverNo)
	attr := graph.AttrServerPower
	_, common, _ := w.a.valueKind(w.serverNo, attr)
	read := func() (graph.VolatileRow, bool) {
		var row graph.VolatileRow
		var found bool
		noErr(t, "read", w.a.sh.tx(ctx, RoleAPIRead, true, func(tx *sql.Tx) error {
			var err error
			row, found, err = (&kgSnapshot{a: w.a}).volatileOf(ctx, tx, regEntry{ref: id.String(), objNo: 7, typeNo: w.serverNo}, id, attr)
			return err
		}))
		return row, found
	}
	if _, found := read(); found {
		t.Fatal("an object with no row of values has a value")
	}
	for _, v := range []any{"on", nil, "off"} {
		noErr(t, "write", w.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
			ch := &kgChunk{writer: newWriter(ctx, tx, w.kg.ID), a: w.a, seq: 1}
			g := "fact"
			if v == nil {
				g = "unknown"
			}
			ch.writeItems([]volatileItem{{objNo: 7, typeNo: w.serverNo, attrNo: attr, value: v, doc: provDoc{Grade: g, Basis: "observed"}}})
			return ch.err
		}))
		row, found := read()
		if !found || row.Value != v || row.SeenAt != "" {
			t.Errorf("after %v: %+v %v", v, row, found)
		}
	}
	var at sql.NullTime
	noErr(t, "at", w.admin.QueryRow(`SELECT `+pgschema.StateColumnAt(attr)+` FROM `+w.a.t(valueTable(common, w.serverNo))+` WHERE obj_no = 7`).Scan(&at))
	if at.Valid {
		t.Error("no time seen is a null in the column of the time")
	}
}

// What the staged apply writes again is compared in the type of the column: the same value in another form (5 and "5.0"), with the same
// grade and basis, is the same value; another time seen, another reference or another form of the same number do not make it differ,
// and a different value, grade or basis, or no entry, does.
// [REQ-760]
func TestVcolTheStagedApplyComparesTheColumnAndTheGradeAndTheBasis(t *testing.T) {
	w := v2rWorldOf(t)
	ctx := context.Background()
	fx, err := graph.NewFixture()
	noErr(t, "fixture", err)
	var numberCol *vcolWhere
	for _, c := range vcolColumns(w) {
		if c.kind == model.BaseNumber {
			c := c
			numberCol = &c
		}
	}
	if numberCol == nil {
		t.Fatal("no volatile attribute of the kind number")
	}
	id := fx.ServerID("VCOL-S")
	vcolRegister(t, w, 9, id, numberCol.typeNo)
	staged := func(v any, grade, basis string) []stagedValue {
		return []stagedValue{{objNo: 9, typeNo: numberCol.typeNo, attrNo: numberCol.attrNo, entry: provEntry{Value: v, Grade: grade, Basis: basis, Seen: "2030-05-05T00:00:00Z"}}}
	}
	differs := func(v []stagedValue) bool {
		var out []stagedValue
		noErr(t, "differing", w.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
			var err error
			out, err = differing(ctx, tx, w.a, v)
			return err
		}))
		return len(out) == 1
	}
	if !differs(staged(int64(5), "fact", "observed")) {
		t.Fatal("a value that is not kept does not differ")
	}
	noErr(t, "write", w.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		ch := &kgChunk{writer: newWriter(ctx, tx, w.kg.ID), a: w.a, seq: 1}
		writeStaged(ch, staged(int64(5), "fact", "observed"))
		return ch.err
	}))
	for name, c := range map[string]struct {
		v     any
		grade string
		basis string
		want  bool
	}{
		"the same value":            {int64(5), "fact", "observed", false},
		"the same number as a text": {"5.0", "fact", "observed", false},
		"another number":            {"5.5", "fact", "observed", true},
		"another grade":             {int64(5), "assumed", "observed", true},
		"another basis":             {int64(5), "fact", "configured", true},
		"no value":                  {nil, "fact", "observed", true},
	} {
		if got := differs(staged(c.v, c.grade, c.basis)); got != c.want {
			t.Errorf("%s: differs is %v", name, got)
		}
	}
	other := []stagedValue{{objNo: 9, typeNo: numberCol.typeNo, attrNo: numberCol.attrNo + 1000000, entry: provEntry{Value: "x"}}}
	if _, err := differingErr(ctx, w, other); err == nil {
		t.Error("an attribute that has no column was taken")
	}
}

func differingErr(ctx context.Context, w *v2rWorld, v []stagedValue) ([]stagedValue, error) {
	var out []stagedValue
	err := w.a.sh.tx(ctx, RoleApplier, false, func(tx *sql.Tx) error {
		var err error
		out, err = differing(ctx, tx, w.a, v)
		return err
	})
	return out, err
}
