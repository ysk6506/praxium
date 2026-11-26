package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the marks of the query schemas and of the server that makes the stale ones again (ADR-0126). The helpers start with kgm
// (kgmake_test.go has the rest of them).

// kgmShape is the query schemas of the KG of number no, by their place in pgschema.QuerySchemaNames: every relation with its kind and
// its columns, every function, and what each role of the KG may use and read there.
func kgmShape(t *testing.T, admin *sql.DB, no int64) string {
	t.Helper()
	roles := pgschema.QueryRoleNames(sxText(t, admin, `SELECT token FROM install_token WHERE id = 1`), no)
	var out []string
	for i, name := range pgschema.QuerySchemaNames(no) {
		rels := sxText(t, admin, `SELECT COALESCE(string_agg(c.relname || ':' || c.relkind::text || ':' || COALESCE((SELECT string_agg(a.attname, ',' ORDER BY a.attnum)
			FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped), ''), ';' ORDER BY c.relname), '')
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1`, name)
		funcs := sxText(t, admin, `SELECT COALESCE(string_agg(p.proname, ',' ORDER BY p.proname), '') FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = $1`, name)
		var rights []string
		for j, role := range roles {
			rights = append(rights, fmt.Sprintf("%d:%s:%d", j, sxText(t, admin, `SELECT has_schema_privilege($1, $2, 'USAGE')::text`, role, name),
				sxInt(t, admin, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $2
					AND has_table_privilege($1, c.oid, 'SELECT')`, role, name)))
		}
		out = append(out, fmt.Sprintf("%d|%s|%s|%s", i, rels, funcs, strings.Join(rights, " ")))
	}
	return strings.Join(out, "\n")
}

// kgmOIDs is the object number of every query schema of the KG of number no: a schema made again has another.
func kgmOIDs(t *testing.T, admin *sql.DB, no int64) string {
	t.Helper()
	return sxText(t, admin, `SELECT COALESCE(string_agg(oid::text, ',' ORDER BY nspname), '') FROM pg_namespace WHERE nspname = ANY (string_to_array($1, ','))`,
		strings.Join(pgschema.QuerySchemaNames(no), ","))
}

// [REQ-921]
func TestKgmAQuerySchemaLeftStaleByAnAdjustmentThatStoppedIsMadeAgainWhenAServerStartsAndIsThenAsIfMadeWhole(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	st, admin, dsn := kgmOpen(t, &m)
	first := kgmKG(t, m, 1)
	if _, err := st.CreateKG(ctx, first); err != nil {
		t.Fatal(err)
	}
	added := qscModelNamed(t, "serial", sxProp(t, "pr.machine.rack", "rack", 22, "vt.string", model.ChangeSlow))
	kgmStopAt(st, 1, nil)
	if _, err := st.AdjustStorageSchema(ctx, first.ID, added); !errors.Is(err, errKgmStop) {
		t.Fatalf("the adjustment that stops: %v", err)
	}
	st.schemaSeen = nil
	if err := st.CheckStorageSchema(ctx, first.ID, added); err != nil {
		t.Fatalf("the storage schema is not adjusted: %v", err)
	}
	// the server that starts holds the new model and makes again the query schemas that are stale, and only those
	m = added
	server := kgmServer(t, dsn, &m)
	steps := kgmCount(server)
	done, err := server.AdjustOldSchemas(ctx)
	if err != nil || len(done) != 1 || !done[0].Adjusted || done[0].KG != first.ID {
		t.Fatalf("the server that starts: %+v, %v", done, err)
	}
	if steps[schemaStepQuery] == 0 || steps[schemaStepQuery]+steps[schemaStepFresh] != len(pgschema.QuerySchemaNames(1)) {
		t.Errorf("the server that starts ran %v", steps)
	}
	second := kgmKG(t, added, 2)
	if _, err := server.CreateKG(ctx, second); err != nil {
		t.Fatal(err)
	}
	if a, b := kgmShape(t, admin, 1), kgmShape(t, admin, 2); a != b {
		t.Errorf("the query schemas made again are not those of a KG made whole:\n%s\n---\n%s", a, b)
	}
	// a server that starts again finds every query schema right and touches none
	oids := kgmOIDs(t, admin, 1)
	clear(steps)
	if done, err := server.AdjustOldSchemas(ctx); err != nil || len(done) != 0 {
		t.Errorf("a second start: %+v, %v", done, err)
	}
	if len(steps) != 0 || kgmOIDs(t, admin, 1) != oids {
		t.Errorf("a second start ran %v", steps)
	}
}

// A rename alone leaves the hash of the model as it was, so the storage schema has nothing to follow; the steps of the query schema
// carry the new name, and the mark tells the server that starts.
//
// [REQ-921]
func TestKgmARenameAloneIsFollowedByTheQuerySchemasWhenAServerStarts(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	_, admin, dsn := kgmOpen(t, &m)
	if _, err := kgmServer(t, dsn, &m).CreateKG(ctx, kgmKG(t, m, 1)); err != nil {
		t.Fatal(err)
	}
	m = qscModelNamed(t, "serial_no")
	done, err := kgmServer(t, dsn, &m).AdjustOldSchemas(ctx)
	if err != nil || len(done) != 1 || !done[0].Adjusted {
		t.Fatalf("the server that starts with the rename: %+v, %v", done, err)
	}
	machine := pgschema.QuoteIdent(pgschema.QuerySchemaName(1, model.SensitivityOpen, false)) + ".machine"
	if cols := sxText(t, admin, `SELECT string_agg(attname, ',' ORDER BY attnum) FROM pg_attribute WHERE attrelid = $1::regclass AND attnum > 0`, machine); !strings.Contains(cols, "serial_no") || strings.Contains(cols, "serial,") {
		t.Errorf("the view of the machine after the rename: %s", cols)
	}
}

// [REQ-921]
func TestKgmServersThatStartTogetherMakeEachStaleQuerySchemaOnce(t *testing.T) {
	ctx := context.Background()
	m := qscModel(t)
	_, admin, dsn := kgmOpen(t, &m)
	servers := []*Store{kgmServer(t, dsn, &m), kgmServer(t, dsn, &m)}
	kg := kgmKG(t, m, 1)
	if _, err := servers[0].CreateKG(ctx, kg); err != nil {
		t.Fatal(err)
	}
	// the query schemas of a KG made before their marks have none
	for _, name := range pgschema.QuerySchemaNames(1) {
		sxRun(t, admin, `COMMENT ON SCHEMA `+pgschema.QuoteIdent(name)+` IS NULL`)
	}
	var mu sync.Mutex
	made := 0
	for _, s := range servers {
		s.schemaSeen = func(_ context.Context, step string, _ *sql.Tx) error {
			mu.Lock()
			defer mu.Unlock()
			if step == schemaStepQuery {
				made++
			}
			return nil
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, len(servers))
	for i, s := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.AdjustOldSchemas(ctx)
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	if want := len(pgschema.QuerySchemaNames(1)); made != want {
		t.Errorf("the servers made %d query schemas, want each of the %d once", made, want)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'q\_1\_%' AND obj_description(oid, 'pg_namespace') LIKE $1`, queryMarkPrefix+"%"); n != made {
		t.Errorf("%d query schemas have their mark", n)
	}
}

// [REQ-921]
func TestKgmTheModelWithoutAQuerySchemaMakesAKGInOneTransactionAndAServerThatStartsMakesNone(t *testing.T) {
	ctx := context.Background()
	st, admin, _ := kgdOpen(t)
	steps := kgmCount(st)
	pgsrcKG(t, st, admin, 1, 0)
	if len(steps) != 1 || steps[schemaStepMake] != 1 {
		t.Errorf("making a KG of the model without a query schema ran %v", steps)
	}
	if done, err := st.AdjustOldSchemas(ctx); err != nil || len(done) != 0 {
		t.Errorf("a server that starts: %+v, %v", done, err)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'q\_%'`) + sxInt(t, admin, `SELECT count(*) FROM kg_making`); n != 0 {
		t.Errorf("%d query schemas or marks", n)
	}
}

// [REQ-921]
func TestKgmTheStepsOfTheQuerySchemaAreCutIntoItsSchemasAndAPlanThatMixesThemIsRefused(t *testing.T) {
	m := qscModel(t)
	m.token = "0123456789ab"
	p, err := buildPlan(m, 7)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := queryParts(p)
	if err != nil {
		t.Fatal(err)
	}
	names := pgschema.QuerySchemaNames(7)
	marks := map[string]bool{}
	steps := 0
	for i, part := range parts {
		if i >= len(names) || part.name != names[i] || !strings.HasPrefix(part.mark, queryMarkPrefix) {
			t.Fatalf("the part %d is %s with the mark %q", i, part.name, part.mark)
		}
		first := part.steps[0]
		if i > 0 && (first.What != pgschema.WhatSchema || !strings.HasPrefix(first.SQL, "DROP SCHEMA")) {
			t.Errorf("the part %s starts with %s %s", part.name, first.What, first.Name)
		}
		if last := part.steps[len(part.steps)-1]; last.What != pgschema.WhatGrant || last.Name != part.name {
			t.Errorf("the part %s ends with %s %s", part.name, last.What, last.Name)
		}
		marks[part.mark] = true
		steps += len(part.steps)
	}
	if len(parts) != len(names) || len(marks) != len(names) || steps != len(p.Query) || parts[0].steps[0].What != pgschema.WhatRows {
		t.Errorf("%d parts, %d marks, %d of %d steps, the first part starts with %s", len(parts), len(marks), steps, len(p.Query), parts[0].steps[0].What)
	}
	again, _ := queryParts(p)
	for i := range again {
		if again[i].mark != parts[i].mark {
			t.Errorf("the mark of %s is not the same the second time", parts[i].name)
		}
	}
	schema := func(name string) pgschema.Step {
		return pgschema.Step{What: pgschema.WhatSchema, Name: name, SQL: "CREATE SCHEMA " + name}
	}
	for _, c := range []struct {
		what  string
		query []pgschema.Step
	}{
		{"no schema", []pgschema.Step{{What: pgschema.WhatRows, Name: "model_type", SQL: "SELECT 1"}}},
		{"a schema twice", []pgschema.Step{schema("q_a"), schema("q_b"), schema("q_a")}},
		{"a step at first", []pgschema.Step{{What: pgschema.WhatView, Name: "v", SQL: "SELECT 1"}}},
	} {
		if _, err := queryParts(pgschema.Plan{Query: c.query}); kernel.CodeOf(err) != kernel.CodeInternal {
			t.Errorf("%s: %v", c.what, err)
		}
	}
}
