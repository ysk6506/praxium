package pgschema_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the grades of the query schema (ADR-0113). Their names start with Qsc.

// [REQ-573] [REQ-626]
func TestQscTheSensitivityOfAPropertyIsReadInOneFileOfTheGenerator(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go") && fi.Name() != "qgrade.go"
	}, 0)
	if err != nil || len(pkgs) == 0 {
		t.Fatalf("no files: %v", err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Sensitivity" && sel.Sel.Name != "EffectiveSensitivity") {
					return true
				}
				// model.Sensitivity is the name of a type; anything else reads the sensitivity of a property
				if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "model" {
					t.Errorf("%s reads %s: the sensitivity of a property is read in qgrade.go only", name, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

// [REQ-573]
func TestQscAHigherGradeSeesEverythingALowerGradeSeesAndTheGradesAreNotTheSameSet(t *testing.T) {
	reg := registry(t, smallDefs(t))
	open := pgschema.VisibleAttrNos(reg, model.SensitivityOpen)
	restricted := pgschema.VisibleAttrNos(reg, model.SensitivityRestricted)
	confidential := pgschema.VisibleAttrNos(reg, model.SensitivityConfidential)
	for no := range open {
		if !restricted[no] {
			t.Errorf("attribute %d is seen by open and not by restricted", no)
		}
	}
	for no := range restricted {
		if !confidential[no] {
			t.Errorf("attribute %d is seen by restricted and not by confidential", no)
		}
	}
	std, err := model.LoadStandard()
	if err != nil {
		t.Fatal(err)
	}
	o, r, c := len(pgschema.VisibleAttrNos(std, model.SensitivityOpen)), len(pgschema.VisibleAttrNos(std, model.SensitivityRestricted)), len(pgschema.VisibleAttrNos(std, model.SensitivityConfidential))
	if !(o <= r && r <= c) || c == 0 {
		t.Errorf("the sets do not grow with the grade: %d %d %d", o, r, c)
	}
}

// [REQ-572]
func TestQscTheSearchPathPutsTheHighestGradeFirstAndStopsAtTheGradeOfTheCaller(t *testing.T) {
	cases := map[model.Sensitivity]string{
		model.SensitivityOpen:         `"q_3_open"`,
		model.SensitivityRestricted:   `"q_3_restricted", "q_3_open"`,
		model.SensitivityConfidential: `"q_3_confidential", "q_3_restricted", "q_3_open"`,
	}
	for grade, want := range cases {
		if got := pgschema.SearchPath(3, grade, false); got != want {
			t.Errorf("%s: %s", grade, got)
		}
	}
	if got := pgschema.SearchPath(3, model.SensitivityOpen, true); got != `"q_3_open_w"` {
		t.Errorf("the set for a large apply: %s", got)
	}
}

// [REQ-573]
func TestQscTheNameOfARoleCarriesTheTokenAndATokenThatIsNotHexadecimalIsRefused(t *testing.T) {
	if got := pgschema.QueryRoleName("0123456789ab", 7, model.SensitivityRestricted); got != "pq_0123456789ab_7_restricted" {
		t.Errorf("the role: %s", got)
	}
	for _, bad := range []string{"", "0123456789a", "0123456789abc", "0123456789AB", `0123456789a"`, "0123456789a;"} {
		if pgschema.CheckInstallToken(bad) == nil {
			t.Errorf("the token %q is accepted", bad)
		}
	}
	if err := pgschema.CheckInstallToken("0123456789ab"); err != nil {
		t.Error(err)
	}
}
