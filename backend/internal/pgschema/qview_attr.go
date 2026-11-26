package pgschema

import (
	"strconv"
	"strings"
)

// attributeView is every attribute of every object, one row each. It is the slow way (a view of a type is the way to read many
// objects), and the one to ask "which objects have an attribute of this name" or to read the evidence of a value.
func (g qGen) attributeView() qObject {
	var branches []string
	for _, t := range g.qm.Types {
		branches = append(branches, g.attributeOfDocs(t))
		for _, a := range g.shown(t.Own) {
			branches = append(branches, g.attributeOfState(a, g.tbl(StateTable(t.No)), "ts"))
		}
	}
	for _, a := range g.shown(g.qm.Common) {
		branches = append(branches, g.attributeOfState(a, g.tbl(TableObjectState), "os"))
	}
	return qObject{What: WhatView, Name: ViewAttribute, Body: strings.Join(branches, "\nUNION ALL\n"),
		Comment: "One row for each attribute of each object, with the evidence of its value. It is slow for many objects: use the view of a type to read them.",
		Columns: []qComment{{colID, "The number of the object."}, {colName, "The name of the attribute."}, {"value", "The value, as jsonb."},
			{"grade", "The evidence grade of the value: fact, assumed or unknown."}, {"basis", "What the value is based on."},
			{"seen_at", "The time the source saw the value (UTC)."}}}
}

// attributeOfDocs is the attributes of the objects of one type that are kept in the document.
func (g qGen) attributeOfDocs(t qType) string {
	return "SELECT t.obj_no AS id, ma.name AS name, e.value->'v' AS value, e.value->>'g' AS grade, e.value->>'b' AS basis, " + TimeFunc + "(e.value->>'t') AS seen_at FROM " +
		g.objectRows(t.No, "t") + " CROSS JOIN LATERAL jsonb_each(" + g.grade.withoutHidden("t.doc") + ") e JOIN " + g.tbl(TableModelAttribute) + " ma ON ma.type_no = " + strconv.Itoa(t.No) +
		" AND ma.attr_no = e.key::integer WHERE " + g.grade.notHidden("ma.attr_no")
}

// attributeOfState is the rows of one volatile attribute: the objects that have an entry for it in the table of values.
func (g qGen) attributeOfState(a qAttr, table, alias string) string {
	no := strconv.Itoa(a.No)
	entry := alias + ".prov->'" + no + "'"
	return "SELECT " + alias + ".obj_no AS id, " + QuoteLiteral(a.Name) + " AS name, to_jsonb(" + alias + "." + QuoteIdent(StateColumn(a.No)) + ") AS value, " +
		entry + "->>'g' AS grade, " + entry + "->>'b' AS basis, " + TimeFunc + "(" + entry + "->>'t') AS seen_at FROM " + table + " " + alias +
		" JOIN " + g.tbl(TableObject) + " o ON o.obj_no = " + alias + ".obj_no WHERE " + entry + " IS NOT NULL AND " + g.alive("o")
}
