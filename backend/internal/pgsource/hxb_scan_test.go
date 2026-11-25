package pgsource

import (
	"fmt"
	"go/ast"
	"go/parser"
	gotok "go/token"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The scan of the way this package reaches the TLS settings of the driver (ADR-0105). Its judgement is the function
// pgsHxbViolations: it takes a set of files and gives what is wrong with it. The scan of the real files and the test of the
// scan itself (the real files with a way around added) use that same function.
//
// What it holds: the settings are never held here. egress.RegisterPostgresTLS is written once, in useTLS of connect.go, and is
// handed pq.RegisterTLSConfig as it is (not wrapped, not called here), which is also the only mention of that function; the
// mode of the connection (SSLMode of the driver) is set to two values only, pq.SSLModeDisable and the mode named for the
// registered settings ("pqgo-" and the key), and none of the words of the other modes is written as a text; reflect and
// unsafe are not imported; and no field of tls.Config is named (a selector of a value, or the key of a literal; the
// selector of a package, such as time.Time, is another thing).

// pgsHxbTLSFields are the fields of tls.Config. This package does not import crypto/tls (not even in a test: the unit is not
// among those that may), so the list is written here; the egress scan, which may import it, takes the names from the type, and a
// field that a later Go version adds is added here.
var pgsHxbTLSFields = []string{
	"Rand", "Time", "Certificates", "NameToCertificate", "GetCertificate", "GetClientCertificate", "GetConfigForClient",
	"VerifyPeerCertificate", "VerifyConnection", "RootCAs", "NextProtos", "ServerName", "ClientAuth", "ClientCAs",
	"InsecureSkipVerify", "CipherSuites", "PreferServerCipherSuites", "SessionTicketsDisabled", "SessionTicketKey",
	"ClientSessionCache", "UnwrapSession", "WrapSession", "MinVersion", "MaxVersion", "CurvePreferences",
	"DynamicRecordSizingDisabled", "Renegotiation", "KeyLogWriter", "EncryptedClientHelloConfigList",
	"EncryptedClientHelloRejectionVerify", "GetEncryptedClientHelloKeys", "EncryptedClientHelloKeys",
}

var pgsHxbBannedImports = []string{"reflect", "unsafe", "runtime/debug", "crypto/tls"}

// pgsHxbModeWords are the modes of the driver that do not verify (or read a file of the home folder), as texts.
var pgsHxbModeWords = []string{"require", "prefer", "allow", "verify-ca", "verify-full", "disable"}

func pgsHxbSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := pgsSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := pgsSources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(data)
	}
	if len(out) < 10 {
		t.Fatalf("only %d source files were read", len(out))
	}
	return out
}

