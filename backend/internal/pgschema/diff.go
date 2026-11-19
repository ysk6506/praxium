package pgschema

import (
	"slices"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// CodeNotAdditive is the code of a change that the schema cannot follow by adding: it needs the KG to be
// built again.
const CodeNotAdditive kernel.Code = "pgschema_not_additive"

// Delta is what Diff returns: the steps, and the manifest to keep once they have run. The manifest is the
// newer one, with the statistics groups of the schema as they will then be.
type Delta struct {
	Steps  []Step
	Result Manifest
}

func notAdditive(format string, args ...any) error {
	return kernel.Errorf(CodeNotAdditive, format, args...)
}

// Diff finds the steps that bring a schema described by old up to the model described by next. It follows
// only additions: a type, a property, a volatile property, a change of the search set, the fixed tables that a
// newer generator adds, and the changes that touch no table (a name, a label, a flag of the model). A value
// kind or a number that changed, a type or a property that is gone, a change of the common state, or a change
// of the generator that is not a known addition is not an addition, and Diff says so without a step.
func Diff(old, next Manifest) (Delta, error) {
	if old.KGNo != next.KGNo {
		return Delta{}, notAdditive("the manifests are of two KGs (%d and %d)", old.KGNo, next.KGNo)
	}
	schema := SchemaName(next.KGNo)
	fixed, err := fixedAdditions(schema, old.GeneratorVersion, next.GeneratorVersion)
	if err != nil {
		return Delta{}, err
	}
	if !slices.Equal(old.CommonState, next.CommonState) {
		return Delta{}, notAdditive("the volatile attributes that every type has changed")
	}
	oldTypes := map[int]TypeSummary{}
	for _, t := range old.Types {
		oldTypes[t.TypeNo] = t
	}
	tx, concurrent := fixed, []Step(nil)
	result := next
	result.State = StateOK
	result.Types = nil
	seen := map[int]bool{}
	for _, nt := range next.Types {
		ot, existed := oldTypes[nt.TypeNo]
		seen[nt.TypeNo] = true
		var (
			sum  TypeSummary
			t, c []Step
			err  error
		)
		if existed {
			sum, t, c, err = diffType(schema, ot, nt)
		} else {
			sum, t, err = addType(schema, nt)
		}
		if err != nil {
			return Delta{}, err
		}
		tx, concurrent = append(tx, t...), append(concurrent, c...)
		result.Types = append(result.Types, sum)
	}
	for no := range oldTypes {
		if !seen[no] {
			return Delta{}, notAdditive("the type %d is gone from the model", no)
		}
	}
	return Delta{Steps: append(tx, concurrent...), Result: result}, nil
}

// addType plans a type that the schema does not have yet.
func addType(schema string, nt TypeSummary) (TypeSummary, []Step, error) {
	ts := typeShape{TypeNo: nt.TypeNo, Docs: nt.Docs, State: nt.State}
	var search SearchSet
	for _, a := range nt.Search {
		search = append(search, SearchEntry{TypeNo: nt.TypeNo, AttrNo: a})
	}
	made, err := newTypeSteps(schema, ts, search)
	if err != nil {
		return TypeSummary{}, nil, err
	}
	steps := append(append(made.tables, made.indexes...), made.stats...)
	return nt, steps, nil
}

// diffType compares one type that the schema has.
func diffType(schema string, ot, nt TypeSummary) (TypeSummary, []Step, []Step, error) {
	newDocs, err := addedColumns(nt.TypeNo, ot.Docs, nt.Docs)
	if err != nil {
		return TypeSummary{}, nil, nil, err
	}
	newState, err := addedColumns(nt.TypeNo, ot.State, nt.State)
	if err != nil {
		return TypeSummary{}, nil, nil, err
	}
	var tx, concurrent []Step
	stateSteps, err := stateAdditions(schema, nt.TypeNo, ot.State, newState)
	if err != nil {
		return TypeSummary{}, nil, nil, err
	}
	tx = append(tx, stateSteps...)
	sum := nt
	sum.Stats = slices.Clone(ot.Stats)
	for _, group := range statsGroups(newDocs) {
		step, err := statsStep(schema, nt.TypeNo, group)
		if err != nil {
			return TypeSummary{}, nil, nil, err
		}
		tx = append(tx, step)
		sum.Stats = append(sum.Stats, attrNos(group))
	}
	concurrent, err = searchChanges(schema, ot, nt)
	return sum, tx, concurrent, err
}

// addedColumns is the columns of next that cur does not have. A column of cur that next lacks or has with
// another kind is not an addition.
func addedColumns(typeNo int, cur, next []Column) ([]Column, error) {
	have := map[int]Column{}
	for _, c := range cur {
		have[c.AttrNo] = c
	}
	var added []Column
	for _, c := range next {
		old, ok := have[c.AttrNo]
		switch {
		case !ok:
			added = append(added, c)
		case old.Kind != c.Kind:
			return nil, notAdditive("the attribute %d of the type %d changed its value kind from %q to %q", c.AttrNo, typeNo, old.Kind, c.Kind)
		}
		delete(have, c.AttrNo)
	}
	for no := range have {
		return nil, notAdditive("the attribute %d is gone from the type %d", no, typeNo)
	}
	return added, nil
}

// stateAdditions adds the volatile attributes that a type did not have: columns that may be null, in the
// table of the type's volatile values, which is made first if the type had none.
func stateAdditions(schema string, typeNo int, had, added []Column) ([]Step, error) {
	if len(added) == 0 {
		return nil, nil
	}
	if len(had) == 0 {
		def, err := typeStateTableDef(typeNo, added)
		if err != nil {
			return nil, err
		}
		return []Step{{Phase: InTx, What: WhatTable, Name: def.Name, SQL: def.sql(schema)}}, nil
	}
	cols, err := valueColumns(added)
	if err != nil {
		return nil, err
	}
	sql := "ALTER TABLE " + Qualified(schema, StateTable(typeNo))
	for i, c := range cols {
		if i > 0 {
			sql += ","
		}
		sql += " ADD COLUMN " + c
	}
	return []Step{{Phase: InTx, What: WhatColumn, Name: StateTable(typeNo), SQL: sql}}, nil
}

// searchChanges is the indexes to build and to drop on a table that has rows: both run outside the
// transaction, one at a time, without blocking the writers.
func searchChanges(schema string, ot, nt TypeSummary) ([]Step, error) {
	var steps []Step
	for _, a := range nt.Search {
		if slices.Contains(ot.Search, a) {
			continue
		}
		col, ok := findColumn(nt.Docs, a)
		if !ok {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the search set of the type %d names the attribute %d, which it does not have", nt.TypeNo, a)
		}
		def, err := searchIndexDef(nt.TypeNo, col)
		if err != nil {
			return nil, err
		}
		steps = append(steps, Step{Phase: Concurrent, What: WhatIndex, Name: def.Name, SQL: def.sql(schema, true)})
	}
	for _, a := range ot.Search {
		if !slices.Contains(nt.Search, a) {
			name := SearchIndex(nt.TypeNo, a)
			steps = append(steps, Step{Phase: Concurrent, What: WhatIndex, Name: name, SQL: "DROP INDEX CONCURRENTLY IF EXISTS " + Qualified(schema, name)})
		}
	}
	return steps, nil
}
