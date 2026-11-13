package worldgen

import (
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

const maxProps = 96

// kv is one explicit property value of an object or a link.
type kv struct {
	name string
	v    val
}

func kS(name, s string) kv          { return kv{name, strVal(s)} }
func kI(name string, n int) kv      { return kv{name, intVal(n)} }
func kB(name string, b bool) kv     { return kv{name, boolVal(b)} }
func kD(name string, tenths int) kv { return kv{name, decVal(tenths)} }

// pendingLink is a context link that goes out right after its object.
type pendingLink struct {
	lt int
	to ref
}

// builder collects one object. It is reused: finish one object (emit) before
// starting the next.
type builder struct {
	g       *gen
	t       *typeSpec
	id      string
	has     [maxProps]bool
	vals    [maxProps]val
	ctx     uint32
	pending []pendingLink
	ids     []ident
	basis   model.Basis
}

// obj starts an object of type t at id.
func (g *gen) obj(t int, id string) *builder {
	b := &g.b
	b.g, b.t, b.id = g, g.sch.types[t], id
	b.has = [maxProps]bool{}
	b.ctx, b.pending, b.ids = 0, b.pending[:0], b.ids[:0]
	b.basis = b.t.basis
	return b
}

// set gives explicit values; a name the type does not have is a bug of the recipe.
func (b *builder) set(kvs ...kv) *builder {
	for _, p := range kvs {
		idx, ok := b.t.byName[p.name]
		if !ok {
			b.g.fail(kernel.Errorf(kernel.CodeNotFound, "%s has no property %s", b.t.name, p.name))
			continue
		}
		b.has[idx], b.vals[idx] = true, p.v
	}
	return b
}

// opt gives explicit values to the properties the type has and skips the others.
func (b *builder) opt(kvs ...kv) *builder {
	for _, p := range kvs {
		if idx, ok := b.t.byName[p.name]; ok {
			b.has[idx], b.vals[idx] = true, p.v
		}
	}
	return b
}

// in makes a context link: the object is identified inside the parent, and
// the link goes out after the object.
func (b *builder) in(lt int, parent ref) *builder {
	b.ctx |= 1 << uint(lt)
	b.pending = append(b.pending, pendingLink{lt, parent})
	return b
}

// satisfied reports whether some identity key of the type can be formed.
func (b *builder) satisfied() bool {
	for _, k := range b.t.keys {
		ok := true
		for _, c := range k.comps {
			if c.prop >= 0 {
				ok = ok && b.t.props[c.prop].hint != hSkip
			} else {
				ok = ok && b.ctx&(1<<uint(c.ctx)) != 0
			}
		}
		if ok && (len(k.comps) > 0 || k.scheme != "") {
			return true
		}
	}
	return false
}

// emit completes the object (fills what the recipe left out), hands it to
// the observers and makes the context links.
func (b *builder) emit() ref {
	g, t := b.g, b.t
	if g.err != nil {
		return ref{id: b.id, t: t.idx}
	}
	h := hash64(b.id)
	d := g.k.forID(b.id)
	o := &g.object
	*o = Object{ID: b.id, T: t, Props: g.propBuf[:0], Unknown: g.unkBuf[:0], Assumed: g.asmBuf[:0], IDs: b.ids, Basis: b.basis, Grade: model.GradeFact}
	b.pickUnknown(&d, o)
	for i := range t.props {
		ps := &t.props[i]
		switch {
		case isIn(o.Unknown, i):
		case b.has[i]:
			o.Props = append(o.Props, propVal{i, b.vals[i]})
		case ps.hint == hSkip:
		case ps.keyPart || ps.required || d.chance(g.p.FillRate):
			o.Props = append(o.Props, propVal{i, g.genValue(ps, i, h, &d)})
		}
	}
	b.addSchemeIDs(o, h)
	b.pickAssumed(&d, o)
	b.classify(o)
	g.propBuf, g.unkBuf, g.asmBuf = o.Props, o.Unknown, o.Assumed
	r := ref{id: b.id, t: t.idx, ni: o.NI != ""}
	g.sendObject(o)
	for _, p := range b.pending {
		g.link(p.lt, r, p.to)
	}
	return r
}

func isIn(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// pickUnknown marks a few properties whose value the world does not know.
func (b *builder) pickUnknown(d *draw, o *Object) {
	if !d.chance(b.g.p.UnknownRatio) {
		return
	}
	for n := 1 + d.intn(2); n > 0; n-- {
		i := d.intn(len(b.t.props))
		ps := &b.t.props[i]
		if !ps.keyPart && ps.hint != hSkip && !b.has[i] && !isIn(o.Unknown, i) {
			o.Unknown = append(o.Unknown, i)
		}
	}
}

// pickAssumed marks one value as assumed: it was filled by an approximate source.
func (b *builder) pickAssumed(d *draw, o *Object) {
	if len(o.Props) == 0 || !d.chance(b.g.p.AssumedRatio) {
		return
	}
	pv := o.Props[d.intn(len(o.Props))]
	if !b.t.props[pv.idx].keyPart {
		o.Assumed = append(o.Assumed, pv.idx)
	}
}

// addSchemeIDs gives every scheme-only identity key an identifier value.
func (b *builder) addSchemeIDs(o *Object, h uint64) {
	for _, k := range b.t.keys {
		if len(k.comps) > 0 || k.scheme == "" {
			continue
		}
		have := false
		for _, x := range o.IDs {
			have = have || x.scheme == k.scheme
		}
		if !have {
			o.IDs = append(o.IDs, ident{k.scheme, b.t.prefix + "-" + base32(mix(h, hash64(k.scheme)), 13)})
		}
	}
}

func (b *builder) classify(o *Object) {
	switch {
	case !b.t.ingest:
		o.NI = "no_identity_key"
	case !b.satisfied():
		o.NI = "key_not_formed"
	}
}

// link makes a link of the generator's types between two objects. A pair the
// standard model has no binding for is a bug of the recipe.
func (g *gen) link(lt int, from, to ref, kvs ...kv) {
	if g.err != nil {
		return
	}
	if !g.sch.bind(from.t, to.t, lt) {
		g.fail(kernel.Errorf(kernel.CodeInvalidValue, "no %s binding from %s to %s", linkNames[lt], typeNames[from.t], typeNames[to.t]))
		return
	}
	l := &g.lnk
	*l = Link{ID: linkID(lt, from.id, to.id), T: g.sch.links[lt], From: from, To: to, Props: g.lpBuf[:0], Basis: g.linkBasis(lt, from), Grade: model.GradeFact}
	for _, p := range kvs {
		idx, ok := l.T.byName[p.name]
		if !ok {
			g.fail(kernel.Errorf(kernel.CodeNotFound, "%s has no property %s", l.T.name, p.name))
			return
		}
		l.Props = append(l.Props, propVal{idx, p.v})
	}
	g.lpBuf = l.Props
	if from.ni || to.ni {
		l.NI = "endpoint_not_ingestible"
	}
	if g.p.AssumedRatio > 0 {
		d := newDraw(mix(g.k.obj, hash64(l.ID)))
		if d.chance(g.p.AssumedRatio) {
			l.Grade = model.GradeAssumed
		}
	}
	g.sendLink(l)
}

// linkBasis is the basis of a link: where the facts about the from-end come from.
func (g *gen) linkBasis(lt int, from ref) model.Basis {
	switch lt {
	case lCabledTo:
		return model.BasisObserved
	case lMemberOf, lUplinkedVia:
		return model.BasisConfigured
	}
	return g.sch.types[from.t].basis
}
