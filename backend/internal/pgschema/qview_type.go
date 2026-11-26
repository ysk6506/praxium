package pgschema

import "strings"

// typeByNo finds a type of the model by its number.
func (g qGen) typeByNo(no int) qType {
	for _, t := range g.qm.Types {
		if t.No == no {
			return t
		}
	}
	return qType{}
}

// typeView is the view of an object type: the objects of the type and of every type below it, one column for each property the grade
// sees (ADR-0113). A type with subtypes is a union of the tables of them all, each taken with the columns of the type
// (a subtype has every property of its parent), so an object is in it once.
func (g qGen) typeView(t qType) qObject {
	var branches []string
	for _, c := range t.Subtree {
		branches = append(branches, g.typeBranch(t, g.typeByNo(c)))
	}
	return qObject{What: WhatView, Name: t.View, Body: strings.Join(branches, "\nUNION ALL\n"), Comment: typeComment(t), Columns: g.typeColumnComments(t)}
}

// typeBranch is the rows of one table of the union: the table of the type c, with the columns of the view of t.
func (g qGen) typeBranch(t, c qType) string {
	sel := []string{"t.obj_no AS " + colID, "t.ref AS " + colRef, "t.name AS " + colName}
	for _, a := range g.shown(t.Docs) {
		if a.column() {
			sel = append(sel, docColumn(a, "t"))
		}
	}
	for _, a := range g.shown(g.qm.Common) {
		sel = append(sel, volatileColumns(a, "os")...)
	}
	own := g.shown(t.Own)
	for _, a := range own {
		sel = append(sel, volatileColumns(a, "ts")...)
	}
	sel = append(sel, gradeText("t.min_grade")+" AS "+colMinGrade, g.provenance(t, "t", "os", "ts")+" AS "+colProvenance)
	from := g.objectRows(c.No, "t") + " LEFT JOIN " + g.tbl(TableObjectState) + " os ON os.obj_no = t.obj_no"
	if len(own) > 0 {
		from += " LEFT JOIN " + g.tbl(StateTable(c.No)) + " ts ON ts.obj_no = t.obj_no"
	}
	return "SELECT " + strings.Join(sel, ", ") + " FROM " + from
}

// docColumn is the column of a versioned property, read out of the document of the alias.
func docColumn(a qAttr, alias string) string { return a.docExpr(alias) + " AS " + QuoteIdent(a.Name) }

// volatileColumns are the value of a volatile property and the time the source saw it, from the table of the alias.
func volatileColumns(a qAttr, alias string) []string {
	return []string{alias + "." + QuoteIdent(StateColumn(a.No)) + " AS " + QuoteIdent(a.Name),
		alias + "." + QuoteIdent(StateColumnAt(a.No)) + " AS " + QuoteIdent(a.timeColumn())}
}

// provenance is the document of the provenance of a row: by the name of the property, what is said of its value (g the evidence
// grade, b the basis, t the time the source saw it, r the record it came from), without the value. A property the object has no
// entry for is not in it. The tables of the volatile values are given as the aliases common and own, and without them (a function of a
// past time) the volatile properties are not in it.
func (g qGen) provenance(t qType, doc, common, own string) string {
	var pairs []pair
	for _, a := range g.shown(t.Docs) {
		pairs = append(pairs, pair{a.Name, "(" + entryOf(a, doc+".doc", "", "") + " - 'v')"})
	}
	if common == "" {
		return objectOf(pairs)
	}
	for _, a := range g.shown(g.qm.Common) {
		pairs = append(pairs, pair{a.Name, entryOf(a, "", common, "")})
	}
	for _, a := range g.shown(t.Own) {
		pairs = append(pairs, pair{a.Name, entryOf(a, "", "", own)})
	}
	return objectOf(pairs)
}

func typeComment(t qType) string {
	text := t.Desc
	if text == "" {
		text = "The objects of the type " + t.Name + "."
	}
	if len(t.Subtree) > 1 {
		text += " It has the objects of the types below it too."
	}
	return text
}

func (g qGen) typeColumnComments(t qType) []qComment {
	out := []qComment{
		{colID, "The number of the object. It is never given again, so it joins safely."},
		{colRef, "The reference that other systems keep for the object."},
		{colName, "The name of the object."},
	}
	add := func(a qAttr) {
		out = append(out, qComment{a.Name, attrComment(a)})
		if a.Volatile {
			out = append(out, qComment{a.timeColumn(), "The time the source saw the value of " + a.Name + " (UTC)."})
		}
	}
	for _, a := range g.shown(t.Docs) {
		if a.column() {
			add(a)
		}
	}
	for _, a := range g.shown(g.qm.Common) {
		add(a)
	}
	for _, a := range g.shown(t.Own) {
		add(a)
	}
	return append(out,
		qComment{colMinGrade, "The weakest evidence grade among the attributes of the object: fact, assumed or unknown."},
		qComment{colProvenance, "Where each attribute came from, by the name of the attribute: g the evidence grade, b the basis, t the time the source saw the value (UTC) and r the record it came from."})
}
