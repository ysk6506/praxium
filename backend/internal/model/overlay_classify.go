package model

import (
	"maps"
	"slices"
	"sort"
)

// ChangeKind is how a change to an overlay is received (definitions.md 11):
// an addition and a compatible change can be merged as they are, a breaking
// change needs the way the definitions document gives (add a new property,
// fill it, deprecate the old one).
type ChangeKind string

// The three kinds of change.
const (
	// ChangeAdded is a property the next overlay has and the previous one did not.
	ChangeAdded ChangeKind = "added"
	// ChangeCompatible is a change that leaves every stored value and every reader as it was.
	ChangeCompatible ChangeKind = "compatible"
	// ChangeBreaking is a change that could make a stored value mean something else or nothing.
	ChangeBreaking ChangeKind = "breaking"
)

// Change is one change to one property, or to the target.
type Change struct {
	Kind ChangeKind
	// Property is the property the change is about; empty for a change of the target.
	Property PropertyID
	// Field is the field that changed ("property" for an addition or a property that is gone).
	Field string
	// Detail says what changed and, for a breaking change, what to do instead.
	Detail string
}

// HasBreaking reports whether any of the changes is breaking.
func HasBreaking(changes []Change) bool {
	return slices.ContainsFunc(changes, func(c Change) bool { return c.Kind == ChangeBreaking })
}

const breakingAdvice = "add a new property, fill it and deprecate the old one"

// ClassifyOverlay compares the overlay as it was (prev; the zero Overlay when
// there was none) with the one that is wanted (next) and sorts what differs,
// property by property, into additions, compatible changes and breaking changes.
//
//   - Added: a property that next has and prev does not.
//   - Compatible: display names, description and aliases, enum values added,
//     the sensitivity raised, the status (deprecating a property is the way to retire it), a name changed
//     while the old name stays as an alias.
//   - Breaking: a property gone, its value type, unit, change class or number changed, enum values
//     removed, the sensitivity lowered, a name changed without the old one kept as an alias.
//
// The properties are matched by id, so a rename is a change of the name and
// not a property gone and another added. The result is in the order of the
// ids and, within one property, of the fields.
func ClassifyOverlay(prev, next Overlay) []Change {
	if len(prev.Properties) > 0 && prev.Target != next.Target {
		return []Change{{Kind: ChangeBreaking, Field: "target", Detail: "the target changes from " + prev.Target.ID + " to " + next.Target.ID + ": an overlay keeps its target"}}
	}
	before := byPropertyID(prev.Properties)
	after := byPropertyID(next.Properties)
	ids := slices.Sorted(maps.Keys(mergeKeys(before, after)))
	var out []Change
	for _, id := range ids {
		old, had := before[id]
		cur, has := after[id]
		switch {
		case !had:
			out = append(out, Change{ChangeAdded, cur.ID, "property", "adds " + cur.Name})
		case !has:
			out = append(out, Change{ChangeBreaking, old.ID, "property", old.Name + " is gone: deprecate it with status deprecated instead, because rows keep its number"})
		default:
			out = append(out, classifyProperty(old, cur)...)
		}
	}
	return out
}

func byPropertyID(props []Property) map[string]Property {
	out := make(map[string]Property, len(props))
	for _, p := range props {
		out[p.ID.String()] = p
	}
	return out
}

