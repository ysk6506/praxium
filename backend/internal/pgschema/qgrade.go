package pgschema

import (
	"sort"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// QueryGrades are the sensitivity grades a query schema is made for, the lowest first.
func QueryGrades() []model.Sensitivity {
	return []model.Sensitivity{model.SensitivityOpen, model.SensitivityRestricted, model.SensitivityConfidential}
}

// gradeRank is the place of a grade in QueryGrades; a grade that is not one of them ranks above all, so what it hides is hidden
// from every grade.
func gradeRank(g model.Sensitivity) int {
	for i, c := range QueryGrades() {
		if c == g {
			return i
		}
	}
	return len(QueryGrades())
}

// attrGrades is the sensitivity of every attribute number of the model: of the properties of the object types and of the link
// types. It is THE place that reads the sensitivity of a property for a query schema (ADR-0113), and VisibleAttrNos is its only
// reader, so every view, function and description of a grade is cut by one set. A test locks that nothing else in the package
// reads a sensitivity. A number that two properties share is as sensitive as the more sensitive of them.
func attrGrades(reg *model.Registry) map[int]model.Sensitivity {
	out := map[int]model.Sensitivity{}
	note := func(p model.Property) {
		if have, ok := out[p.AttrNo]; !ok || gradeRank(p.EffectiveSensitivity()) > gradeRank(have) {
			out[p.AttrNo] = p.EffectiveSensitivity()
		}
	}
	for _, t := range reg.ObjectTypes() {
		for _, p := range reg.EffectiveProperties(t.ID) {
			note(p)
		}
	}
	for _, l := range reg.LinkTypes() {
		for _, p := range l.Properties {
			note(p)
		}
	}
	return out
}

// VisibleAttrNos is the set of attribute numbers a grade sees: every property of an object type or of a link type whose
// sensitivity is at or below the grade. A property that the set leaves out has no column in the schema of the grade, no key in the
// provenance, no row among the attributes, no key in the detail of an object, no column in a function of a past time and no row
// among the changes of values.
func VisibleAttrNos(reg *model.Registry, grade model.Sensitivity) map[int]bool {
	out := map[int]bool{}
	for no, s := range attrGrades(reg) {
		if gradeRank(s) <= gradeRank(grade) {
			out[no] = true
		}
	}
	return out
}

// gradeView is what one grade sees: the set of attribute numbers and the same set seen from the other side, the numbers the
// grade does not see, which a statement that reads a table by attribute number leaves out.
type gradeView struct {
	grade   model.Sensitivity
	visible map[int]bool
	hidden  []int
}

func newGradeView(reg *model.Registry, grade model.Sensitivity) gradeView {
	visible := VisibleAttrNos(reg, grade)
	g := gradeView{grade: grade, visible: visible}
	for no := range attrGrades(reg) {
		if !visible[no] {
			g.hidden = append(g.hidden, no)
		}
	}
	sort.Ints(g.hidden)
	return g
}

// shows says whether the grade sees an attribute.
func (g gradeView) shows(attrNo int) bool { return g.visible[attrNo] }

// notHidden is the condition that keeps the rows whose attribute number (an SQL expression) the grade sees; it is "true" when the
// grade hides nothing.
func (g gradeView) notHidden(attrNo string) string {
	if len(g.hidden) == 0 {
		return "true"
	}
	nos := make([]string, len(g.hidden))
	for i, n := range g.hidden {
		nos[i] = strconv.Itoa(n)
	}
	return attrNo + " <> ALL (ARRAY[" + strings.Join(nos, ", ") + "])"
}
