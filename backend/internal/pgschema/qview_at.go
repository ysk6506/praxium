package pgschema

import "strings"

// atFunction is the function that reads a type as it was at a change number: the same objects as the view, from the open rows and the
// history, without the volatile properties and the state, which have no history. A change number that the KG has not reached, or
// that the history no longer keeps, is an error with the code PXNRD and not an answer that looks whole.
func (g qGen) atFunction(t qType) qObject {
	cols := []string{colID + " bigint", colRef + " text", colName + " text"}
	for _, a := range g.shown(t.Docs) {
		if a.column() {
			cols = append(cols, QuoteIdent(a.Name)+" "+a.ColType)
		}
	}
	cols = append(cols, colMinGrade+" text", colProvenance+" jsonb")
	var branches []string
	for _, c := range t.Subtree {
		branches = append(branches, g.atBranch(t, c))
	}
	body := "#variable_conflict use_column\nDECLARE rt bigint; fl bigint;\nBEGIN\n  SELECT h.readable_through, h.history_floor INTO rt, fl FROM " + g.tbl(TableHead) + " h;\n" +
		"  IF p_seq IS NULL OR p_seq < fl OR p_seq > rt THEN\n    RAISE EXCEPTION 'the change number % is not readable', p_seq USING ERRCODE = '" + CodeNotReadable + "';\n  END IF;\n" +
		"  RETURN QUERY " + strings.Join(branches, "\n  UNION ALL ") + ";\nEND"
	return qObject{What: WhatFunction, Name: AtFunctionName(t.View), Language: "plpgsql", Sig: "(p_seq bigint) RETURNS TABLE(" + strings.Join(cols, ", ") + ")",
		Body: body, Comment: "The objects of the type " + t.Name + " as they were at a change number. The volatile attributes and the state are not in it, because they have no history."}
}

// CodeNotReadable is the SQLSTATE that a function of a past time raises for a change number that cannot be read.
const CodeNotReadable = "PXNRD"

func (g qGen) atBranch(t qType, c int) string {
	sel := []string{"t.obj_no", "t.ref", "t.name"}
	for _, a := range g.shown(t.Docs) {
		if a.column() {
			sel = append(sel, a.docExpr("t"))
		}
	}
	sel = append(sel, gradeText("t.min_grade"), g.provenance(t, "t", "", ""))
	return "SELECT " + strings.Join(sel, ", ") + " FROM (" + g.objectRowsAt(c, "p_seq") + ") t"
}
