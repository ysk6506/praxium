package pgingest

// The statements of the computation of the links (ADR-0122). The ends are read by the index calc_end_key (the side, the link definition,
// the key and the record), a range of them at a time. What the To side has for a key is read from the references that are kept (by the
// index of the keys of the references, for the records the run did not read again) and from the ends of the run, each time by the key, and
// it is counted once for a key: the count is kept in calc_tokey and the later pages find it there.

// rclObjOf is the object the record rec (an alias of the table of the records) is bound to for the feed (an expression): the binding the run
// made, and for a record that the run only reads again (its identification was not redone, so it has no binding) the object its reference
// of the identification says. A record has one object for a feed (the rows of a binding of several keys say the same one); the smallest is
// taken, so that the choice does not hang on the order rows are found in. It is an aggregate of the rows of the record and not the first
// row found: the planner reads the rows of the record by the index, and does not read the table from the start for each end of a page
// (which a small table invites, and which makes the rows read go with the square of the ends).
func rclObjOf(rec, feed string) string {
	return "COALESCE((SELECT min(b.obj_no) FROM {bind} b WHERE b.rid = " + rec + ".rid AND b.feed_no = " + feed + "), " +
		"(SELECT min(y.target_no) FROM {rref} y WHERE " + rec + ".why = {WHY} AND y.src_no = " + rec + ".src_no AND y.dataset_no = " + rec + ".dataset_no AND y.key = " + rec +
		".key AND y.role = {IDENT} AND y.def_no = " + feed + "))"
}

// rclRecordsExtentSQL is the next $2 records after $1 that the run only reads again: their ends are the references that are kept.
const rclRecordsExtentSQL = "SELECT c.rid FROM {rec} c WHERE c.why = {WHY} AND c.rid > $1 ORDER BY c.rid LIMIT $2"

// rclCarrySQL gives the records of the range ($1, $2] that the run only reads again the ends their references say (the run did not make
// them: the identification made the ends of the records it read), so that they are read like the others.
const rclCarrySQL = "INSERT INTO {end} (rid, def_no, side, lookup_hash, held) SELECT c.rid, r.def_no, r.role, r.lookup_hash, false FROM {rec} c " +
	"JOIN {rref} r ON r.src_no = c.src_no AND r.dataset_no = c.dataset_no AND r.key = c.key WHERE c.why = {WHY} AND c.rid > $1 AND c.rid <= $2 " +
	"AND r.role IN ({FROM}, {TO}) AND r.lookup_hash IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {end} e WHERE e.rid = c.rid AND e.side = r.role AND e.def_no = r.def_no)"

// rclRange is the ends of a side of the range of a page: after the place ($1, $2, $3) up to the end of the page ($4, $5, $6).
const rclRange = "e.side = {FROM} AND (e.def_no, e.lookup_hash, e.rid) > ($1, COALESCE($2::bytea, ''::bytea), $3) AND (e.def_no, e.lookup_hash, e.rid) <= ($4, $5::bytea, $6)"

// rclExtentSQL is the next $5 ends of the side $1 after the place ($2, $3, $4).
const rclExtentSQL = "SELECT e.def_no, e.lookup_hash, e.rid FROM {end} e WHERE e.side = $1 AND (e.def_no, e.lookup_hash, e.rid) > ($2, COALESCE($3::bytea, ''::bytea), $4) " +
	"ORDER BY e.def_no, e.lookup_hash, e.rid LIMIT $5"

// rclEndsSQL is the ends of the From side of a page whose records have an object (an end of a record without one makes nothing, and the
// identification said why), with the cells of the record (NULL when they were kept under another declaration than the dataset has now), the
// object and, when the key was counted before, what the To side has for it.
var rclEndsSQL = "WITH {defs}, pg AS (SELECT e.rid, e.def_no, e.lookup_hash, e.held FROM {end} e WHERE " + rclRange + ") " +
	"SELECT pg.rid, c.src_no, c.dataset_no, ds.dataset_id, c.key, CASE WHEN c.decl_rev = ds.decl_rev THEN c.fields END, pg.def_no, pg.lookup_hash, pg.held, " +
	"b.obj_no, COALESCE(fo.ref, fn.ref), t.targets, t.obj_no, COALESCE(tx.ref, tn.ref) " +
	"FROM pg JOIN {rec} c ON c.rid = pg.rid JOIN defs d ON d.src_no = c.src_no AND d.def_no = pg.def_no JOIN {ds} ds ON ds.dataset_no = c.dataset_no " +
	"CROSS JOIN LATERAL (SELECT " + rclObjOf("c", "d.from_feed") + " AS obj_no) b " +
	"LEFT JOIN {obj} fo ON fo.obj_no = b.obj_no LEFT JOIN {newobj} fn ON fn.obj_no = b.obj_no " +
	"LEFT JOIN {tokey} t ON t.src_no = c.src_no AND t.def_no = pg.def_no AND t.lookup_hash = pg.lookup_hash " +
	"LEFT JOIN {obj} tx ON tx.obj_no = t.obj_no LEFT JOIN {newobj} tn ON tn.obj_no = t.obj_no " +
	"WHERE COALESCE(fo.ref, fn.ref) IS NOT NULL ORDER BY pg.def_no, pg.lookup_hash, pg.rid"

