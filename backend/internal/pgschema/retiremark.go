package pgschema

import (
	"strconv"
	"strings"
)

// The mark of an object that is held to retire for good (object_retire, ADR-0118): the objects a reference of the run says a feed that
// retires made. The ingestion writes the references (calc_ref) and the apply writes the registry, and neither imports the other, so the
// statements are here (ADR-0128). The apply of a chunk marks the objects of the chunk after it writes the registry, and takes the mark of
// an object it retires away after that, in the same transaction: an object that was retired and comes back by a feed that does not retire
// is not retired again by a mark that stayed (G-2 of the review of ADR-0118).

// retireNames writes the tables of a storage schema and the codes into a statement.
func retireNames(schema, query string) string {
	n := func(v int16) string { return strconv.Itoa(int(v)) }
	return strings.NewReplacer("{retire}", Qualified(schema, TableObjectRetire), "{obj}", Qualified(schema, TableObject), "{ref}", Qualified(schema, TableCalcRef),
		"{diff}", Qualified(schema, TableIngestDiff), "{IDENT}", n(RefRoleIdentity), "{RES}", n(RefResolved), "{KO}", n(DiffKindObject), "{CR}", n(ChangeKindRetired),
	).Replace(query)
}

// retirableOf is the condition that a solved reference of the identity of the run that is retirable points at the object o, read by the
// index of the references by their target and stopping at the first.
const retirableOf = "((SELECT true FROM {ref} r WHERE r.role = {IDENT} AND r.target_no = %s AND r.state = {RES} AND r.retirable LIMIT 1) IS NOT NULL)"

// MarkRetirableSQL marks every object of the registry that a retirable reference of the run points at.
func MarkRetirableSQL(schema string) string {
	return retireNames(schema, "INSERT INTO {retire} (obj_no) SELECT DISTINCT r.target_no FROM {ref} r JOIN {obj} o ON o.obj_no = r.target_no WHERE r.role = {IDENT} "+
		"AND r.state = {RES} AND r.retirable ON CONFLICT DO NOTHING")
}

// MarkRetirableChunkSQL marks the objects of the chunk $1 of the difference that are in the registry and that a retirable reference of the
// run points at.
func MarkRetirableChunkSQL(schema string) string {
	return retireNames(schema, "INSERT INTO {retire} (obj_no) SELECT DISTINCT d.entity_no FROM {diff} d WHERE d.chunk_no = $1 AND d.kind = {KO} "+
		"AND ((SELECT true FROM {obj} o WHERE o.obj_no = d.entity_no) IS NOT NULL) AND "+strings.Replace(retirableOf, "%s", "d.entity_no", 1)+" ON CONFLICT DO NOTHING")
}

// UnmarkRetiredChunkSQL takes the mark away from the objects the chunk $1 of the difference retires.
func UnmarkRetiredChunkSQL(schema string) string {
	return retireNames(schema, "DELETE FROM {retire} WHERE obj_no = ANY(ARRAY(SELECT d.entity_no FROM {diff} d WHERE d.chunk_no = $1 AND d.kind = {KO} AND d.change_kind = {CR}))")
}
