package pgschema

import (
	"strconv"
	"strings"
)

// volatileEntries is the entries of the volatile attributes of an alias that have a value, by name, as they stand in a document: the
// value as v and what is said of it. It is an empty object when there are none.
func volatileEntries(attrs []qAttr, alias string) string {
	if len(attrs) == 0 {
		return "'{}'::jsonb"
	}
	var parts []string
	for _, a := range attrs {
		entry := alias + ".prov->'" + strconv.Itoa(a.No) + "'"
		parts = append(parts, "COALESCE(CASE WHEN "+entry+" IS NOT NULL THEN jsonb_build_object("+QuoteLiteral(a.Name)+", jsonb_build_object('v', to_jsonb("+
			alias+"."+QuoteIdent(StateColumn(a.No))+")) || "+entry+") END, '{}'::jsonb)")
	}
	return strings.Join(parts, " || ")
}

// objectDetail is the function that reads one object with every attribute the grade sees, by name, with the evidence of each value:
// the document of the object with its numbers turned into names. It is a function of the grade (the grade is in what it reads, and
// not an argument that a caller could raise), and it runs with the rights of its owner because the role that calls it has none on the
// tables. It answers null for a number that no object has.
func (g qGen) objectDetail() qObject {
	var branches []string
	for _, t := range g.qm.Types {
		branches = append(branches, g.detailBranch(t))
	}
	body := "WITH o AS (SELECT o.obj_no, o.ref, o.type_no FROM " + g.tbl(TableObject) + " o WHERE o.obj_no = p_id AND " + g.alive("o") + "),\nr AS (" +
		strings.Join(branches, "\n UNION ALL ") + ")\n" +
		"SELECT jsonb_build_object('id', r.obj_no, 'ref', r.ref, 'type', mt.name, 'name', r.name, 'min_grade', " + gradeText("r.min_grade") + ", 'attributes', " +
		"COALESCE((SELECT jsonb_object_agg(ma.name, e.value) FROM jsonb_each(r.doc) e JOIN " + g.tbl(TableModelAttribute) +
		" ma ON ma.type_no = r.type_no AND ma.attr_no = e.key::integer WHERE " + g.grade.notHidden("ma.attr_no") + "), '{}'::jsonb) || " +
		volatileEntries(g.shown(g.qm.Common), "os") + " || r.vol) FROM r JOIN " + g.tbl(TableModelType) + " mt ON mt.type_no = r.type_no LEFT JOIN " +
		g.tbl(TableObjectState) + " os ON os.obj_no = r.obj_no"
	return qObject{What: WhatFunction, Name: FuncObjectDetail, Language: "sql", Sig: "(p_id bigint) RETURNS jsonb", Body: body,
		Comment: "One object with every attribute, by name, as a jsonb document: id, ref, type, name, min_grade and attributes. Each attribute has v the value and g, b, t and r what is said of it. It is null for a number that no object has."}
}

// detailBranch is the row of an object of one type, with the entries of the volatile attributes of the type.
func (g qGen) detailBranch(t qType) string {
	own := g.shown(t.Own)
	vol := "'{}'::jsonb"
	from := "o JOIN " + g.objectRows(t.No, "t") + " ON t.obj_no = o.obj_no AND o.type_no = " + strconv.Itoa(t.No)
	if len(own) > 0 {
		vol = volatileEntries(own, "ts")
		from += " LEFT JOIN " + g.tbl(StateTable(t.No)) + " ts ON ts.obj_no = t.obj_no"
	}
	return "SELECT o.type_no, o.ref, t.obj_no, t.name, t.min_grade, t.doc, " + vol + " AS vol FROM " + from
}
