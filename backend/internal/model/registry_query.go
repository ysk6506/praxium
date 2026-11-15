package model

import (
	"slices"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// buildInfo works out, once, what each object type inherits. The registry is
// checked, so the parent chains are free of cycles.
func (r *Registry) buildInfo(c *checker) {
	r.info = make(map[ObjectTypeID]*typeInfo, len(r.defs.ObjectTypes))
	for i := range r.defs.ObjectTypes {
		t := &r.defs.ObjectTypes[i]
		info := &typeInfo{traits: map[TraitID]bool{}}
		chain := c.lineage(t)
		for _, cur := range chain[1:] {
			info.ancestors = append(info.ancestors, cur.ID)
		}
		for _, cur := range chain {
			for _, id := range cur.Traits {
				info.traits[id] = true
			}
		}
		for _, p := range c.effectiveProps(t) {
			info.props = append(info.props, p.clone())
			if p.ScopeKey {
				info.scopeKeys = append(info.scopeKeys, p.ID)
			}
		}
		info.keys = slices.Clone(c.effectiveKeys(t))
		info.needs = r.effectiveNeeds(chain)
		r.info[t.ID] = info
	}
}

// effectiveNeeds merges the needs along a chain, nearest first: the needs of
// the traits of a type, then its own, which override a trait's need for the
// same channel; a descendant overrides its ancestors. The result follows the
// order of the channels.
func (r *Registry) effectiveNeeds(chain []*ObjectType) []Need {
	byChannel := map[Channel]Need{}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, traitID := range chain[i].Traits {
			for _, need := range r.defs.Traits[r.traits[traitID]].Needs {
				byChannel[need.Channel] = need
			}
		}
		for _, need := range chain[i].Needs {
			byChannel[need.Channel] = need
		}
	}
	var out []Need
	for _, channel := range Channels() {
		if need, ok := byChannel[channel]; ok {
			out = append(out, need)
		}
	}
	return out
}

// Ancestors returns the ids of the parents of a type, nearest first.
func (r *Registry) Ancestors(id ObjectTypeID) []ObjectTypeID {
	if info := r.info[id]; info != nil {
		return slices.Clone(info.ancestors)
	}
	return nil
}

// IsSubtypeOf reports whether child is ancestor or has it among its ancestors.
func (r *Registry) IsSubtypeOf(child, ancestor ObjectTypeID) bool {
	info := r.info[child]
	if info == nil {
		return false
	}
	return child == ancestor || slices.Contains(info.ancestors, ancestor)
}

// Subtypes returns every type that has id among its ancestors (not id
// itself), in the order of their numbers.
func (r *Registry) Subtypes(id ObjectTypeID) []ObjectType {
	var out []ObjectType
	var walk func(ObjectTypeID)
	walk = func(parent ObjectTypeID) {
		for _, child := range r.children[parent] {
			t, _ := r.ObjectType(child)
			out = append(out, t)
			walk(child)
		}
	}
	walk(id)
	sort.Slice(out, func(i, j int) bool { return out[i].TypeNo < out[j].TypeNo })
	return out
}

// TypesInLayer returns the types of a layer in the order of their numbers.
func (r *Registry) TypesInLayer(layer Layer) []ObjectType {
	var out []ObjectType
	for _, t := range r.ObjectTypes() {
		if t.Layer == layer {
			out = append(out, t)
		}
	}
	return out
}

// HasTrait reports whether a type carries a trait, its own or an ancestor's.
func (r *Registry) HasTrait(id ObjectTypeID, trait TraitID) bool {
	info := r.info[id]
	return info != nil && info.traits[trait]
}

// TypesWithTrait returns the types that carry a trait, own or inherited, in the order of their numbers.
func (r *Registry) TypesWithTrait(trait TraitID) []ObjectType {
	var out []ObjectType
	for _, t := range r.ObjectTypes() {
		if r.HasTrait(t.ID, trait) {
			out = append(out, t)
		}
	}
	return out
}

// EffectiveProperties returns the properties a type has: those of its
// ancestors first, then its own, trait properties before own ones at each level.
func (r *Registry) EffectiveProperties(id ObjectTypeID) []Property {
	info := r.info[id]
	if info == nil {
		return nil
	}
	return cloneEach(info.props, Property.clone)
}

// ScopeKeys returns the properties that are scope keys of a type, inherited
// ones first, in the order the storage slots use.
func (r *Registry) ScopeKeys(id ObjectTypeID) []PropertyID {
	if info := r.info[id]; info != nil {
		return slices.Clone(info.scopeKeys)
	}
	return nil
}