func mergeKeys(a, b map[string]Property) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// classifyProperty compares two versions of one property.
func classifyProperty(old, cur Property) []Change {
	var out []Change
	add := func(kind ChangeKind, field, detail string) {
		if kind == ChangeBreaking {
			detail += ": " + breakingAdvice
		}
		out = append(out, Change{kind, cur.ID, field, detail})
	}
	if old.AttrNo != cur.AttrNo {
		add(ChangeBreaking, "attr_no", "the number changes from a number rows already use")
	}
	classifyName(old, cur, add)
	if old.ValueType != cur.ValueType {
		add(ChangeBreaking, "value_type", "the value type changes from "+old.ValueType.String()+" to "+cur.ValueType.String())
	}
	if old.Unit != cur.Unit {
		add(ChangeBreaking, "unit", "the unit changes from "+old.Unit+" to "+cur.Unit)
	}
	if old.ChangeClass != cur.ChangeClass {
		add(ChangeBreaking, "change_class", "the change class changes from "+string(old.ChangeClass)+" to "+string(cur.ChangeClass))
	}
	classifyEnum(old, cur, add)
	classifySensitivity(old, cur, add)
	if statusOf(old.Status) != statusOf(cur.Status) {
		add(ChangeCompatible, "status", "the status changes from "+string(statusOf(old.Status))+" to "+string(statusOf(cur.Status)))
	}
	if old.Description != cur.Description {
		add(ChangeCompatible, "description", "the description changes")
	}
	if !maps.Equal(old.DisplayNames, cur.DisplayNames) {
		add(ChangeCompatible, "display_names", "the display names change")
	}
	if !sameSet(old.Aliases, aliasesWithoutOldName(old, cur)) {
		add(ChangeCompatible, "aliases", "the aliases change")
	}
	return out
}

// classifyName: a rename that keeps the old name as an alias still finds the
// property by the old name; one that does not breaks every mapping and filter that names it.
func classifyName(old, cur Property, add func(ChangeKind, string, string)) {
	switch {
	case old.Name == cur.Name:
	case slices.Contains(cur.Aliases, old.Name):
		add(ChangeCompatible, "name", "the name changes from "+old.Name+" to "+cur.Name+" and the old name stays as an alias")
	default:
		add(ChangeBreaking, "name", "the name changes from "+old.Name+" to "+cur.Name+" without the old name as an alias")
	}
}

func classifyEnum(old, cur Property, add func(ChangeKind, string, string)) {
	var removed, added []string
	for _, v := range old.EnumValues {
		if !slices.Contains(cur.EnumValues, v) {
			removed = append(removed, v)
		}
	}
	for _, v := range cur.EnumValues {
		if !slices.Contains(old.EnumValues, v) {
			added = append(added, v)
		}
	}
	sort.Strings(removed)
	sort.Strings(added)
	if len(removed) > 0 {
		add(ChangeBreaking, "enum_values", "the enum values "+joinValues(removed)+" are removed")
	}
	if len(added) > 0 {
		add(ChangeCompatible, "enum_values", "the enum values "+joinValues(added)+" are added")
	}
}

func joinValues(values []string) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

func sensitivityRank(s Sensitivity) int {
	switch s {
	case SensitivityRestricted:
		return 1
	case SensitivityConfidential:
		return 2
	}
	return 0
}

// classifySensitivity: a higher sensitivity only hides more; a lower one shows
// to more readers what they were not allowed to see.
func classifySensitivity(old, cur Property, add func(ChangeKind, string, string)) {
	from, to := sensitivityRank(old.Sensitivity), sensitivityRank(cur.Sensitivity)
	switch {
	case to > from:
		add(ChangeCompatible, "sensitivity", "the sensitivity goes up from "+string(old.EffectiveSensitivity())+" to "+string(cur.EffectiveSensitivity()))
	case to < from:
		add(ChangeBreaking, "sensitivity", "the sensitivity goes down from "+string(old.EffectiveSensitivity())+" to "+string(cur.EffectiveSensitivity()))
	}
}

// aliasesWithoutOldName leaves out of the new aliases the old name that a
// rename keeps there: that is part of the change of the name, not a change of aliases.
func aliasesWithoutOldName(old, cur Property) []string {
	if old.Name == cur.Name || slices.Contains(old.Aliases, old.Name) {
		return cur.Aliases
	}
	return slices.DeleteFunc(slices.Clone(cur.Aliases), func(a string) bool { return a == old.Name })
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(x, y)
}
