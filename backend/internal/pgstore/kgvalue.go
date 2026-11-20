package pgstore

import (
	"database/sql"
	"sort"
	"strconv"

	"github.com/lib/pq"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The value of a volatile attribute is the column of its attribute number in the table of values (pgschema.StateColumn), typed
// by the kind of the value, and the time the source saw it is the column next to it (StateColumnAt, a time with a zone). What
// says how far the value can be trusted is the entry of the attribute in prov: the grade (g), the basis (b), the time as the
// source gave it (t) and the reference (r). The value is in no other place (ADR-0101).
//
// A column holds a value of its kind, not every form that the port lets a value have: a number or a quantity may be an int64
// or a decimal text, and the column holds the number, so it is read back as an int64 when it is a whole number and else as
// decimal text; a time is read back in the one form that is written (UTC, ending in Z, at most six digits of a second without a
// trailing zero), which is the only form the check of a value (graph) lets in.

// provDoc is the entry of an attribute in prov: no value, only what is said about it.
type provDoc struct {
	Grade string `json:"g"`
	Basis string `json:"b"`
	Seen  string `json:"t,omitempty"`
	Ref   string `json:"r,omitempty"`
}

// volatileItem is one volatile value on its way into a table of values.
type volatileItem struct {
	objNo  int64
	typeNo int
	attrNo int
	value  any
	doc    provDoc
}

// valueText is the text a value is sent as; the statement casts it to the type of the column (valueCast). ok is false for a
// null, which is a value that is not there (an unknown one).
func valueText(kind model.BaseKind, v any) (text string, ok bool, err error) {
	if v == nil {
		return "", false, nil
	}
	bad := func() (string, bool, error) {
		return "", false, kernel.Errorf(kernel.CodeInvalidValue, "a value of the Go type %T is not a value of the kind %q", v, kind)
	}
	switch kind {
	case model.BaseInteger:
		n, isInt := v.(int64)
		if !isInt {
			return bad()
		}
		return strconv.FormatInt(n, 10), true, nil
	case model.BaseBoolean:
		b, isBool := v.(bool)
		if !isBool {
			return bad()
		}
		return strconv.FormatBool(b), true, nil
	case model.BaseNumber, model.BaseQuantity:
		switch x := v.(type) {
		case int64:
			return strconv.FormatInt(x, 10), true, nil
		case string:
			return x, true, nil
		}
		return bad()
	case model.BaseList, model.BaseMap:
		raw, err := marshal(v)
		return string(raw), true, err
	}
	s, isText := v.(string)
	if !isText {
		return bad()
	}
	return s, true, nil
}

// valueCast is the expression that turns the text of a value (the expression text) into the type of the column of the kind.
func valueCast(kind model.BaseKind, text string) (string, error) {
	typ, err := pgschema.ColumnType(kind)
	if err != nil {
		return "", err
	}
	switch {
	case typ == "text":
		return text, nil
	case kind == model.BaseTime:
		return pgschema.TimeFunc + "(" + text + ")", nil
	}
	return text + "::" + typ, nil
}

// valueRead is the expression that reads the column as text, in the form valueFrom reads back.
func valueRead(kind model.BaseKind, col string) string {
	if kind != model.BaseTime {
		return col + "::text"
	}
	utc := col + " AT TIME ZONE 'UTC'"
	return `to_char(` + utc + `, 'YYYY-MM-DD"T"HH24:MI:SS') || COALESCE(NULLIF(rtrim(to_char(` + utc + `, '.US'), '0'), '.'), '') || 'Z'`
}

// valueFrom is the value of a column read as valueRead says; a null is a value that is not there.
func valueFrom(kind model.BaseKind, text sql.NullString) (any, error) {
	if !text.Valid {
		return nil, nil
	}
	switch kind {
	case model.BaseInteger:
		n, err := strconv.ParseInt(text.String, 10, 64)
		if err != nil {
			return nil, kernel.Wrap(kernel.CodeInternal, err, "a stored integer is damaged")
		}
		return n, nil
	case model.BaseBoolean:
		return text.String == "true", nil
	case model.BaseNumber, model.BaseQuantity:
		if n, err := strconv.ParseInt(text.String, 10, 64); err == nil {
			return n, nil
		}
		return text.String, nil
	case model.BaseList, model.BaseMap:
		return decodeValue([]byte(text.String))
	}
	return text.String, nil
}

// valueKind is the kind of the value of an attribute in the table of values that has it, and whether the attribute is in the
// common table.
func (a *kgAccess) valueKind(typeNo, attrNo int) (kind model.BaseKind, common, ok bool) {
	if k, isCommon := a.common[attrNo]; isCommon {
		return k, true, true
	}
	k, own := a.state[typeNo][attrNo]
	return k, false, own
}

// valueTable is the table of values that has the attribute of the type.
func valueTable(common bool, typeNo int) string {
	if common {
		return pgschema.TableObjectState
	}
	return pgschema.StateTable(typeNo)
}

// valueArrays are the columns of the rows of one attribute as the statements take them: the object numbers, the type numbers,
// the text of the values, whether there is a value, the grades, the bases, the times seen (as given), the references.
type valueArrays struct {
	nos, types []int64
	vals       []string
	has        []bool
	docs       []string
}

func (v *valueArrays) add(kind model.BaseKind, it volatileItem) error {
	text, ok, err := valueText(kind, it.value)
	if err != nil {
		return err
	}
	doc, err := marshal(map[string]provDoc{strconv.Itoa(it.attrNo): it.doc})
	if err != nil {
		return err
	}
	v.nos, v.types, v.vals, v.has, v.docs = append(v.nos, it.objNo), append(v.types, int64(it.typeNo)), append(v.vals, text), append(v.has, ok), append(v.docs, string(doc))
	return nil
}

// writeValues writes the values of one attribute of one table of values: the value and the time seen into the columns, the entry
// into prov, merged into what is there. Of two values for one object the later one stays. A change of values alone is not a
// change of the object, so the table of the type's rows is not touched.
func (c *kgChunk) writeValues(table string, common bool, attrNo int, kind model.BaseKind, items []volatileItem) {
	val, err := valueCast(kind, "u.v")
	if err != nil {
		c.fail(err)
		return
	}
	col, at := pgschema.StateColumn(attrNo), pgschema.StateColumnAt(attrNo)
	seen := pgschema.TimeFunc + "(NULLIF(u.doc->'" + strconv.Itoa(attrNo) + "'->>'t', ''))"
	slices(len(items), func(lo, hi int) {
		var arr valueArrays
		for _, it := range items[lo:hi] {
			if err := arr.add(kind, it); err != nil {
				c.fail(err)
				return
			}
		}
		cols, sel := `(obj_no, `+col+`, `+at+`, prov)`, `u.obj_no, CASE WHEN u.has THEN `+val+` END, `+seen+`, u.doc`
		if common {
			cols, sel = `(obj_no, type_no, `+col+`, `+at+`, prov)`, `u.obj_no, u.type_no, CASE WHEN u.has THEN `+val+` END, `+seen+`, u.doc`
		}
		t := c.a.t(table)
		c.exec("write volatile values", `INSERT INTO `+t+` `+cols+` SELECT `+sel+` FROM unnest($1::bigint[], $2::bigint[], $3::text[], $4::boolean[], $5::jsonb[])
			AS u(obj_no, type_no, v, has, doc) ON CONFLICT (obj_no) DO UPDATE SET `+col+` = EXCLUDED.`+col+`, `+at+` = EXCLUDED.`+at+`, prov = `+t+`.prov || EXCLUDED.prov`,
			pq.Array(arr.nos), pq.Array(arr.types), pq.Array(arr.vals), pq.Array(arr.has), pq.Array(arr.docs))
	})
}

// valueGroup is the values that go into one column of one table.
type valueGroup struct {
	table  string
	common bool
	attrNo int
	kind   model.BaseKind
	items  []volatileItem
}

// groupValues sorts the items by the column they go to, in the order of the table and the attribute number, so the statements
// run in one order.
func (a *kgAccess) groupValues(items []volatileItem) ([]valueGroup, error) {
	type key struct {
		table string
		attr  int
	}
	byKey := map[key]*valueGroup{}
	var keys []key
	for _, it := range items {
		kind, common, ok := a.valueKind(it.typeNo, it.attrNo)
		if !ok {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the attribute %d of the type %d has no table of values", it.attrNo, it.typeNo)
		}
		k := key{valueTable(common, it.typeNo), it.attrNo}
		g := byKey[k]
		if g == nil {
			g = &valueGroup{table: k.table, common: common, attrNo: it.attrNo, kind: kind}
			byKey[k], keys = g, append(keys, k)
		}
		g.items = append(g.items, it)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].table < keys[j].table || (keys[i].table == keys[j].table && keys[i].attr < keys[j].attr)
	})
	out := make([]valueGroup, 0, len(keys))
	for _, k := range keys {
		out = append(out, *byKey[k])
	}
	return out, nil
}
