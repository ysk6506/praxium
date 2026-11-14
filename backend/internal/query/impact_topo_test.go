package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// itTopo builds small invented dependency graphs on the standard model: objects
// of any type with a key made from the name, and the links between them.
type itTopo struct {
	r     *rig
	ids   map[string]graph.ObjectID
	batch graph.Batch
}

func newItTopo(r *rig) *itTopo {
	return &itTopo{r: r, ids: map[string]graph.ObjectID{}, batch: graph.Batch{Kind: graph.BatchSync, SourceID: "topo"}}
}

// itAttr is an attribute given by its name.
type itAttr struct {
	name  string
	value any
	grade model.Grade
}

func itFact(name string, v any) itAttr    { return itAttr{name, v, model.GradeFact} }
func itAssumed(name string, v any) itAttr { return itAttr{name, v, model.GradeAssumed} }

// add creates an object; parent is the object a PART_OF or LOCATED_IN context key needs.
func (tp *itTopo) itAdd(typ, name string, parent string, attrs ...itAttr) string {
	tp.r.t.Helper()
	return tp.itAddWith(typ, name, parent, nil, attrs...)
}

// addSet creates an object that is identified by its members (a domain of switches).
func (tp *itTopo) itAddSet(typ, name string, members []string, attrs ...itAttr) string {
	tp.r.t.Helper()
	return tp.itAddWith(typ, name, "", members, attrs...)
}

func (tp *itTopo) itAddWith(typ, name string, parent string, members []string, attrs ...itAttr) string {
	tp.r.t.Helper()
	ty, ok := tp.r.reg.ObjectTypeByName(typ)
	if !ok {
		tp.r.t.Fatalf("no type %s", typ)
	}
	up := graph.ObjectUpsert{Type: ty.ID, Name: name}
	for _, k := range tp.r.reg.IdentityKeys(ty.ID) {
		if parts, ok := tp.itKeyParts(k, name, parent, members); ok {
			up.Key = parts
			break
		}
	}
	if up.Key == nil {
		tp.r.t.Fatalf("no key for %s", typ)
	}
	up.Attrs = map[int]graph.Attr{}
	for _, a := range attrs {
		found := false
		for _, p := range tp.r.reg.EffectiveProperties(ty.ID) {
			if p.Name == a.name {
				up.Attrs[p.AttrNo] = graph.Attr{Value: a.value, Grade: a.grade, Basis: model.BasisConfigured}
				found = true
			}
		}
		if !found {
			tp.r.t.Fatalf("%s has no property %s", typ, a.name)
		}
	}
	id, err := graph.NewObjectID(ty.ID, up.Key)
	if err != nil {
		tp.r.t.Fatal(err)
	}
	tp.ids[name] = id
	tp.batch.Objects = append(tp.batch.Objects, up)
	return name
}

func (tp *itTopo) itKeyParts(k model.IdentityKey, name, parent string, members []string) ([]graph.KeyPart, bool) {
	if !k.Scheme.IsZero() {
		return nil, false
	}
	var parts []graph.KeyPart
	for _, c := range k.Components {
		switch {
		case !c.Property.IsZero():
			ref, _ := tp.r.reg.Property(c.Property)
			value := strings.ToUpper(name)
			if ref.Property.Name == "manufacturer" {
				value = "ACME"
			}
			parts = append(parts, graph.KeyPart{Kind: graph.PartProperty, Ref: c.Property.String(), Value: value})
		case c.Set:
			if len(members) == 0 {
				return nil, false
			}
			var ids []graph.ObjectID
			for _, m := range members {
				ids = append(ids, tp.ids[m])
			}
			parts = append(parts, graph.KeyPart{Kind: graph.PartSet, Ref: c.Context.String(), Objects: ids})
		default:
			pid, ok := tp.ids[parent]
			if !ok {
				return nil, false
			}
			parts = append(parts, graph.KeyPart{Kind: graph.PartContext, Ref: c.Context.String(), Objects: []graph.ObjectID{pid}})
		}
	}
	return parts, true
}

// link adds a link of a link type between two named objects.
func (tp *itTopo) itLink(typ, from, to string, grade model.Grade, attrs ...itAttr) graph.LinkID {
	tp.r.t.Helper()
	lt, ok := tp.r.reg.LinkTypeByName(typ)
	if !ok {
		tp.r.t.Fatalf("no link type %s", typ)
	}
	l := graph.LinkUpsert{Type: lt.ID, Src: tp.ids[from], Dst: tp.ids[to], Grade: grade, Basis: model.BasisConfigured}
	id, err := graph.NewLinkID(lt.ID, l.Src, l.Dst, "")
	if err != nil {
		tp.r.t.Fatal(err)
	}
	tp.batch.Links = append(tp.batch.Links, l)
	return id
}

func (tp *itTopo) itFactLink(typ, from, to string) graph.LinkID {
	return tp.itLink(typ, from, to, model.GradeFact)
}

func (tp *itTopo) itID(name string) string { return tp.ids[name].String() }

func (tp *itTopo) itIDs(names ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, tp.itID(n))
	}
	return out
}

// load applies what was built so far.
func (tp *itTopo) itLoad() {
	tp.r.t.Helper()
	tp.r.apply(tp.batch)
	tp.batch = graph.Batch{Kind: graph.BatchSync, SourceID: "topo"}
}

// impact asks the downstream question.
func itImpactOf(r *rig, in ImpactInput) Answer[ImpactData] {
	r.t.Helper()
	in.Common = r.common(func(c *Common) { c.Limit = 1000 })
	a, err := r.eng.Impact(r.ctx, in, AllowAll())
	if err != nil {
		r.t.Fatalf("impact: %v", err)
	}
	return a
}

// itResults maps the display names of the items of a section to their result.
func itResults(sec *ImpactSection) map[string]string {
	out := map[string]string{}
	if sec == nil {
		return out
	}
	for _, it := range sec.Items {
		out[it.DisplayName] = it.Result
	}
	return out
}

func itWant(t *testing.T, got, want map[string]string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("results:\n got %v\nwant %v", got, want)
	}
}

// link2 is the id of an existing link of a type between two named objects.
func (tp *itTopo) itLinkID(typ, from, to string) string {
	tp.r.t.Helper()
	lt, ok := tp.r.reg.LinkTypeByName(typ)
	if !ok {
		tp.r.t.Fatalf("no link type %s", typ)
	}
	id, err := graph.NewLinkID(lt.ID, tp.ids[from], tp.ids[to], "")
	if err != nil {
		tp.r.t.Fatal(err)
	}
	return id.String()
}