// rclCountSQL counts what the To side has for the keys of the ends of a page that no page counted: the objects of the records that are kept
// (the references of the To side that point at an object, except the ones of the records the run read again) and of the ends of the run
// (by the object their record is bound to). A key that has none has the count 0; the count is 0, 1 or 2 for more than one, and the first
// of the objects is given with its id. The rows a key has are read once, whatever the number of the ends that look for it.
var rclCountSQL = "WITH {defs}, pg AS (SELECT e.rid, e.def_no, e.lookup_hash FROM {end} e WHERE " + rclRange + "), " +
	"k AS (SELECT DISTINCT c.src_no, pg.def_no, pg.lookup_hash FROM pg JOIN {rec} c ON c.rid = pg.rid JOIN defs d ON d.src_no = c.src_no AND d.def_no = pg.def_no " +
	"CROSS JOIN LATERAL (SELECT " + rclObjOf("c", "d.from_feed") + " AS obj_no) b " +
	"WHERE b.obj_no IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {tokey} t WHERE t.src_no = c.src_no AND t.def_no = pg.def_no AND t.lookup_hash = pg.lookup_hash)), " +
	"a AS (SELECT k.src_no, k.def_no, k.lookup_hash, CASE WHEN count(x.obj) = 0 THEN 0 WHEN min(x.obj) = max(x.obj) THEN 1 ELSE 2 END AS targets, min(x.obj) AS obj FROM k " +
	"LEFT JOIN LATERAL (" +
	"SELECT r.target_no AS obj FROM {rref} r WHERE r.role = {TO} AND r.def_no = k.def_no AND r.lookup_hash = k.lookup_hash AND r.src_no = k.src_no AND r.target_no IS NOT NULL " +
	"AND NOT EXISTS (SELECT 1 FROM {rec} c2 WHERE c2.src_no = r.src_no AND c2.dataset_no = r.dataset_no AND c2.key = r.key) " +
	"UNION ALL SELECT b2.obj_no FROM {end} e2 JOIN {rec} c3 ON c3.rid = e2.rid AND c3.src_no = k.src_no JOIN defs d2 ON d2.src_no = c3.src_no AND d2.def_no = e2.def_no " +
	"CROSS JOIN LATERAL (SELECT " + rclObjOf("c3", "d2.to_feed") + " AS obj_no) b2 WHERE b2.obj_no IS NOT NULL AND e2.side = {TO} AND e2.def_no = k.def_no AND e2.lookup_hash = k.lookup_hash" +
	") x ON true GROUP BY k.src_no, k.def_no, k.lookup_hash) " +
	"SELECT a.src_no, a.def_no, a.lookup_hash, a.targets, a.obj, COALESCE(o.ref, n.ref) FROM a LEFT JOIN {obj} o ON o.obj_no = a.obj LEFT JOIN {newobj} n ON n.obj_no = a.obj"

// rclConflictRefsSQL is the next $2 links after $1 that a record lost, and rclCellRefsSQL the ones whose record that is used has cells that
// gave no value.
const (
	rclConflictRefsSQL = "SELECT DISTINCT ref FROM {lost} WHERE ref > $1 ORDER BY ref LIMIT $2"
	rclCellRefsSQL     = "SELECT ref FROM {win} WHERE ref > $1 AND jsonb_array_length(left_cells) > 0 ORDER BY ref LIMIT $2"
)

