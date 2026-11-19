package pgschema

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// maxTypeNo is the largest type number: the type number is a smallint in the tables.
const maxTypeNo = 32767

// Column is one attribute that has a place in a table: its number and the kind of its value.
type Column struct {
	AttrNo int            `json:"attr_no"`
	Kind   model.BaseKind `json:"kind"`
}

// Standard reports whether the attribute is one of the standard model's. Only those get statistics.
func (c Column) Standard() bool { return c.AttrNo < model.OrgNumberBase }

// typeShape is what the plan needs to know of one object type: the attributes that live in its document and
// the volatile ones that live in its table of values (the common state excluded: that is in object_state).
type typeShape struct {
	TypeNo int
	Docs   []Column
	State  []Column
}

// shape is the model as the plan reads it: the volatile attributes that every type has, and the types.
type shape struct {
	Common []Column
	Types  []typeShape
}

// collect reads the registry into a shape, in ascending order of the numbers. The common state is the set of
// volatile attributes that every type has (in the standard model: lifecycle_state and oper_state).
func collect(reg *model.Registry) (shape, error) {
	if reg == nil {
		return shape{}, kernel.Errorf(kernel.CodeInvalidValue, "the plan needs a registry")
	}
	types := reg.ObjectTypes()
	sort.Slice(types, func(i, j int) bool { return types[i].TypeNo < types[j].TypeNo })
	var sh shape
	volatile := make([][]Column, len(types))
	docs := make([][]Column, len(types))
	for i, t := range types {
		if t.TypeNo < 1 || t.TypeNo > maxTypeNo {
			return shape{}, kernel.Errorf(kernel.CodeInvalidValue, "the type %s has the number %d; it must be 1 to %d", t.ID, t.TypeNo, maxTypeNo)
		}
		var err error
		if docs[i], volatile[i], err = columnsOf(reg, t); err != nil {
			return shape{}, err
		}
	}
	sh.Common = commonOf(volatile)
	for i, t := range types {
		sh.Types = append(sh.Types, typeShape{TypeNo: t.TypeNo, Docs: docs[i], State: without(volatile[i], sh.Common)})
	}
	return sh, nil
}

// columnsOf splits the properties of a type, those it inherits included, into document and volatile attributes.
func columnsOf(reg *model.Registry, t model.ObjectType) (docs, volatile []Column, err error) {
	seen := map[int]bool{}
	for _, p := range reg.EffectiveProperties(t.ID) {
		vt, ok := reg.ValueType(p.ValueType)
		if !ok {
			return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "the property %s has the unknown value type %s", p.ID, p.ValueType)
		}
		if p.AttrNo < 1 || seen[p.AttrNo] {
			return nil, nil, kernel.Errorf(kernel.CodeInvalidValue, "the type %s has the attribute number %d twice or not at all", t.ID, p.AttrNo)
		}
		seen[p.AttrNo] = true
		col := Column{AttrNo: p.AttrNo, Kind: vt.Base}
		if p.EffectiveHistory() == model.HistoryVolatile {
			volatile = append(volatile, col)
		} else {
			docs = append(docs, col)
		}
	}
	sortColumns(docs)
	sortColumns(volatile)
	return docs, volatile, nil
}

func sortColumns(cols []Column) {
	sort.Slice(cols, func(i, j int) bool { return cols[i].AttrNo < cols[j].AttrNo })
}

// commonOf is the volatile attributes that every type has.
func commonOf(volatile [][]Column) []Column {
	if len(volatile) == 0 {
		return nil
	}
	count := map[int]int{}
	for _, cols := range volatile {
		for _, c := range cols {
			count[c.AttrNo]++
		}
	}
	var out []Column
	for _, c := range volatile[0] {
		if count[c.AttrNo] == len(volatile) {
			out = append(out, c)
		}
	}
	return out
}

// without is cols minus the attributes of drop.
func without(cols, drop []Column) []Column {
	skip := map[int]bool{}
	for _, c := range drop {
		skip[c.AttrNo] = true
	}
	var out []Column
	for _, c := range cols {
		if !skip[c.AttrNo] {
			out = append(out, c)
		}
	}
	return out
}
