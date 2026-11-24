package pgstore

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the name indexes of the tables of the types (ADR-0101) and of where an object is kept. Their names start with tix.

// tixPlan is the plan of a statement as the lines of EXPLAIN, with a transaction that holds the settings.
func tixPlan(t *testing.T, w *v2rWorld, setup []string, query string) string {
	t.Helper()
	ctx := context.Background()
	tx, err := w.admin.BeginTx(ctx, nil)
	noErr(t, "begin", err)
	defer func() { _ = tx.Rollback() }()
	for _, s := range setup {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	rows, err := tx.QueryContext(ctx, `EXPLAIN (COSTS OFF) `+query)
	noErr(t, "explain", err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var line string
		noErr(t, "scan", rows.Scan(&line))
		out = append(out, line)
	}
	noErr(t, "rows", rows.Err())
	return strings.Join(out, "\n")
}

// The exact name and the start of the name with upper and lower case taken as one are answered from the indexes of the table of a type; the
// reference is not (the registry finds an object by its reference), and the table has no other index than its key and these two.
// [REQ-761]
func TestTixTheNameIndexesAnswerTheExactNameAndTheStartOfTheNameWithoutCase(t *testing.T) {
	w := v2rWorldOf(t)
	table := w.a.typeTable(w.serverNo)
	v2rExec(t, w.admin, `INSERT INTO `+table+` (obj_no, ref, name, min_grade, doc, content_hash, row_hash, seq_from)
		SELECT n, 'obj_' || lpad(n::text, 32, '0'), CASE WHEN n % 2 = 0 THEN 'Srv-' ELSE 'srv-' END || n, 0, '{}', '\x00', '\x00', 1 FROM generate_series(1, 30000) AS n`)
	v2rExec(t, w.admin, `ANALYZE `+table)
	name, fold := pgschema.NameIndex(w.serverNo), pgschema.NameIndexFold(w.serverNo)
	cases := map[string]struct{ query, index string }{
		"the exact name": {`SELECT obj_no FROM ` + table + ` WHERE name = 'srv-12345'`, name},
		"the start of the name, as it is written": {`SELECT obj_no FROM ` + table + ` WHERE lower(name) LIKE 'srv-123%'`, fold},
		"the start of the name, in other case":    {`SELECT obj_no FROM ` + table + ` WHERE lower(name) LIKE lower('SRV-123') || '%'`, fold},
		"the name without case":                   {`SELECT obj_no FROM ` + table + ` WHERE lower(name) = lower('SRV-12345')`, fold},
		"the order of the names":                  {`SELECT obj_no FROM ` + table + ` WHERE name > 'srv-29990' ORDER BY name LIMIT 5`, name},
	}
	names := make([]string, 0, len(cases))
	for n := range cases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := cases[n]
		plan := tixPlan(t, w, nil, c.query)
		if !strings.Contains(plan, "Index") || !strings.Contains(plan, c.index) {
			t.Errorf("%s does not use the index %s:\n%s", n, c.index, plan)
		}
	}
	// a statement that takes the start of the name as a parameter uses the index too, when its plan is made for the value
	plan := tixPlan(t, w, []string{`SET LOCAL plan_cache_mode = force_custom_plan`, `PREPARE tix_start(text) AS SELECT obj_no FROM ` + table + ` WHERE lower(name) LIKE $1`},
		`EXECUTE tix_start('srv-123%')`)
	if !strings.Contains(plan, fold) {
		t.Errorf("the start of the name as a parameter:\n%s", plan)
	}
	// the reference is found in the registry, which is where the table of a type is reached from, and not in the table of the type
	if plan := tixPlan(t, w, nil, `SELECT obj_no FROM `+table+` WHERE ref = 'obj_00000000000000000000000000012345'`); strings.Contains(plan, "Index Scan") || strings.Contains(plan, "Index Only") {
		t.Errorf("the table of a type has an index on the reference:\n%s", plan)
	}
	if plan := tixPlan(t, w, nil, `SELECT obj_no FROM `+w.a.t("object")+` WHERE ref = 'obj_00000000000000000000000000012345'`); !strings.Contains(plan, "object_ref") {
		t.Errorf("the registry does not find an object by its reference with its index:\n%s", plan)
	}
	var have []string
	rows, err := w.admin.Query(`SELECT indexname FROM pg_indexes WHERE schemaname = $1 AND tablename = $2 ORDER BY indexname`, w.a.rec.Schema(), pgschema.TypeTable(w.serverNo))
	noErr(t, "indexes", err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var n string
		noErr(t, "scan", rows.Scan(&n))
		if !searchIndexName.MatchString(n) { // the indexes of the search set are the model's, not these
			have = append(have, n)
		}
	}
	want := []string{name, fold, pgschema.TypeTable(w.serverNo) + "_pkey"}
	sort.Strings(want)
	if strings.Join(have, ",") != strings.Join(want, ",") {
		t.Errorf("the indexes of the table of a type besides those of the search set: %v, want %v", have, want)
	}
}

