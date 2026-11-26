package pgschema

import (
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// The tests of the grades of the query schema that need what the package does not show (ADR-0113). Their names start with Qsc.

// A model refuses two properties of one number and a sensitivity that is not one of the three, so a registry cannot hold them; the
// grades are read by a function that is told one property at a time, and it must give the safe answer for both.
//
// [REQ-626]
func TestQscANumberThatTwoPropertiesShareIsAsSensitiveAsTheMoreSensitiveAndASensitivityThatIsNotKnownIsHiddenFromEveryGrade(t *testing.T) {
	prop := func(no int, s model.Sensitivity) model.Property { return model.Property{AttrNo: no, Sensitivity: s} }
	order := [][]model.Property{
		{prop(5, model.SensitivityOpen), prop(5, model.SensitivityConfidential)},
		{prop(5, model.SensitivityConfidential), prop(5, model.SensitivityOpen)},
		{prop(5, model.SensitivityRestricted), prop(5, model.SensitivityConfidential), prop(5, model.SensitivityOpen)},
	}
	for i, props := range order {
		grades := map[int]model.Sensitivity{}
		for _, p := range props {
			noteGrade(grades, p)
		}
		if grades[5] != model.SensitivityConfidential {
			t.Errorf("order %d: the number is as sensitive as %q, want confidential", i, grades[5])
		}
		if visibleOf(grades, model.SensitivityRestricted)[5] || !visibleOf(grades, model.SensitivityConfidential)[5] {
			t.Errorf("order %d: the number is seen by the wrong grades", i)
		}
	}
	grades := map[int]model.Sensitivity{}
	noteGrade(grades, prop(6, model.SensitivityConfidential))
	noteGrade(grades, prop(6, "secret"))
	noteGrade(grades, prop(7, ""))
	noteGrade(grades, prop(8, "secret"))
	for _, grade := range QueryGrades() {
		seen := visibleOf(grades, grade)
		if seen[6] || seen[8] {
			t.Errorf("%s sees a number whose sensitivity is not known: %v", grade, seen)
		}
		if !seen[7] {
			t.Errorf("%s does not see a number with no sensitivity said (it is open): %v", grade, seen)
		}
	}
}

// What the statements use to keep a value from a grade: the keys that it does not see are taken out of the document before it is
// spread, and the condition on the number of the row stays as the second fence.
//
// [REQ-626]
func TestQscTheDocumentThatIsSpreadHasTheKeysOfTheHiddenAttributesTakenOutFirst(t *testing.T) {
	g := gradeView{grade: model.SensitivityOpen, hidden: []int{11, 12}}
	if got, want := g.withoutHidden("t.doc"), "(t.doc - ARRAY['11', '12']::text[])"; got != want {
		t.Errorf("the document of a grade that hides two attributes: %s, want %s", got, want)
	}
	if got := (gradeView{grade: model.SensitivityConfidential}).withoutHidden("t.doc"); got != "t.doc" {
		t.Errorf("the document of a grade that hides nothing: %s", got)
	}
	if got, want := g.notHidden("ma.attr_no"), "ma.attr_no <> ALL (ARRAY[11, 12])"; got != want {
		t.Errorf("the condition on the number of a row: %s, want %s", got, want)
	}
	spread := (qGen{grade: g, store: "s_1"}).attributeOfDocs(qType{No: 1})
	cut := strings.Index(spread, "jsonb_each(")
	if cut < 0 || !strings.HasPrefix(spread[cut:], "jsonb_each((t.doc - ARRAY['11', '12']::text[]))") {
		t.Errorf("the attributes of the documents spread the document whole: %s", spread)
	}
	if !strings.Contains(spread, "ma.attr_no <> ALL (ARRAY[11, 12])") {
		t.Errorf("the attributes of the documents lost the condition of the second fence: %s", spread)
	}
	detail := (qGen{grade: g, store: "s_1"}).objectDetail().Body
	if !strings.Contains(detail, "jsonb_each((r.doc - ARRAY['11', '12']::text[]))") || strings.Contains(detail, "jsonb_each(r.doc)") {
		t.Errorf("the detail of an object spreads the document whole: %s", detail)
	}
}
