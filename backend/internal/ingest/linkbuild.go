package ingest

import (
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// linkKey is the tidied key a row has for a link end, or false when a cell of it is empty or does not fit the value
// type: such a row makes no link and it is no problem (a row may have no rack).
func (lp *linkPlan) linkKey(row Row, columns []string) (string, bool) {
	parts := make([]string, len(columns))
	for i, c := range columns {
		cell := strings.TrimSpace(row.Fields[c])
		if cell == "" {
			return "", false
		}
		if !lp.keyType.IsZero() {
			v, err := model.NormalizeValue(lp.keyType, cell)
			if err != nil || v == "" {
				return "", false
			}
			cell = v
		}
		parts[i] = cell
	}
	return strings.Join(parts, keySep), true
}

// buildLinks makes the links of every link definition from the rows that became objects (pipeline.md 5.2): a link
// goes from the object of a row of the From feed to the object of the row of the To feed with the same key. A key
// no row of the To feed has, or one that two different objects have, makes no link and is reported.
func (d *derivation) buildLinks() error {
	for _, lp := range d.p.links {
		index := map[string]map[string]bool{}
		toDS := d.p.roots[lp.to.feed.Dataset]
		for _, row := range d.rows[lp.to.feed.Dataset] {
			obj, ok := d.rowObj[lp.to.feed.ID][row.Key]
			if !ok {
				continue
			}
			if k, ok := lp.linkKey(row, lp.def.To.Columns); ok {
				if index[k] == nil {
					index[k] = map[string]bool{}
				}
				index[k][obj] = true
			}
		}
		fromDS := d.p.roots[lp.from.feed.Dataset]
		for _, row := range d.rows[lp.from.feed.Dataset] {
			src, ok := d.rowObj[lp.from.feed.ID][row.Key]
			if !ok {
				continue
			}
			k, ok := lp.linkKey(row, lp.def.From.Columns)
			if !ok {
				continue
			}
			if err := d.link(lp, fromDS, toDS, row, src, index[k]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *derivation) link(lp *linkPlan, fromDS, toDS string, row Row, src string, targets map[string]bool) error {
	switch len(targets) {
	case 0:
		d.problem(ProblemLinkTarget, fromDS, lp.def.ID, row, "no row of "+toDS+" has this key")
		return nil
	case 1:
	default:
		d.problem(ProblemLinkAmbiguous, fromDS, lp.def.ID, row, "rows of "+toDS+" with this key are different objects")
		return nil
	}
	var dst string
	for t := range targets {
		dst = t
	}
	srcID, err := parseObject(src)
	if err != nil {
		return err
	}
	dstID, err := parseObject(dst)
	if err != nil {
		return err
	}
	id, err := graph.NewLinkID(lp.lt.ID, srcID, dstID, "")
	if err != nil {
		return err
	}
	ref := d.p.Source.ID + "/" + fromDS + "/" + strings.ReplaceAll(row.Key, keySep, "|")
	d.links[id.String()] = desiredLink{from: srcID, up: graph.LinkUpsert{Type: lp.lt.ID, Src: srcID, Dst: dstID, Grade: model.GradeFact,
		Basis: lp.basis, SourceRefs: []string{ref}}}
	return nil
}

// checkCardinality drops the links that would give an object more links of a type than a binding allows on its :1
// side. The graph refuses a batch that has one, and a bad row of a source must not stop the others, so the later
// link (in the order of the link id) is set aside and reported.
func (d *derivation) checkCardinality() {
	type end struct{ lt, obj string }
	perFrom, perTo := map[end]string{}, map[end]string{}
	ids := make([]string, 0, len(d.links))
	for id := range d.links {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		l := d.links[id]
		b, ok := d.bindingOf(l.up)
		if !ok {
			continue
		}
		out, in := string(b.Cardinality) == "N:1" || string(b.Cardinality) == "1:1", string(b.Cardinality) == "1:N" || string(b.Cardinality) == "1:1"
		fe, te := end{l.up.Type.String(), l.up.Src.String()}, end{l.up.Type.String(), l.up.Dst.String()}
		if (out && perFrom[fe] != "") || (in && perTo[te] != "") {
			d.problem(ProblemCardinality, "", "", Row{Key: l.up.Src.String()}, "the link of type "+l.up.Type.String()+" to "+l.up.Dst.String()+" would break the cardinality "+string(b.Cardinality))
			delete(d.links, id)
			continue
		}
		if out {
			perFrom[fe] = id
		}
		if in {
			perTo[te] = id
		}
	}
}

func (d *derivation) bindingOf(u graph.LinkUpsert) (model.Binding, bool) {
	from, to := d.objects[u.Src.String()], d.objects[u.Dst.String()]
	if from == nil || to == nil {
		return model.Binding{}, false
	}
	return d.p.reg.BindingFor(from.typ.ID, to.typ.ID, u.Type)
}
