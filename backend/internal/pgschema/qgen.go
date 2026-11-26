package pgschema

import (
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// qComment is the comment of one column of a view.
type qComment struct{ Column, Text string }

// qObject is one view or function of a query schema, without the schema it goes into: the text that two grades compare to see
// whether the higher one needs one of its own, and what makes the statements for a schema.
type qObject struct {
	What     string // WhatView or WhatFunction
	Name     string
	Sig      string // a function: the arguments and what it returns, as they stand after its name
	Language string // a function: sql or plpgsql
	Body     string // a view: the query; a function: the body
	Comment  string
	Columns  []qComment
}

// key is what two objects are compared by.
func (o qObject) key() string {
	var b strings.Builder
	b.WriteString(o.What + "\x00" + o.Name + "\x00" + o.Sig + "\x00" + o.Language + "\x00" + o.Body + "\x00" + o.Comment)
	for _, c := range o.Columns {
		b.WriteString("\x00" + c.Column + "\x00" + c.Text)
	}
	return b.String()
}

// bodyTag is the quote of the body of a function. A body has only names that were checked and numbers, so it never holds it.
const bodyTag = "$qb$"

// statement is the SQL that makes the object in a schema: the object, what a function needs to be closed to the public, and the
// comments. Several statements go in one text, so a view with its columns' comments is one step.
func (o qObject) statement(schema string) string {
	q := Qualified(schema, o.Name)
	var b strings.Builder
	if o.What == WhatView {
		b.WriteString("CREATE VIEW " + q + " AS " + o.Body + ";\n")
		b.WriteString("COMMENT ON VIEW " + q + " IS " + QuoteLiteral(o.Comment) + ";\n")
		for _, c := range o.Columns {
			b.WriteString("COMMENT ON COLUMN " + q + "." + QuoteIdent(c.Column) + " IS " + QuoteLiteral(c.Text) + ";\n")
		}
		return b.String()
	}
	args := q + o.argTypes()
	b.WriteString("CREATE FUNCTION " + q + o.Sig + " LANGUAGE " + o.Language + " STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS " +
		bodyTag + o.Body + bodyTag + ";\n")
	b.WriteString("REVOKE EXECUTE ON FUNCTION " + args + " FROM PUBLIC;\n")
	b.WriteString("COMMENT ON FUNCTION " + args + " IS " + QuoteLiteral(o.Comment) + ";\n")
	return b.String()
}

// argTypes is the argument list of a function for a statement that names it: the types of the arguments, in parentheses.
func (o qObject) argTypes() string {
	open := strings.Index(o.Sig, "(")
	end := strings.Index(o.Sig, ")")
	args := strings.Split(o.Sig[open+1:end], ",")
	for i, a := range args {
		fields := strings.Fields(a)
		args[i] = fields[len(fields)-1]
	}
	return "(" + strings.Join(args, ", ") + ")"
}

// qGen makes the objects of one query schema set for one grade.
type qGen struct {
	qm        qModel
	grade     gradeView
	store     string // the storage schema of the KG
	watermark bool   // the set for a large apply: it shows the KG as of the read watermark
}

func (g qGen) tbl(name string) string { return Qualified(g.store, name) }

// readWatermark is the read watermark of the KG, as a value that the planner takes once for the whole statement.
func (g qGen) readWatermark() string {
	return "(SELECT readable_through FROM " + g.tbl(TableHead) + ")"
}

// gradeText is the text of an evidence grade from the number the tables keep.
func gradeText(col string) string {
	var b strings.Builder
	b.WriteString("CASE " + col)
	list := model.Grades()
	for i, grade := range list[:len(list)-1] {
		b.WriteString(" WHEN " + strconv.Itoa(i) + " THEN " + QuoteLiteral(string(grade)))
	}
	b.WriteString(" ELSE " + QuoteLiteral(string(list[len(list)-1])) + " END")
	return b.String()
}

// objectRows is where the rows of the objects of a type come from, with the columns obj_no, ref, name, min_grade and doc: the table
// of the type, or for the set of a large apply the rows as of the read watermark.
func (g qGen) objectRows(typeNo int, alias string) string {
	if !g.watermark {
		return g.tbl(TypeTable(typeNo)) + " " + alias
	}
	return "(" + g.objectRowsAt(typeNo, g.readWatermark()) + ") " + alias
}

// objectRowsAt is the rows of the objects of a type as they stood at a change number: the open rows from then on and the rows of the
// history that cover the number. at is an SQL value.
func (g qGen) objectRowsAt(typeNo int, at string) string {
	cols := "t.obj_no, t.ref, t.name, t.min_grade, t.doc"
	hist := "h.obj_no, o.ref, h.name, h.min_grade, h.doc"
	return "SELECT " + cols + " FROM " + g.tbl(TypeTable(typeNo)) + " t WHERE t.seq_from <= " + at +
		" UNION ALL SELECT " + hist + " FROM " + g.tbl(TableObjectHist) + " h JOIN " + g.tbl(TableObject) + " o ON o.obj_no = h.obj_no" +
		" WHERE h.type_no = " + strconv.Itoa(typeNo) + " AND h.seq_from <= " + at + " AND h.seq_to > " + at
}

// linkRows is where the rows of the links come from, with the columns link_no, type_no, src, dst, grade, basis and doc.
func (g qGen) linkRows(alias string) string {
	if !g.watermark {
		return g.tbl(TableLink) + " " + alias
	}
	at := g.readWatermark()
	cols := "link_no, type_no, src, dst, grade, basis, doc"
	return "(SELECT " + cols + " FROM " + g.tbl(TableLink) + " WHERE seq_from <= " + at +
		" UNION ALL SELECT " + cols + " FROM " + g.tbl(TableLinkHist) + " WHERE seq_from <= " + at + " AND seq_to > " + at + ") " + alias
}

// alive is the condition that the registry row of an object (alias o) is of an object that exists.
func (g qGen) alive(o string) string {
	if !g.watermark {
		return o + ".retired_seq IS NULL"
	}
	at := g.readWatermark()
	return o + ".seq_from <= " + at + " AND (" + o + ".retired_seq IS NULL OR " + o + ".retired_seq > " + at + ")"
}

// shown are the properties among attrs that a view of the grade shows.
func (g qGen) shown(attrs []qAttr) []qAttr {
	var out []qAttr
	for _, a := range attrs {
		if g.grade.shows(a.No) {
			out = append(out, a)
		}
	}
	return out
}

// pair is a name and the expression of its entry, for a document that has the properties by name.
type pair struct{ name, expr string }

// objectOf puts the pairs into one jsonb object. A pair whose expression is null is not in it. jsonb_build_object takes at most
// fifty pairs, so a long list is built in parts that are put together.
func objectOf(pairs []pair) string {
	if len(pairs) == 0 {
		return "'{}'::jsonb"
	}
	const part = 20
	var parts []string
	for len(pairs) > 0 {
		n := min(part, len(pairs))
		var args []string
		for _, p := range pairs[:n] {
			args = append(args, QuoteLiteral(p.name)+", "+p.expr)
		}
		parts = append(parts, "jsonb_build_object("+strings.Join(args, ", ")+")")
		pairs = pairs[n:]
	}
	return "jsonb_strip_nulls(" + strings.Join(parts, " || ") + ")"
}

// entryOf is where the entry of an attribute is for each place a value can be kept: the document, the common state, the state of the
// type.
func entryOf(a qAttr, doc, common, own string) string {
	switch {
	case !a.Volatile:
		return "(" + doc + "->'" + strconv.Itoa(a.No) + "')"
	case common != "":
		return "(" + common + ".prov->'" + strconv.Itoa(a.No) + "')"
	}
	return "(" + own + ".prov->'" + strconv.Itoa(a.No) + "')"
}

// attrComment is the comment of a column of a property: what the model says, the unit, the values an enumeration has.
func attrComment(a qAttr) string {
	text := a.Desc
	if text == "" {
		text = "The attribute " + a.Name + "."
	}
	if a.Unit != "" {
		text += " Unit: " + a.Unit + "."
	}
	if len(a.Enum) > 0 {
		text += " Values: " + strings.Join(a.Enum, ", ") + "."
	}
	return text
}
