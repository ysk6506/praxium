package egress

import (
	"crypto/tls"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The scan of the settings of TLS (ADR-0105). Its judgement is one function, hxbViolations, which takes a set of files and
// gives the list of what is wrong with it. The scan of the code of the package and the test of the scan itself use that same
// function: the second one adds a file of a way around to the real files and wants a violation of that file.
//
// What the scan holds is the whole of the way the verification of a certificate is turned off:
//   - crypto/tls is imported by its own name (no other name, no dot), and the files import only the list hxbAllowedImports
//     (so nothing reaches a field behind the type system: no reflect, unsafe, runtime/debug, encoding/json, encoding/gob,
//     encoding/xml);
//   - the type tls.Config is spelled only in the functions that make or pass the settings, and there only as a pointer or as
//     the type of a literal: no declaration of a type or a variable of it, no other name for it, no new(tls.Config);
//   - the settings are made by two literals, each in its own function and with the fields it may have;
//   - no field of the settings is assigned, incremented, ranged into or has its address taken (a pointer to a field is a way
//     to set it without an assignment), whatever the type or the name the settings are held by, and no whole of the settings
//     is assigned through a pointer;
//   - the two words that turn the verification off are written once each, in the function of the pins;
//   - the functions that make settings are called only by the functions that may call them, directly and not held, and the
//     one that makes the pinned settings is given the pins of the connection and nothing else.
//
// The scan reads names, not types: the names of the fields of tls.Config are taken from the type itself, and a field of
// another type that has one of them is a hit too, which is the safe side (there is none in the package). A scan by types
// would see the same hits, since the type can be spelled in no place that the first rule does not list.

// hxbSource is the text of the source files of the package that are not tests, by file name.
func hxbSource(t *testing.T) map[string]string {
	t.Helper()
	entries, err := scanSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := scanSources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(data)
	}
	if len(out) < 8 {
		t.Fatalf("only %d source files were read", len(out))
	}
	return out
}

// hxbParse parses files given as text.
func hxbParse(t *testing.T, sources map[string]string) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
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

// hxbSite is a function of a file ("" is outside any function).
type hxbSite struct{ file, fn string }

func hxbName(s hxbSite) string { return s.file + ":" + s.fn }

// The places the scan allows. Everything else that the scan sees is a violation.
var (
	// hxbTypeSites are the functions that may spell the type tls.Config (as a pointer, or as the type of a literal).
	hxbTypeSites = []string{"tls.go:tlsConfig", "tls.go:RegisterPostgresTLS", "tls.go:connectionTLS", "pin.go:pinnedConfig", "pin.go:probeConfig", "pin.go:pinnedTLSConfig"}
	// hxbLiteralSites are the two places that make settings, with the fields each may write.
	hxbLiteralFields = map[string][]string{
		"tls.go:tlsConfig":       {"MinVersion", "RootCAs"},
		"pin.go:pinnedTLSConfig": {"InsecureSkipVerify", "MinVersion", "ServerName", "VerifyConnection"},
	}
	// hxbCallers are who may use a function of the package that makes or holds settings (each use is a call, and each place
	// is one use).
	hxbCallers = map[string][]string{
		"pinnedTLSConfig": {"pin.go:pinnedConfig", "pin.go:probeConfig"},
		"pinnedConfig":    {"tls.go:connectionTLS"},
		"probeConfig":     {"probe.go:probe"},
		"connectionTLS":   {"client.go:NewClient", "tls.go:RegisterPostgresTLS"},
		"tlsConfig":       {"tls.go:connectionTLS"},
		"parsePins":       {"pin.go:pinnedConfig"},
	}
	hxbBannedImports = []string{"reflect", "unsafe", "runtime/debug"}
	// hxbAllowedImports is the list of the imports of the package as it is written now. A file of the package imports
	// nothing else: an import that reaches a field by its name (encoding/json, encoding/gob, encoding/xml) is a way round too.
	hxbAllowedImports = []string{
		"context", "crypto/sha256", "crypto/subtle", "crypto/tls", "crypto/x509", "encoding/base64", "encoding/binary",
		"errors", "github.com/ysk6506/praxium/backend/internal/kernel", "io", "net", "net/http", "net/netip", "net/url",
		"strconv", "strings", "sync", "sync/atomic", "time", "unicode", "unicode/utf8",
	}
	// hxbPinOnly are the words written once, in pinnedTLSConfig.
	hxbPinOnly = []string{"InsecureSkipVerify", "VerifyConnection"}
)

