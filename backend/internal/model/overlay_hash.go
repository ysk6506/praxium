package model

import (
	"sort"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The hash of an overlay item and of a set of items (ADR-0050). The item hash
// covers every field of the body, labels included: a rename or a new display
// name is a new revision of the item. The model hash (Registry.Hash) covers
// only what gives a property its meaning, so a rename changes the item and
// not the model.

const (
	overlayHashRule = "praxium.overlay.v1"
	defSetHashRule  = "praxium.defset.v1"
)

// OverlayHash is the content hash of an overlay item: every field of every
// property, the properties as a set sorted by id, the lists whose order means
// nothing (aliases, enum values) sorted. The same overlay always has the same
// hash, whatever the order the properties and the lists were written in.
func OverlayHash(o Overlay) (kernel.Hash, error) {
	return kernel.ContentHash(map[string]any{
		"hash_rule":      overlayHashRule,
		"schema_version": o.SchemaVersion,
		"kind":           o.Kind,
		"target":         map[string]any{"kind": o.Target.Kind, "id": o.Target.ID},
		"properties":     setOf(o.Properties, overlayPropertyTree),
	})
}

func overlayPropertyTree(p Property) (string, any) {
	names := map[string]any{}
	for lang, name := range p.DisplayNames {
		names[lang] = name
	}
	return p.ID.String(), map[string]any{
		"id":            p.ID.String(),
		"attr_no":       p.AttrNo,
		"name":          p.Name,
		"aliases":       sortedStrings(p.Aliases),
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
		"display_names": names,
		"description":   p.Description,
	}
}

// SetEntry is one item of a definition set: the id of the item and the hash of its content.
type SetEntry struct {
	ItemID      string
	ContentHash kernel.Hash
}

// DefinitionSetHash is the hash of a definition set: the standard model and
// the items the organisation has on top of it.
//
// With no items it is the hash of the standard registry itself, the value a
// graph has always recorded, so a store without an organisation model keeps
// its versions. With items it is the content hash of the rule name, the
// standard hash and the items as (item id, content hash) sorted by item id.
// The set it was made from (its parent, its number, who wrote it) is not in it:
// the same items always give the same hash. A new standard model gives
// another hash for the same items, because the standard is part of the set.
func DefinitionSetHash(standard kernel.Hash, entries []SetEntry) (kernel.Hash, error) {
	if len(entries) == 0 {
		return standard, nil
	}
	sorted := append([]SetEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ItemID < sorted[j].ItemID })
	items := make([]any, len(sorted))
	for i, e := range sorted {
		items[i] = []any{e.ItemID, e.ContentHash.String()}
	}
	return kernel.ContentHash([]any{defSetHashRule, standard.String(), items})
}

// OverlaySetEntries makes the entries of a definition set of overlays.
func OverlaySetEntries(items []Overlay) ([]SetEntry, error) {
	out := make([]SetEntry, 0, len(items))
	for _, o := range items {
		h, err := OverlayHash(o)
		if err != nil {
			return nil, err
		}
		out = append(out, SetEntry{ItemID: o.ItemID(), ContentHash: h})
	}
	return out, nil
}
