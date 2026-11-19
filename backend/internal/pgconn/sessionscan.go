package pgconn

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A connection pool may hand a different connection to every statement, so nothing may stay behind on a session
// (postgres-ha.md, rule 6): no LISTEN or NOTIFY, temporary tables, SET that is not SET LOCAL, PREPARE, session
// advisory locks. SQL keywords are looked for in capitals, as the SQL of this repository is written. Writes always go through a transaction (Pool.Tx), because the driver may send the same write
// again when a connection breaks and the pool tries another one.
var sessionStatements = []struct {
	what string
	re   *regexp.Regexp
}{
	{"listening", regexp.MustCompile(`(^|;)\s*LISTEN\b`)},
	{"notifying", regexp.MustCompile(`(^|;)\s*NOTIFY\b|(?i)\bpg_notify\s*\(`)},
	{"a temporary table", regexp.MustCompile(`(?i)\bCREATE\s+(GLOBAL\s+|LOCAL\s+)?TEMP(ORARY)?\b`)},
	{"a session setting", regexp.MustCompile(`(^|;)\s*SET\s+(SESSION\s+)?[a-z_]`)},
	{"set_config for the session", regexp.MustCompile(`(?i)\bset_config\s*\([^)]*,\s*false\s*\)`)},
	{"a prepared statement", regexp.MustCompile(`(^|;)\s*PREPARE\b`)},
	{"a session advisory lock", regexp.MustCompile(`(?i)\bpg_(try_)?advisory_lock(_shared)?\s*\(`)},
}

var setLocal = regexp.MustCompile(`(^|;)\s*SET\s+LOCAL\b`)

// execExempt are the files that may run statements on something that is not a transaction: the helper that makes
// test databases, and the one place that runs a statement PostgreSQL does not allow in a transaction (CREATE INDEX
// CONCURRENTLY; Pool.ExecOutsideTx). Neither is scanned.
var execExempt = map[string]bool{"testdb.go": true, "online_exec.go": true}

// SessionStateViolations reads the Go files of fsys (the files of a PostgreSQL adapter, tests left out) and lists what
// breaks the rules above: an SQL string that leaves something on the session, a statement prepared on a pool or a
// connection, and an Exec on anything but a transaction (a variable named tx). The test helper of the connection
// base that makes databases (testdb.go) and the one that runs a statement outside a transaction (online_exec.go) are the
// files that may run statements on a pool or a connection.
func SessionStateViolations(fsys fs.FS) ([]string, error) {
	names, err := fs.Glob(fsys, "*.go")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") || execExempt[name] {
			continue
		}
		src, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if msg := violationOf(n); msg != "" {
				out = append(out, fmt.Sprintf("%s: %s", fset.Position(n.Pos()), msg))
			}
			return true
		})
	}
	sort.Strings(out)
	return out, nil
}

func violationOf(n ast.Node) string {
	switch v := n.(type) {
	case *ast.BasicLit:
		return literalViolation(v)
	case *ast.CallExpr:
		return callViolation(v)
	}
	return ""
}

// literalViolation names the session statement a string literal holds, or "".
func literalViolation(v *ast.BasicLit) string {
	if v.Kind != token.STRING {
		return ""
	}
	text, err := strconv.Unquote(v.Value)
	if err != nil {
		return ""
	}
	for _, st := range sessionStatements {
		if st.re.MatchString(text) && !(st.what == "a session setting" && setLocal.MatchString(text)) {
			return "SQL with " + st.what
		}
	}
	return ""
}

// callViolation names the forbidden call a call expression makes, or "".
func callViolation(v *ast.CallExpr) string {
	sel, ok := v.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	switch sel.Sel.Name {
	case "Prepare", "PrepareContext":
		return "a prepared statement"
	case "Exec", "ExecContext":
		if !isTx(sel.X) {
			return "an Exec that does not run on a transaction (use Pool.Tx)"
		}
	}
	return ""
}

// isTx says whether an expression is a variable or a field named tx.
func isTx(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return v.Name == "tx"
	case *ast.SelectorExpr:
		return v.Sel.Name == "tx"
	}
	return false
}
