package pgschema

import (
	"encoding/json"
	"strconv"
	"strings"
)

// modelTableDefs are the tables that describe the model to a user of the query schema (generator version 8). The names of the
// types are the names of their views, so a user can join the description to what a view says. Every row says a thing of the
// model and nothing of a KG's data.
func modelTableDefs() []tableDef {
	return []tableDef{
		{Name: TableModelType, Cols: []string{
			"type_no smallint NOT NULL PRIMARY KEY",
			"name text NOT NULL",
			"model_name text NOT NULL",
			"parent text NOT NULL DEFAULT ''",
			"layer text NOT NULL",
			"description text NOT NULL",
		}},
		{Name: TableModelAttribute, Cols: []string{
			"type_no smallint",
			"link_type_no smallint",
			"attr_no integer NOT NULL",
			"type text NOT NULL DEFAULT ''",
			"link_type text NOT NULL DEFAULT ''",
			"name text NOT NULL",
			"value_type text NOT NULL",
			"column_type text NOT NULL",
			"unit text NOT NULL",
			"enum_values jsonb NOT NULL",
			"searched boolean NOT NULL",
			"history text NOT NULL",
			"sensitivity text NOT NULL",
			"description text NOT NULL",
			"CHECK ((type_no IS NULL) <> (link_type_no IS NULL))",
		}},
		{Name: TableModelLinkType, Cols: []string{
			"link_type_no smallint NOT NULL PRIMARY KEY",
			"name text NOT NULL",
			"model_name text NOT NULL",
			"description text NOT NULL",
		}},
		{Name: TableModelLinkRule, Cols: []string{
			"link_type_no smallint NOT NULL",
			"link_type text NOT NULL",
			"from_kind text NOT NULL",
			"from_name text NOT NULL",
			"to_kind text NOT NULL",
			"to_name text NOT NULL",
			"cardinality text NOT NULL",
		}},
	}
}

// modelIndexDefs is the index of the description of the attributes by type and number: the detail of an object reads the names of its
// attributes from it.
func modelIndexDefs() []indexDef {
	return []indexDef{{Name: "model_attribute_type", Table: TableModelAttribute, Expr: "type_no, attr_no"}}
}

// modelTableSteps makes the tables of the model description and their index, in a schema that was made by an older generator.
func modelTableSteps(schema string) []Step {
	var steps []Step
	for _, d := range modelTableDefs() {
		steps = append(steps, Step{Phase: InTx, What: WhatTable, Name: d.Name, SQL: strings.Replace(d.sql(schema), "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1)})
	}
	for _, d := range modelIndexDefs() {
		steps = append(steps, Step{Phase: InTx, What: WhatIndex, Name: d.Name, SQL: strings.Replace(d.sql(schema, false), " INDEX ", " INDEX IF NOT EXISTS ", 1)})
	}
	return steps
}

// maxRowsPerInsert keeps one statement of the filling of a table of the model description to a size that PostgreSQL parses at once.
const maxRowsPerInsert = 400

// fillSteps are the statements that put the description of the model into its tables: each table is emptied and filled again, so the
// tables say what the model says now. They run in the transaction that makes the views, so a user never reads a view with the
// description of another model.
func (qm qModel) fillSteps(schema string) []Step {
	rows := map[string][]string{}
	for _, t := range qm.Types {
		rows[TableModelType] = append(rows[TableModelType], row(num(t.No), lit(t.View), lit(t.Name), lit(t.Parent), lit(t.Layer), lit(t.Desc)))
		for _, a := range qm.typeAttrs(t) {
			rows[TableModelAttribute] = append(rows[TableModelAttribute], qm.attributeRow(t.View, t.No, 0, "", a, qm.Search[[2]int{t.No, a.No}]))
		}
	}
	for _, l := range qm.Links {
		rows[TableModelLinkType] = append(rows[TableModelLinkType], row(num(l.No), lit(l.View), lit(l.Name), lit(l.Desc)))
		for _, a := range l.Props {
			rows[TableModelAttribute] = append(rows[TableModelAttribute], qm.attributeRow("", 0, l.No, l.View, a, false))
		}
	}
	views := map[int]string{}
	for _, l := range qm.Links {
		views[l.No] = l.View
	}
	for _, r := range qm.Rules {
		rows[TableModelLinkRule] = append(rows[TableModelLinkRule], row(num(r.LinkNo), lit(views[r.LinkNo]), lit(r.FromKind), lit(r.From), lit(r.ToKind), lit(r.To), lit(r.Cardinality)))
	}
	var steps []Step
	for _, table := range ModelTables() {
		steps = append(steps, Step{Phase: InTx, What: WhatRows, Name: table, SQL: "DELETE FROM " + Qualified(schema, table)})
		list := rows[table]
		for len(list) > 0 {
			n := min(maxRowsPerInsert, len(list))
			steps = append(steps, Step{Phase: InTx, What: WhatRows, Name: table, SQL: "INSERT INTO " + Qualified(schema, table) + " VALUES\n" + strings.Join(list[:n], ",\n")})
			list = list[n:]
		}
	}
	return steps
}

func (qm qModel) attributeRow(typeView string, typeNo, linkNo int, linkView string, a qAttr, searched bool) string {
	typeCol, linkCol := "NULL", "NULL"
	if typeNo > 0 {
		typeCol = num(typeNo)
	} else {
		linkCol = num(linkNo)
	}
	enum, _ := json.Marshal(append([]string{}, a.Enum...))
	history := "versioned"
	if a.Volatile {
		history = "volatile"
	}
	return row(typeCol, linkCol, num(a.No), lit(typeView), lit(linkView), lit(a.Name), lit(a.ValueType), lit(a.ColType), lit(a.Unit), lit(string(enum))+"::jsonb",
		strconv.FormatBool(searched), lit(history), lit(string(a.Sens)), lit(a.Desc))
}

func row(cells ...string) string { return "(" + strings.Join(cells, ", ") + ")" }
func num(n int) string           { return strconv.Itoa(n) }
func lit(s string) string        { return QuoteLiteral(s) }
