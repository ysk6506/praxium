package graph

import (
	"context"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// computeDigest is the hash of the batch's content in a fixed order. It is the
// batch's part of the input chain, so it covers what the source said, not only
// what is hashed into rows.
func (p *plan) computeDigest() (kernel.Hash, error) {
	objects := make([]any, 0, len(p.objects))
	objs := append([]plannedObject(nil), p.objects...)
	sort.Slice(objs, func(i, j int) bool { return objs[i].id.String() < objs[j].id.String() })
	for _, o := range objs {
		objects = append(objects, []any{o.id.String(), o.typ.ID.String(), o.upsert.Name, attrRepr(o.upsert.Attrs), strs(o.upsert.SourceRefs)})
	}
	links := make([]any, 0, len(p.links))
	lks := append([]plannedLink(nil), p.links...)
	sort.Slice(lks, func(i, j int) bool { return lks[i].id.String() < lks[j].id.String() })
	for _, l := range lks {
		links = append(links, []any{l.id.String(), string(l.upsert.Grade), string(l.upsert.Basis), attrRepr(l.upsert.Attrs), strs(l.upsert.SourceRefs)})
	}
	retire := []any{}
	for _, it := range p.items {
		if it.retire {
			retire = append(retire, []any{it.entity, it.id})
		}
	}
	vol := make([]any, 0, len(p.volatile))
	for _, w := range p.volatile {
		vol = append(vol, []any{w.Object.String(), w.AttrNo, w.Value, string(w.Grade), string(w.Basis), w.SeenAt, w.Ref})
	}
	ids := make([]any, 0, len(p.idents))
	for _, w := range p.idents {
		ids = append(ids, []any{w.Object.String(), w.Scheme.String(), w.Value, w.Scope, w.Close})
	}
	evs := make([]any, 0, len(p.events))
	for _, e := range p.events {
		evs = append(evs, []any{string(e.Kind), e.Retired.String(), e.Into.String(), len(e.LinkMap)})
	}
	return kernel.ContentHash([]any{"praxium.batch.v1", string(p.batch.Kind), p.batch.SourceID, string(p.batch.Event), p.batch.EventRef,
		objects, links, retire, vol, ids, evs})
}

func attrRepr(attrs map[int]Attr) []any {
	nos := make([]int, 0, len(attrs))
	for no := range attrs {
		nos = append(nos, no)
	}
	sort.Ints(nos)
	out := make([]any, 0, len(nos))
	for _, no := range nos {
		a := attrs[no]
		out = append(out, []any{no, a.Value, string(a.Grade), string(a.Basis), a.SourceTime, a.SourceRef})
	}
	return out
}

func strs(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

// stateCheck holds what the checks of a batch against the state share.
type stateCheck struct {
	snap     Snapshot
	reg      *model.Registry
	v        *validator
	p        *plan
	retObj   map[ObjectID]bool
	retLink  map[LinkID]bool
	byEnd    map[string]map[LinkID]bool
	planned  map[LinkID]plannedLink
	bindMemo map[bindKey]*model.Binding
	typeMemo map[ObjectID]model.ObjectType
}

// checkState checks a batch against the state it will be applied to: ends of
// links, retirements that leave links dangling, cardinalities, volatile writes
// and identifiers. It reads only what the batch touches.
func checkState(ctx context.Context, snap Snapshot, reg *model.Registry, v *validator, p *plan) error {
	c := &stateCheck{snap: snap, reg: reg, v: v, p: p, retObj: map[ObjectID]bool{}, retLink: map[LinkID]bool{},
		byEnd: map[string]map[LinkID]bool{}, planned: map[LinkID]plannedLink{}, bindMemo: map[bindKey]*model.Binding{}, typeMemo: map[ObjectID]model.ObjectType{}}
	for _, id := range p.retObjs {
		c.retObj[id] = true
	}
	for _, id := range p.retLinks {
		c.retLink[id] = true
	}
	for _, l := range p.links {
		c.planned[l.id] = l
		c.index("out", l.typ.ID, l.upsert.Src, l.id)
		c.index("in", l.typ.ID, l.upsert.Dst, l.id)
	}
	for _, step := range []func(context.Context) error{c.links, c.retirements, c.volatile, c.identifiers} {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *stateCheck) index(dir string, lt model.LinkTypeID, end ObjectID, id LinkID) {
	key := dir + "|" + lt.String() + "|" + end.String()
	if c.byEnd[key] == nil {
		c.byEnd[key] = map[LinkID]bool{}
	}
	c.byEnd[key][id] = true
}

// typeOf is the type of an object that is live after the batch, or false.
func (c *stateCheck) typeOf(ctx context.Context, id ObjectID) (model.ObjectType, bool, error) {
	if c.retObj[id] {
		return model.ObjectType{}, false, nil
	}
	if t, ok := c.p.objTypes[id]; ok {
		return t, true, nil
	}
	if t, ok := c.typeMemo[id]; ok {
		return t, true, nil
	}
	row, ok, err := c.snap.Object(ctx, id)
	if err != nil || !ok {
		return model.ObjectType{}, false, err
	}
	typ, ok := c.reg.ObjectTypeByNo(row.TypeNo)
	if ok {
		c.typeMemo[id] = typ
	}
	return typ, ok, nil
}

func (c *stateCheck) links(ctx context.Context) error {
	for _, l := range c.p.links {
		src, okSrc, err := c.typeOf(ctx, l.upsert.Src)
		if err != nil {
			return err
		}
		dst, okDst, err := c.typeOf(ctx, l.upsert.Dst)
		if err != nil {
			return err
		}
		if !okSrc || !okDst {
			return kernel.Errorf(CodeDanglingLink, "link %s ends at an object that is not live", l.id)
		}
		found := c.bindingFor(src.ID, dst.ID, l.typ.ID)
		if found == nil {
			return kernel.Errorf(CodeBindingMissing, "%s has no binding from %s to %s", l.typ.Name, src.Name, dst.Name)
		}
		if err := c.cardinality(ctx, l, *found); err != nil {
			return err
		}
	}
	return nil
}

// cardinality checks the degree rule of the binding the link is on. A rule belongs to a binding, not to the
// link type: a cluster may hold one control plane through a 1:1 binding and many namespaces through others (ADR-0027).
func (c *stateCheck) cardinality(ctx context.Context, l plannedLink, bind model.Binding) error {
	card := string(bind.Cardinality)
	if strings.HasSuffix(card, ":1") {
		if err := c.degree(ctx, l, "out", l.upsert.Src, Out, bind.ID); err != nil {
			return err
		}
	}
	if strings.HasPrefix(card, "1:") {
		return c.degree(ctx, l, "in", l.upsert.Dst, In, bind.ID)
	}
	return nil
}

// onBinding says whether a link of the type between the two objects is on the binding.
func (c *stateCheck) onBinding(ctx context.Context, lt model.LinkTypeID, src, dst ObjectID, want model.BindingID) (bool, error) {
	st, okSrc, err := c.typeOf(ctx, src)
	if err != nil || !okSrc {
		return false, err
	}
	dt, okDst, err := c.typeOf(ctx, dst)
	if err != nil || !okDst {
		return false, err
	}
	b := c.bindingFor(st.ID, dt.ID, lt)
	return b != nil && b.ID == want, nil
}

type bindKey struct {
	src, dst model.ObjectTypeID
	lt       model.LinkTypeID
}

// bindingFor is the binding of the link type that covers the two object types, or nil. The registry copies the
// bindings it gives out, so the answers are kept: a load asks the same few questions for every link.
func (c *stateCheck) bindingFor(src, dst model.ObjectTypeID, lt model.LinkTypeID) *model.Binding {
	key := bindKey{src, dst, lt}
	if b, ok := c.bindMemo[key]; ok {
		return b
	}
	var found *model.Binding
	if b, ok := c.reg.BindingFor(src, dst, lt); ok {
		found = &b
	}
	c.bindMemo[key] = found
	return found
}

func (c *stateCheck) degree(ctx context.Context, l plannedLink, dir string, end ObjectID, d Direction, want model.BindingID) error {
	set := map[LinkID]bool{}
	for id := range c.byEnd[dir+"|"+l.typ.ID.String()+"|"+end.String()] {
		pl := c.planned[id]
		on, err := c.onBinding(ctx, l.typ.ID, pl.upsert.Src, pl.upsert.Dst, want)
		if err != nil {
			return err
		}
		if on {
			set[id] = true
		}
	}
	rows, err := c.snap.LinksOf(ctx, end, d)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.TypeNo != l.typ.LinkTypeNo || c.retLink[r.ID] || set[r.ID] {
			continue
		}
		on, err := c.onBinding(ctx, l.typ.ID, r.Src, r.Dst, want)
		if err != nil {
			return err
		}
		if on {
			set[r.ID] = true
		}
	}
	if len(set) > 1 {
		return kernel.Errorf(CodeCardinality, "%s allows one link %s object %s", l.typ.Name, dir, end)
	}
	return nil
}

func (c *stateCheck) retirements(ctx context.Context) error {
	for _, id := range c.p.retObjs {
		rows, err := c.snap.LinksOf(ctx, id, Both)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !c.retLink[r.ID] {
				return kernel.Errorf(CodeDanglingLink, "object %s retires while link %s still ends at it", id, r.ID)
			}
		}
	}
	return nil
}

func (c *stateCheck) volatile(ctx context.Context) error {
	for _, w := range c.p.volatile {
		typ, ok, err := c.typeOf(ctx, w.Object)
		if err != nil {
			return err
		}
		if !ok {
			return invalid("volatile value for object %s that is not live", w.Object)
		}
		if err := c.v.volatile(typ, w); err != nil {
			return err
		}
	}
	return nil
}

func (c *stateCheck) identifiers(ctx context.Context) error {
	closing := map[string]bool{}
	for _, w := range c.p.idents {
		if w.Close {
			closing[w.Scheme.String()+"|"+w.Value+"|"+w.Object.String()] = true
		}
	}
	for _, w := range c.p.idents {
		if _, ok, err := c.typeOf(ctx, w.Object); err != nil {
			return err
		} else if !ok {
			return invalid("identifier for object %s that is not live", w.Object)
		}
		if w.Close {
			continue
		}
		recs, err := c.snap.Identifiers(ctx, w.Scheme.String(), w.Value)
		if err != nil {
			return err
		}
		for _, r := range recs {
			if r.Object != w.Object && r.Scope == w.Scope && !closing[r.Scheme+"|"+r.Value+"|"+r.Object.String()] {
				return kernel.Errorf(CodeIdentifierTaken, "identifier %s %q already names %s", r.Scheme, r.Value, r.Object)
			}
		}
	}
	return nil
}