// An object is kept in the table of its own type and in no table of a type above it, and the table of a type that has subtypes is made and
// stays empty. A view of a type with its subtypes (UNION ALL of the tables) therefore has every object once.
// [REQ-764]
func TestTixAnObjectIsInTheTableOfItsOwnTypeOnlyAndAParentTypeHasAnEmptyTable(t *testing.T) {
	r := v2wRigOf(t, graph.Options{})
	r.v2wApply(t, r.fx.World(1, 2, 3))
	site := r.fx.SiteID("S00")
	groupType, ok := r.fx.Reg.ObjectTypeByName("PowerRedundancyGroup")
	if !ok || groupType.Parent.IsZero() {
		t.Fatal("the standard model has no subtype PowerRedundancyGroup")
	}
	group := graph.ObjectUpsert{Type: groupType.ID, Name: "Group A", Attrs: map[int]graph.Attr{647: {Value: int64(1), Grade: "fact", Basis: "configured"}},
		Key: []graph.KeyPart{{Kind: graph.PartContext, Ref: "lt.part_of", Objects: []graph.ObjectID{site}}, {Kind: graph.PartProperty, Ref: "pr.common.name", Value: "A"}}}
	r.v2wApply(t, graph.Batch{Objects: []graph.ObjectUpsert{group}})
	parent, _ := r.fx.Reg.ObjectType(groupType.Parent)
	all := map[int]int{}
	for _, ty := range r.fx.Reg.ObjectTypes() {
		all[ty.TypeNo] = r.v2wCount(t, `SELECT count(*) FROM `+r.v2wTable(pgschema.TypeTable(ty.TypeNo)))
	}
	inRegistry := map[int]int{}
	rows, err := r.admin.Query(`SELECT type_no, count(*) FROM ` + r.v2wTable("object") + ` GROUP BY type_no`)
	noErr(t, "registry", err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var no, n int
		noErr(t, "scan", rows.Scan(&no, &n))
		inRegistry[no] = n
	}
	for no, n := range all {
		if n != inRegistry[no] {
			t.Errorf("the table of the type %d has %d rows and the registry has %d objects of it", no, n, inRegistry[no])
		}
	}
	if all[groupType.TypeNo] != 1 || all[parent.TypeNo] != 0 {
		t.Errorf("the group is in %d rows of its own table and %d of the table of its parent %s", all[groupType.TypeNo], all[parent.TypeNo], parent.Name)
	}
	var tables int
	noErr(t, "table", r.admin.QueryRow(`SELECT count(*) FROM pg_tables WHERE schemaname = $1 AND tablename = $2`, r.a.rec.Schema(), pgschema.TypeTable(parent.TypeNo)).Scan(&tables))
	if tables != 1 {
		t.Errorf("the table of the parent type is made: %d", tables)
	}
}