type hxbUse struct {
	site hxbSite
	call *ast.CallExpr // nil when the name is not called
}

type hxbScan struct {
	fields   map[string]bool
	out      []string
	literals map[string]bool
	uses     map[string][]hxbUse
	words    map[string][]string
	callee   map[*ast.Ident]*ast.CallExpr
	decl     map[*ast.Ident]bool
}

func (s *hxbScan) hxbReport(site hxbSite, format string, args ...any) {
	s.out = append(s.out, hxbName(site)+": "+strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// hxbViolations is the judgement of the scan: what is wrong with a set of files of the package, sorted. A set that is right
// gives none. It does not look at the names of the files but for the places it knows by file and function.
func hxbViolations(files map[string]*ast.File) []string {
	s := &hxbScan{
		fields: map[string]bool{}, literals: map[string]bool{}, uses: map[string][]hxbUse{}, words: map[string][]string{},
		callee: map[*ast.Ident]*ast.CallExpr{}, decl: map[*ast.Ident]bool{},
	}
	rt := reflect.TypeOf(tls.Config{})
	for i := 0; i < rt.NumField(); i++ {
		s.fields[rt.Field(i).Name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		f := files[name]
		s.hxbCheckImports(name, f)
		for _, d := range f.Decls {
			fn := ""
			if fd, ok := d.(*ast.FuncDecl); ok {
				fn = fd.Name.Name
				s.decl[fd.Name] = true
			}
			var stack []ast.Node
			ast.Inspect(d, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, n)
				s.hxbCheckNode(hxbSite{name, fn}, stack)
				return true
			})
		}
	}
	s.hxbCheckTotals()
	sort.Strings(s.out)
	return s.out
}

func (s *hxbScan) hxbCheckImports(name string, f *ast.File) {
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path == "crypto/tls" && imp.Name != nil {
			s.hxbReport(hxbSite{name, ""}, "crypto/tls is imported as %q; it is imported by its own name only", imp.Name.Name)
		}
		if slices.Contains(hxbBannedImports, path) {
			s.hxbReport(hxbSite{name, ""}, "imports %s, a way to a field behind the type system", path)
		} else if !slices.Contains(hxbAllowedImports, path) {
			s.hxbReport(hxbSite{name, ""}, "imports %s, which is not in the list of the imports of the package", path)
		}
	}
}

