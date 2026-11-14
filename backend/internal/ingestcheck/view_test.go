package ingestcheck

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// covered says, for each object type the files describe, the property that names an object across two KGs and the
// properties the files carry. Object ids differ between the KGs (the loader keys by the truth, the pipeline by the
// source keys), so the comparison goes by these.
var covered = map[string]struct {
	align string
	props []string
}{
	"Rack":           {"name", []string{"name", "total_u"}},
	"PhysicalServer": {"serial_number", []string{"manufacturer", "serial_number", "power_state"}},
	"HypervisorHost": {"hostname", []string{"hostname"}},
	"VirtualMachine": {"name", []string{"name", "power_state"}},
}

// coveredLinks are the links the files carry, as (type, source type, target type).
var coveredLinks = map[string][2]string{
	"MOUNTED_IN/PhysicalServer": {"PhysicalServer", "Rack"},
	"RUNS_ON/VirtualMachine":    {"VirtualMachine", "HypervisorHost"},
	"RUNS_ON/HypervisorHost":    {"HypervisorHost", "PhysicalServer"},
}

// kgView is a KG reduced to what the two ways of filling it must agree on.
type kgView struct {
	objects map[string]map[string]string // "Type|align" -> property -> value
	links   map[string]bool              // "LINK|Type|align|Type|align"
}

func readView(t testing.TB, reg *model.Registry, store graph.Reader, kg graph.KG) kgView {
	t.Helper()
	ctx := context.Background()
	snap, err := store.Snapshot(ctx, graph.ReadPoint{KG: kg.ID, Kind: graph.ReadLatest})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := snap.ScanObjects(ctx, graph.ObjectID{}, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	v := kgView{objects: map[string]map[string]string{}, links: map[string]bool{}}
	name := map[graph.ObjectID]string{}
	for _, o := range rows {
		ot, _ := reg.ObjectTypeByNo(o.TypeNo)
		c, ok := covered[ot.Name]
		if !ok {
			continue
		}
		vals := map[string]string{}
		for no, a := range o.Attrs {
			if ref, ok := reg.PropertyByNo(no); ok && a.Value != nil {
				vals[ref.Property.Name] = fmt.Sprint(a.Value)
			}
		}
		for _, p := range reg.EffectiveProperties(ot.ID) {
			if p.EffectiveHistory() != model.HistoryVolatile {
				continue
			}
			if vr, ok, err := snap.Volatile(ctx, o.ID, p.AttrNo); err != nil {
				t.Fatal(err)
			} else if ok && vr.Value != nil {
				vals[p.Name] = fmt.Sprint(vr.Value)
			}
		}
		if vals[c.align] == "" {
			t.Fatalf("%s %s has no %s", ot.Name, o.ID, c.align)
		}
		key := ot.Name + "|" + vals[c.align]
		if _, dup := v.objects[key]; dup {
			t.Fatalf("two objects share %s", key)
		}
		kept := map[string]string{}
		for _, p := range c.props {
			if x, ok := vals[p]; ok {
				kept[p] = x
			}
		}
		v.objects[key] = kept
		name[o.ID] = key
	}
	links, err := snap.ScanLinks(ctx, graph.LinkID{}, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range links {
		lt, _ := reg.LinkTypeByNo(l.TypeNo)
		a, b := name[l.Src], name[l.Dst]
		if a == "" || b == "" {
			continue
		}
		if ends, ok := coveredLinks[lt.Name+"/"+typeOf(a)]; ok && typeOf(b) == ends[1] {
			v.links[lt.Name+"|"+a+"|"+b] = true
		}
	}
	return v
}

func typeOf(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i]
		}
	}
	return key
}

// diff lists what differs, at most a few lines, for a failure message.
func (v kgView) diff(w kgView) []string {
	var out []string
	for k, props := range v.objects {
		other, ok := w.objects[k]
		if !ok {
			out = append(out, "only in the first: "+k)
			continue
		}
		for p, x := range props {
			if other[p] != x {
				out = append(out, fmt.Sprintf("%s.%s: %q vs %q", k, p, x, other[p]))
			}
		}
		for p := range other {
			if _, ok := props[p]; !ok {
				out = append(out, fmt.Sprintf("%s.%s: missing in the first", k, p))
			}
		}
	}
	for k := range w.objects {
		if _, ok := v.objects[k]; !ok {
			out = append(out, "only in the second: "+k)
		}
	}
	for k := range v.links {
		if !w.links[k] {
			out = append(out, "link only in the first: "+k)
		}
	}
	for k := range w.links {
		if !v.links[k] {
			out = append(out, "link only in the second: "+k)
		}
	}
	sort.Strings(out)
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}
