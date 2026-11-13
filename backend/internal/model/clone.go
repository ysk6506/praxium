package model

import (
	"maps"
	"slices"
)

// Clone returns a copy that shares no slice or map with d.
func (d Definitions) Clone() Definitions {
	out := d
	out.Reserved = Reserved{
		TypeNos:     slices.Clone(d.Reserved.TypeNos),
		LinkTypeNos: slices.Clone(d.Reserved.LinkTypeNos),
		AttrNos:     slices.Clone(d.Reserved.AttrNos),
	}
	out.ValueTypes = slices.Clone(d.ValueTypes)
	out.Schemes = cloneEach(d.Schemes, IdentifierScheme.clone)
	out.Traits = cloneEach(d.Traits, Trait.clone)
	out.ObjectTypes = cloneEach(d.ObjectTypes, ObjectType.clone)
	out.LinkTypes = cloneEach(d.LinkTypes, LinkType.clone)
	out.ActionTypes = cloneEach(d.ActionTypes, ActionType.clone)
	return out
}

func cloneEach[T any](items []T, clone func(T) T) []T {
	if items == nil {
		return nil
	}
	out := make([]T, len(items))
	for i, item := range items {
		out[i] = clone(item)
	}
	return out
}

func (p Property) clone() Property {
	p.Aliases = slices.Clone(p.Aliases)
	p.EnumValues = slices.Clone(p.EnumValues)
	return p
}

func (t Trait) clone() Trait {
	t.Aliases = slices.Clone(t.Aliases)
	t.Properties = cloneEach(t.Properties, Property.clone)
	t.Needs = slices.Clone(t.Needs)
	return t
}

func (t ObjectType) clone() ObjectType {
	t.Aliases = slices.Clone(t.Aliases)
	t.Traits = slices.Clone(t.Traits)
	t.Needs = slices.Clone(t.Needs)
	t.Properties = cloneEach(t.Properties, Property.clone)
	t.IdentityKeys = cloneEach(t.IdentityKeys, IdentityKey.clone)
	t.DisplayNames = maps.Clone(t.DisplayNames)
	t.Contacts = slices.Clone(t.Contacts)
	return t
}

// Clone returns a copy of the binding that shares no map with it.
func (b Binding) Clone() Binding {
	if b.ParamConstraints != nil {
		constraints := make(map[string][]string, len(b.ParamConstraints))
		for name, values := range b.ParamConstraints {
			constraints[name] = slices.Clone(values)
		}
		b.ParamConstraints = constraints
	}
	return b
}

func (l LinkType) clone() LinkType {
	l.Aliases = slices.Clone(l.Aliases)
	l.MeaningParams = maps.Clone(l.MeaningParams)
	l.Properties = cloneEach(l.Properties, Property.clone)
	l.Bindings = cloneEach(l.Bindings, Binding.Clone)
	return l
}

func (a ActionType) clone() ActionType {
	a.Aliases = slices.Clone(a.Aliases)
	a.AppliesTo = slices.Clone(a.AppliesTo)
	a.DeclaredEffects = slices.Clone(a.DeclaredEffects)
	return a
}

func (s IdentifierScheme) clone() IdentifierScheme {
	s.Aliases = slices.Clone(s.Aliases)
	return s
}

func (k IdentityKey) clone() IdentityKey {
	k.Components = slices.Clone(k.Components)
	return k
}
