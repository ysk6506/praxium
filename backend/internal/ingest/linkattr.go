package ingest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// ProblemLinkConflict is the problem of a link that rows or definitions make more than once with different attributes.
const ProblemLinkConflict = "link_conflict"

// attrValues gives the attributes the definition fills for the link of one row, and the reasons of the cells that gave
// none. A cell has the grade and the basis of the link; a fixed value is assumed, since no source said it, and has the
// basis of the link. The time the source saw a value is not set: a link has no time of observation, and giving it
// one would let the link go stale in a query that today leaves it alone (ADR-0066).
func (lp *linkPlan) attrValues(row Row, ref string) (map[int]graph.Attr, []string) {
	if len(lp.attrs) == 0 {
		return nil, nil
	}
	out := map[int]graph.Attr{}
	var left []string
	for i := range lp.attrs {
		a := &lp.attrs[i]
		if a.isFixed {
			out[a.prop.AttrNo] = graph.Attr{Value: a.fixed, Grade: model.Lowest(lp.grade(), model.GradeAssumed), Basis: lp.basis, SourceRef: ref}
			continue
		}
		v, has, reason := cellValue(a.valueSpec, row.Fields[a.column])
		if reason != "" {
			left = append(left, reason+"; the link is made without this value (the attribute is empty)")
		}
		if has {
			out[a.prop.AttrNo] = graph.Attr{Value: v, Grade: lp.grade(), Basis: lp.basis, SourceRef: ref}
		}
	}
	if len(out) == 0 {
		return nil, left
	}
	return out, left
}

// reportLinkCells reports the cells of the links that are made and gave no value: the wrong ones are counted with those
// of the objects (cells_left_out), and the row they are in is a used row with a problem.
func (d *derivation) reportLinkCells() {
	ids := make([]string, 0, len(d.links))
	for id := range d.links {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		l := d.links[id]
		for _, reason := range l.left {
			if f := d.fates[l.dataset][l.row.Key]; f != nil {
				f.problem = true
			}
			d.rep.dataset(l.dataset).CellsLeftOut++
			d.problem(ProblemInvalidValue, l.dataset, l.defID, l.row, reason)
		}
	}
}

// linkMadeAgain is called when a link that is already wanted is made once more, by another row of the same definition
// or by a later definition: the later one is used and the attributes of the two are not merged (a link has one
// value of an attribute). When they differ, the one that was left is reported.
func (d *derivation) linkMadeAgain(prev, next desiredLink) {
	if prev.held || next.held || sameLinkAttrs(prev.up.Attrs, next.up.Attrs) {
		return
	}
	name := next.up.Type.String()
	if lt, ok := d.p.reg.LinkType(next.up.Type); ok {
		name = lt.Name
	}
	d.problem(ProblemLinkConflict, prev.dataset, prev.defID, prev.row, fmt.Sprintf(
		"the link %s from %s to %s is made again by %s (dataset %s, %s) with other attributes: %s here, %s there; the later one is used and the attributes are not merged",
		name, d.objects[next.up.Src.String()].nameFall, d.objects[next.up.Dst.String()].nameFall, next.defID, next.dataset, rowPlace(next.row),
		d.attrsText(prev.up.Attrs), d.attrsText(next.up.Attrs)))
}

func sameLinkAttrs(a, b map[int]graph.Attr) bool {
	if len(a) != len(b) {
		return false
	}
	for no, x := range a {
		y, ok := b[no]
		if !ok || x.Value != y.Value || x.Grade != y.Grade || x.Basis != y.Basis {
			return false
		}
	}
	return true
}

// attrsText lists the attributes of a link by name, with the grade: {position_u=5 fact}, or {no attributes}.
func (d *derivation) attrsText(attrs map[int]graph.Attr) string {
	if len(attrs) == 0 {
		return "{no attributes}"
	}
	nos := make([]int, 0, len(attrs))
	for no := range attrs {
		nos = append(nos, no)
	}
	sort.Ints(nos)
	parts := make([]string, len(nos))
	for i, no := range nos {
		name := fmt.Sprint(no)
		if ref, ok := d.p.reg.PropertyByNo(no); ok {
			name = ref.Property.Name
		}
		parts[i] = fmt.Sprintf("%s=%v %s", name, attrs[no].Value, attrs[no].Grade)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