func pgsHxbParse(t *testing.T, sources map[string]string) map[string]*ast.File {
	t.Helper()
	fset := gotok.NewFileSet()
	out := map[string]*ast.File{}
	for name, text := range sources {
		f, err := parser.ParseFile(fset, name, text, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = f
	}
	return out
}

type pgsHxbScan struct {
	fields                 map[string]bool
	out                    []string
	registerTLS, registers int // how often the two names are written
	rightCall              int // calls of RegisterPostgresTLS that are in the one place and given the driver's function
	packages               map[string]bool
}

func (s *pgsHxbScan) pgsHxbReport(file, fn, format string, args ...any) {
	s.out = append(s.out, file+":"+fn+": "+fmt.Sprintf(format, args...))
}

func pgsHxbViolations(files map[string]*ast.File) []string {
	s := &pgsHxbScan{fields: map[string]bool{}}
	for _, name := range pgsHxbTLSFields {
		s.fields[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		f := files[name]
		s.packages = map[string]bool{}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if slices.Contains(pgsHxbBannedImports, p) {
				s.pgsHxbReport(name, "", "imports %s", p)
			}
			s.packages[path.Base(p)] = true
			if imp.Name != nil {
				s.packages[imp.Name.Name] = true
			}
		}
		for _, d := range f.Decls {
			fn := ""
			if fd, ok := d.(*ast.FuncDecl); ok {
				fn = fd.Name.Name
			}
			ast.Inspect(d, func(n ast.Node) bool {
				if n != nil {
					s.pgsHxbCheckNode(name, fn, n)
				}
				return true
			})
		}
	}
	if s.registerTLS != 1 || s.registers != 1 || s.rightCall != 1 {
		s.out = append(s.out, fmt.Sprintf("connect.go:useTLS: RegisterPostgresTLS is written %d times and RegisterTLSConfig %d times, and each has to be once, in the one call %d", s.registerTLS, s.registers, s.rightCall))
	}
	sort.Strings(s.out)
	return s.out
}

func (s *pgsHxbScan) pgsHxbCheckNode(file, fn string, n ast.Node) {
	switch x := n.(type) {
	case *ast.Ident:
		switch x.Name {
		case "RegisterPostgresTLS":
			s.registerTLS++
		case "RegisterTLSConfig":
			s.registers++
		}
	case *ast.CallExpr:
		s.pgsHxbCheckCall(file, fn, x)
	case *ast.SelectorExpr:
		if s.fields[x.Sel.Name] && !s.pgsHxbIsPackage(x.X) {
			s.pgsHxbReport(file, fn, "the field %s of tls.Config is named", x.Sel.Name)
		}
	case *ast.BasicLit:
		if x.Kind == gotok.STRING && slices.Contains(pgsHxbModeWords, strings.Trim(x.Value, "\"`")) {
			s.pgsHxbReport(file, fn, "%s is the word of a mode of the driver", x.Value)
		}
	case *ast.KeyValueExpr:
		s.pgsHxbCheckKeyValue(file, fn, x)
	case *ast.AssignStmt:
		for i, l := range x.Lhs {
			if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "SSLMode" && !pgsHxbRegisteredMode(x.Rhs, i, file, fn) {
				s.pgsHxbReport(file, fn, "SSLMode is set to something that is not pq.SSLMode(\"pqgo-\" + key)")
			}
		}
	}
}

func (s *pgsHxbScan) pgsHxbIsPackage(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && s.packages[id.Name]
}

// call holds the one call of the registration: where it is and what it is given.
func (s *pgsHxbScan) pgsHxbCheckCall(file, fn string, c *ast.CallExpr) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "RegisterPostgresTLS" {
		return
	}
	if file != "connect.go" || fn != "useTLS" {
		s.pgsHxbReport(file, fn, "RegisterPostgresTLS is called outside useTLS of connect.go")
		return
	}
	if len(c.Args) == 0 {
		return
	}
	arg, ok := c.Args[0].(*ast.SelectorExpr)
	if !ok || arg.Sel.Name != "RegisterTLSConfig" {
		s.pgsHxbReport(file, fn, "RegisterPostgresTLS is not given pq.RegisterTLSConfig as it is")
		return
	}
	s.rightCall++
}

// keyValue holds the mode that a literal of the configuration gives: the mode that does not use TLS.
func (s *pgsHxbScan) pgsHxbCheckKeyValue(file, fn string, kv *ast.KeyValueExpr) {
	id, ok := kv.Key.(*ast.Ident)
	if !ok {
		return
	}
	if s.fields[id.Name] {
		s.pgsHxbReport(file, fn, "the field %s of tls.Config is named as the key of a literal", id.Name)
	}
	if id.Name == "SSLMode" {
		if sel, ok := kv.Value.(*ast.SelectorExpr); !ok || sel.Sel.Name != "SSLModeDisable" {
			s.pgsHxbReport(file, fn, "SSLMode is given something other than pq.SSLModeDisable")
		}
	}
}

// pgsHxbRegisteredMode is whether the right side of the assignment to SSLMode is pq.SSLMode("pqgo-" + key), in useTLS.
func pgsHxbRegisteredMode(rhs []ast.Expr, i int, file, fn string) bool {
	if file != "connect.go" || fn != "useTLS" || i >= len(rhs) {
		return false
	}
	conv, ok := rhs[i].(*ast.CallExpr)
	if !ok || len(conv.Args) != 1 {
		return false
	}
	if sel, ok := conv.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "SSLMode" {
		return false
	}
	sum, ok := conv.Args[0].(*ast.BinaryExpr)
	if !ok || sum.Op != gotok.ADD {
		return false
	}
	prefix, ok := sum.X.(*ast.BasicLit)
	key, isKey := sum.Y.(*ast.Ident)
	return ok && isKey && prefix.Value == `"pqgo-"` && key.Name == "key"
}

