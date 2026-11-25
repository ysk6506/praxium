package egress

import (
	"crypto/tls"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The scan of who registers TLS settings with a driver (ADR-0105). RegisterPostgresTLS is public so that pgsource can call it,
// and what it is handed is a function that takes the settings: a package that called it with a function of its own would hold
// the settings. So the only package of the backend that writes its name, or the name of the driver's function that it is given
// (RegisterTLSConfig), is pgsource, in connect.go; egress defines the first. The judgement is hxbCallerViolations, given the
// source files of the backend by their path from the backend folder; the scan of the real folder and the test of the scan use it.

// hxbRegistrationHolders are the files that may write the two names, by path from the backend folder.
var hxbRegistrationHolders = []string{"internal/egress/tls.go", "internal/pgsource/connect.go"}

func hxbCallerViolations(files map[string]*ast.File) []string {
	var out []string
	seen := map[string]int{}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		ast.Inspect(files[name], func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || (id.Name != "RegisterPostgresTLS" && id.Name != "RegisterTLSConfig") {
				return true
			}
			seen[name]++
			if !slices.Contains(hxbRegistrationHolders, name) {
				out = append(out, name+": "+id.Name+" is written outside egress and pgsource, which are the only two that may")
			}
			return true
		})
	}
	for _, at := range hxbRegistrationHolders {
		if seen[at] == 0 {
			out = append(out, at+": the name is not here any more, so the scan looks at nothing")
		}
	}
	return out
}

// hxbBackendFiles parses every source file (not a test) of the backend folder. The parser reads the files, so that this
// test needs no package of the operating system.
func hxbBackendFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = f
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 100 {
		t.Fatalf("the scan of the backend read %d files", len(out))
	}
	return out
}

// [REQ-659] No package of the backend but pgsource registers settings of TLS with a driver.
func TestHxbOnlyPgsourceRegistersTLSSettingsWithTheDriver(t *testing.T) {
	for _, v := range hxbCallerViolations(hxbBackendFiles(t)) {
		t.Error(v)
	}
}

// [REQ-659] The scan sees a package that registers: another package, another file of pgsource, a command, and a use of the
// driver's own function.
func TestHxbTheScanOfTheRegistrationFailsForAnotherPackage(t *testing.T) {
	real := hxbBackendFiles(t)
	fset := token.NewFileSet()
	for name, src := range map[string]string{
		"internal/other/x.go":    "package other\nfunc f() { _, _ = egress.RegisterPostgresTLS(nil, \"k\", \"\", nil) }",
		"internal/pgsource/z.go": "package pgsource\nfunc f() { _, _ = egress.RegisterPostgresTLS(nil, \"k\", \"\", nil) }",
		"cmd/praxium/x.go":       "package main\nfunc f() { _ = pq.RegisterTLSConfig(\"k\", nil) }",
		"internal/egress/zz.go":  "package egress\nfunc f() { _ = RegisterPostgresTLS }",
	} {
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files := maps.Clone(real)
		files[name] = f
		hit := false
		for _, v := range hxbCallerViolations(files) {
			hit = hit || strings.HasPrefix(v, name+":")
		}
		if !hit {
			t.Errorf("%s: the scan does not see it", name)
		}
	}
	// a set of files that has lost one of the two places is not a pass
	files := maps.Clone(real)
	delete(files, "internal/pgsource/connect.go")
	if len(hxbCallerViolations(files)) == 0 {
		t.Error("the scan passes without connect.go")
	}
}

// [REQ-823] The list of the fields of tls.Config that the scan of pgsource names (that unit may not import crypto/tls, so it
// writes the names down) is the list of the fields of the type: this test reads the list from that file with the parser and
// compares it with the fields the type has, so that a field that a later Go version adds fails here until it is added there.
func TestHxbThePgsourceListOfTLSFieldsIsTheListOfTheType(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "pgsource", "hxb_scan_test.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "pgsHxbTLSFields" || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatal("pgsHxbTLSFields is not a list written out")
			}
			for _, el := range lit.Elts {
				b, ok := el.(*ast.BasicLit)
				if !ok {
					t.Fatal("pgsHxbTLSFields holds something that is not a text")
				}
				listed = append(listed, strings.Trim(b.Value, `"`))
			}
		}
	}
	rt := reflect.TypeOf(tls.Config{})
	var actual []string
	for i := 0; i < rt.NumField(); i++ {
		if rt.Field(i).IsExported() {
			actual = append(actual, rt.Field(i).Name)
		}
	}
	slices.Sort(listed)
	slices.Sort(actual)
	if len(listed) == 0 || !slices.Equal(listed, actual) {
		t.Errorf("pgsource names %v and tls.Config has %v", listed, actual)
	}
}
