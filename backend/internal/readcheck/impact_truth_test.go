package readcheck

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/query"
)

// itObj and itLink are the records of the generator's truth, which the scenarios read to
// derive what an answer must be from the structure of the world, not from the engine.
type itObj struct {
	ID    string                 `json:"id"`
	Type  string                 `json:"type"`
	Props map[string]interface{} `json:"props"`
}

type itLink struct {
	ID    string                 `json:"id"`
	Type  string                 `json:"type"`
	From  string                 `json:"from"`
	To    string                 `json:"to"`
	Props map[string]interface{} `json:"props"`
	Grade string                 `json:"grade"`
	Basis string                 `json:"basis"`
}

type itTruth struct {
	objs    map[string]*itObj
	order   []string
	links   []*itLink
	out, in map[string][]*itLink
	names   map[string]string // type + "\x00" + name -> id of the KG
}

// truth reads the truth of the world and the ids the KG gave the objects.
func (w *impactWorld) itTruthOf() *itTruth {
	w.t.Helper()
	tr := &itTruth{objs: map[string]*itObj{}, out: map[string][]*itLink{}, in: map[string][]*itLink{}, names: map[string]string{}}
	sc := bufio.NewScanner(bytes.NewReader(w.truth))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var head struct {
			Kind string `json:"kind"`
		}
		line := sc.Bytes()
		if err := json.Unmarshal(line, &head); err != nil {
			w.t.Fatal(err)
		}
		switch head.Kind {
		case "object":
			o := &itObj{}
			if err := json.Unmarshal(line, o); err != nil {
				w.t.Fatal(err)
			}
			tr.objs[o.ID] = o
			tr.order = append(tr.order, o.ID)
		case "link":
			l := &itLink{}
			if err := json.Unmarshal(line, l); err != nil {
				w.t.Fatal(err)
			}
			tr.links = append(tr.links, l)
			tr.out[l.From] = append(tr.out[l.From], l)
			tr.in[l.To] = append(tr.in[l.To], l)
		}
	}
	return tr
}

func (o *itObj) itName() string {
	n, _ := o.Props["name"].(string)
	return n
}

// ofType lists the truth ids of a type in the order they were made.
func (tr *itTruth) itOfTypeT(typ string) []string {
	var out []string
	for _, id := range tr.order {
		if tr.objs[id].Type == typ {
			out = append(out, id)
		}
	}
	return out
}

// outTo lists the targets of the links of a type that leave an object.
func (tr *itTruth) itOutTo(id, linkType string) []string {
	var out []string
	for _, l := range tr.out[id] {
		if l.Type == linkType {
			out = append(out, l.To)
		}
	}
	return out
}

// inFrom lists the sources of the links of a type that arrive at an object.
func (tr *itTruth) itInFrom(id, linkType string) []string {
	var out []string
	for _, l := range tr.in[id] {
		if l.Type == linkType {
			out = append(out, l.From)
		}
	}
	return out
}

// kgIDs maps the names of the objects of a type to the ids of the KG (names of that type are unique in the generated world).
func (w *impactWorld) itKGIDs(typ string) map[string]string {
	w.t.Helper()
	out := map[string]string{}
	in := query.FindInput{Common: query.Common{KG: graph.MainAlias, Limit: query.MaxLimit, Shape: query.ShapeItems}, ObjectType: typ}
	for {
		a, err := w.eng.Find(w.ctx, in, query.AllowAll())
		if err != nil {
			w.t.Fatal(err)
		}
		for _, it := range a.Data.Items {
			if _, dup := out[it.DisplayName]; dup {
				w.t.Fatalf("two %s objects are named %s", typ, it.DisplayName)
			}
			out[it.DisplayName] = it.ObjectID
		}
		if a.Meta.ResultComplete.NextCursor == nil {
			return out
		}
		in.Cursor = *a.Meta.ResultComplete.NextCursor
	}
}

// results maps the names of the items of an answer to their result; items of other names are kept by id.
func itNamed(sec *query.ImpactSection) map[string]string {
	out := map[string]string{}
	for _, it := range sec.Items {
		out[it.DisplayName] = it.Result
	}
	return out
}

// idOf is the id of the KG of a truth object.
func (w *impactWorld) itIDOf(tr *itTruth, id string) string {
	w.t.Helper()
	o := tr.objs[id]
	if w.cache == nil {
		w.cache = map[string]map[string]string{}
	}
	m, ok := w.cache[o.Type]
	if !ok {
		m = w.itKGIDs(itTypeName(o.Type))
		w.cache[o.Type] = m
	}
	kg, ok := m[o.itName()]
	if !ok {
		w.t.Fatalf("no object %s (%s) in the KG", o.itName(), o.Type)
	}
	return kg
}

// idsOf is idOf for several objects.
func (w *impactWorld) itIDsOf(tr *itTruth, ids ...string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, w.itIDOf(tr, id))
	}
	return out
}

// itTypeName turns the id of a type in the truth ("ot.rack_pdu") into the name in the model ("RackPdu").
func itTypeName(t string) string {
	var b strings.Builder
	for _, part := range strings.Split(strings.TrimPrefix(t, "ot."), "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}
