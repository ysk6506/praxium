package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of what the other tests of the query schema left open (ADR-0113). Their names start with Qsc.

// What an organisation writes in a description is text: a quote, a backslash, a dollar quote, a statement and a new line go into the
// comments and the tables of the model as they were and come out as they were, and nothing else is run.
//
// [REQ-574] [REQ-576]
func TestQscADescriptionWithQuotesBackslashesAndStatementsComesBackAsTheTextItWasAndRunsNothing(t *testing.T) {
	evil := `it's \ "q" $qb$ $$ \' '); DROP SCHEMA public CASCADE; -- ` + "\n" + `E'x' A \n %DOC%`
	st, admin := sxOpen(t)
	m := qscModel(t)
	defs := m.Registry.Definitions()
	for i := range defs.ObjectTypes {
		defs.ObjectTypes[i].Description = evil
		for j := range defs.ObjectTypes[i].Properties {
			defs.ObjectTypes[i].Properties[j].Description = evil
		}
	}
	reg, err := model.NewRegistry(defs)
	if err != nil {
		t.Fatalf("the model with the hostile descriptions is refused: %v", err)
	}
	m.Registry = reg
	rec, err := st.CreateStorageSchema(context.Background(), sxKG(t, 1), m)
	if err != nil {
		t.Fatalf("the KG with the hostile descriptions is not made: %v", err)
	}
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_namespace WHERE nspname = 'public'`); n != 1 {
		t.Fatal("the schema public is gone")
	}
	open := pgschema.QuerySchemaName(rec.KGNo, model.SensitivityOpen, false)
	if got := sxText(t, admin, `SELECT description FROM `+pgschema.QuoteIdent(open)+`.model_type WHERE name = 'machine'`); got != evil {
		t.Errorf("the description of a type came out as %q", got)
	}
	if got := sxText(t, admin, `SELECT description FROM `+pgschema.QuoteIdent(open)+`.model_attribute WHERE name = 'serial'`); got != evil {
		t.Errorf("the description of an attribute came out as %q", got)
	}
	view := sxText(t, admin, `SELECT obj_description(c.oid, 'pg_class') FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = 'machine'`, open)
	if !strings.Contains(view, evil) {
		t.Errorf("the comment of the view came out as %q", view)
	}
	column := sxText(t, admin, `SELECT col_description(c.oid, a.attnum) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid WHERE n.nspname = $1 AND c.relname = 'machine' AND a.attname = 'serial'`, open)
	if !strings.Contains(column, evil) {
		t.Errorf("the comment of the column came out as %q", column)
	}
}

// What PostgreSQL says of the functions and of the roles, asked of PostgreSQL and not read from the text of a statement.
//
// [REQ-573]
func TestQscEveryFunctionOfTheQuerySchemaHasAFixedSearchPathAndNoRightForThePublicAndNoRoleCanLogInOrMakeRoles(t *testing.T) {
	w := qscOpen(t)
	functions := `FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname ~ '^q_[0-9]+_'`
	if n := sxInt(t, w.admin, `SELECT count(*) `+functions); n < 6 {
		t.Fatalf("the query schemas have %d functions", n)
	}
	for _, c := range []struct{ what, cond string }{
		{"has no fixed search path", `NOT COALESCE(p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp'], false)`},
		{"does not run as its owner", `NOT p.prosecdef`},
		{"may be run by the public", `has_function_privilege('public', p.oid, 'EXECUTE')`},
		{"has an acl that gives the public", `EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner))) a WHERE a.grantee = 0 AND a.privilege_type = 'EXECUTE')`},
	} {
		if bad := sxText(t, w.admin, `SELECT COALESCE(string_agg(n.nspname || '.' || p.proname, ', '), '') `+functions+` AND `+c.cond); bad != "" {
			t.Errorf("a function %s: %s", c.what, bad)
		}
	}
	rows, err := w.admin.Query(`SELECT rolname, rolcanlogin, rolcreaterole, rolcreatedb, rolsuper, rolinherit, rolreplication, rolbypassrls, rolconnlimit
		FROM pg_roles WHERE rolname LIKE $1 ORDER BY rolname`, `pq\_`+w.token+`\_`+fmt.Sprint(w.no)+`\_%`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name string
		var login, createRole, createDB, super, inherit, replication, bypass bool
		var limit int
		if err := rows.Scan(&name, &login, &createRole, &createDB, &super, &inherit, &replication, &bypass, &limit); err != nil {
			t.Fatal(err)
		}
		if login || createRole || createDB || super || inherit || replication || bypass {
			t.Errorf("the role %s: login %v, createrole %v, createdb %v, superuser %v, inherit %v, replication %v, bypassrls %v", name, login, createRole, createDB, super, inherit, replication, bypass)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(qscGrades) {
		t.Errorf("the KG has %d roles", seen)
	}
}

// The row at the watermark is read, and the row after it is not, by the views that do not take their rows from a table of a type.
//
// [REQ-627]
func TestQscTheSetForALargeApplyHasTheObjectAtTheWatermarkAndNotTheObjectAfterItInTheViewsOfEveryObject(t *testing.T) {
	w := qscOpen(t)
	w.as(t, model.SensitivityOpen, true, func(tx *sql.Tx) {
		if got := qscStrings(t, tx, `SELECT id::text FROM object ORDER BY id`); strings.Join(got, ",") != "1,2" {
			t.Errorf("the objects at the watermark 3 (object 1 is of the change 3, object 3 of the change 5): %v", got)
		}
		if got := qscStrings(t, tx, `SELECT id::text FROM attribute WHERE name = 'temp' ORDER BY id`); strings.Join(got, ",") != "1" {
			t.Errorf("the volatile attributes at the watermark: %v", got)
		}
		if got := qscStrings(t, tx, `SELECT (object_detail(1) IS NOT NULL)::text || ',' || (object_detail(3) IS NULL)::text`); strings.Join(got, ",") != "true,true" {
			t.Errorf("the detail of the object at the watermark and of the one after it: %v", got)
		}
	})
	w.as(t, model.SensitivityOpen, false, func(tx *sql.Tx) {
		if got := qscStrings(t, tx, `SELECT id::text FROM object ORDER BY id`); strings.Join(got, ",") != "1,2,3" {
			t.Errorf("the objects of the set that is not for a large apply: %v", got)
		}
	})
}

// A KG that the server makes has the query schema, and a server that starts makes it for a KG of an older generator, which had none. The
// other tests of the unit make KGs from a model that has the tables only (StandardModels(false)), so these are what cover the two ways.
//
// [REQ-572] [REQ-685]
func TestQscAKGMadeByTheCatalogHasItsQuerySchemaAndRolesAndAServerThatStartsMakesThemForAKGOfAnOlderGenerator(t *testing.T) {
	ctx := context.Background()
	st, admin := igtOpenStandard(t)
	kg, _ := ssuOldKG(t, st, admin, 61)
	rec, err := st.StorageSchema(ctx, kg)
	if err != nil {
		t.Fatal(err)
	}
	schemas := `SELECT count(*) FROM pg_namespace WHERE nspname LIKE 'q\_'||$1::text||'\_%'`
	// roles belong to the cluster, so the count is the roles of this installation (its token) and not of every database of the cluster
	roles := `SELECT count(*) FROM pg_roles WHERE rolname LIKE 'pq\_'||(SELECT token FROM install_token)||'\_'||$1::text||'\_%'`
	if n := sxInt(t, admin, schemas, rec.KGNo); n != 6 {
		t.Fatalf("the KG that the catalog made has %d query schemas", n)
	}
	if n := sxInt(t, admin, roles, rec.KGNo); n != 3 {
		t.Fatalf("the KG that the catalog made has %d roles", n)
	}
	// a KG of the generator before has no query schema: take it away as it was not there
	for _, name := range pgschema.QuerySchemaNames(rec.KGNo) {
		sxRun(t, admin, `DROP SCHEMA `+pgschema.QuoteIdent(name)+` CASCADE`)
	}
	for _, role := range pgschema.QueryRoleNames(sxText(t, admin, `SELECT token FROM install_token`), rec.KGNo) {
		sxRun(t, admin, `DROP ROLE `+pgschema.QuoteIdent(role))
	}
	if n := sxInt(t, admin, schemas, rec.KGNo) + sxInt(t, admin, roles, rec.KGNo); n != 0 {
		t.Fatalf("the query schema of the KG is not taken away: %d", n)
	}
	done, err := st.AdjustOldSchemas(ctx)
	if err != nil || len(done) != 1 || !done[0].Adjusted || done[0].KG != kg {
		t.Fatalf("the adjustment at the start: %+v, %v", done, err)
	}
	if n := sxInt(t, admin, schemas, rec.KGNo); n != 6 {
		t.Errorf("a server that starts made %d query schemas for the KG of the generator before", n)
	}
	if n := sxInt(t, admin, roles, rec.KGNo); n != 3 {
		t.Errorf("a server that starts made %d roles for the KG of the generator before", n)
	}
	open := pgschema.QuerySchemaName(rec.KGNo, model.SensitivityOpen, false)
	if n := sxInt(t, admin, `SELECT count(*) FROM pg_views WHERE schemaname = $1 AND viewname IN ('object', 'attribute', 'link')`, open); n != 3 {
		t.Errorf("the views of the query schema that the start made: %d of 3", n)
	}
}

// Only the tests make a model that has no query schema: a file of a server that calls StandardModels with anything but true, calls
// TestOnlyModelsWithoutQuerySchema or sets SchemaModel.noQuery would make KGs that no role can read through, and those are the ways
// the plan is cut.
//
// [REQ-626]
func TestQscNoFileOfAServerMakesTheLightModelThatHasNoQuerySchema(t *testing.T) {
	root := filepath.Join("..", "..")
	var calls []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == "testdata" || d.Name() == "vendor" || d.Name() == "node_modules"):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || path == filepath.Join(root, "internal", "pgstore", "testdb.go"):
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if qscWritesNoQuery(path == filepath.Join(root, "internal", "pgstore", "kgstore.go"), n) {
				calls = append(calls, path)
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch f := call.Fun.(type) {
			case *ast.Ident:
				name = f.Name
			case *ast.SelectorExpr:
				name = f.Sel.Name
			}
			if name == "TestOnlyModelsWithoutQuerySchema" {
				calls = append(calls, path)
			}
			if name == "StandardModels" {
				onlyTrue := false
				if len(call.Args) == 1 {
					arg, isIdent := call.Args[0].(*ast.Ident)
					onlyTrue = isIdent && arg.Name == "true"
				}
				if !onlyTrue {
					calls = append(calls, path)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) > 0 {
		t.Errorf("a file that is not a test helper makes the model that has no query schema: %v", calls)
	}
}

// qscWritesNoQuery says whether a node gives SchemaModel.noQuery a value: as a field of a composite literal, in a SchemaModel literal
// without field names, or by an assignment. The one write a file of a server may have is the `noQuery: !withStats` of StandardModels
// (inKGStore), which is true only for StandardModels(false), and the scan above keeps that call to the tests.
func qscWritesNoQuery(inKGStore bool, n ast.Node) bool {
	switch x := n.(type) {
	case *ast.KeyValueExpr:
		if key, ok := x.Key.(*ast.Ident); !ok || key.Name != "noQuery" {
			return false
		}
		not, isNot := x.Value.(*ast.UnaryExpr)
		if !inKGStore || !isNot || not.Op != token.NOT {
			return true
		}
		arg, isIdent := not.X.(*ast.Ident)
		return !isIdent || arg.Name != "withStats"
	case *ast.CompositeLit:
		typ, ok := x.Type.(*ast.Ident)
		if !ok || typ.Name != "SchemaModel" || len(x.Elts) == 0 {
			return false
		}
		_, keyed := x.Elts[0].(*ast.KeyValueExpr)
		return !keyed
	case *ast.AssignStmt:
		for _, lhs := range x.Lhs {
			if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "noQuery" {
				return true
			}
		}
	}
	return false
}
