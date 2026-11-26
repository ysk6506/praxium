package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgconn"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The helpers of the tests of the schema executor. Their names start with sx so that they never meet a helper of
// another test file of this folder.

func sxKG(t *testing.T, n int) graph.KGID {
	t.Helper()
	id, err := kernel.ParseID[graph.KGKind](fmt.Sprintf("kg-%016x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// sxSerial are the tests on an empty database that run alone, after the others of this unit (ADR-0120): the ones that hold the plans
// of the planner or a time to what they expect, and the ones that measure locks or wait for one with a time limit.
var sxSerial = map[string]bool{
	"TestTheIndexOfTheSearchSetIsTheOneTheQueryPlannerChooses":                                   true,
	"TestIngestMakingAndDroppingAKGOfTheStandardModelFitsInTheDefaultLockTable":                  true,
	"TestIngestTheDigestOfAChunkInSQLIsTheDigestTheGoCodeGives":                                  true,
	"TestIngestPuttingASourceToAKGDoesNotWaitForAnotherSourceThatIsBeingReceived":                true,
	"TestQscATypeViewReadsOneObjectByAnIndexAndTheViewOfAParentIsAUnionAllOfTheTablesOfItsTypes": true,
	"TestQscTheLinksOfAnObjectAreReadFromTheIndexAloneByTheirNumber":                             true,
	"TestQscTheTimeToBuildAllTheViewsAgainIsMeasuredByKindOfStatement":                           true,
}

// sxParallel lets the test run beside the other tests of this unit, each on its own empty database, unless it is one of sxSerial.
func sxParallel(t *testing.T) {
	if !sxSerial[t.Name()] {
		pgconn.TestInParallel(t)
	}
}

// sxOpen is a store on a new empty database (not a copy of a template: the tests of the query schema make roles named by the
// token of the installation that the migrations of this database made).
func sxOpen(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	sxParallel(t)
	dsn := NewTestDatabase(t)
	st, err := Open(context.Background(), Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return st, admin
}

func sxInt(t *testing.T, admin *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func sxText(t *testing.T, admin *sql.DB, query string, args ...any) string {
	t.Helper()
	var s string
	if err := admin.QueryRowContext(context.Background(), query, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return s
}

func sxRun(t *testing.T, admin *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := admin.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func sxTables(t *testing.T, admin *sql.DB, schema string) int {
	return sxInt(t, admin, `SELECT count(*) FROM pg_tables WHERE schemaname = $1`, schema)
}

func sxInvalid(t *testing.T, admin *sql.DB, schema string) int {
	return sxInt(t, admin, `SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND NOT i.indisvalid`, schema)
}

// sxSnapshot is the schema as one text: every relation with its kind and file number, and the row of the state.
func sxSnapshot(t *testing.T, admin *sql.DB, schema string) string {
	t.Helper()
	rels := sxText(t, admin, `SELECT COALESCE(string_agg(c.relname || ':' || c.relkind::text || ':' || c.relfilenode::text, ',' ORDER BY c.relname), '')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1`, schema)
	row := sxText(t, admin, `SELECT COALESCE(string_agg(k::text, ';'), '') FROM kg_schema_state k`)
	return rels + "|" + row
}

func sxStandard(t *testing.T, withStats bool) SchemaModel {
	t.Helper()
	reg, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	search, err := pgschema.DefaultSearchSet(reg)
	if err != nil {
		t.Fatal(err)
	}
	return SchemaModel{Registry: reg, Search: search, WithStats: withStats}
}

func sxID[K any](t *testing.T, raw string) kernel.ID[K] {
	t.Helper()
	id, err := kernel.ParseID[K](raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func sxProp(t *testing.T, id, name string, no int, vt string, class model.ChangeClass) model.Property {
	t.Helper()
	return model.Property{ID: sxID[model.PropertyKind](t, id), AttrNo: no, Name: name, ValueType: sxID[model.ValueTypeKind](t, vt), ChangeClass: class}
}

func sxType(t *testing.T, id string, no int, name string, props ...model.Property) model.ObjectType {
	t.Helper()
	return model.ObjectType{
		ID: sxID[model.ObjectTypeKind](t, id), TypeNo: no, Name: name, Namespace: model.NamespaceStd,
		Layer: model.LayerHardware, Status: model.StatusActive, Properties: props,
		Traits: []model.TraitID{sxID[model.TraitKind](t, "tr.common")},
	}
}

// sxSmall is a model of two types (a server with a key, a number, a time, a list and a volatile value, and a
// rack), changed by the words: "type" adds a third type, "serverstate" adds a volatile attribute to the server
// (a column to add), "rackstate" adds one to the rack (a table to make), "kind" changes the value kind of an
// attribute and "notype" takes the rack away.
func sxSmall(t *testing.T, search []pgschema.SearchEntry, words ...string) SchemaModel {
	t.Helper()
	has := map[string]bool{}
	for _, w := range words {
		has[w] = true
	}
	common := model.Trait{
		ID: sxID[model.TraitKind](t, "tr.common"), Name: "Common",
		Properties: []model.Property{
			sxProp(t, "pr.common.name", "name", 1, "vt.string", model.ChangeSlow),
			sxProp(t, "pr.common.life", "life", 4, "vt.string", model.ChangeDynamic),
			sxProp(t, "pr.common.oper", "oper", 7, "vt.string", model.ChangeDynamic),
		},
	}
	cpuKind := "vt.integer"
	if has["kind"] {
		cpuKind = "vt.string"
	}
	serverProps := []model.Property{
		sxProp(t, "pr.server.serial", "serial", 10, "vt.string", model.ChangeStatic),
		sxProp(t, "pr.server.cpus", "cpus", 11, cpuKind, model.ChangeSlow),
		sxProp(t, "pr.server.booted", "booted", 12, "vt.time", model.ChangeSlow),
		sxProp(t, "pr.server.temp", "temp", 13, "vt.number", model.ChangeDynamic),
		sxProp(t, "pr.server.tags", "tags", 14, "vt.list", model.ChangeSlow),
	}
	if has["serverstate"] {
		serverProps = append(serverProps, sxProp(t, "pr.server.fan", "fan", 15, "vt.number", model.ChangeDynamic))
	}
	server := sxType(t, "ot.server", 1, "Server", serverProps...)
	units := sxProp(t, "pr.rack.units", "units", 20, "vt.quantity", model.ChangeSlow)
	units.Unit = "u"
	rackProps := []model.Property{units}
	if has["rackstate"] {
		rackProps = append(rackProps, sxProp(t, "pr.rack.load", "load", 21, "vt.number", model.ChangeDynamic))
	}
	types := []model.ObjectType{server}
	if !has["notype"] {
		types = append(types, sxType(t, "ot.rack", 2, "Rack", rackProps...))
	}
	if has["type"] {
		types = append(types, sxType(t, "ot.pdu", 3, "Pdu", sxProp(t, "pr.pdu.model", "model", 30, "vt.string", model.ChangeStatic)))
	}
	reg, err := model.NewRegistry(model.Definitions{ModelVersion: 1, Traits: []model.Trait{common}, ObjectTypes: types})
	if err != nil {
		t.Fatal(err)
	}
	return SchemaModel{Registry: reg, Search: pgschema.NewSearchSet(search...), WithStats: true}
}

func sxSearch(pairs ...int) []pgschema.SearchEntry {
	var out []pgschema.SearchEntry
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, pgschema.SearchEntry{TypeNo: pairs[i], AttrNo: pairs[i+1]})
	}
	return out
}

// sxRow puts one object into the table of a type, with the document as given.
func sxRow(t *testing.T, admin *sql.DB, schema, table string, no int, doc string) {
	t.Helper()
	sxRun(t, admin, fmt.Sprintf(`INSERT INTO %s (obj_no, ref, name, min_grade, doc, content_hash, row_hash, seq_from)
		VALUES ($1::bigint, 'ref-'||$1::bigint::text, 'n', 0, $2::jsonb, '\x00', '\x00', 1)`, pgschema.Qualified(schema, table)), no, doc)
}
