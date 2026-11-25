package pgschema_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
	"github.com/ysk6506/praxium/backend/internal/pgschema"
)

// The tests of the grades of the query schema (ADR-0113). Their names start with Qsc.

// [REQ-573] [REQ-626]
func TestQscTheSensitivityOfAPropertyIsReadInOneFileOfTheGenerator(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no files: %v", err)
	}
	use := regexp.MustCompile(`(\w+)\.(Sensitivity|EffectiveSensitivity)\b`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "qgrade.go" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range use.FindAllStringSubmatch(string(raw), -1) {
			if m[1] != "model" {
				t.Errorf("%s reads %s: the sensitivity of a property is read in qgrade.go only", f, m[0])
			}
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