// [REQ-659] [REQ-660] The package reaches the settings of TLS of the driver in one way only (see the top of this file).
func TestPgsHxbTheOnlyWayToTheTLSSettingsIsTheRegistrationOfEgress(t *testing.T) {
	for _, v := range pgsHxbViolations(pgsHxbParse(t, pgsHxbSources(t))) {
		t.Error(v)
	}
}

var pgsHxbWaysAround = map[string]string{
	"the settings changed by a wrap that reflects": `package pgsource
import (
	"github.com/lib/pq"
	"github.com/ysk6506/praxium/backend/internal/egress"
)
func wrap[T any](register func(string, T) error) func(string, T) error {
	return func(key string, cfg T) error {
		reflect.ValueOf(cfg).Elem().FieldByName("Verify" + "Connection").SetZero()
		return register(key, cfg)
	}
}
func other(key string, pins []string) (func(), error) {
	return egress.RegisterPostgresTLS(wrap(pq.RegisterTLSConfig), key, "", pins)
}`,
	"a wrap without reflect": `package pgsource
import "github.com/ysk6506/praxium/backend/internal/egress"
func other(register func(string, int) error) { _, _ = egress.RegisterPostgresTLS(nil, "k", "", nil) }`,
	"the registration of the driver called here": `package pgsource
import "github.com/lib/pq"
func other() { _ = pq.RegisterTLSConfig("k", nil) }`,
	"the mode require": `package pgsource
func other(cfg interface{}) { cfg.SSLMode = "require" }`,
	"the mode of the home folder": `package pgsource
func other(cfg interface{}) { cfg.SSLMode = "verify-full" }`,
	"a mode that is another value": `package pgsource
func other(cfg interface{}, mode string) { cfg.SSLMode = mode }`,
	"a mode in a literal": `package pgsource
import "github.com/lib/pq"
func other() pq.Config { return pq.Config{SSLMode: pq.SSLModeRequire} }`,
	"a word of a mode": `package pgsource
func other() string { return "verify-ca" }`,
	"the time of the settings": `package pgsource
func other(cfg interface{}) { cfg.Time = nil }`,
	"a field of the settings": `package pgsource
func other(cfg interface{}) { cfg.ServerName = "another.example.test" }`,
	"a field as the key of a literal": `package pgsource
type thing struct{ Time int }
func other() thing { return thing{Time: 1} }`,
	"crypto/tls": `package pgsource
import "crypto/tls"
var _ = tls.VersionTLS10`,
	"unsafe": `package pgsource
import "unsafe"
var _ unsafe.Pointer`,
}

// [REQ-659] The scan finds what it is made to find: the real files give no violation, and the real files with any one of the
// ways around added give one, and so do the real files without the one call.
func TestPgsHxbTheScanFailsForTheWaysAround(t *testing.T) {
	real := pgsHxbSources(t)
	if got := pgsHxbViolations(pgsHxbParse(t, real)); len(got) != 0 {
		t.Fatalf("the real files are not clean: %v", got)
	}
	for _, name := range slices.Sorted(maps.Keys(pgsHxbWaysAround)) {
		sources := maps.Clone(real)
		sources["zzz_around.go"] = pgsHxbWaysAround[name]
		if len(pgsHxbViolations(pgsHxbParse(t, sources))) == 0 {
			t.Errorf("%s: the scan does not see it", name)
		}
	}
	for name, change := range map[string]struct{ from, to string }{
		"the call is gone":     {"egress.RegisterPostgresTLS(pq.RegisterTLSConfig,", "egress.Other(pq.RegisterTLSConfig,"},
		"the call is wrapped":  {"egress.RegisterPostgresTLS(pq.RegisterTLSConfig,", "egress.RegisterPostgresTLS(wrap(pq.RegisterTLSConfig),"},
		"another mode is used": {`pq.SSLMode("pqgo-" + key)`, `pq.SSLMode("pqgo-" + s.st.host)`},
	} {
		sources := maps.Clone(real)
		if !strings.Contains(sources["connect.go"], change.from) {
			t.Fatalf("%s: %q is not in connect.go any more", name, change.from)
		}
		sources["connect.go"] = strings.Replace(sources["connect.go"], change.from, change.to, 1)
		if len(pgsHxbViolations(pgsHxbParse(t, sources))) == 0 {
			t.Errorf("%s: the scan does not see it", name)
		}
	}
}