// IdentityKeys returns the identity keys of a type in priority order: its own,
// or those of the nearest ancestor that has some.
func (r *Registry) IdentityKeys(id ObjectTypeID) []IdentityKey {
	if info := r.info[id]; info != nil {
		return cloneEach(info.keys, IdentityKey.clone)
	}
	return nil
}

// EffectiveNeeds returns the channels a type needs, by channel order.
func (r *Registry) EffectiveNeeds(id ObjectTypeID) []Need {
	if info := r.info[id]; info != nil {
		return slices.Clone(info.needs)
	}
	return nil
}

// BoundLink is a binding that applies to a pair of types, with the link type that owns it.
type BoundLink struct {
	LinkType LinkTypeID
	Binding  Binding
}

// matches reports whether an end covers an object of the given type: the
// type itself or a subtype, a type that carries the trait, or any object.
func (r *Registry) matches(e Endpoint, id ObjectTypeID) bool {
	switch e.Kind {
	case EndpointAny:
		return r.info[id] != nil
	case EndpointType:
		ref, err := kernel.ParseID[ObjectTypeKind](e.Ref)
		return err == nil && r.IsSubtypeOf(id, ref)
	case EndpointTrait:
		ref, err := kernel.ParseID[TraitKind](e.Ref)
		return err == nil && r.HasTrait(id, ref)
	}
	return false
}

// endScore says how narrow one end of a binding is: any object 0, a trait 1, a type 2 plus the number of its
// ancestors (a root type has none).
func (r *Registry) endScore(e Endpoint) int {
	switch e.Kind {
	case EndpointTrait:
		return 1
	case EndpointType:
		ref, err := kernel.ParseID[ObjectTypeKind](e.Ref)
		if info := r.info[ref]; err == nil && info != nil {
			return 2 + len(info.ancestors)
		}
		return 2
	}
	return 0
}

// indexBindingScores works out the score of every binding once: the sum of its two ends.
func (r *Registry) indexBindingScores() {
	r.bindScore = map[BindingID]int{}
	for _, l := range r.defs.LinkTypes {
		for _, b := range l.Bindings {
			r.bindScore[b.ID] = r.endScore(b.From) + r.endScore(b.To)
		}
	}
}

// better is the one rule that chooses among bindings that all fit a pair of types (ADR-0059): the binding with
// the higher score, the narrower ends, wins, and the lower binding id wins a tie. Every reader that needs "the"
// binding of a pair goes through it, so the choice is the same everywhere and for the same input.
func (r *Registry) better(a, b BindingID) bool {
	if sa, sb := r.bindScore[a], r.bindScore[b]; sa != sb {
		return sa > sb
	}
	return a.String() < b.String()
}

// BindingFor returns the binding of the link type that lets a link run from an object of type from to an object
// of type to: the one `better` prefers among those that fit, which is the one BindingsFor lists first for that
// link type. It reads one link type, so it costs far less than BindingsFor, which copies them all.
func (r *Registry) BindingFor(from, to ObjectTypeID, lt LinkTypeID) (Binding, bool) {
	b, _, ok := r.bindingOf(from, to, lt)
	if !ok {
		return Binding{}, false
	}
	return b.Clone(), true
}

// bindingOf is BindingFor without the copy; it also gives the link type.
func (r *Registry) bindingOf(from, to ObjectTypeID, lt LinkTypeID) (*Binding, *LinkType, bool) {
	i, ok := r.links[lt]
	if !ok {
		return nil, nil, false
	}
	var best *Binding
	bindings := r.defs.LinkTypes[i].Bindings
	for j := range bindings {
		b := &bindings[j]
		if r.matches(b.From, from) && r.matches(b.To, to) && (best == nil || r.better(b.ID, best.ID)) {
			best = b
		}
	}
	if best == nil {
		return nil, nil, false
	}
	return best, &r.defs.LinkTypes[i], true
}

// BindingsFor returns the bindings that let a link run from an object of type
// from to an object of type to, in the order of the link type numbers and
// then of the preference among the bindings of one link type (the first is
// the one BindingFor gives). A binding on a parent type applies to its subtypes.
func (r *Registry) BindingsFor(from, to ObjectTypeID) []BoundLink {
	var out []BoundLink
	for _, l := range r.LinkTypes() {
		for _, b := range l.Bindings {
			if r.matches(b.From, from) && r.matches(b.To, to) {
				out = append(out, BoundLink{LinkType: l.ID, Binding: b})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := r.defs.LinkTypes[r.links[out[i].LinkType]], r.defs.LinkTypes[r.links[out[j].LinkType]]
		if a.LinkTypeNo != b.LinkTypeNo {
			return a.LinkTypeNo < b.LinkTypeNo
		}
		return r.better(out[i].Binding.ID, out[j].Binding.ID)
	})
	return out
}
