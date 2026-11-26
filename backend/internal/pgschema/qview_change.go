package pgschema

import "strconv"

// versionView is the versions of the KG that were sealed.
func (g qGen) versionView() qObject {
	body := "SELECT v.version_id AS version_id, v.version_no AS version_no, v.sealed_seq AS change_seq, v.sealed_at AS sealed_at, v.seal_kind AS kind FROM " +
		g.tbl(TableVersion) + " v"
	if g.watermark {
		body += " WHERE v.sealed_seq <= " + g.readWatermark()
	}
	return qObject{What: WhatView, Name: ViewKGVersion, Body: body, Comment: "The versions of the KG that were sealed. A version is a change number: read the KG as it was then with the functions of a past time.",
		Columns: []qComment{{"version_id", "The id of the version."}, {"version_no", "The number of the version, rising."},
			{"change_seq", "The change number the version was sealed at."}, {"sealed_at", "The time the version was sealed."}, {"kind", "How the version was sealed."}}}
}

// changeView is the change records: which object or link was added, changed or retired by which change.
func (g qGen) changeView() qObject {
	entity := "CASE e.entity_kind WHEN " + strconv.Itoa(int(EntityKindObject)) + " THEN 'object' ELSE 'link' END"
	change := "CASE e.change_kind WHEN " + strconv.Itoa(int(ChangeKindAdded)) + " THEN 'added' WHEN " + strconv.Itoa(int(ChangeKindChanged)) + " THEN 'changed' ELSE 'retired' END"
	body := "SELECT e.change_seq AS change_seq, b.applied_at AS applied_at, b.kind AS kind, " + entity + " AS entity, e.entity_no AS entity_id, " + change + " AS change FROM " +
		g.tbl(TableChangeEntry) + " e JOIN " + g.tbl(TableChangeBatch) + " b ON b.change_seq = e.change_seq"
	if g.watermark {
		body += " WHERE e.change_seq <= " + g.readWatermark()
	}
	return qObject{What: WhatView, Name: ViewChange, Body: body, Comment: "The changes of the KG: what each change added, changed or retired.",
		Columns: []qComment{{"change_seq", "The change number."}, {"applied_at", "The time the change was applied."}, {"kind", "What kind of change it was."},
			{"entity", "object or link."}, {"entity_id", "The number of the object (the id of its views) or of the link."}, {"change", "added, changed or retired."}}}
}

// valueChangeView is the changes of volatile values: which value of which object changed at which change. The value itself is not in
// it: the KG keeps the current value only.
func (g qGen) valueChangeView() qObject {
	cond := g.grade.notHidden("lc.attr_no")
	if g.watermark {
		cond += " AND lc.change_seq <= " + g.readWatermark()
	}
	body := "SELECT lc.change_seq AS change_seq, b.applied_at AS applied_at, lc.obj_no AS id, ma.name AS name FROM " + g.tbl(TableLightChange) + " lc JOIN " +
		g.tbl(TableChangeBatch) + " b ON b.change_seq = lc.change_seq JOIN " + g.tbl(TableObject) + " o ON o.obj_no = lc.obj_no JOIN " +
		g.tbl(TableModelAttribute) + " ma ON ma.type_no = o.type_no AND ma.attr_no = lc.attr_no WHERE " + cond
	return qObject{What: WhatView, Name: ViewValueChange, Body: body, Comment: "The times a volatile value of an object changed. The value itself is not in it: the KG keeps the current value only.",
		Columns: []qComment{{"change_seq", "The change number."}, {"applied_at", "The time the change was applied."}, {colID, "The number of the object."},
			{colName, "The name of the attribute that changed."}}}
}