// The statements that write a page.
var (
	// rclToSQL writes the references of the To ends of the range ($1 to $6, with the To side in the range): the object the record is bound to,
	// or the state the identification gave the record when it has none.
	rclToSQL = "WITH {defs} INSERT INTO {cref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) " +
		"SELECT e.rid, {TO}, e.def_no, 0, e.lookup_hash, b.obj_no, CASE WHEN b.obj_no IS NOT NULL THEN {RES} ELSE COALESCE(i.state, {UNRES}) END, false " +
		"FROM {end} e JOIN {rec} c ON c.rid = e.rid JOIN defs d ON d.src_no = c.src_no AND d.def_no = e.def_no " +
		"CROSS JOIN LATERAL (SELECT " + rclObjOf("c", "d.to_feed") + " AS obj_no) b " +
		"LEFT JOIN LATERAL (SELECT x.state FROM {cref} x WHERE x.rid = e.rid AND x.role = {IDENT} AND x.def_no = d.to_feed LIMIT 1) i ON true " +
		"WHERE e.side = {TO} AND (e.def_no, e.lookup_hash, e.rid) > ($1, COALESCE($2::bytea, ''::bytea), $3) AND (e.def_no, e.lookup_hash, e.rid) <= ($4, $5::bytea, $6)"

	rclKeysSQL = "INSERT INTO {tokey} (src_no, def_no, lookup_hash, targets, obj_no) SELECT u.src, u.def, decode(u.hash, 'hex'), u.targets, NULLIF(u.obj, 0) FROM " +
		"unnest($1::text::int[], $2::text::int[], $3::text::text[], $4::text::int[], $5::text::bigint[]) AS u(src, def, hash, targets, obj) ON CONFLICT DO NOTHING"

	rclLinksSQL = "INSERT INTO {stlink} (link_no, ref, def_no, type_no, src, dst, discriminator, grade, basis, doc, source_refs, content_hash, row_hash) " +
		"SELECT u.no, u.ref, u.def, u.typ, u.src, u.dst, '', u.grade, u.basis, u.doc::jsonb, u.refs::jsonb, decode(u.ch, 'hex'), decode(u.rh, 'hex') FROM " +
		"unnest($1::text::bigint[], $2::text::text[], $3::text::int[], $4::text::int[], $5::text::bigint[], $6::text::bigint[], $7::text::text[], $8::text::text[], " +
		"$9::text::text[], $10::text::text[], $11::text::text[], $12::text::text[]) AS u(no, ref, def, typ, src, dst, grade, basis, doc, refs, ch, rh) " +
		"ON CONFLICT (link_no) DO UPDATE SET def_no = EXCLUDED.def_no, type_no = EXCLUDED.type_no, src = EXCLUDED.src, dst = EXCLUDED.dst, grade = EXCLUDED.grade, " +
		"basis = EXCLUDED.basis, doc = EXCLUDED.doc, source_refs = EXCLUDED.source_refs, content_hash = EXCLUDED.content_hash, row_hash = EXCLUDED.row_hash RETURNING (xmax = 0)"

	// rclWinSQL keeps the record that is used for each link (the holder a later page compares with), and rclLostSQL the ones that lost.
	rclWinSQL = "INSERT INTO {win} (ref, rid, def_no, src_no, dataset_no, key, held, sig, shown, link_name, def_id, dataset_id, link_no, left_cells) " +
		"SELECT u.ref, u.rid, u.def, u.src, u.ds, u.key, u.held, u.sig, u.shown, u.lname, u.defid, u.dsid, u.no, u.cells::jsonb FROM " +
		"unnest($1::text::text[], $2::text::bigint[], $3::text::int[], $4::text::int[], $5::text::int[], $6::text::text[], $7::text::boolean[], $8::text::text[], $9::text::text[], " +
		"$10::text::text[], $11::text::text[], $12::text::text[], $13::text::bigint[], $14::text::text[]) AS u(ref, rid, def, src, ds, key, held, sig, shown, lname, defid, dsid, no, cells) " +
		"ON CONFLICT (ref) DO UPDATE SET rid = EXCLUDED.rid, def_no = EXCLUDED.def_no, src_no = EXCLUDED.src_no, dataset_no = EXCLUDED.dataset_no, key = EXCLUDED.key, " +
		"held = EXCLUDED.held, sig = EXCLUDED.sig, shown = EXCLUDED.shown, link_name = EXCLUDED.link_name, def_id = EXCLUDED.def_id, dataset_id = EXCLUDED.dataset_id, " +
		"link_no = EXCLUDED.link_no, left_cells = EXCLUDED.left_cells"

	rclLostSQL = "INSERT INTO {lost} (ref, rid, def_no, src_no, dataset_no, key, held, sig, shown, link_name, def_id, dataset_id) " +
		"SELECT u.ref, u.rid, u.def, u.src, u.ds, u.key, u.held, u.sig, u.shown, u.lname, u.defid, u.dsid FROM " +
		"unnest($1::text::text[], $2::text::bigint[], $3::text::int[], $4::text::int[], $5::text::int[], $6::text::text[], $7::text::boolean[], $8::text::text[], $9::text::text[], " +
		"$10::text::text[], $11::text::text[], $12::text::text[]) AS u(ref, rid, def, src, ds, key, held, sig, shown, lname, defid, dsid)"

	// rclFromRefsSQL writes the references of the ends of the From side of a page: the link the end made (when its record is the one used)
	// or none, with the state that says why.
	rclFromRefsSQL = "INSERT INTO {cref} (rid, role, def_no, sub, lookup_hash, target_no, state, retirable) SELECT u.rid, {FROM}, u.def, 0, decode(u.hash, 'hex'), NULLIF(u.link, 0), u.state, false FROM " +
		"unnest($1::text::bigint[], $2::text::int[], $3::text::text[], $4::text::bigint[], $5::text::int[]) AS u(rid, def, hash, link, state)"

	// rclFlipSQL makes the references of the records that were used for a link and are not any more not solved.
	rclFlipSQL = "UPDATE {cref} x SET target_no = NULL, state = u.state FROM unnest($1::text::bigint[], $2::text::int[], $3::text::int[]) AS u(rid, def, state) " +
		"WHERE x.rid = u.rid AND x.role = {FROM} AND x.def_no = u.def"

	// rclHoldersSQL is the records that are used for the links of a list, and rclRegistrySQL the numbers the open links of the KG have.
	rclHoldersSQL  = "SELECT ref, rid, def_no, src_no, dataset_no, key, held, sig, shown, link_name, def_id, dataset_id, link_no FROM {win} WHERE ref = ANY($1::text::text[])"
	rclRegistrySQL = "SELECT ref, link_no FROM {link} WHERE ref = ANY($1::text::text[])"
	rclNumbersSQL  = "SELECT n FROM public.praxium_next_link_nos($1::int) AS t(n) ORDER BY n"

	// rclConflictsSQL (kind source_conflict, the kind of a value that lost to another; the reason begins with "the link ", which tells it
	// from the conflicts of the objects) says, of the records that made a link (the one used and the ones that lost, in the order that decides), that a record
	// whose next one made the link with other attributes made it with other attributes (the attributes are compared as the old flow did, and
	// a record that a rule filtered out is not told). The links are the ones of the range ($1, $2].
	rclConflictsSQL = "INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT y.rid, y.def_no, '', 'source_conflict', 'the link ' || y.link_name || ' from ' || COALESCE(a.name, ao.name, '') || " +
		"' to ' || COALESCE(b.name, bo.name, '') || ' is made again by ' || y.n_def_id || ' (dataset ' || y.n_dataset_id || ', row ' || replace(y.n_key, chr(31), '|') || ') with other attributes: ' || " +
		"y.shown || ' here, ' || y.n_shown || ' there; the later one is used and the attributes are not merged' FROM (" +
		"SELECT x.ref, x.rid, x.def_no, x.held, x.sig, x.shown, x.link_name, lead(x.held) OVER w AS n_held, lead(x.sig) OVER w AS n_sig, lead(x.shown) OVER w AS n_shown, " +
		"lead(x.def_id) OVER w AS n_def_id, lead(x.dataset_id) OVER w AS n_dataset_id, lead(x.key) OVER w AS n_key FROM (" +
		"SELECT ref, rid, def_no, src_no, dataset_no, key, held, sig, shown, link_name, def_id, dataset_id FROM {win} WHERE ref > $1 AND ref <= $2 " +
		"UNION ALL SELECT ref, rid, def_no, src_no, dataset_no, key, held, sig, shown, link_name, def_id, dataset_id FROM {lost} WHERE ref > $1 AND ref <= $2) x " +
		"WINDOW w AS (PARTITION BY x.ref ORDER BY x.src_no, x.def_no, x.held, x.dataset_no, x.key COLLATE \"C\")) y " +
		"JOIN {win} w ON w.ref = y.ref JOIN {stlink} sl ON sl.link_no = w.link_no " +
		"LEFT JOIN {stobj} a ON a.obj_no = sl.src LEFT JOIN {obj} ao ON ao.obj_no = sl.src LEFT JOIN {stobj} b ON b.obj_no = sl.dst LEFT JOIN {obj} bo ON bo.obj_no = sl.dst " +
		"WHERE y.n_held IS NOT NULL AND NOT y.held AND NOT y.n_held AND y.sig <> y.n_sig"

	// rclCellsSQL says, for the links of the range ($1, $2], the cells of the record that is used that gave no value (the record's cells are
	// the ones a link's attributes are read from).
	rclCellsSQL = "INSERT INTO {prob} (rid, def_no, field, kind, reason) SELECT w.rid, w.def_no, '', 'invalid_value', l.reason FROM {win} w " +
		"CROSS JOIN LATERAL jsonb_array_elements_text(w.left_cells) AS l(reason) WHERE w.ref > $1 AND w.ref <= $2 AND NOT w.held"
)
