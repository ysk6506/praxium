package ingest

import (
	"fmt"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// desiredObject is what the KG should hold of an object after this run.
type desiredObject struct {
	upsert   graph.ObjectUpsert
	volatile []graph.VolatileWrite
	idents   []graph.IdentifierWrite
	acc      *objAcc
}

// pick chooses the value of one attribute among the feeds that give it: the lowest rank wins; among equal ranks
// the later run, then the earlier feed of the mapping (pipeline.md 3.2). A value that lost and differs from the
// winner is a source conflict, not an error: it is reported and not lost.
func (d *derivation) pick(acc *objAcc, no int, cs []contrib) contrib {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.run != b.run {
			return a.run > b.run
		}
		return a.feed < b.feed
	})
	win := cs[0]
	for _, c := range cs[1:] {
		if c.text != win.text {
			d.problem(ProblemSourceConflict, "", c.feedID, acc.id.String(), 0,
				fmt.Sprintf("attribute %s: %q from %s lost to %q from %s", c.attr.prop.Name, c.text, c.feedID, win.text, win.feedID))
		}
	}
	return win
}

// objectsDone turns the accounts into the desired objects, in the order of their ids.
func (d *derivation) objectsDone(seen string) ([]desiredObject, error) {
	ids := make([]string, 0, len(d.objects))
	for id, acc := range d.objects {
		if acc.creates {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]desiredObject, 0, len(ids))
	for _, id := range ids {
		o, err := d.objectDone(d.objects[id], seen)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

func (d *derivation) objectDone(acc *objAcc, seen string) (desiredObject, error) {
	up := graph.ObjectUpsert{Type: acc.typ.ID, Key: acc.key, Name: acc.nameFall, Attrs: map[int]graph.Attr{}}
	o := desiredObject{acc: acc}
	nos := make([]int, 0, len(acc.contribs))
	for no := range acc.contribs {
		nos = append(nos, no)
	}
	sort.Ints(nos)
	for _, no := range nos {
		win := d.pick(acc, no, acc.contribs[no])
		a := graph.Attr{Value: win.value, Grade: win.grade, Basis: win.basis, SourceTime: seen, SourceRef: win.ref}
		switch {
		case win.attr.volatile:
			o.volatile = append(o.volatile, graph.VolatileWrite{Object: acc.id, AttrNo: no, Value: win.value, Grade: win.grade,
				Basis: win.basis, SeenAt: seen, Ref: win.ref})
		default:
			up.Attrs[no] = a
		}
		if win.attr.prop.Name == "name" {
			if s, ok := win.value.(string); ok && s != "" {
				up.Name = s
			}
		}
	}
	if up.Name == "" {
		return o, kernel.Errorf(kernel.CodeInternal, "object %s has no name", acc.id)
	}
	for ref := range acc.refs {
		up.SourceRefs = append(up.SourceRefs, ref)
	}
	sort.Strings(up.SourceRefs)
	o.upsert = up
	o.idents = identWrites(acc)
	return o, nil
}

func identWrites(acc *objAcc) []graph.IdentifierWrite {
	schemes := make([]string, 0, len(acc.idents))
	for s := range acc.idents {
		schemes = append(schemes, s)
	}
	sort.Strings(schemes)
	var out []graph.IdentifierWrite
	for _, s := range schemes {
		id, err := kernel.ParseID[model.IdentifierSchemeKind](s)
		if err != nil {
			continue
		}
		out = append(out, graph.IdentifierWrite{Object: acc.id, Scheme: id, Value: acc.idents[s]})
	}
	return out
}