// node applies the rules to the node at the top of the stack.
func (s *hxbScan) hxbCheckNode(site hxbSite, stack []ast.Node) {
	switch x := stack[len(stack)-1].(type) {
	case *ast.SelectorExpr:
		if hxbIsTLSConfig(x) {
			s.hxbCheckTypeSpelled(site, stack)
		}
	case *ast.CompositeLit:
		s.hxbCheckLiteral(site, x)
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok {
			s.callee[id] = x
		}
	case *ast.AssignStmt:
		for _, l := range x.Lhs {
			s.hxbCheckAssigned(site, l)
		}
	case *ast.IncDecStmt:
		s.hxbCheckAssigned(site, x.X)
	case *ast.RangeStmt:
		for _, e := range []ast.Expr{x.Key, x.Value} {
			if e != nil {
				s.hxbCheckAssigned(site, e)
			}
		}
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if field := s.hxbFindField(x.X); field != "" {
				s.hxbReport(site, "the address of the field %s of tls.Config is taken", field)
			}
		}
	case *ast.Ident:
		s.hxbCheckIdent(site, x)
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

// typeSpelled holds the places the type tls.Config is written: in the list of the functions, as a pointer or as the type
// of a literal, and in no declaration of a type or of a variable, no conversion and no new.
func (s *hxbScan) hxbCheckTypeSpelled(site hxbSite, stack []ast.Node) {
	if !slices.Contains(hxbTypeSites, hxbName(site)) {
		s.hxbReport(site, "tls.Config is written outside the functions that make or pass the settings")
		return
	}
	if len(stack) < 2 {
		return
	}
	switch p := stack[len(stack)-2].(type) {
	case *ast.StarExpr:
	case *ast.CompositeLit:
		_ = p
	default:
		s.hxbReport(site, "tls.Config is written as %T: it is allowed as a pointer and as the type of a literal only", p)
	}
}

func (s *hxbScan) hxbCheckLiteral(site hxbSite, lit *ast.CompositeLit) {
	if id, ok := lit.Type.(*ast.Ident); ok && id.Name == "pinSet" && site.fn != "parsePins" {
		s.hxbReport(site, "a pinSet is written by hand")
	}
	if !hxbIsTLSConfig(lit.Type) {
		return
	}
	want, ok := hxbLiteralFields[hxbName(site)]
	if !ok {
		s.hxbReport(site, "a tls.Config is written here: the settings are made in two places only")
		return
	}
	s.literals[hxbName(site)] = true
	var got []string
	for _, el := range lit.Elts {
		kv, isKV := el.(*ast.KeyValueExpr)
		id, isID := ast.Expr(nil), false
		if isKV {
			id = kv.Key
			_, isID = kv.Key.(*ast.Ident)
		}
		if !isKV || !isID {
			s.hxbReport(site, "a tls.Config is written without its field names")
			continue
		}
		got = append(got, id.(*ast.Ident).Name)
		s.hxbCheckFieldValue(site, kv)
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		s.hxbReport(site, "the settings have the fields %v, and may have %v", got, want)
	}
}

// fieldValue holds the value of the fields that decide the strength of the settings.
func (s *hxbScan) hxbCheckFieldValue(site hxbSite, kv *ast.KeyValueExpr) {
	name := kv.Key.(*ast.Ident).Name
	switch name {
	case "InsecureSkipVerify":
		if id, ok := kv.Value.(*ast.Ident); !ok || id.Name != "true" {
			s.hxbReport(site, "InsecureSkipVerify is not the constant true")
		}
	case "VerifyConnection":
		if _, ok := kv.Value.(*ast.FuncLit); !ok {
			s.hxbReport(site, "VerifyConnection is not a function written in place")
		}
	case "MinVersion":
		if sel, ok := kv.Value.(*ast.SelectorExpr); !ok || sel.Sel.Name != "VersionTLS12" {
			s.hxbReport(site, "the settings do not begin at TLS 1.2")
		}
	}
}

// assigned is a target of an assignment: no field of the settings may be in it, and no whole of them behind a pointer.
func (s *hxbScan) hxbCheckAssigned(site hxbSite, target ast.Expr) {
	if field := s.hxbFindField(target); field != "" {
		s.hxbReport(site, "the field %s of tls.Config is assigned", field)
		return
	}
	if star, ok := ast.Unparen(target).(*ast.StarExpr); ok {
		s.hxbReport(site, "a value behind a pointer is assigned as a whole (%T), which can replace the settings", star.X)
	}
}

// fieldIn gives the name of the field of tls.Config that an expression reaches through a chain of parentheses, pointers,
// indexes, slices and selectors ("" when there is none): `cfg.F`, `(cfg.F)`, `cfg.F[0]`, `cfg.F.G`.
func (s *hxbScan) hxbFindField(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.SliceExpr:
			e = x.X
		case *ast.SelectorExpr:
			if s.fields[x.Sel.Name] {
				return x.Sel.Name
			}
			e = x.X
		default:
			return ""
		}
	}
}

func (s *hxbScan) hxbCheckIdent(site hxbSite, id *ast.Ident) {
	if slices.Contains(hxbPinOnly, id.Name) {
		s.words[id.Name] = append(s.words[id.Name], hxbName(site))
	}
	if _, watched := hxbCallers[id.Name]; watched && !s.decl[id] {
		s.uses[id.Name] = append(s.uses[id.Name], hxbUse{site: site, call: s.callee[id]})
	}
}

