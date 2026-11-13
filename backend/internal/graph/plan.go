package graph

import (
	"sort"
	"strconv"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const (
	entityObject = "object"
	entityLink   = "link"
)

type plannedObject struct {
	id     ObjectID
	typ    model.ObjectType
	upsert ObjectUpsert
}

type plannedLink struct {
	id     LinkID
	typ    model.LinkType
	upsert LinkUpsert
}

// item is one candidate of a batch: an upsert or a retirement, addressed by
// entity and id. Candidates are sorted by (entity, id) and cut into chunks, so
// the cut does not depend on what the state looks like (a retry cuts the same).
type item struct {
	entity string
	id     string
	retire bool
	index  int
}

// plan is a validated batch with its candidates in order.
type plan struct {
	batch    Batch
	objects  []plannedObject
	links    []plannedLink
	retObjs  []ObjectID
	retLinks []LinkID
	items    []item
	volatile []VolatileWrite
	idents   []IdentifierWrite
	events   []IdentityEvent
	digest   kernel.Hash
	objTypes map[ObjectID]model.ObjectType
}

func (p *plan) chunkCount(limit int) int {
	if len(p.items) == 0 {
		return 1
	}
	return (len(p.items) + limit - 1) / limit
}

// chunkItems returns the candidates of chunk k and whether it is the last chunk.
func (p *plan) chunkItems(k, limit int) ([]item, bool) {
	lo := k * limit
	if lo > len(p.items) {
		lo = len(p.items)
	}
	hi := lo + limit
	if hi > len(p.items) {
		hi = len(p.items)
	}
	return p.items[lo:hi], k == p.chunkCount(limit)-1
}

// buildPlan checks a batch on its own (no state) and orders it.
func buildPlan(v *validator, b Batch) (*plan, error) {
	if b.Kind == "" {
		b.Kind = BatchSync
	}
	if b.Event == "" {
		b.Event = OrderSourceChange
	}
	if !b.Kind.Valid() {
		return nil, invalid("unknown batch kind %q", b.Kind)
	}
	if !b.Event.Valid() || b.Event == OrderSeal {
		return nil, invalid("%q is not an ordering event a batch can be", b.Event)
	}
	p := &plan{batch: b, objTypes: map[ObjectID]model.ObjectType{}}
	steps := []func(*validator) error{p.addObjects, p.addLinks, p.addRetirements, p.addVolatile, p.addIdentity}
	for _, step := range steps {
		if err := step(v); err != nil {
			return nil, err
		}
	}
	sort.Slice(p.items, func(i, j int) bool {
		a, c := p.items[i], p.items[j]
		if a.entity != c.entity {
			return a.entity == entityObject
		}
		return a.id < c.id
	})
	for i := 1; i < len(p.items); i++ {
		if p.items[i].entity == p.items[i-1].entity && p.items[i].id == p.items[i-1].id {
			return nil, invalid("%s %s appears twice in one batch", p.items[i].entity, p.items[i].id)
		}
	}
	digest, err := p.computeDigest()
	p.digest = digest
	return p, err
}

func (p *plan) addObjects(v *validator) error {
	for i, u := range p.batch.Objects {
		typ, id, err := v.object(u)
		if err != nil {
			return err
		}
		p.objects = append(p.objects, plannedObject{id: id, typ: typ, upsert: u})
		p.objTypes[id] = typ
		p.items = append(p.items, item{entity: entityObject, id: id.String(), index: i})
	}
	return nil
}

func (p *plan) addLinks(v *validator) error {
	for i, u := range p.batch.Links {
		typ, id, err := v.link(u)
		if err != nil {
			return err
		}
		p.links = append(p.links, plannedLink{id: id, typ: typ, upsert: u})
		p.items = append(p.items, item{entity: entityLink, id: id.String(), index: i})
	}
	return nil
}

func (p *plan) addRetirements(*validator) error {
	for i, id := range p.batch.RetireObjects {
		if id.IsZero() {
			return invalid("an object retirement has no id")
		}
		p.retObjs = append(p.retObjs, id)
		p.items = append(p.items, item{entity: entityObject, id: id.String(), retire: true, index: i})
	}
	for i, id := range p.batch.RetireLinks {
		if id.IsZero() {
			return invalid("a link retirement has no id")
		}
		p.retLinks = append(p.retLinks, id)
		p.items = append(p.items, item{entity: entityLink, id: id.String(), retire: true, index: i})
	}
	return nil
}

func (p *plan) addVolatile(*validator) error {
	seen := map[string]bool{}
	for _, w := range p.batch.Volatile {
		key := w.Object.String() + "/" + itoa(w.AttrNo)
		if w.Object.IsZero() || seen[key] {
			return invalid("a volatile write has no object or repeats %s", key)
		}
		seen[key] = true
	}
	p.volatile = append([]VolatileWrite(nil), p.batch.Volatile...)
	sort.Slice(p.volatile, func(i, j int) bool {
		a, b := p.volatile[i], p.volatile[j]
		if a.Object != b.Object {
			return a.Object.String() < b.Object.String()
		}
		return a.AttrNo < b.AttrNo
	})
	return nil
}

func (p *plan) addIdentity(v *validator) error {
	for _, w := range p.batch.Identifiers {
		scheme, ok := v.reg.IdentifierScheme(w.Scheme)
		if !ok || w.Object.IsZero() {
			return invalid("an identifier write needs an object and a known scheme")
		}
		value, err := model.NormalizeValue(scheme.Normalizer, w.Value)
		if err != nil || value == "" {
			return invalid("identifier %q does not fit scheme %s", w.Value, scheme.Name)
		}
		w.Value = value
		p.idents = append(p.idents, w)
	}
	sort.SliceStable(p.idents, func(i, j int) bool {
		a, b := p.idents[i], p.idents[j]
		if a.Scheme != b.Scheme {
			return a.Scheme.String() < b.Scheme.String()
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		return a.Object.String() < b.Object.String()
	})
	for _, e := range p.batch.Events {
		if err := checkIdentityEvent(e); err != nil {
			return err
		}
	}
	p.events = append([]IdentityEvent(nil), p.batch.Events...)
	return nil
}

func checkIdentityEvent(e IdentityEvent) error {
	if e.Kind != EventMerge && e.Kind != EventSplit && e.Kind != EventRevive {
		return invalid("unknown identity event kind %q", e.Kind)
	}
	if e.Retired.IsZero() || e.Into.IsZero() || (e.Kind == EventMerge && e.Retired == e.Into) {
		return invalid("an identity event needs two different objects")
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }
