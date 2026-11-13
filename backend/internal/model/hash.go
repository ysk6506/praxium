package model

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// hashRuleVersion is the version of the rule that decides what goes into the
// model hash and in what form. Changing the rule is a new version, never an
// edit: a stored hash must stay comparable with the rule that made it.
const hashRuleVersion = 1

// computeHash is the content hash of the model: SHA-256 of the kernel's
// canonical encoding (version 1) of a tree built here. What goes in:
//
//   - every definition by its id, with every field that gives it a meaning;
//   - references between definitions by id;
//   - sets (types, traits, properties, bindings, enum values) sorted by id or
//     value, lists where the order means something (identity keys by priority)
//     in their order.
//
// What stays out: names, aliases, descriptions, display names and contacts,
// which are labels; so is the stable number. Renaming a definition, or
// changing a label, does not change the hash (REQ-233, kg-model.md 11.2). The
// numbers are covered by NumberingHash.
func (r *Registry) computeHash() (kernel.Hash, error) {
	d := &r.defs
	tree := map[string]any{
		"hash_rule":     hashRuleVersion,
		"model_version": d.ModelVersion,
		"reserved": map[string]any{
			"type_no":      sortedInts(d.Reserved.TypeNos),
			"link_type_no": sortedInts(d.Reserved.LinkTypeNos),
			"attr_no":      sortedInts(d.Reserved.AttrNos),
		},
		"value_types":  setOf(d.ValueTypes, func(v ValueType) (string, any) { return v.ID.String(), valueTypeTree(v) }),
		"schemes":      setOf(d.Schemes, schemeTree),
		"traits":       setOf(d.Traits, func(t Trait) (string, any) { return t.ID.String(), traitTree(t) }),
		"object_types": setOf(d.ObjectTypes, func(t ObjectType) (string, any) { return t.ID.String(), objectTypeTree(t) }),
		"link_types":   setOf(d.LinkTypes, func(l LinkType) (string, any) { return l.ID.String(), linkTypeTree(l) }),
		"action_types": setOf(d.ActionTypes, func(a ActionType) (string, any) { return a.ID.String(), actionTypeTree(a) }),
	}
	return kernel.ContentHash(tree)
}

// NumberingHash is the content hash of the numbers: which id has which
// type_no, link_type_no and attr_no. Rows store these numbers, so a registry
// whose numbering hash differs cannot read the rows of another.
func (r *Registry) NumberingHash() (kernel.Hash, error) {
	types, links, attrs := r.defs.slots()
	toMap := func(slots []numberSlot) map[string]any {
		out := make(map[string]any, len(slots))
		for _, slot := range slots {
			out[slot.id] = *slot.no
		}
		return out
	}
	return kernel.ContentHash(map[string]any{
		"hash_rule":    hashRuleVersion,
		"type_no":      toMap(types),
		"link_type_no": toMap(links),
		"attr_no":      toMap(attrs),
	})
}

type keyed struct {
	key   string
	value any
}

// setOf makes a list of the items sorted by their keys.
func setOf[T any](items []T, tree func(T) (string, any)) []any {
	entries := make([]keyed, 0, len(items))
	for _, item := range items {
		key, value := tree(item)
		entries = append(entries, keyed{key, value})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	out := make([]any, len(entries))
	for i, entry := range entries {
		out[i] = entry.value
	}
	return out
}

func sortedInts(values []int) []any {
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	out := make([]any, len(sorted))
	for i, v := range sorted {
		out[i] = v
	}
	return out
}

func sortedStrings(values []string) []any {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	out := make([]any, len(sorted))
	for i, v := range sorted {
		out[i] = v
	}
	return out
}

func valueTypeTree(v ValueType) map[string]any {
	return map[string]any{"id": v.ID.String(), "base": string(v.Base)}
}

func needsTree(needs []Need) []any {
	return setOf(needs, func(n Need) (string, any) {
		return string(n.Channel), map[string]any{"channel": string(n.Channel), "strength": string(n.Strength)}
	})
}

func propertyTree(p Property) (string, any) {
	return p.ID.String(), map[string]any{
		"id":            p.ID.String(),
		"value_type":    p.ValueType.String(),
		"unit":          p.Unit,
		"enum_values":   sortedStrings(p.EnumValues),
		"required":      p.Required,
		"change_class":  string(p.ChangeClass),
		"sensitivity":   string(p.EffectiveSensitivity()),
		"history_kind":  string(p.EffectiveHistory()),
		"scope_key":     p.ScopeKey,
		"status":        string(statusOf(p.Status)),
		"superseded_by": p.SupersededBy.String(),
	}
}

func traitTree(t Trait) map[string]any {
	return map[string]any{
		"id":         t.ID.String(),
		"needs":      needsTree(t.Needs),
		"properties": setOf(t.Properties, propertyTree),
	}
}

func objectTypeTree(t ObjectType) map[string]any {
	keys := make([]any, len(t.IdentityKeys))
	for i, key := range t.IdentityKeys {
		keys[i] = identityKeyTree(key)
	}
	traits := make([]string, len(t.Traits))
	for i, id := range t.Traits {
		traits[i] = id.String()
	}
	return map[string]any{
		"id":             t.ID.String(),
		"namespace":      string(t.Namespace),
		"layer":          string(t.Layer),
		"tier":           string(t.EffectiveTier()),
		"parent":         t.Parent.String(),
		"traits":         sortedStrings(traits),
		"needs":          needsTree(t.Needs),
		"properties":     setOf(t.Properties, propertyTree),
		"identity_keys":  keys,
		"title_property": t.TitleProperty.String(),
		"standard_ref":   t.StandardRef,
		"status":         string(statusOf(t.Status)),
		"superseded_by":  t.SupersededBy.String(),
	}
}

// identityKeyTree keeps the components in order: the order of the parts of a
// key is part of what it means, as is the order of the keys of a type.
func identityKeyTree(key IdentityKey) map[string]any {
	parts := make([]any, len(key.Components))
	for i, c := range key.Components {
		parts[i] = map[string]any{"property": c.Property.String(), "context": c.Context.String()}
	}
	return map[string]any{"components": parts, "strength": string(key.Strength), "scheme": key.Scheme.String()}
}

func schemeTree(s IdentifierScheme) (string, any) {
	return s.ID.String(), map[string]any{"id": s.ID.String(), "namespace": string(s.Namespace), "normalizer": s.Normalizer.String()}
}

func endpointTree(e Endpoint) map[string]any {
	return map[string]any{"kind": string(e.Kind), "ref": e.Ref}
}

func bindingTree(b Binding) (string, any) {
	constraints := map[string]any{}
	for name, values := range b.ParamConstraints {
		constraints[name] = sortedStrings(values)
	}
	return b.ID.String(), map[string]any{
		"id":          b.ID.String(),
		"from":        endpointTree(b.From),
		"to":          endpointTree(b.To),
		"cardinality": string(b.Cardinality),
		"constraints": constraints,
	}
}

func linkTypeTree(l LinkType) map[string]any {
	params := map[string]any{}
	for name, value := range l.MeaningParams {
		params[name] = value
	}
	return map[string]any{
		"id":             l.ID.String(),
		"namespace":      string(l.Namespace),
		"meaning":        string(l.Meaning),
		"meaning_params": params,
		"delay_class":    string(l.EffectiveDelay()),
		"properties":     setOf(l.Properties, propertyTree),
		"bindings":       setOf(l.Bindings, bindingTree),
		"status":         string(statusOf(l.Status)),
		"superseded_by":  l.SupersededBy.String(),
	}
}

func actionTypeTree(a ActionType) map[string]any {
	targets := setOf(a.AppliesTo, func(e Endpoint) (string, any) {
		return string(e.Kind) + ":" + e.Ref, endpointTree(e)
	})
	return map[string]any{
		"id":               a.ID.String(),
		"namespace":        string(a.Namespace),
		"applies_to":       targets,
		"mutates":          a.Mutates,
		"reversibility":    string(a.Reversibility),
		"disruption":       string(a.Disruption),
		"physical":         a.Physical,
		"declared_effects": sortedStrings(a.DeclaredEffects),
		"inverse":          a.Inverse.String(),
		"status":           string(statusOf(a.Status)),
		"superseded_by":    a.SupersededBy.String(),
	}
}