// finish judges what is counted over the whole set.
func (s *hxbScan) hxbCheckTotals() {
	for _, word := range hxbPinOnly {
		for _, at := range s.words[word] {
			if at != "pin.go:pinnedTLSConfig" {
				s.out = append(s.out, at+": "+word+" is written outside pinnedTLSConfig")
			}
		}
		if n := len(s.words[word]); n != 1 {
			s.out = append(s.out, "pin.go:pinnedTLSConfig: "+word+" is written "+strconv.Itoa(n)+" times in all, and has to be once")
		}
	}
	for _, at := range slices.Sorted(maps.Keys(hxbLiteralFields)) {
		if !s.literals[at] {
			s.out = append(s.out, at+": the settings are not made here")
		}
	}
	for _, name := range slices.Sorted(maps.Keys(hxbCallers)) {
		var places []string
		for _, u := range s.uses[name] {
			if u.call == nil {
				s.hxbReport(u.site, "%s is held and not called", name)
			}
			places = append(places, hxbName(u.site))
			if name == "pinnedConfig" && u.call != nil && !hxbPassesPins(u.call) {
				s.hxbReport(u.site, "pinnedConfig is not given the pins of the connection (the parameter pins) and nothing else")
			}
		}
		for _, at := range places {
			if !slices.Contains(hxbCallers[name], at) {
				s.out = append(s.out, at+": "+name+" is used here, and may be used only in "+strings.Join(hxbCallers[name], ", "))
			}
		}
		for _, at := range hxbCallers[name] {
			if !slices.Contains(places, at) {
				s.out = append(s.out, at+": "+name+" is not used here any more")
			}
		}
	}
}

func hxbPassesPins(call *ast.CallExpr) bool {
	if len(call.Args) != 1 {
		return false
	}
	id, ok := call.Args[0].(*ast.Ident)
	return ok && id.Name == "pins"
}

// [REQ-823] The one way to turn the verification of a certificate off is the settings of the pins, and it has one shape (see
// the top of this file). The scan is the same function as the one that its own test uses.
func TestHxbTheOnlyWayToSkipTheVerificationIsTheConfigOfThePins(t *testing.T) {
	for _, v := range hxbViolations(hxbParse(t, hxbSource(t))) {
		t.Error(v)
	}
}

