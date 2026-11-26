package pgschema

import (
	"strconv"
	"strings"
)

// linkView is the view of a link type: the links of the type and the properties of the type that the grade sees as columns.
func (g qGen) linkView(l qLink) qObject {
	sel := []string{"l.src AS src", "l.dst AS dst", "l.grade AS grade", "l.basis AS basis"}
	cols := linkBaseComments()
	for _, a := range g.shown(l.Props) {
		sel = append(sel, docColumn(a, "l"))
		cols = append(cols, qComment{a.Name, attrComment(a)})
	}
	text := l.Desc
	if text == "" {
		text = "The links of the type " + l.Name + "."
	}
	return qObject{What: WhatView, Name: l.View, Comment: text + " It goes from src to dst, so the other way round is dst to src.", Columns: cols,
		Body: "SELECT " + strings.Join(sel, ", ") + " FROM " + g.linkRows("l") + " WHERE l.type_no = " + strconv.Itoa(l.No)}
}

func linkBaseComments() []qComment {
	return []qComment{
		{"src", "The number (id) of the object the link starts at."},
		{"dst", "The number (id) of the object the link ends at."},
		{"grade", "The evidence grade of the link: fact, assumed or unknown."},
		{"basis", "What the link is based on: configured, observed, declared or derived."},
	}
}

// linkAll is the view of every link: the name of the type and the properties by name.
func (g qGen) linkAll() qObject {
	var branches []string
	for _, l := range g.qm.Links {
		var pairs []pair
		for _, a := range g.shown(l.Props) {
			pairs = append(pairs, pair{a.Name, "(l.doc->'" + strconv.Itoa(a.No) + "'->'v')"})
		}
		if len(pairs) > 0 {
			branches = append(branches, "WHEN "+strconv.Itoa(l.No)+" THEN "+objectOf(pairs))
		}
	}
	props := "'{}'::jsonb"
	if len(branches) > 0 {
		props = "CASE l.type_no " + strings.Join(branches, " ") + " ELSE '{}'::jsonb END"
	}
	body := "SELECT lt.name AS type, l.src AS src, l.dst AS dst, l.grade AS grade, l.basis AS basis, " + props + " AS props FROM " + g.linkRows("l") +
		" JOIN " + g.tbl(TableModelLinkType) + " lt ON lt.link_type_no = l.type_no"
	return qObject{What: WhatView, Name: ViewLink, Body: body, Comment: "Every link of the KG. To read one type of link, use the view of the type: it has the properties as columns.",
		Columns: append([]qComment{{"type", "The name of the link type, the same as the name of its view."}}, append(linkBaseComments(),
			qComment{"props", "The properties of the link by name, as a jsonb document."})...)}
}

// objectView is the view of every object, of any type: to find an object by its name or reference, or to read the name of what a
// link reaches.
func (g qGen) objectView() qObject {
	sel := []string{"o.obj_no AS " + colID, "o.ref AS " + colRef, "mt.name AS type", "o.name AS " + colName}
	cols := []qComment{{colID, "The number of the object. It is never given again, so it joins safely."},
		{colRef, "The reference that other systems keep for the object."},
		{"type", "The name of the type of the object, the same as the name of its view."},
		{colName, "The name of the object."}}
	for _, a := range g.shown(g.qm.Common) {
		sel = append(sel, "os."+QuoteIdent(StateColumn(a.No))+" AS "+QuoteIdent(a.Name))
		cols = append(cols, qComment{a.Name, attrComment(a)})
	}
	body := "SELECT " + strings.Join(sel, ", ") + " FROM " + g.tbl(TableObject) + " o JOIN " + g.tbl(TableModelType) + " mt ON mt.type_no = o.type_no" +
		" LEFT JOIN " + g.tbl(TableObjectState) + " os ON os.obj_no = o.obj_no WHERE " + g.alive("o")
	return qObject{What: WhatView, Name: ViewObject, Body: body, Columns: cols,
		Comment: "Every object of the KG, of any type. To read the properties of a type, use the view of the type."}
}

func (g qGen) identifierView() qObject {
	cond := "i.seq_to IS NULL"
	if g.watermark {
		cond = "i.seq_from <= " + g.readWatermark() + " AND (i.seq_to IS NULL OR i.seq_to > " + g.readWatermark() + ")"
	}
	return qObject{What: WhatView, Name: ViewIdentifier, Comment: "The identifiers that objects have in other systems (a serial number, an id in a management system).",
		Body: "SELECT i.obj_no AS id, i.scheme AS scheme, i.value AS value, i.scope AS scope FROM " + g.tbl(TableObjectIdentifier) + " i WHERE " + cond,
		Columns: []qComment{{colID, "The number of the object."}, {"scheme", "The kind of identifier."}, {"value", "The identifier."},
			{"scope", "The border the identifier is valid in; empty when it is valid everywhere."}}}
}

// modelViews are the views of the tables that describe the model.
func (g qGen) modelViews() []qObject {
	plain := func(name, comment, cols, table, where string, comments []qComment) qObject {
		body := "SELECT " + cols + " FROM " + g.tbl(table)
		if where != "" {
			body += " WHERE " + where
		}
		return qObject{What: WhatView, Name: name, Body: body, Comment: comment, Columns: comments}
	}
	return []qObject{
		plain(ViewModelType, "The object types of the model: one row for each view of a type.", "name, model_name, parent, layer, description", TableModelType, "",
			[]qComment{{colName, "The name of the type, the same as the name of its view."}, {"model_name", "The name of the type in the model."},
				{"parent", "The name of the type this one is a kind of; empty when there is none."}, {"layer", "The layer of the model the type is in."},
				{"description", "What the type is."}}),
		plain(ViewModelAttribute, "The attributes of the types and of the link types that this grade sees: one row for each attribute of a type, those it has from the types above it included.",
			"type, link_type, name, value_type, column_type, unit, enum_values, searched, history, sensitivity, description", TableModelAttribute, g.grade.notHidden("attr_no"),
			[]qComment{{"type", "The type that has the attribute; empty for the attribute of a link type."}, {"link_type", "The link type that has the attribute; empty for the attribute of a type."},
				{colName, "The name of the attribute, the same as the name of its column."}, {"value_type", "The value type of the model."},
				{"column_type", "The type of the column in SQL."}, {"unit", "The unit of the value; empty when it has none."},
				{"enum_values", "The values an enumeration may take, as a jsonb array."}, {"searched", "Whether a search by the attribute has an index."},
				{"history", "versioned (every value is kept) or volatile (the current value only)."},
				{"sensitivity", "The sensitivity grade of the attribute."}, {"description", "What the attribute is."}}),
		plain(ViewModelLinkType, "The link types of the model: one row for each view of a link type.", "name, model_name, description", TableModelLinkType, "",
			[]qComment{{colName, "The name of the link type, the same as the name of its view."}, {"model_name", "The name of the link type in the model."},
				{"description", "What the link type is."}}),
		plain(ViewModelLinkRule, "What each link type may join: from a kind of object to a kind of object, with how many.",
			"link_type, from_kind, from_name, to_kind, to_name, cardinality", TableModelLinkRule, "",
			[]qComment{{"link_type", "The name of the link type."}, {"from_kind", "What the link starts at: type, trait or any."},
				{"from_name", "The name of the type (as the name of its view) or of the trait; empty for any."}, {"to_kind", "What the link ends at: type, trait or any."},
				{"to_name", "The name of the type or of the trait; empty for any."}, {"cardinality", "How many: 1:1, 1:N, N:1 or N:M."}}),
	}
}
