package egress

import (
	"crypto/tls"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// hxbFiles parses the source files of the package that are not tests.
func hxbFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := scanSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := scanSources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, data, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = f
	}
	if len(out) < 8 {
		t.Fatalf("only %d source files were read", len(out))
	}
	return out
}

// hxbWalk calls visit for every node of every file with the name of the function it is in ("" outside any).
func hxbWalk(files map[string]*ast.File, visit func(file, fn string, n ast.Node)) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, decl := range files[name].Decls {
			fn := ""
			if fd, ok := decl.(*ast.FuncDecl); ok {
				fn = fd.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if n != nil {
					visit(name, fn, n)
				}
				return true
			})
		}
	}
}

func hxbIsTLSConfig(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "tls" && sel.Sel.Name == "Config"
}

// [REQ-823] The one way to turn the verification of a certificate off is the settings of the pins, and it has one shape: the
// package makes the settings of TLS in exactly two places (TLSConfig, which verifies, and pinnedTLSConfig in pin.go), each
// with the fields it may have; nothing is assigned to a field of the settings afterwards; the function that skips the
// verification is called by two functions only, directly; the two words are written once; and the package imports no way to
// reach a field behind the type system.
func TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins(t *testing.T) {
	files := hxbFiles(t)
	type literal struct {
		file, fn string
		keys     map[string]ast.Expr
	}
	var literals []literal
	var assigned, newCalls, skipWords, hookWords []string
	var refs, calls []string
	fields := map[string]bool{}
	rt := reflect.TypeOf(tls.Config{})
	for i := 0; i < rt.NumField(); i++ {
		fields[rt.Field(i).Name] = true
	}
	hxbWalk(files, func(file, fn string, n ast.Node) {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if hxbIsTLSConfig(x.Type) {
				keys := map[string]ast.Expr{}
				for _, el := range x.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						t.Errorf("%s: a tls.Config written without its field names", file)
						continue
					}
					if id, ok := kv.Key.(*ast.Ident); ok {
						keys[id.Name] = kv.Value
					}
				}
				literals = append(literals, literal{file, fn, keys})
			}
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if sel, ok := l.(*ast.SelectorExpr); ok && fields[sel.Sel.Name] {
					assigned = append(assigned, file+":"+fn+":"+sel.Sel.Name)
				}
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 && hxbIsTLSConfig(x.Args[0]) {
				newCalls = append(newCalls, file+":"+fn)
			}
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "pinnedTLSConfig" {
				calls = append(calls, file+":"+fn)
			}
		case *ast.Ident:
			switch x.Name {
			case "InsecureSkipVerify":
				skipWords = append(skipWords, file+":"+fn)
			case "VerifyConnection":
				hookWords = append(hookWords, file+":"+fn)
			case "pinnedTLSConfig":
				refs = append(refs, file+":"+fn)
			}
		}
	})
	// 1. the settings of TLS are made in two places, with these fields and no others
	sort.Slice(literals, func(i, j int) bool { return literals[i].file < literals[j].file })
	if len(literals) != 2 {
		t.Fatalf("tls.Config is written in %d places, and has to be in two", len(literals))
	}
	pinLit, tlsLit := literals[0], literals[1] // pin.go, then tls.go
	if tlsLit.file != "tls.go" || tlsLit.fn != "TLSConfig" || pinLit.file != "pin.go" || pinLit.fn != "pinnedTLSConfig" {
		t.Fatalf("the settings are made in %s:%s and %s:%s", tlsLit.file, tlsLit.fn, pinLit.file, pinLit.fn)
	}
	keys := func(l literal) []string {
		var out []string
		for k := range l.keys {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if got := keys(tlsLit); !slices.Equal(got, []string{"MinVersion", "RootCAs"}) {
		t.Errorf("TLSConfig sets %v", got)
	}
	if got := keys(pinLit); !slices.Equal(got, []string{"InsecureSkipVerify", "MinVersion", "ServerName", "VerifyConnection"}) {
		t.Errorf("pinnedTLSConfig sets %v", got)
	}
	if id, ok := pinLit.keys["InsecureSkipVerify"].(*ast.Ident); !ok || id.Name != "true" {
		t.Error("InsecureSkipVerify is not the constant true")
	}
	if _, ok := pinLit.keys["VerifyConnection"].(*ast.FuncLit); !ok {
		t.Error("VerifyConnection is not a function written in place")
	}
	if sel, ok := pinLit.keys["MinVersion"].(*ast.SelectorExpr); !ok || sel.Sel.Name != "VersionTLS12" {
		t.Error("the pinned settings do not begin at TLS 1.2")
	}
	// 2. nothing is assigned to a field of the settings, and no settings are made another way
	if len(assigned) != 0 {
		t.Errorf("a field of tls.Config is assigned in %v", assigned)
	}
	if len(newCalls) != 0 {
		t.Errorf("tls.Config is made with new in %v", newCalls)
	}
	// 3. the two words are written once each, in the function of the pins
	if !slices.Equal(skipWords, []string{"pin.go:pinnedTLSConfig"}) || !slices.Equal(hookWords, []string{"pin.go:pinnedTLSConfig"}) {
		t.Errorf("InsecureSkipVerify is in %v, VerifyConnection in %v", skipWords, hookWords)
	}
	// 4. pinnedTLSConfig (written once, as the name of the function) is called, and only called, by pinnedConfig and probeConfig
	want := []string{"pin.go:pinnedConfig", "pin.go:probeConfig"}
	sort.Strings(calls)
	sort.Strings(refs)
	if !slices.Equal(calls, want) {
		t.Errorf("pinnedTLSConfig is called in %v", calls)
	}
	refs = slices.DeleteFunc(refs, func(s string) bool { return s == "pin.go:pinnedTLSConfig" }) // its own name
	if !slices.Equal(refs, want) {
		t.Errorf("pinnedTLSConfig is named in %v: it may be called and not held", refs)
	}
	// 5. a set of pins is made in one place, and is not empty
	hxbWalk(files, func(file, fn string, n ast.Node) {
		if lit, ok := n.(*ast.CompositeLit); ok {
			if id, ok := lit.Type.(*ast.Ident); ok && id.Name == "pinSet" && fn != "parsePins" {
				t.Errorf("%s:%s writes a pinSet by hand", file, fn)
			}
		}
	})
	// 6. no way to a field behind the type system
	for name, f := range files {
		for _, imp := range f.Imports {
			if path := strings.Trim(imp.Path.Value, `"`); path == "reflect" || path == "unsafe" || path == "runtime/debug" {
				t.Errorf("%s imports %s", name, path)
			}
		}
	}
}

// [REQ-823] The scan of the syntax tree sees what it is made to see: it does fail for a settings that is written in another
// place, a field that is assigned, and a second way to skip the verification.
func TestHxbTheScanOfTheSettingsFailsForTheWaysAround(t *testing.T) {
	for name, src := range map[string]string{
		"a field assigned": `package egress
func weaken(cfg *tls.Config) { cfg.VerifyConnection = nil }`,
		"a literal elsewhere": `package egress
func another() *tls.Config { return &tls.Config{InsecureSkipVerify: true} }`,
		"the function held": `package egress
func hold() { f := pinnedTLSConfig; _ = f }`,
	} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		hit := false
		hxbWalk(map[string]*ast.File{"x.go": f}, func(file, fn string, n ast.Node) {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "VerifyConnection" {
						hit = true
					}
				}
			case *ast.CompositeLit:
				hit = hit || hxbIsTLSConfig(x.Type)
			case *ast.Ident:
				hit = hit || x.Name == "pinnedTLSConfig"
			}
		})
		if !hit {
			t.Errorf("%s: the scan does not see it", name)
		}
	}
}