// hxbWaysAround are the files that a person who wants to weaken the settings could add, each written to be the way round of
// one rule. The scan has to find every one of them in the real set of files plus the one.
var hxbWaysAround = map[string]string{
	"the name of the import": `package egress
import (
	"time"
	xt "crypto/tls"
)
func weak() *xt.Config {
	return &xt.Config{MinVersion: xt.VersionTLS10, Time: func() time.Time { return time.Unix(0, 0) }, ServerName: "another.example.test"}
}`,
	"the import without a name": `package egress
import . "crypto/tls"
func weak() { _ = VersionTLS10 }`,
	"another name of the type": `package egress
import "crypto/tls"
type weakCfg = tls.Config
func weak() *tls.Config { return &weakCfg{MinVersion: tls.VersionTLS10} }`,
	"a new type of it": `package egress
import "crypto/tls"
type weakCfg tls.Config`,
	"a type that holds it": `package egress
import "crypto/tls"
type holder struct{ *tls.Config }`,
	"a variable of the type": `package egress
import "crypto/tls"
func weak() { var c tls.Config; _ = c }`,
	"new of the type": `package egress
import "crypto/tls"
func weak() { _ = new(tls.Config) }`,
	"a literal elsewhere": `package egress
import "crypto/tls"
func another() *tls.Config { return &tls.Config{InsecureSkipVerify: true} }`,
	"a literal without field names": `package egress
import "crypto/tls"
func another(r func() *tls.Config) { _ = r }`,
	"a field assigned": `package egress
func weaken(cfg interface{}) { cfg.VerifyConnection = nil }`,
	"a field assigned in parentheses": `package egress
func weaken(cfg interface{}) { (cfg.MinVersion) = 1 }`,
	"a field inside a field assigned": `package egress
func weaken(cfg interface{}) { cfg.Certificates[0].Leaf = nil }`,
	"a field incremented": `package egress
func weaken(cfg interface{}) { cfg.MinVersion-- }`,
	"a field set by its address": `package egress
func set[T any](p *T, v T) { *p = v }
func weaken(cfg interface{}) { set(&cfg.ServerName, "another.example.test") }`,
	"a field set through its address": `package egress
func weaken(cfg interface{}) { *(&cfg.MaxVersion) = 1 }`,
	"a field ranged into": `package egress
func weaken(cfg interface{}) { for cfg.MinVersion = range 1 {} }`,
	"the whole of the settings assigned": `package egress
import "crypto/tls"
func weaken(dst, src *tls.Config) { *dst = *src }`,
	"a second word that skips the verification": `package egress
func skip() bool { return InsecureSkipVerify }`,
	"the hook written elsewhere": `package egress
func hook(cfg interface{}) { _ = cfg.VerifyConnection }`,
	"the settings of the pins called with fixed pins": `package egress
func fixed() { _, _ = pinnedConfig([]string{"sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}) }`,
	"the settings of the pins called from another function with the pins": `package egress
func other(pins []string) { _, _ = pinnedConfig(pins) }`,
	"the settings of a probe called elsewhere": `package egress
func probing(rec *presented) { _ = probeConfig("", rec) }`,
	"the function of the skip held": `package egress
func hold() { f := pinnedTLSConfig; _ = f }`,
	"the function of the skip called elsewhere": `package egress
func call() { _ = pinnedTLSConfig("", nil) }`,
	"a connection made from fixed pins": `package egress
func fixed() { _, _ = connectionTLS("", []string{"sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}) }`,
	"a set of pins written by hand": `package egress
func hand() pinSet { return pinSet{} }`,
	"a decoder that fills the settings by the names of fields": `package egress
import "encoding/json"
func loosen(v any) { _ = json.Unmarshal([]byte(` + "`" + `{"insecureskipverify":true}` + "`" + `), v) }`,
	"a gob decoder": `package egress
import "encoding/gob"
var _ = gob.NewDecoder`,
	"an xml decoder": `package egress
import "encoding/xml"
var _ = xml.Unmarshal`,
	"reflect": `package egress
import "reflect"
var _ = reflect.TypeOf`,
	"unsafe": `package egress
import "unsafe"
var _ unsafe.Pointer`,
}

// [REQ-823] The scan of the settings is a scan that finds what it is made to find: the real files give no violation, and the
// real files with any one of the ways around added give one in the file that was added. (The ways around of the review of
// ADR-0105 are the first five of the list and the ones for the pins; the others are the older cases of the scan.)
func TestHxbTheScanOfTheSettingsFailsForTheWaysAround(t *testing.T) {
	real := hxbSource(t)
	if got := hxbViolations(hxbParse(t, real)); len(got) != 0 {
		t.Fatalf("the real files are not clean: %v", got)
	}
	for _, name := range slices.Sorted(maps.Keys(hxbWaysAround)) {
		sources := maps.Clone(real)
		sources["zzz_around.go"] = hxbWaysAround[name]
		hit := false
		for _, v := range hxbViolations(hxbParse(t, sources)) {
			hit = hit || strings.HasPrefix(v, "zzz_around.go:")
		}
		if !hit {
			t.Errorf("%s: the scan does not see it", name)
		}
	}
}

// [REQ-823] The scan is not made to pass by the files of a package that has lost a part of it: if the place of the settings
// is gone (the literal, the one call) it says so.
func TestHxbTheScanOfTheSettingsFailsWhenAPlaceIsGone(t *testing.T) {
	real := hxbSource(t)
	for name, change := range map[string]struct{ file, from, to string }{
		"the call of the pins":    {"tls.go", "return pinnedConfig(pins)", "return nil, nil"},
		"the literal of the pins": {"pin.go", "InsecureSkipVerify: true,", ""},
		"the check of the pins":   {"pin.go", "VerifyConnection: func", "Time: func"},
	} {
		sources := maps.Clone(real)
		if !strings.Contains(sources[change.file], change.from) {
			t.Fatalf("%s: %q is not in %s any more", name, change.from, change.file)
		}
		sources[change.file] = strings.Replace(sources[change.file], change.from, change.to, 1)
		if len(hxbViolations(hxbParse(t, sources))) == 0 {
			t.Errorf("%s: the scan does not see it", name)
		}
	}
}
