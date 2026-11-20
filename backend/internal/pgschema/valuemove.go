package pgschema

import (
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// WhatData is a step that changes rows and not the shape of the schema.
const WhatData = "data"

// moveValuesSteps is one statement for each table of values of a schema of generator version 4: the common one and the
// ones of the types that have their own. In a schema of that version the value of a volatile attribute is the entry of
// its attribute number in prov (v the value, g the grade, b the basis, s the time seen, r the reference); version 5 keeps
// the value and the time in the columns of the attribute and in prov only g, b, t (the time as the source gave it) and r.
func moveValuesSteps(schema string, old Manifest) ([]Step, error) {
	var steps []Step
	if len(old.CommonState) > 0 {
		st, err := moveValuesStep(schema, TableObjectState, old.CommonState)
		if err != nil {
			return nil, err
		}
		steps = append(steps, st)
	}
	for _, t := range old.Types {
		if len(t.State) == 0 {
			continue
		}
		st, err := moveValuesStep(schema, StateTable(t.TypeNo), t.State)
		if err != nil {
			return nil, err
		}
		steps = append(steps, st)
	}
	return steps, nil
}

// moveValuesStep is the statement that moves the values of one table of values. All the expressions read the row as it
// was, so the columns and the new prov are made from the same old prov.
func moveValuesStep(schema, table string, cols []Column) (Step, error) {
	var set []string
	for _, c := range cols {
		val, err := Expr("t.prov", c.AttrNo, c.Kind)
		if err != nil {
			return Step{}, err
		}
		if c.Kind == "list" || c.Kind == "map" {
			val = "NULLIF(" + val + ", 'null'::jsonb)"
		}
		has := "jsonb_exists(t.prov->" + quoteKey(c.AttrNo) + ", 'v')"
		col, at := StateColumn(c.AttrNo), StateColumnAt(c.AttrNo)
		seen := TimeFunc + "(NULLIF(t.prov->" + quoteKey(c.AttrNo) + "->>'s', ''))"
		set = append(set,
			col+" = CASE WHEN "+has+" THEN "+val+" ELSE t."+col+" END",
			at+" = CASE WHEN "+has+" THEN "+seen+" ELSE t."+at+" END")
	}
	if len(set) == 0 {
		return Step{}, kernel.Errorf(kernel.CodeInvalidValue, "the table %s has no volatile attribute", table)
	}
	entry := "(x.value - 'v' - 's') || CASE WHEN jsonb_exists(x.value, 's') THEN jsonb_build_object('t', x.value->'s') ELSE '{}'::jsonb END"
	set = append(set, "prov = (SELECT COALESCE(jsonb_object_agg(x.key, CASE WHEN jsonb_typeof(x.value) = 'object' THEN "+entry+
		" ELSE x.value END), '{}'::jsonb) FROM jsonb_each(t.prov) AS x)")
	sql := "UPDATE " + Qualified(schema, table) + " AS t SET " + strings.Join(set, ", ") +
		" WHERE EXISTS (SELECT 1 FROM jsonb_each(t.prov) AS y WHERE jsonb_typeof(y.value) = 'object' AND (jsonb_exists(y.value, 'v') OR jsonb_exists(y.value, 's')))"
	return Step{Phase: InTx, What: WhatData, Name: table, SQL: sql}, nil
}

func quoteKey(attrNo int) string { return "'" + strconv.Itoa(attrNo) + "'" }
